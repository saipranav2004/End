package genome

import (
	"context"
	"nhi-backend-api/internal/logging"
	"sort"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
	"nhi-backend-api/internal/scope"
)

// DashboardSummaryService implements GET /api/v1/dashboard/summary
// (backend_api_contract.md §4.1 / §10.1) on top of the caller's tenant
// schema in PostgreSQL, resolved through scope.Resolver like every other
// data API.
//
// Flow (the contract's two-step process):
//  1. read the latest non-deleted discovery snapshot per resource from the
//     tenant's identities table inside the caller's account/region scope;
//  2. normalize every row to the monitoring key (source_type, identity_name,
//     workload_key) with the §1.4 registry rules — never by joining raw
//     discovery columns in SQL;
//  3. look the keys up in baselines / identity_risk_scores / alerts and
//     aggregate the cards and panels.
//
// Nothing is invented: every value is a query result or null. A card whose
// table cannot be read is null (the failure is logged server-side); the
// ClickHouse-backed cards (critical_anomalies, mean_provenance_confidence)
// are null until log_events is connected; mean_confidence_percent and
// drifting_baselines are null because the pipeline does not persist them.
type DashboardSummaryService struct {
	repo     *repo.DashboardSummaryRepository
	resolver ScopeResolver
}

// ScopeResolver is the part of scope.Resolver the service uses.
type ScopeResolver interface {
	Resolve(ctx context.Context, userID, tenantID, dbName string) (*scope.DataScope, error)
}

// NewDashboardSummaryService wires the service (pass the shared scope.Resolver).
func NewDashboardSummaryService(repo *repo.DashboardSummaryRepository, resolver ScopeResolver) *DashboardSummaryService {
	return &DashboardSummaryService{repo: repo, resolver: resolver}
}

// TimeWindow is a half-open UTC range [From, To).
type TimeWindow struct {
	From time.Time
	To   time.Time
}

// DashboardSummaryParams are the parsed query parameters (§4.1).
type DashboardSummaryParams struct {
	Charts     TimeWindow // departures, anomalies_by_day (default: last 14 days)
	Cards      TimeWindow // open-anomaly cards (default: last 24 hours)
	SourceType string     // optional upper-case service family, e.g. ECS
	AccountID  string     // optional
	Region     string     // optional
}

// StoreUnavailableError means the tenant scope or the discovery table could
// not be read, so there is no data at all; the handler maps it to 503.
type StoreUnavailableError struct {
	Op  string
	Err error
}

func (e *StoreUnavailableError) Error() string { return e.Op + ": " + e.Err.Error() }
func (e *StoreUnavailableError) Unwrap() error { return e.Err }

const (
	summaryTopN           = 10 // §4.1: top 10 / most recent 10
	summaryRecentFeedRows = 50 // raw baseline events read before collapsing to 10 identities
)

