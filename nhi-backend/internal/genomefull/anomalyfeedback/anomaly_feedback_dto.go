// Package anomalyfeedback serves
// POST /api/v1/genome/anomalies/{alert_id}/feedback: the only write of the
// genome API. It upserts one alert_feedback row keyed by the alert's
// (source_type, event_id); alerts.status is never changed by it.
package anomalyfeedback

import (
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Allowed values of alert_feedback.decision (schema guide §10).
const (
	DecisionLegitimate = "legitimate"
	DecisionMalicious  = "malicious"
	DecisionUnknown    = "unknown"
)

// Request is the request body. Decision is required; ApprovedForBaseline
// defaults to false and Reason to null. The reviewer is taken from the
// access token, never from the body.
type Request struct {
	Decision            string  `json:"decision"`
	ApprovedForBaseline *bool   `json:"approved_for_baseline"`
	Reason              *string `json:"reason"`
}

// Response is the data block returned after the upsert: the stored row in
// the same shape the evidence endpoint reports it.
type Response struct {
	AlertID   int64           `json:"alert_id"`
	Feedback  common.Feedback `json:"feedback"`
	UpdatedAt time.Time       `json:"updated_at"`
}
