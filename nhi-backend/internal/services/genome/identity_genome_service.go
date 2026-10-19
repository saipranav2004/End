package genome

import (
	"context"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
)

// IdentityGenomeService implements
// GET /api/v1/identities/{source_type}/{identity_name}/genome
// (backend_api_contract.md §5.4 / §10.3) — the behavioural fingerprint,
// typical actions, typical resources and the activity schedule. Everything
// is aggregated by ClickHouse from log_events inside the requested window;
// the service only resolves the identity, forwards the §2 limit and turns
// distinct-event counts into the contract's share percentages
// (count / all calls × 100).
//
// Nothing is invented: while the monitoring ClickHouse is not configured or
// cannot be read all four lists are empty (logged server-side).
type IdentityGenomeService struct {
	lookup *MonitoredIdentityLookup
	events *repo.MonitoringEvents // nil when no monitoring ClickHouse is configured
}

// NewIdentityGenomeService wires the service.
func NewIdentityGenomeService(lookup *MonitoredIdentityLookup, events *repo.MonitoringEvents) *IdentityGenomeService {
	return &IdentityGenomeService{lookup: lookup, events: events}
}

// IdentityGenomeParams are the parsed query parameters.
type IdentityGenomeParams struct {
	Window TimeWindow // §2 from (inclusive) / to (exclusive)
	Limit  int        // §2 limit: rows of typical_actions / typical_resources and values per fingerprint dimension
}

// genomeFingerprintDimensions are the dimensions_json keys the contract
// allows the API to parse for the fingerprint (§5.4), including the ECS- and
// Step Functions-specific ones; keys absent from an identity's events simply
// yield no points.
var genomeFingerprintDimensions = []string{
	"api_operation", "api_service", "region", "read_write", "result", "resource_type", "resource_pattern",
	"source_network_class", "user_agent_family", "hour_of_week_utc", "timing",
	"cluster_name", "launch_type", "task_definition_family", // ECS
	"state_machine_name", "execution_status", // Step Functions
}

// GetGenome builds the §5.4 response. Unknown identities are
// ErrIdentityNotFound; scope/discovery failures are *StoreUnavailableError.
func (s *IdentityGenomeService) GetGenome(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, p IdentityGenomeParams) (*models.IdentityGenomeResponse, error) {
	res, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName)
	if err != nil {
		return nil, err
	}
	key := res.identity.Key
	from, to := p.Window.From, p.Window.To

	out := &models.IdentityGenomeResponse{
		Fingerprint:      []models.IdentityFingerprintPoint{},
		TypicalActions:   []models.IdentityTypicalAction{},
		TypicalResources: []models.IdentityTypicalResource{},
		ActivitySchedule: []models.IdentityScheduleCell{},
	}

	// Denominator of every share: all calls of the identity in the window.
	total, err := s.events.TotalCalls(ctx, key.SourceType, key.IdentityName, from, to)
	if !eventsReadOK("identity genome total calls", err) {
		return out, nil
	}

	if points, err := s.events.Fingerprint(ctx, key.SourceType, key.IdentityName, from, to, genomeFingerprintDimensions, p.Limit); eventsReadOK("identity fingerprint", err) {
		for _, pt := range points {
			out.Fingerprint = append(out.Fingerprint, models.IdentityFingerprintPoint{Dimension: pt.Dimension, Value: pt.Value, SharePercent: sharePercent(pt.Calls, total)})
		}
	}
	if actions, err := s.events.TypicalActions(ctx, key.SourceType, key.IdentityName, from, to, p.Limit); eventsReadOK("identity typical actions", err) {
		for _, a := range actions {
			out.TypicalActions = append(out.TypicalActions, models.IdentityTypicalAction{APIOperation: apiOperation(a.EventSource, a.EventName), Calls: a.Calls, SharePercent: sharePercent(a.Calls, total)})
		}
	}
	if resources, err := s.events.TypicalResources(ctx, key.SourceType, key.IdentityName, from, to, p.Limit); eventsReadOK("identity typical resources", err) {
		for _, r := range resources {
			out.TypicalResources = append(out.TypicalResources, models.IdentityTypicalResource{Resource: r.ResourceARN, ResourceType: optionalString(r.ResourceType), Calls: r.Calls})
		}
	}
	if cells, err := s.events.ActivitySchedule(ctx, key.SourceType, key.IdentityName, from, to); eventsReadOK("identity activity schedule", err) {
		for _, c := range cells {
			out.ActivitySchedule = append(out.ActivitySchedule, models.IdentityScheduleCell{DayOfWeek: int(c.DayOfWeek), Hour: int(c.Hour), Calls: c.Calls})
		}
	}
	return out, nil
}

// sharePercent is §10.3 share_percent: part / all calls × 100, one decimal.
func sharePercent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round1(float64(part) / float64(total) * 100)
}
