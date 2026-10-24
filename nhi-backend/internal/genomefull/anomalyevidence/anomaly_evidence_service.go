package anomalyevidence

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements GET /api/v1/genome/anomalies/{alert_id}/evidence: the
// log_events row of the alert's event, the stored comparison, the
// event-level risk result and the reviewer feedback (alert_feedback), if
// any.
//
// log_events.request_parameters is never read or returned: its redaction
// policy is unconfirmed, so include_request_parameters=true only adds an
// explicit "unavailable" marker. include_raw_risk=true adds the raw
// risk_reasons_json / risk_components_json strings next to the parsed
// arrays.
type Service struct {
	lookup *common.MonitoredIdentityLookup
	repo   *Repository
	events *common.MonitoringEvents
}

// NewService wires the service. events may be nil: event and risk are
// then null.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository, events *common.MonitoringEvents) *Service {
	return &Service{lookup: lookup, repo: repo, events: events}
}

// Params are the parsed query parameters (both default false).
type Params struct {
	IncludeRequestParameters bool
	IncludeRawRisk           bool
}

// GetEvidence builds the response. Unknown ids are common.ErrAnomalyNotFound.
func (s *Service) GetEvidence(ctx context.Context, userID, tenantID, dbName string, alertID int64, p Params) (*Response, error) {
	sc, _, err := s.lookup.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	a, err := s.repo.Alert(ctx, sc, alertID)
	if err != nil {
		return nil, &common.StoreUnavailableError{Op: "read alert", Err: err}
	}
	if a == nil {
		return nil, common.ErrAnomalyNotFound
	}
	ev := common.EventOf(common.AlertEvents(ctx, s.events, "anomaly evidence event", []common.AlertRow{*a}), *a)

	out := &Response{
		AlertID: a.ID,
		Comparison: Comparison{
			Dimension:     a.Dimension,
			Reason:        a.Reason,
			BaselineValue: common.RawJSON(a.BaselineValue),
			ObservedValue: common.RawJSON(a.ObservedValue),
			Score01:       a.Score,
		},
	}
	if ev != nil {
		out.Event = &Event{
			EventID:      ev.EventID,
			EventTime:    ev.EventTime,
			EventSource:  ev.EventSource,
			EventName:    ev.EventName,
			RequestID:    ev.RequestID,
			AccountID:    ev.AccountID,
			Region:       ev.Region,
			SourceIP:     ev.SourceIP,
			UserAgent:    ev.UserAgent,
			ResourceARNs: common.SplitResourceARNs(ev.ResourceARNs),
		}
		if out.Event.ResourceARNs == nil {
			out.Event.ResourceARNs = []string{}
		}
		out.Risk = &Risk{
			Status:     ev.RiskStatus,
			Score:      common.EventRiskScoreOf(ev),
			Severity:   ev.EventRiskSeverity,
			Reasons:    common.RiskReasons(ev, a.Reason),
			Components: common.RiskComponents(ev),
		}
		if p.IncludeRawRisk {
			reasons, components := ev.RiskReasonsJSON, ev.RiskComponentsJSON
			out.Risk.RawReasonsJSON, out.Risk.RawComponentsJSON = &reasons, &components
		}
	}
	if accountID := common.AnomalyAccountID(*a, nil); accountID != nil {
		if fb, err := s.repo.Feedback(ctx, sc, *accountID, a.Key.SourceType, a.EventID); common.ReadOK("anomaly evidence feedback", err) && fb != nil {
			out.Feedback = &common.Feedback{
				Decision:            fb.Decision,
				Reviewer:            fb.Reviewer,
				Reason:              fb.Reason,
				ApprovedForBaseline: fb.ApprovedForBaseline,
			}
		}
	}
	if p.IncludeRequestParameters {
		out.RequestParameters = &Unavailable{
			Available: false,
			Reason:    "NEEDS CONFIRMATION: request_parameters is not returned until its redaction policy is confirmed",
		}
	}
	if err := common.BudgetExceeded(ctx, "anomaly evidence"); err != nil {
		return nil, err
	}
	return out, nil
}
