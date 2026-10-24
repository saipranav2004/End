package anomalydetail

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements GET /api/v1/genome/anomalies/{alert_id}: one alerts
// row with the stored comparison (dimension, baseline_value,
// observed_value) and the context of its event from log_events (account,
// region, api, resource, source ip, risk components). The event-backed
// fields are null when the event row cannot be read; why_this_is_graded is
// always null because no grading narrative is stored anywhere (NEEDS TO BE
// ADDED).
type Service struct {
	lookup *common.MonitoredIdentityLookup
	repo   *Repository
	events *common.MonitoringEvents
}

// NewService wires the service. events may be nil.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository, events *common.MonitoringEvents) *Service {
	return &Service{lookup: lookup, repo: repo, events: events}
}

// GetDetail builds the response. Unknown ids are common.ErrAnomalyNotFound.
func (s *Service) GetDetail(ctx context.Context, userID, tenantID, dbName string, alertID int64) (*Response, error) {
	sc, fleet, err := s.lookup.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	a, err := s.repo.ByID(ctx, sc, alertID)
	if err != nil {
		return nil, &common.StoreUnavailableError{Op: "read alert", Err: err}
	}
	if a == nil {
		return nil, common.ErrAnomalyNotFound
	}
	ev := common.EventOf(common.AlertEvents(ctx, s.events, "anomaly detail event", []common.AlertRow{*a}), *a)

	out := &Response{
		ID:       a.ID,
		Identity: common.AnomalyIdentityRefFor(*a, common.NewIdentityIndex(fleet)),
		WhatChanged: WhatChanged{
			Dimension: a.Dimension,
			Baseline:  common.RawJSON(a.BaselineValue),
			Observed:  common.RawJSON(a.ObservedValue),
		},
		ProvenanceConfidence: common.AnomalyProvenance(ev),
		Severity:             common.AnomalySeverity(ev),
		State:                a.Status,
		Detail: Context{
			Region:     common.AnomalyRegion(*a, ev),
			DetectedAt: a.CreatedAt,
		},
		Evidence: Evidence{
			EventID:        a.EventID,
			RequestID:      a.RequestID,
			BaselineValue:  common.RawJSON(a.BaselineValue),
			ObservedValue:  common.RawJSON(a.ObservedValue),
			RiskComponents: common.RiskComponents(ev),
		},
	}
	if ev != nil {
		out.Detail.AccountID = common.OptionalString(ev.AccountID)
		out.Detail.API = common.OptionalString(common.APIOperation(ev.EventSource, ev.EventName))
		out.Detail.Resource = common.OptionalString(common.FirstResourceARN(ev.ResourceARNs))
		out.Detail.SourceIP = common.OptionalString(ev.SourceIP)
		if out.Evidence.RequestID == nil {
			out.Evidence.RequestID = common.OptionalString(ev.RequestID)
		}
	}
	if err := common.BudgetExceeded(ctx, "anomaly detail"); err != nil {
		return nil, err
	}
	return out, nil
}
