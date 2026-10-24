package anomalylist

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements GET /api/v1/genome/anomalies: the tenant's alerts
// newest first, each joined to its log_events row for
// provenance_confidence, severity and region.
//
// Query plan: one keyset page from PostgreSQL alerts ordered created_at
// DESC, id DESC, then ONE ClickHouse batch read of the page's event ids —
// never a ClickHouse query per alert. dimension, state, source_type,
// search and from/to are applied in SQL. severity lives only on
// log_events, so it is applied after the join; when it drops rows the
// service keeps reading further pages (bounded) until the page is full or
// the alerts run out, and meta.next_cursor always points at the last
// alert consumed.
type Service struct {
	lookup *common.MonitoredIdentityLookup
	repo   *Repository
	events *common.MonitoringEvents
}

// NewService wires the service. events may be nil (monitoring ClickHouse
// not configured): the event-backed fields are then null and a severity
// filter matches nothing.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository, events *common.MonitoringEvents) *Service {
	return &Service{lookup: lookup, repo: repo, events: events}
}

// Params are the parsed query parameters.
type Params struct {
	Severity   string     // Critical | High | Medium | Low ("" = all); matched against log_events.event_risk_severity
	Dimension  string     // exact alerts.dimension
	State      string     // exact alerts.status
	SourceType string     // exact alerts.source_type
	Search     string     // substring of identity_name / display name
	From, To   *time.Time // optional created_at window [From, To)
	Limit      int        // page size: default 25, max 100
	Cursor     string     // opaque, from a previous meta.next_cursor; "" for the first page
}

// maxPages bounds the alerts pages one request reads while a severity
// filter is discarding rows; the returned cursor lets the client continue
// from exactly where the request stopped.
const maxPages = 5

// List builds the page and the meta.next_cursor (nil on the last page).
func (s *Service) List(ctx context.Context, userID, tenantID, dbName string, p Params) (*Response, *string, error) {
	after, err := decodeCursor(p.Cursor)
	if err != nil {
		return nil, nil, err
	}
	sc, fleet, err := s.lookup.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, nil, err
	}
	set := common.NewIdentityIndex(fleet)

	out := &Response{Items: []Item{}}
	if p.Severity != "" && !s.events.Enabled() {
		return out, nil, nil // severity is an event-level column; without the event store nothing can match
	}
	filter := Filter{
		Dimension: p.Dimension, Status: p.State, SourceType: p.SourceType, Search: p.Search,
		From: p.From, To: p.To, After: after,
	}
	if p.Search != "" {
		needle := strings.ToLower(p.Search)
		for _, id := range fleet {
			if strings.Contains(strings.ToLower(id.DisplayName), needle) {
				filter.SearchKeys = append(filter.SearchKeys, id.AllKeys()...)
			}
		}
	}

	var next *string
	for page := 0; page < maxPages; page++ {
		want := p.Limit - len(out.Items)
		filter.Limit = want + 1
		rows, err := s.repo.Page(ctx, sc, filter)
		if err != nil {
			return nil, nil, &common.StoreUnavailableError{Op: "read alerts", Err: err}
		}
		more := len(rows) > want
		if more {
			rows = rows[:want]
		}
		events := common.AlertEvents(ctx, s.events, "anomaly list events", rows)
		for _, a := range rows {
			item := listItem(a, common.EventOf(events, a), set)
			if p.Severity != "" && (item.Severity == nil || !strings.EqualFold(*item.Severity, p.Severity)) {
				continue
			}
			out.Items = append(out.Items, item)
		}
		if !more {
			next = nil
			break
		}
		last := rows[len(rows)-1]
		filter.After = &common.AlertCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		next = encodeCursor(*filter.After)
		if len(out.Items) >= p.Limit {
			break
		}
	}
	if err := common.BudgetExceeded(ctx, "anomaly list"); err != nil {
		return nil, nil, err
	}
	return out, next, nil
}

// listItem maps one alerts row (+ its event, if read) to a list item.
func listItem(a common.AlertRow, ev *common.AlertEventRow, set *common.IdentityIndex) Item {
	return Item{
		ID:                   a.ID,
		Identity:             common.AnomalyIdentityRefFor(a, set),
		WhatChanged:          common.WhatChangedText(a),
		Dimension:            a.Dimension,
		ProvenanceConfidence: common.AnomalyProvenance(ev),
		Severity:             common.AnomalySeverity(ev),
		State:                a.Status,
		DetectedAt:           a.CreatedAt,
		Region:               common.AnomalyRegion(a, ev),
	}
}

// ---------------------------------------------------------------------------
// keyset cursor: (created_at, id) of the last alert consumed
// ---------------------------------------------------------------------------

type cursorPayload struct {
	CreatedAt string `json:"t"` // RFC 3339 with nanoseconds, UTC
	ID        int64  `json:"i"`
}

func encodeCursor(c common.AlertCursor) *string {
	raw, _ := json.Marshal(cursorPayload{CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339Nano), ID: c.ID})
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

func decodeCursor(s string) (*common.AlertCursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, common.ErrInvalidCursor
	}
	var c cursorPayload
	if err := json.Unmarshal(raw, &c); err != nil || c.ID < 1 {
		return nil, common.ErrInvalidCursor
	}
	at, err := time.Parse(time.RFC3339Nano, c.CreatedAt)
	if err != nil {
		return nil, common.ErrInvalidCursor
	}
	return &common.AlertCursor{CreatedAt: at.UTC(), ID: c.ID}, nil
}
