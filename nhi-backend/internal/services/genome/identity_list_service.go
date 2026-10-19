package genome

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
	"nhi-backend-api/internal/scope"
)

// IdentityListService implements GET /api/v1/identities
// (backend_api_contract.md §5.1 / §10.2) — the fleet table:
//
//  1. latest non-deleted discovery snapshot per resource, normalized to the
//     monitoring key in application code (MonitoredIdentityLookup.Fleet);
//  2. the §5.1 filters and sort, applied to the normalized set, then one
//     keyset page (limit + opaque cursor);
//  3. for the page: baselines, latest identity_risk_scores and the grouped
//     open-alert counts from PostgreSQL, last_seen from ClickHouse — each a
//     single grouped query, never one per identity. The sort key (risk or
//     last_seen) is read for the whole filtered set first because the page
//     depends on it; name sorting needs nothing beyond discovery.
//
// Nothing is invented: a section whose store cannot be read is null (logged
// server-side) and provenance_confidence stays null until the §10.2
// selection rule is confirmed.
type IdentityListService struct {
	lookup *MonitoredIdentityLookup
	repo   *repo.IdentityListRepository
	events *repo.MonitoringEvents // nil when no monitoring ClickHouse is configured
}

// NewIdentityListService wires the service.
func NewIdentityListService(lookup *MonitoredIdentityLookup, repo *repo.IdentityListRepository, events *repo.MonitoringEvents) *IdentityListService {
	return &IdentityListService{lookup: lookup, repo: repo, events: events}
}

// Values of the §5.1 sort parameter.
const (
	IdentitySortRiskDesc     = "risk_desc"      // latest risk score, highest first, unscored last
	IdentitySortLastSeenDesc = "last_seen_desc" // ClickHouse last_seen, newest first, never seen last
	IdentitySortNameAsc      = "name_asc"       // display_name, then identity key
)

// Values of the baseline filter. Established is a baselines row with status
// "active" — the dashboard summary's baselines_established definition;
// every other monitored identity (no row, or not active) is still learning.
const (
	BaselineEstablished = "established"
	BaselineLearning    = "learning"
)

// IdentityListParams are the parsed §5.1 query parameters. Empty filter
// strings mean "no filter".
type IdentityListParams struct {
	SourceType string // compact upper-case family (§2)
	Search     string // case-insensitive substring of identity_name, display_name, workload_key, service_name or role_arn
	AccountID  string
	Region     string
	Status     string // raw identities.status value (lifecycle values NEED CONFIRMATION, §10.2)
	Baseline   string // BaselineEstablished | BaselineLearning; "" = both
	Sort       string // IdentitySort* (default IdentitySortRiskDesc)
	Limit      int    // §2: default 25, max 100
	Cursor     string // opaque, from a previous meta.next_cursor; "" for the first page
}

