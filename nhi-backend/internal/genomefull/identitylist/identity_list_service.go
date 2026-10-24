package identitylist

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Service implements GET /api/v1/genome/identities — the fleet table:
//
//  1. latest non-deleted discovery snapshot per resource, normalized to the
//     monitoring key in application code (MonitoredIdentityLookup.Fleet);
//  2. the filters and sort, applied to the normalized set, then one
//     keyset page (limit + opaque cursor);
//  3. for the page: baselines, latest identity_risk_scores and the grouped
//     open-alert counts from PostgreSQL, last_seen from ClickHouse — each a
//     single grouped query, never one per identity. The sort key (risk or
//     last_seen) is read for the whole filtered set first because the page
//     depends on it; name sorting needs nothing beyond discovery.
//
// Nothing is invented: a section whose store cannot be read is null (logged
// server-side) and provenance_confidence stays null until the selection
// rule is confirmed.
type Service struct {
	lookup *common.MonitoredIdentityLookup
	repo   *Repository
	events *common.MonitoringEvents // nil when no monitoring ClickHouse is configured
}

// NewService wires the service.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository, events *common.MonitoringEvents) *Service {
	return &Service{lookup: lookup, repo: repo, events: events}
}

// Values of the sort parameter.
const (
	SortRiskDesc     = "risk_desc"      // latest risk score, highest first, unscored last
	SortLastSeenDesc = "last_seen_desc" // ClickHouse last_seen, newest first, never seen last
	SortNameAsc      = "name_asc"       // display_name, then identity key
)

// Params are the parsed query parameters. Empty filter strings mean "no filter".
type Params struct {
	SourceType string // compact upper-case family
	Search     string // case-insensitive substring of identity_name, display_name, workload_key, service_name or role_arn
	AccountID  string
	Region     string
	Status     string // raw identities.status value (lifecycle values NEED CONFIRMATION)
	Sort       string // Sort* (default SortRiskDesc)
	Limit      int    // default 25, max 100
	Cursor     string // opaque, from a previous meta.next_cursor; "" for the first page
}