// GetSummary builds the dashboard summary for the caller's tenant scope.
func (s *DashboardSummaryService) GetSummary(ctx context.Context, userID, tenantID, dbName string, p DashboardSummaryParams) (*models.DashboardSummaryResponse, error) {
	sc, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, &StoreUnavailableError{Op: "resolve tenant scope", Err: err}
	}
	rows, err := s.repo.LatestIdentities(ctx, sc)
	if err != nil {
		return nil, &StoreUnavailableError{Op: "read discovery identities", Err: err}
	}

	// ---- step 2: normalize + filter ----------------------------------------
	var ids []monitoredIdentity
	for _, row := range rows {
		id, ok := normalizeDiscoveryRow(row)
		if !ok {
			continue // deferred (EKS), human/resource-only or unknown type
		}
		if p.SourceType != "" && id.Key.SourceType != p.SourceType {
			continue
		}
		if p.AccountID != "" && id.AccountID != p.AccountID {
			continue
		}
		if p.Region != "" && !strings.EqualFold(id.Region, p.Region) {
			continue
		}
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := strings.ToLower(ids[i].DisplayName), strings.ToLower(ids[j].DisplayName)
		if a != b {
			return a < b
		}
		return ids[i].Key.IdentityName < ids[j].Key.IdentityName
	})
	set := newIdentityIndex(ids)
	keys := set.lookupKeys()

	out := &models.DashboardSummaryResponse{
		DeparturesByDimension:   []models.DashboardDimensionCount{},
		AnomaliesByDay:          []models.DashboardDayCount{},
		MostAnomalousIdentities: []models.DashboardAnomalousIdentity{},
		RecentlyBaselined:       []models.DashboardRecentlyBaselined{},
	}
	out.Cards.WindowHours = int(p.Cards.To.Sub(p.Cards.From).Hours())
	out.Cards.IdentitiesWatched = intPtr(len(ids))

	// ---- baseline cards ----------------------------------------------------
	baselines, err := s.repo.BaselineStates(ctx, sc, keys)
	if readOK(ctx, "dashboard summary baselines", err) {
		established := 0
		for _, id := range ids {
			if b, ok := set.baselineFor(baselines, id); ok && b.Status == "active" {
				established++
			}
		}
		out.Cards.BaselinesEstablished = intPtr(established)
		out.Cards.StillLearning = intPtr(len(ids) - established)
		if len(ids) > 0 { // §4.1: baselines_established / identities_watched * 100
			out.Cards.BaselineCoveragePercent = floatPtr(round1(float64(established) / float64(len(ids)) * 100))
		}
	}

	// ---- open anomalies (cards window) -------------------------------------
	if n, err := s.repo.CountOpenAlerts(ctx, sc, keys, p.Cards.From, p.Cards.To); readOK(ctx, "dashboard summary open alerts", err) {
		out.Cards.OpenAnomalies = intPtr(n)
	}
	// critical_anomalies and mean_provenance_confidence come from ClickHouse
	// log_events (§10.1) and stay null until that store is connected.
	// mean_confidence_percent (no product formula) and drifting_baselines
	// (not persisted) are null; drifting_baselines_available is false (§4.1).

	// ---- most anomalous identities -----------------------------------------
	risk, err := s.repo.LatestRiskScores(ctx, sc, keys)
	riskOK := readOK(ctx, "dashboard summary risk scores", err)
	if riskOK {
		type scored struct {
			idx int
			row repo.IdentityRiskRow
		}
		var list []scored
		for i, id := range ids {
			if rr, ok := set.riskFor(risk, id); ok && rr.RiskScore != nil {
				list = append(list, scored{i, rr})
			}
		}
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if *a.row.RiskScore != *b.row.RiskScore {
				return *a.row.RiskScore > *b.row.RiskScore
			}
			if a.row.AnomalousEventCount != b.row.AnomalousEventCount {
				return a.row.AnomalousEventCount > b.row.AnomalousEventCount
			}
			return ids[a.idx].DisplayName < ids[b.idx].DisplayName
		})
		for i, item := range list {
			if i >= summaryTopN {
				break
			}
			id := ids[item.idx]
			out.MostAnomalousIdentities = append(out.MostAnomalousIdentities, models.DashboardAnomalousIdentity{
				DashboardIdentityRef: id.ref(),
				AccountID:            id.AccountID,
				Region:               id.Region,
				ServiceName:          id.ServiceName,
				WorkloadKey:          id.WorkloadKey,
				RiskScore:            item.row.RiskScore,
				Severity:             item.row.Severity,
				ScoreStatus:          item.row.ScoreStatus,
				EventCount:           item.row.EventCount,
				AnomalousEventCount:  item.row.AnomalousEventCount,
				WindowStart:          item.row.WindowStart.UTC(),
				WindowEnd:            item.row.WindowEnd.UTC(),
				ComputedAt:           item.row.ComputedAt.UTC(),
			})
		}
	}

	// ---- departures by dimension / anomalies per day (charts window) -------
	dims, err := s.repo.OpenAlertsByDimension(ctx, sc, keys, p.Charts.From, p.Charts.To)
	if readOK(ctx, "dashboard summary alerts by dimension", err) {
		for _, d := range dims {
			out.DeparturesByDimension = append(out.DeparturesByDimension, models.DashboardDimensionCount{Dimension: d.Dimension, Count: d.Count})
		}
	}
	days, err := s.repo.AlertsByDay(ctx, sc, keys, p.Charts.From, p.Charts.To)
	if readOK(ctx, "dashboard summary alerts by day", err) {
		for _, d := range days {
			out.AnomaliesByDay = append(out.AnomaliesByDay, models.DashboardDayCount{Day: d.Day.Format("2006-01-02"), Count: d.Count})
		}
	}

	// ---- recently baselined ------------------------------------------------
	// §4.1: "the most recent 10 identities where the baseline became active
	// or a new version was published" — one row per identity, newest event.
	recent, err := s.repo.RecentlyBaselined(ctx, sc, keys, summaryRecentFeedRows)
	if readOK(ctx, "dashboard summary recently baselined", err) {
		seen := map[int]struct{}{}
		for _, ev := range recent {
			idx, known := set.indexOf(ev.Key)
			if !known {
				continue
			}
			if _, dup := seen[idx]; dup {
				continue
			}
			seen[idx] = struct{}{}
			id := ids[idx]
			out.RecentlyBaselined = append(out.RecentlyBaselined, models.DashboardRecentlyBaselined{
				DashboardIdentityRef: id.ref(), AccountID: id.AccountID, Region: id.Region,
				Event: ev.Event, OccurredAt: ev.OccurredAt, Version: ev.Version,
			})
			if len(out.RecentlyBaselined) >= summaryTopN {
				break
			}
		}
	}
	return out, nil
}