// List builds the §5.1 page and the meta.next_cursor (nil on the last
// page). Scope/discovery failures are *StoreUnavailableError; a cursor not
// produced by this endpoint (or produced for another sort) is ErrInvalidCursor.
func (s *IdentityListService) List(ctx context.Context, userID, tenantID, dbName string, p IdentityListParams) (*models.IdentityListResponse, *string, error) {
	if p.Sort == "" {
		p.Sort = IdentitySortRiskDesc
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
		if matchesListFilters(id, p) {
			entries = append(entries, listEntry{id: id, sortID: id.Key.SourceType + ":" + id.Key.IdentityName})
		}
	}

	if p.Baseline != "" {
		entries = s.filterByBaseline(ctx, sc, entries, p.Baseline == BaselineEstablished)
	}

	// Sort inputs the page depends on are read for the whole filtered set.
	var (
		risk     map[repo.MonitoringKey]repo.IdentityRiskSnapshotRow
		lastSeen map[repo.MonitoringKey]time.Time
	)
	switch p.Sort {
	case IdentitySortRiskDesc:
		risk = s.riskFor(ctx, sc, entries)
	case IdentitySortLastSeenDesc:
		lastSeen = s.lastSeenFor(ctx, entries)
	}
	for i := range entries {
		entries[i].score = riskScore(risk, entries[i].id)
		if t, ok := lastSeen[entries[i].id.Key]; ok {
			entries[i].lastSeen = &t
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return compareListEntries(p.Sort, entries[i], entries[j]) < 0 })
	if after != nil {
		cut := sort.Search(len(entries), func(i int) bool { return compareListEntries(p.Sort, entries[i], *after) > 0 })
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
	if !readOK("identity list baselines", err) {
		baselines = nil
	}
	openAlerts, err := s.repo.OpenAlertCounts(ctx, sc, keys)
	alertsOK := readOK("identity list open alerts", err)

	out := &models.IdentityListResponse{Items: make([]models.IdentityListItem, 0, len(entries))}
	for _, e := range entries {
		id := e.id
		item := models.IdentityListItem{
			ID:           e.sortID,
			SourceType:   id.Key.SourceType,
			IdentityName: id.Key.IdentityName,
			DisplayName:  id.DisplayName,
			WorkloadKey:  id.WorkloadKey,
			Status:       optionalString(id.Status),
			AccountID:    id.AccountID,
			Region:       optionalString(id.Region),
			ServiceName:  optionalString(id.ServiceName),
			RoleType:     optionalString(id.RoleType),
			RoleARN:      optionalString(id.RoleARN),
		}
		for _, k := range id.allKeys() { // identity key first, then the shared workload key
			if b, ok := baselines[k]; ok {
				item.Baseline = models.IdentityBaselineSummary{Status: optionalString(b.Status), BuiltDays: intPtr(b.BuiltDays), RequiredDays: intPtr(b.RequiredDays)}
				break
			}
		}
		for _, k := range id.allKeys() {
			if r, ok := risk[k]; ok {
				item.Risk = models.IdentityRiskSnapshot{Score: r.RiskScore, Severity: optionalString(r.Severity), Status: optionalString(r.ScoreStatus)}
				break
			}
		}
		if t, ok := lastSeen[id.Key]; ok {
			item.LastSeen = &t
		}
		if alertsOK {
			n := 0
			for _, k := range id.allKeys() {
				n += openAlerts[k]
			}
			item.OpenAnomalyCount = intPtr(n)
		}
		out.Items = append(out.Items, item)
	}
	return out, next, nil
}

// matchesListFilters applies the §5.1 filters to one normalized identity.
func matchesListFilters(id monitoredIdentity, p IdentityListParams) bool {
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

// filterByBaseline keeps the entries whose baseline is (or is not) active,
// reading the baselines of the whole filtered set in one query. When the
// baselines cannot be read nothing counts as established.
func (s *IdentityListService) filterByBaseline(ctx context.Context, sc *scope.DataScope, entries []listEntry, established bool) []listEntry {
	baselines, err := s.repo.BaselineSummaries(ctx, sc, listKeys(entries))
	if !readOK("identity list baseline filter", err) {
		baselines = nil
	}
	kept := entries[:0]
	for _, e := range entries {
		active := false
		for _, k := range e.id.allKeys() { // identity key first, as the item's baseline
			if b, ok := baselines[k]; ok {
				active = b.Status == "active"
				break
			}
		}
		if active == established {
			kept = append(kept, e)
		}
	}
	return kept
}

// riskFor reads the latest risk snapshot of every entry key (identity and
// workload keys); a failed read is logged and yields no risk (null fields).
func (s *IdentityListService) riskFor(ctx context.Context, sc *scope.DataScope, entries []listEntry) map[repo.MonitoringKey]repo.IdentityRiskSnapshotRow {
	risk, err := s.repo.LatestRiskSnapshots(ctx, sc, listKeys(entries))
	if !readOK("identity list risk scores", err) {
		return map[repo.MonitoringKey]repo.IdentityRiskSnapshotRow{}
	}
	return risk
}

// lastSeenFor reads last_seen for the identity keys of the entries; while
// ClickHouse is not configured or cannot be read every last_seen is null.
func (s *IdentityListService) lastSeenFor(ctx context.Context, entries []listEntry) map[repo.MonitoringKey]time.Time {
	keys := make([]repo.MonitoringKey, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.id.Key)
	}
	last, err := s.events.LastSeenByIdentity(ctx, keys)
	if !eventsReadOK("identity list last seen", err) {
		return map[repo.MonitoringKey]time.Time{}
	}
	return last
}

// riskScore resolves the score of an identity (identity key first, then
// the shared workload key); nil when unscored.
func riskScore(risk map[repo.MonitoringKey]repo.IdentityRiskSnapshotRow, id monitoredIdentity) *int {
	for _, k := range id.allKeys() {
		if r, ok := risk[k]; ok {
			return r.RiskScore
		}
	}
	return nil
}

// listKeys returns the identity and workload keys of the entries, deduplicated.
func listKeys(entries []listEntry) []repo.MonitoringKey {
	seen := map[repo.MonitoringKey]struct{}{}
	out := make([]repo.MonitoringKey, 0, len(entries))
	for _, e := range entries {
		for _, k := range e.id.allKeys() {
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
	id       monitoredIdentity
	sortID   string     // source_type:identity_name — unique, the final tiebreak
	score    *int       // latest risk score (risk_desc)
	lastSeen *time.Time // ClickHouse last_seen (last_seen_desc)
}

// compareListEntries orders two entries for sort: the sort value (nulls
// last), then the unique id ascending, so the order is total and a keyset
// cursor can resume it.
func compareListEntries(sortBy string, a, b listEntry) int {
	switch sortBy {
	case IdentitySortLastSeenDesc:
		if c := compareDesc(a.lastSeen != nil, b.lastSeen != nil, func() int { return a.lastSeen.Compare(*b.lastSeen) }); c != 0 {
			return c
		}
	case IdentitySortNameAsc:
		if c := strings.Compare(strings.ToLower(a.id.DisplayName), strings.ToLower(b.id.DisplayName)); c != 0 {
			return c
		}
		if c := strings.Compare(a.id.DisplayName, b.id.DisplayName); c != 0 {
			return c
		}
	default: // IdentitySortRiskDesc
		if c := compareDesc(a.score != nil, b.score != nil, func() int { return *a.score - *b.score }); c != 0 {
			return c
		}
	}
	return strings.Compare(a.sortID, b.sortID)
}

// compareDesc orders two optional values descending with nulls last; cmp
// compares the two present values ascending.
func compareDesc(aPresent, bPresent bool, cmp func() int) int {
	switch {
	case aPresent && bPresent:
		return -cmp()
	case aPresent:
		return -1
	case bPresent:
		return 1
	}
	return 0
}

// listCursor is the opaque §2 cursor of the list: the sort it was produced
// for and the sort value + id of the last returned row.
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
	case IdentitySortLastSeenDesc:
		if last.lastSeen != nil {
			ms := last.lastSeen.UnixMilli()
			c.LastSeen = &ms
		}
	case IdentitySortNameAsc:
		c.Name = last.id.DisplayName
	default:
		c.Score = last.score
	}
	raw, _ := json.Marshal(c)
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

// decodeListCursor rebuilds the comparable entry of the last returned row;
// nil for the first page. Any mismatch with sortBy is ErrInvalidCursor.
func decodeListCursor(s, sortBy string) (*listEntry, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var c listCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Sort != sortBy || c.ID == "" {
		return nil, ErrInvalidCursor
	}
	e := &listEntry{sortID: c.ID, score: c.Score}
	e.id.DisplayName = c.Name
	if c.LastSeen != nil {
		t := time.UnixMilli(*c.LastSeen).UTC()
		e.lastSeen = &t
	}
	return e, nil
}