// List builds the page and the meta.next_cursor (nil on the last page).
// Scope/discovery failures are *common.StoreUnavailableError; a cursor not
// produced by this endpoint (or produced for another sort) is
// common.ErrInvalidCursor.
func (s *Service) List(ctx context.Context, userID, tenantID, dbName string, p Params) (*Response, *string, error) {
	if p.Sort == "" {
		p.Sort = SortRiskDesc
	}
	after, err := decodeListCursor(p.Cursor, p.Sort)
	if err != nil {
		return nil, nil, err
	}
	sc, fleet, err := s.lookup.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, nil, err
	}

	entries := make([]listEntry, 0, len(fleet))
	for _, id := range fleet {
		if matchesFilters(id, p) {
			entries = append(entries, listEntry{id: id, sortID: id.Key.SourceType + ":" + id.Key.IdentityName})
		}
	}

	// Sort inputs the page depends on are read for the whole filtered set.
	var (
		risk     map[common.MonitoringKey]common.IdentityRiskSnapshotRow
		lastSeen map[common.MonitoringKey]time.Time
	)
	switch p.Sort {
	case SortRiskDesc:
		risk = s.riskFor(ctx, sc, entries)
	case SortLastSeenDesc:
		lastSeen = s.lastSeenFor(ctx, entries)
	}
	for i := range entries {
		entries[i].score = riskScore(risk, entries[i].id)
		if t, ok := lastSeen[entries[i].id.EventKey()]; ok {
			entries[i].lastSeen = &t
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return compareEntries(p.Sort, entries[i], entries[j]) < 0 })
	if after != nil {
		cut := sort.Search(len(entries), func(i int) bool { return compareEntries(p.Sort, entries[i], *after) > 0 })
		entries = entries[cut:]
	}
	var next *string
	if len(entries) > p.Limit {
		entries = entries[:p.Limit]
		next = encodeListCursor(p.Sort, entries[len(entries)-1])
	}

	// The remaining facts are read for the page only.
	if risk == nil {
		risk = s.riskFor(ctx, sc, entries)
	}
	if lastSeen == nil {
		lastSeen = s.lastSeenFor(ctx, entries)
	}
	keys := listKeys(entries)
	baselines, err := s.repo.BaselineSummaries(ctx, sc, keys)
	if !common.ReadOK("identity list baselines", err) {
		baselines = nil
	}
	openAlerts, err := s.repo.OpenAlertCounts(ctx, sc, keys)
	alertsOK := common.ReadOK("identity list open alerts", err)

	out := &Response{Items: make([]Item, 0, len(entries))}
	for _, e := range entries {
		id := e.id
		item := Item{
			ID:              e.sortID,
			SourceType:      id.Key.SourceType,
			NHIIdentityType: id.Subtype,
			IdentityName:    id.Key.IdentityName,
			DisplayName:     id.DisplayName,
			WorkloadKey:     id.WorkloadKey,
			Status:          common.OptionalString(id.Status),
			AccountID:       id.AccountID,
			Region:          common.OptionalString(id.Region),
			ServiceName:     common.OptionalString(id.ServiceName),
			RoleType:        common.OptionalString(id.RoleType),
			RoleARN:         common.OptionalString(id.RoleARN),
		}
		for _, k := range id.AllKeys() { // identity key first, then the shared workload key
			if b, ok := baselines[k]; ok {
				item.Baseline = BaselineSummary{Status: common.OptionalString(b.Status), BuiltDays: common.IntPtr(b.BuiltDays), RequiredDays: common.IntPtr(b.RequiredDays)}
				break
			}
		}
		for _, k := range id.AllKeys() {
			if r, ok := risk[k]; ok {
				item.Risk = RiskSnapshot{Score: r.RiskScore, Severity: common.OptionalString(r.Severity), Status: common.OptionalString(r.ScoreStatus)}
				break
			}
		}
		if t, ok := lastSeen[id.EventKey()]; ok {
			item.LastSeen = &t
		}
		if alertsOK {
			n := 0
			for _, k := range id.AllKeys() {
				n += openAlerts[k]
			}
			item.OpenAnomalyCount = common.IntPtr(n)
		}
		out.Items = append(out.Items, item)
	}
	if err := common.BudgetExceeded(ctx, "identity list"); err != nil {
		return nil, nil, err
	}
	return out, next, nil
}

