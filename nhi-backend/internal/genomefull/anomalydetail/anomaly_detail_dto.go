// Package anomalydetail serves GET /api/v1/genome/anomalies/{alert_id}: the
// alerts row plus the log_events row of its event_id.
package anomalydetail

import (
	"encoding/json"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Response is the data block.
type Response struct {
	ID                   int64                     `json:"id"`
	Identity             common.AnomalyIdentityRef `json:"identity"`
	WhatChanged          WhatChanged               `json:"what_changed"`
	WhyThisIsGraded      *string                   `json:"why_this_is_graded"` // always null: no grading narrative is stored (NEEDS TO BE ADDED)
	ProvenanceConfidence *int                      `json:"provenance_confidence"`
	Severity             *string                   `json:"severity"`
	State                string                    `json:"state"`
	Detail               Context                   `json:"detail"`
	Evidence             Evidence                  `json:"evidence"`
}

// WhatChanged is the stored comparison: alerts.dimension with the
// baseline_value and observed_value JSON exactly as the pipeline wrote them.
type WhatChanged struct {
	Dimension string          `json:"dimension"`
	Baseline  json.RawMessage `json:"baseline"`
	Observed  json.RawMessage `json:"observed"`
}

// Context is the event context of the anomaly. Every field but
// detected_at (= alerts.created_at) comes from log_events and is null when
// the event row is not readable.
type Context struct {
	AccountID  *string   `json:"account_id"`
	Region     *string   `json:"region"`
	API        *string   `json:"api"`      // event_source without ".amazonaws.com" + ":" + event_name
	Resource   *string   `json:"resource"` // first entry of resource_arns
	SourceIP   *string   `json:"source_ip"`
	DetectedAt time.Time `json:"detected_at"`
}

// Evidence is the evidence summary. RiskComponents is the parsed
// log_events.risk_components_json (null when the event row is not readable).
type Evidence struct {
	EventID        string          `json:"event_id"`
	RequestID      *string         `json:"request_id"`
	BaselineValue  json.RawMessage `json:"baseline_value"`
	ObservedValue  json.RawMessage `json:"observed_value"`
	RiskComponents json.RawMessage `json:"risk_components"`
}
