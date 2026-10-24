// Package anomalyevidence serves
// GET /api/v1/genome/anomalies/{alert_id}/evidence: the event, the stored
// comparison, the event-level risk result and the reviewer feedback, if
// any. log_events.request_parameters is never returned (redaction policy
// NEEDS CONFIRMATION).
package anomalyevidence

import (
	"encoding/json"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Response is the data block.
type Response struct {
	AlertID    int64            `json:"alert_id"`
	Event      *Event           `json:"event"` // null when the log_events row is not readable
	Comparison Comparison       `json:"comparison"`
	Risk       *Risk            `json:"risk"`     // null when the log_events row is not readable
	Feedback   *common.Feedback `json:"feedback"` // null when no alert_feedback row exists
	// RequestParameters is present only when include_request_parameters=true
	// was requested; it reports that the field is unavailable.
	RequestParameters *Unavailable `json:"request_parameters,omitempty"`
}

// Event is the log_events row of alerts.event_id.
type Event struct {
	EventID      string    `json:"event_id"`
	EventTime    time.Time `json:"event_time"`
	EventSource  string    `json:"event_source"`
	EventName    string    `json:"event_name"`
	RequestID    string    `json:"request_id"`
	AccountID    string    `json:"account_id"`
	Region       string    `json:"region"`
	SourceIP     string    `json:"source_ip"`
	UserAgent    string    `json:"user_agent"`
	ResourceARNs []string  `json:"resource_arns"`
}

// Comparison is the alerts comparison: dimension, reason, baseline_value,
// observed_value and the 0–1 score.
type Comparison struct {
	Dimension     string          `json:"dimension"`
	Reason        string          `json:"reason"`
	BaselineValue json.RawMessage `json:"baseline_value"`
	ObservedValue json.RawMessage `json:"observed_value"`
	Score01       float64         `json:"score_0_1"`
}

// Risk is the event-level risk result stored on log_events. Reasons and
// Components are the parsed JSON arrays; the raw strings are added only
// when include_raw_risk=true.
type Risk struct {
	Status            string          `json:"status"`
	Score             *int            `json:"score"`
	Severity          string          `json:"severity"`
	Reasons           json.RawMessage `json:"reasons"`
	Components        json.RawMessage `json:"components"`
	RawReasonsJSON    *string         `json:"raw_reasons_json,omitempty"`
	RawComponentsJSON *string         `json:"raw_components_json,omitempty"`
}

// Unavailable marks an evidence field that cannot be served.
type Unavailable struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}
