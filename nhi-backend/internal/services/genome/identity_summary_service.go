package genome

import (
	"context"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
)

// IdentitySummaryService implements
// GET /api/v1/genome/identities/{source_type}/{identity_name}
// (backend_api_contract.md §5.2 / §10.2) — the identity page header:
//
//   - identity          latest discovery snapshot, normalized (PostgreSQL)
//   - baseline_summary  baselines                                (PostgreSQL)
//   - headline_metrics  open alerts (PostgreSQL); calls in the requested
//     window and last seen (ClickHouse log_events)
//
// Monitoring facts are looked up by the identity key first and the shared
// workload key second (the contract's fallback). Nothing is invented: a
// section whose store cannot be read is omitted (logged server-side), and the
// ClickHouse metrics are omitted until the pipeline's log_events is reachable.
type IdentitySummaryService struct {
	lookup *MonitoredIdentityLookup
	repo   *repo.IdentitySummaryRepository
	events *repo.MonitoringEvents // nil when no monitoring ClickHouse is configured
}

// NewIdentitySummaryService wires the service.
func NewIdentitySummaryService(lookup *MonitoredIdentityLookup, repo *repo.IdentitySummaryRepository, events *repo.MonitoringEvents) *IdentitySummaryService {
	return &IdentitySummaryService{lookup: lookup, repo: repo, events: events}
}

// IdentitySummaryParams are the parsed query parameters (§2 from/to).
type IdentitySummaryParams struct {
	Window TimeWindow // window of headline_metrics.calls_in_window
}

// GetIdentity builds the §5.2 response. Unknown identities are
// ErrIdentityNotFound; scope/discovery failures are *StoreUnavailableError.
func (s *IdentitySummaryService) GetIdentity(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, p IdentitySummaryParams) (*models.IdentitySummaryResponse, error) {
	res, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName)
	if err != nil {
		return nil, err
	}
	id, sc := res.identity, res.scope
	keys := id.allKeys() // identity key, then the workload key when it differs

	out := &models.IdentitySummaryResponse{
		Identity: models.IdentityRecord{
			SourceType:      id.Key.SourceType,
			NHIIdentityType: id.Subtype,
			IdentityName:    id.Key.IdentityName,
			DisplayName:     id.DisplayName,
			WorkloadKey:     id.WorkloadKey,
			Status:          id.Status,
			AccountID:       id.AccountID,
			Region:          id.Region,
			ServiceName:     id.ServiceName,
			RoleType:        id.RoleType,
			RoleARN:         id.RoleARN,
		},
	}

	// ---- PostgreSQL monitoring tables -------------------------------------
	if b, err := s.repo.BaselineSummary(ctx, sc, keys); readOK(ctx, "identity baseline summary", err) && b != nil {
		out.BaselineSummary = &models.IdentityBaselineSummary{Status: b.Status, BuiltDays: b.BuiltDays, RequiredDays: b.RequiredDays}
	}
	if n, err := s.repo.OpenAlertCount(ctx, sc, keys); readOK(ctx, "identity open alerts", err) {
		out.HeadlineMetrics.OpenAnomalies = intPtr(n)
	}

	// ---- ClickHouse log_events ------------------------------------------------
	if calls, err := s.events.TotalCalls(ctx, res.scope.AccountIDs, id.Key.SourceType, id.eventName(), p.Window.From, p.Window.To); eventsReadOK(ctx, "identity calls in window", err) {
		out.HeadlineMetrics.CallsInWindow = &calls
	}
	if last, err := s.events.LastSeen(ctx, res.scope.AccountIDs, id.Key.SourceType, id.eventName()); eventsReadOK(ctx, "identity last seen", err) {
		out.HeadlineMetrics.LastSeen = last
	}
	return out, nil
}
