package identitypeers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements
// GET /api/v1/genome/identities/{source_type}/{identity_name}/peers — the
// peer-group comparison:
//
//   - peers are the other monitored identities of the caller's scope whose
//     MVP peer key (source_type + account_id + region + service_name +
//     cluster_name + role_type) equals the requested identity's;
//   - per peer: latest identity_risk_scores.risk_score, open alerts and the
//     dominant open alerts.dimension (PostgreSQL, one grouped query each)
//     and uniqExact(event_id) in the requested window (ClickHouse, one
//     grouped query).
//
// Peers are ordered by risk score descending (unscored last), then
// identity_name. Nothing is invented: a store that cannot be read leaves
// its fields null (logged server-side).
type Service struct {
	lookup *common.MonitoredIdentityLookup
	repo   *Repository
	events *common.MonitoringEvents // nil when no monitoring ClickHouse is configured
}

// NewService wires the service.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository, events *common.MonitoringEvents) *Service {
	return &Service{lookup: lookup, repo: repo, events: events}
}

// Params are the parsed query parameters.
type Params struct {
	Window common.TimeWindow // from/to of recent_call_count
	Limit  int               // page size: default 25, max 100
	Cursor string            // opaque, from a previous meta.next_cursor; "" for the first page
}

// GetPeers builds the page and the meta.next_cursor (nil on the last
// page). Unknown identities are common.ErrIdentityNotFound,
// scope/discovery failures *common.StoreUnavailableError, a foreign cursor
// common.ErrInvalidCursor.
func (s *Service) GetPeers(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, p Params) (*Response, *string, error) {
	after, err := decodeCursor(p.Cursor)
	if err != nil {
		return nil, nil, err
	}
	sc, fleet, err := s.lookup.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, nil, err
	}
	var (
		self  common.MonitoredIdentity
		found bool
	)
	for _, id := range fleet {
		if id.Key.SourceType == sourceType && id.Key.IdentityName == identityName {
			self, found = id, true
			break
		}
	}
	if !found {
		return nil, nil, common.ErrIdentityNotFound
	}

	var peers []peerEntry
	for _, id := range fleet {
		if id.Key != self.Key && id.PeerKey() == self.PeerKey() {
			peers = append(peers, peerEntry{id: id})
		}
	}
	// The order depends on the risk score, so it is read for the whole group.
	risk, err := common.LatestRiskSnapshots(ctx, sc, peerKeys(peers))
	if !common.ReadOK("identity peers risk scores", err) {
		risk = map[common.MonitoringKey]common.IdentityRiskSnapshotRow{}
	}
	for i := range peers {
		peers[i].score = riskScore(risk, peers[i].id)
	}
	sort.SliceStable(peers, func(i, j int) bool { return compareEntries(peers[i], peers[j]) < 0 })
	if after != nil {
		cut := sort.Search(len(peers), func(i int) bool { return compareEntries(peers[i], *after) > 0 })
		peers = peers[cut:]
	}
	var next *string
	if len(peers) > p.Limit {
		peers = peers[:p.Limit]
		next = encodeCursor(peers[len(peers)-1])
	}

	// The remaining facts are read for the page only, one grouped query each.
	keys := peerKeys(peers)
	dimensionCounts, err := s.repo.OpenAlertsByIdentityDimension(ctx, sc, keys)
	alertsOK := common.ReadOK("identity peers open alerts", err)
	identityKeys := make([]common.MonitoringKey, 0, len(peers))
	for _, pe := range peers {
		identityKeys = append(identityKeys, pe.id.Key)
	}
	calls, err := s.events.CallCountsByIdentity(ctx, identityKeys, p.Window.From, p.Window.To)
	callsOK := common.EventsReadOK("identity peers call counts", err)

	out := &Response{Peers: make([]Peer, 0, len(peers))}
	for _, pe := range peers {
		peer := Peer{IdentityName: pe.id.Key.IdentityName, DisplayName: pe.id.DisplayName, RiskScore: pe.score}
		if alertsOK {
			open, outlier := peerAlertSummary(dimensionCounts, pe.id)
			peer.OpenAnomalyCount = common.IntPtr(open)
			peer.OutlierDimension = common.OptionalString(outlier)
		}
		if callsOK {
			n := calls[pe.id.Key]
			peer.RecentCallCount = &n
		}
		out.Peers = append(out.Peers, peer)
	}
	if err := common.BudgetExceeded(ctx, "identity peers"); err != nil {
		return nil, nil, err
	}
	return out, next, nil
}

// peerAlertSummary derives open_anomaly_count (sum over the peer's keys
// and dimensions) and outlier_dimension (the dimension with the most open
// alerts; ties by name, as ordered by the query) from the grouped rows.
func peerAlertSummary(rows []PeerDimensionCountRow, id common.MonitoredIdentity) (open int, outlier string) {
	mine := map[common.MonitoringKey]struct{}{}
	for _, k := range id.AllKeys() {
		mine[k] = struct{}{}
	}
	perDimension := map[string]int{} // merged over the peer's identity and workload keys
	for _, r := range rows {
		if _, ok := mine[r.Key]; !ok {
			continue
		}
		open += r.Count
		perDimension[r.Dimension] += r.Count
	}
	best := 0
	for dimension, n := range perDimension {
		if n > best || (n == best && dimension < outlier) {
			best, outlier = n, dimension
		}
	}
	return open, outlier
}

// peerKeys returns the identity and workload keys of the peers, deduplicated.
func peerKeys(peers []peerEntry) []common.MonitoringKey {
	seen := map[common.MonitoringKey]struct{}{}
	out := make([]common.MonitoringKey, 0, len(peers))
	for _, pe := range peers {
		for _, k := range pe.id.AllKeys() {
			if _, dup := seen[k]; !dup {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
	}
	return out
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

// ---------------------------------------------------------------------------
// ordering and the keyset cursor
// ---------------------------------------------------------------------------

// peerEntry is one peer with its sort value.
type peerEntry struct {
	id    common.MonitoredIdentity
	score *int
}

// compareEntries orders peers by risk score descending (unscored last),
// then identity_name ascending — a total order within a peer group
// (identity_name is unique per source_type there).
func compareEntries(a, b peerEntry) int {
	if c := common.CompareDesc(a.score != nil, b.score != nil, func() int { return *a.score - *b.score }); c != 0 {
		return c
	}
	return strings.Compare(a.id.Key.IdentityName, b.id.Key.IdentityName)
}

// cursor is the opaque cursor: the sort value and identity_name of the
// last returned peer.
type cursor struct {
	Score *int   `json:"r,omitempty"`
	Name  string `json:"id"`
}

func encodeCursor(last peerEntry) *string {
	raw, _ := json.Marshal(cursor{Score: last.score, Name: last.id.Key.IdentityName})
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

func decodeCursor(s string) (*peerEntry, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, common.ErrInvalidCursor
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Name == "" {
		return nil, common.ErrInvalidCursor
	}
	e := &peerEntry{score: c.Score}
	e.id.Key.IdentityName = c.Name
	return e, nil
}