// matchesFilters applies the filters to one normalized identity.
func matchesFilters(id common.MonitoredIdentity, p Params) bool {
	if p.SourceType != "" && id.Key.SourceType != p.SourceType {
		return false
	}
	if p.AccountID != "" && id.AccountID != p.AccountID {
		return false
	}
	if p.Region != "" && id.Region != p.Region {
		return false
	}
	if p.Status != "" && !strings.EqualFold(id.Status, p.Status) {
		return false
	}
	if p.Search != "" {
		needle := strings.ToLower(p.Search)
		found := false
		for _, hay := range []string{id.Key.IdentityName, id.DisplayName, id.WorkloadKey, id.ServiceName, id.RoleARN} {
			if strings.Contains(strings.ToLower(hay), needle) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// riskFor reads the latest risk snapshot of every entry key (identity and
// workload keys); a failed read is logged and yields no risk (null fields).
func (s *Service) riskFor(ctx context.Context, sc *scope.DataScope, entries []listEntry) map[common.MonitoringKey]common.IdentityRiskSnapshotRow {
	risk, err := common.LatestRiskSnapshots(ctx, sc, listKeys(entries))
	if !common.ReadOK("identity list risk scores", err) {
		return map[common.MonitoringKey]common.IdentityRiskSnapshotRow{}
	}
	return risk
}

// lastSeenFor reads last_seen for the identity keys of the entries; while
// ClickHouse is not configured or cannot be read every last_seen is null.
func (s *Service) lastSeenFor(ctx context.Context, entries []listEntry) map[common.MonitoringKey]time.Time {
	keys := make([]common.MonitoringKey, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.id.EventKey())
	}
	last, err := s.events.LastSeenByIdentity(ctx, keys)
	if !common.EventsReadOK("identity list last seen", err) {
		return map[common.MonitoringKey]time.Time{}
	}
	return last
}

// riskScore resolves the score of an identity (identity key first, then
// the shared workload key); nil when unscored.
func riskScore(risk map[common.MonitoringKey]common.IdentityRiskSnapshotRow, id common.MonitoredIdentity) *int {
	for _, k := range id.AllKeys() {
		if r, ok := risk[k]; ok {
			return r.RiskScore
		}
	}
	return nil
}

// listKeys returns the identity and workload keys of the entries, deduplicated.
func listKeys(entries []listEntry) []common.MonitoringKey {
	seen := map[common.MonitoringKey]struct{}{}
	out := make([]common.MonitoringKey, 0, len(entries))
	for _, e := range entries {
		for _, k := range e.id.AllKeys() {
			if _, dup := seen[k]; !dup {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// ordering and the keyset cursor
// ---------------------------------------------------------------------------

// listEntry is one filtered identity with the values the sort may need.
type listEntry struct {
	id       common.MonitoredIdentity
	sortID   string     // source_type:identity_name — unique, the final tiebreak
	score    *int       // latest risk score (risk_desc)
	lastSeen *time.Time // ClickHouse last_seen (last_seen_desc)
}

// compareEntries orders two entries for sort: the sort value (nulls last),
// then the unique id ascending, so the order is total and a keyset cursor
// can resume it.
func compareEntries(sortBy string, a, b listEntry) int {
	switch sortBy {
	case SortLastSeenDesc:
		if c := common.CompareDesc(a.lastSeen != nil, b.lastSeen != nil, func() int { return a.lastSeen.Compare(*b.lastSeen) }); c != 0 {
			return c
		}
	case SortNameAsc:
		if c := strings.Compare(strings.ToLower(a.id.DisplayName), strings.ToLower(b.id.DisplayName)); c != 0 {
			return c
		}
		if c := strings.Compare(a.id.DisplayName, b.id.DisplayName); c != 0 {
			return c
		}
	default: // SortRiskDesc
		if c := common.CompareDesc(a.score != nil, b.score != nil, func() int { return *a.score - *b.score }); c != 0 {
			return c
		}
	}
	return strings.Compare(a.sortID, b.sortID)
}

// listCursor is the opaque cursor of the list: the sort it was produced for
// and the sort value + id of the last returned row.
type listCursor struct {
	Sort     string `json:"s"`
	ID       string `json:"id"`
	Score    *int   `json:"r,omitempty"`
	LastSeen *int64 `json:"t,omitempty"` // unix milliseconds
	Name     string `json:"n,omitempty"`
}

func encodeListCursor(sortBy string, last listEntry) *string {
	c := listCursor{Sort: sortBy, ID: last.sortID}
	switch sortBy {
	case SortLastSeenDesc:
		if last.lastSeen != nil {
			ms := last.lastSeen.UnixMilli()
			c.LastSeen = &ms
		}
	case SortNameAsc:
		c.Name = last.id.DisplayName
	default:
		c.Score = last.score
	}
	raw, _ := json.Marshal(c)
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

// decodeListCursor rebuilds the comparable entry of the last returned row;
// nil for the first page. Any mismatch with sortBy is common.ErrInvalidCursor.
func decodeListCursor(s, sortBy string) (*listEntry, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, common.ErrInvalidCursor
	}
	var c listCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Sort != sortBy || c.ID == "" {
		return nil, common.ErrInvalidCursor
	}
	e := &listEntry{sortID: c.ID, score: c.Score}
	e.id.DisplayName = c.Name
	if c.LastSeen != nil {
		t := time.UnixMilli(*c.LastSeen).UTC()
		e.lastSeen = &t
	}
	return e, nil
}