// readOK reports whether a monitoring-table read succeeded; failures are
// logged and leave the dependent fields null/empty (graceful degradation).
func readOK(ctx context.Context, op string, err error) bool {
	if err == nil {
		return true
	}
	if repo.IsMonitoringTableMissing(err) {
		logging.FromContext(ctx).Warn("genome: monitoring table not present in the tenant schema", "op", op, "err", err)
	} else {
		logging.FromContext(ctx).Warn("genome read failed, field left empty", "op", op, "err", err, "degraded", true)
	}
	return false
}

// ---------------------------------------------------------------------------
// Identity index: lookups by identity key with workload-key fallback
// ---------------------------------------------------------------------------

type identityIndex struct {
	ids        []monitoredIdentity
	byKey      map[repo.MonitoringKey]int
	byWorkload map[repo.MonitoringKey]int // first identity sharing the workload key
}

func newIdentityIndex(ids []monitoredIdentity) *identityIndex {
	set := &identityIndex{ids: ids, byKey: map[repo.MonitoringKey]int{}, byWorkload: map[repo.MonitoringKey]int{}}
	for i, id := range ids {
		if _, dup := set.byKey[id.Key]; !dup {
			set.byKey[id.Key] = i
		}
		if wk, ok := id.workloadKey(); ok {
			if _, dup := set.byWorkload[wk]; !dup {
				set.byWorkload[wk] = i
			}
		}
	}
	return set
}

// lookupKeys returns every key worth querying: identity keys plus workload
// keys (the pipeline may store a shared profile under the workload key).
func (s *identityIndex) lookupKeys() []repo.MonitoringKey {
	out := make([]repo.MonitoringKey, 0, len(s.byKey)+len(s.byWorkload))
	seen := map[repo.MonitoringKey]struct{}{}
	for _, id := range s.ids {
		for _, k := range id.allKeys() {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
	}
	return out
}

// indexOf resolves a monitoring key (identity key first, then workload key).
func (s *identityIndex) indexOf(k repo.MonitoringKey) (int, bool) {
	if i, ok := s.byKey[k]; ok {
		return i, true
	}
	if i, ok := s.byWorkload[k]; ok {
		return i, true
	}
	return 0, false
}

func (s *identityIndex) baselineFor(m map[repo.MonitoringKey]repo.BaselineStateRow, id monitoredIdentity) (repo.BaselineStateRow, bool) {
	for _, k := range id.allKeys() {
		if b, ok := m[k]; ok {
			return b, true
		}
	}
	return repo.BaselineStateRow{}, false
}

func (s *identityIndex) riskFor(m map[repo.MonitoringKey]repo.IdentityRiskRow, id monitoredIdentity) (repo.IdentityRiskRow, bool) {
	for _, k := range id.allKeys() {
		if r, ok := m[k]; ok {
			return r, true
		}
	}
	return repo.IdentityRiskRow{}, false
}
