package dashboardactivity

import (
	"context"
	"strings"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements GET /api/v1/genome/dashboard/activity: the fleet
// call-volume series from the monitoring ClickHouse log_events table and
// the anomalies-per-day series from the tenant's alerts table, both over
// [from, to).
//
// Scope: call volume is bound to the caller's tenant accounts/regions (the
// same DataScope the PostgreSQL reads use); anomalies are the alerts of the
// identities the caller can see (the same identity keys the summary
// counts), so the two charts of the dashboard agree with the summary
// cards. The optional source_type/account_id/region filters narrow both
// series. A store that cannot be read contributes an empty series (logged),
// never an error.
type Service struct {
	lookup *common.MonitoredIdentityLookup
	repo   *Repository
	events *common.MonitoringEvents
}

// NewService wires the service. events may be nil (monitoring ClickHouse
// not configured): call_volume is then empty.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository, events *common.MonitoringEvents) *Service {
	return &Service{lookup: lookup, repo: repo, events: events}
}

// Params are the parsed query parameters.
type Params struct {
	Window     common.TimeWindow // [From, To); default last 14 days
	SourceType string            // optional compact family (EC2, ECS, LAMBDA, ...)
	AccountID  string            // optional exact account id
	Region     string            // optional exact region
}

// GetActivity builds the response.
func (s *Service) GetActivity(ctx context.Context, userID, tenantID, dbName string, p Params) (*Response, error) {
	sc, fleet, err := s.lookup.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	var ids []common.MonitoredIdentity
	for _, id := range fleet {
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
	keys := common.NewIdentityIndex(ids).LookupKeys()

	out := &Response{
		CallVolume: []CallVolumePoint{},
		Anomalies:  []AnomalyPoint{},
	}
	if days, err := s.repo.AnomaliesByDay(ctx, sc, keys, p.Window.From, p.Window.To); common.ReadOK("dashboard activity anomalies", err) {
		for _, d := range days {
			out.Anomalies = append(out.Anomalies, AnomalyPoint{Bucket: d.Day.Format("2006-01-02"), Count: d.Count})
		}
	}
	filter := common.FleetVolumeFilter{Accounts: sc.AccountIDs, Regions: sc.Regions, SourceType: p.SourceType, AccountID: p.AccountID, Region: p.Region}
	if points, err := s.events.FleetVolume(ctx, filter, p.Window.From, p.Window.To); common.EventsReadOK("dashboard activity call volume", err) {
		for _, pt := range points {
			out.CallVolume = append(out.CallVolume, CallVolumePoint{Bucket: pt.Bucket, Calls: pt.Calls})
		}
	}
	if err := common.BudgetExceeded(ctx, "dashboard activity"); err != nil {
		return nil, err
	}
	return out, nil
}
