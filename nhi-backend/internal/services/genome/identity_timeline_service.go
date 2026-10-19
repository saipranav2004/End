package genome

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
)

// IdentityTimelineService implements
// GET /api/v1/identities/{source_type}/{identity_name}/timeline
// (backend_api_contract.md §5.6 / §10.3) — the lifecycle timeline derived
// from the current tables, newest first:
//
//   - anomaly                     one item per detection window, dimension and
//                                 status (alerts grouped; occurred_at = window_end),
//                                 titled "N active anomalies" while open
//   - baseline_version_published  one item per version      (baseline_versions.published_at)
//   - learning_started            baselines.first_seen_date
//   - baseline_established        baselines.active_since
//
// The identity is resolved from the tenant's discovery table first (unknown
// → ErrIdentityNotFound). Nothing is invented: a source table that cannot
// be read contributes no items (logged server-side); the milestones exist
// only when the baselines row carries the timestamp.
type IdentityTimelineService struct {
	lookup *MonitoredIdentityLookup
	repo   *repo.IdentityTimelineRepository
}

// NewIdentityTimelineService wires the service.
func NewIdentityTimelineService(lookup *MonitoredIdentityLookup, repo *repo.IdentityTimelineRepository) *IdentityTimelineService {
	return &IdentityTimelineService{lookup: lookup, repo: repo}
}

// IdentityTimelineParams are the parsed §5.6 query parameters.
type IdentityTimelineParams struct {
	Limit  int    // §2 page size: default 25, max 100
	Cursor string // opaque, from a previous meta.next_cursor; "" for the first page
}

// Display titles per item type (§10.3: "API display text from the timeline type").
var timelineTitles = map[string]string{
	models.TimelineTypeAnomaly:                  "Anomaly detected",
	models.TimelineTypeBaselineVersionPublished: "Baseline version published",
	models.TimelineTypeLearningStarted:          "Learning started",
	models.TimelineTypeBaselineEstablished:      "Baseline established",
}

// GetTimeline builds the §5.6 page and the meta.next_cursor (nil on the
// last page). Unknown identities are ErrIdentityNotFound, scope/discovery
// failures *StoreUnavailableError, a foreign cursor ErrInvalidCursor.
func (s *IdentityTimelineService) GetTimeline(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, p IdentityTimelineParams) (*models.IdentityTimelineResponse, *string, error) {
	after, err := decodeTimelineCursor(p.Cursor)
	if err != nil {
		return nil, nil, err
	}
	res, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName)
	if err != nil {
		return nil, nil, err
	}
	id, sc := res.identity, res.scope
	keys := id.allKeys() // identity key, then the shared workload key

	var entries []timelineEntry
	if alerts, err := s.repo.Alerts(ctx, sc, keys); readOK("identity timeline alerts", err) {
		entries = append(entries, anomalyEntries(alerts)...)
	}
	versionsKey := id.Key
	if row, err := s.repo.BaselineMilestones(ctx, sc, keys); readOK("identity timeline baseline", err) && row != nil {
		versionsKey = row.Key
		if row.FirstSeenDate != nil {
			entries = append(entries, milestone(models.TimelineTypeLearningStarted, *row.FirstSeenDate))
		}
		if row.ActiveSince != nil {
			entries = append(entries, milestone(models.TimelineTypeBaselineEstablished, *row.ActiveSince))
		}
	}
	if versions, err := s.repo.PublishedVersions(ctx, sc, versionsKey); readOK("identity timeline versions", err) {
		for _, v := range versions {
			desc := "Version " + strconv.FormatInt(v.Version, 10)
			entries = append(entries, timelineEntry{
				key: timelineKey{At: v.PublishedAt.UnixMilli(), Type: models.TimelineTypeBaselineVersionPublished, Ref: strconv.FormatInt(v.Version, 10)},
				item: models.IdentityTimelineItem{Type: models.TimelineTypeBaselineVersionPublished, Title: timelineTitles[models.TimelineTypeBaselineVersionPublished],
					Description: &desc, OccurredAt: v.PublishedAt},
			})
		}
	}

	sort.SliceStable(entries, func(i, j int) bool { return compareTimelineKeys(entries[i].key, entries[j].key) < 0 })
	if after != nil {
		cut := sort.Search(len(entries), func(i int) bool { return compareTimelineKeys(entries[i].key, *after) > 0 })
		entries = entries[cut:]
	}
	var next *string
	if len(entries) > p.Limit {
		entries = entries[:p.Limit]
		next = encodeTimelineCursor(entries[len(entries)-1].key)
	}
	out := &models.IdentityTimelineResponse{Items: make([]models.IdentityTimelineItem, 0, len(entries))}
	for _, e := range entries {
		out.Items = append(out.Items, e.item)
	}
	return out, next, nil
}

// anomalyEntries groups alerts raised by the same detection window for the
// same dimension and status into one item: one window usually raises several
// alerts on one dimension, and the reviewer reads them as one departure.
// alerts arrive newest first; the group's Ref is its newest alert id.
func anomalyEntries(alerts []repo.TimelineAlertRow) []timelineEntry {
	type groupKey struct {
		at                int64
		dimension, status string
	}
	type group struct {
		first   repo.TimelineAlertRow
		count   int
		reasons map[string]struct{}
	}
	order := []groupKey{}
	groups := map[groupKey]*group{}
	for _, a := range alerts {
		at := a.WindowEnd
		if at.IsZero() {
			at = a.CreatedAt
		}
		k := groupKey{at.UnixMilli(), a.Dimension, strings.ToLower(strings.TrimSpace(a.Status))}
		g := groups[k]
		if g == nil {
			g = &group{first: a, reasons: map[string]struct{}{}}
			groups[k] = g
			order = append(order, k)
		}
		g.count++
		g.reasons[a.Reason] = struct{}{}
	}
	out := make([]timelineEntry, 0, len(order))
	for _, k := range order {
		g := groups[k]
		desc := "Dimension: " + g.first.Dimension + "; " + g.first.Reason
		if extra := len(g.reasons) - 1; extra > 0 {
			desc += fmt.Sprintf(" (and %d other %s)", extra, plural(extra, "reason", "reasons"))
		}
		status := g.first.Status
		out = append(out, timelineEntry{
			key: timelineKey{At: k.at, Type: models.TimelineTypeAnomaly, Ref: strconv.FormatInt(g.first.ID, 10)},
			item: models.IdentityTimelineItem{Type: models.TimelineTypeAnomaly, Title: anomalyTitle(g.count, k.status),
				Description: &desc, OccurredAt: time.UnixMilli(k.at).UTC(), Status: &status},
		})
	}
	return out
}

// anomalyTitle: "1 active anomaly", "3 active anomalies", "2 resolved anomalies".
func anomalyTitle(n int, status string) string {
	state := "active"
	if status != "" && status != "open" {
		state = strings.ReplaceAll(status, "_", " ")
	}
	return fmt.Sprintf("%d %s %s", n, state, plural(n, "anomaly", "anomalies"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// milestone builds a baselines-derived item (no description, no status).
func milestone(typ string, at time.Time) timelineEntry {
	return timelineEntry{
		key:  timelineKey{At: at.UnixMilli(), Type: typ},
		item: models.IdentityTimelineItem{Type: typ, Title: timelineTitles[typ], OccurredAt: at},
	}
}

// ---------------------------------------------------------------------------
// ordering and the keyset cursor
// ---------------------------------------------------------------------------

// timelineEntry is one item with its total-order key.
type timelineEntry struct {
	key  timelineKey
	item models.IdentityTimelineItem
}

// timelineKey orders items: occurred_at descending, then type, then the
// source row reference (alert id / version) descending — a total order,
// so a keyset cursor can resume it.
type timelineKey struct {
	At   int64  `json:"t"` // occurred_at, unix milliseconds
	Type string `json:"k"`
	Ref  string `json:"r,omitempty"`
}

func compareTimelineKeys(a, b timelineKey) int {
	switch {
	case a.At != b.At:
		if a.At > b.At {
			return -1
		}
		return 1
	case a.Type != b.Type:
		return strings.Compare(a.Type, b.Type)
	}
	ra, ea := strconv.ParseInt(a.Ref, 10, 64)
	rb, eb := strconv.ParseInt(b.Ref, 10, 64)
	if ea == nil && eb == nil {
		switch {
		case ra > rb:
			return -1
		case ra < rb:
			return 1
		}
		return 0
	}
	return -strings.Compare(a.Ref, b.Ref)
}

func encodeTimelineCursor(k timelineKey) *string {
	raw, _ := json.Marshal(k)
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

func decodeTimelineCursor(s string) (*timelineKey, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var k timelineKey
	if err := json.Unmarshal(raw, &k); err != nil || timelineTitles[k.Type] == "" {
		return nil, ErrInvalidCursor
	}
	return &k, nil
}
