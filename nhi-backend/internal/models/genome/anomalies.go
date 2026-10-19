package genome

import "time"

// Anomaly list and reviewer feedback (backend-nhi-genome.md §6.1 and §6.4).

// Reviewer decisions stored in alert_feedback.decision (§6.4). The console
// shows legitimate as "False positive" and malicious as "Real anomaly".
const (
	FeedbackLegitimate = "legitimate"
	FeedbackMalicious  = "malicious"
	FeedbackUnknown    = "unknown"
)

// AnomalyListResponse is the data of GET /genome/anomalies.
type AnomalyListResponse struct {
	Items []AnomalyItem `json:"items"`
	// Counts covers every anomaly that matches the filters except review,
	// so the console can label its "Needs review / Reviewed" views.
	Counts AnomalyCounts `json:"counts"`
	// FeedbackAvailable is false when the tenant has no alert_feedback
	// table yet: anomalies are listed, feedback cannot be saved.
	FeedbackAvailable bool `json:"feedback_available"`
}

// AnomalyCounts are the review counts behind the list.
type AnomalyCounts struct {
	Total         int `json:"total"`
	NeedsReview   int `json:"needs_review"`
	Reviewed      int `json:"reviewed"`
	RealAnomaly   int `json:"real_anomaly"`
	FalsePositive int `json:"false_positive"`
}

// AnomalyItem is one §6.1 response item.
type AnomalyItem struct {
	ID          int64                `json:"id"`
	Identity    DashboardIdentityRef `json:"identity"`
	AccountID   string               `json:"account_id"`
	WhatChanged string               `json:"what_changed"`
	Dimension   string               `json:"dimension"`
	Observed    *string              `json:"observed"`
	Baseline    *string              `json:"baseline"`
	State       string               `json:"state"`
	DetectedAt  time.Time            `json:"detected_at"`
	Event       *AnomalyEvent        `json:"event"` // null when the event store cannot be read
	Feedback    *AnomalyFeedback     `json:"feedback"`
	CanReview   bool                 `json:"can_review"` // false when the alert has no event_id to key feedback on
}

// AnomalyEvent is the ClickHouse event the alert was raised on.
type AnomalyEvent struct {
	EventTime   *time.Time `json:"event_time"`
	EventName   string     `json:"event_name"`
	EventSource string     `json:"event_source"`
	Region      string     `json:"region"`
	SourceIP    string     `json:"source_ip"`
	RiskScore   *int       `json:"risk_score"`
}

// AnomalyFeedback is the current alert_feedback row.
type AnomalyFeedback struct {
	Decision            string    `json:"decision"`
	ApprovedForBaseline bool      `json:"approved_for_baseline"`
	Reviewer            string    `json:"reviewer"`
	Reason              *string   `json:"reason"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// AnomalyFeedbackRequest is the §6.4 request body.
type AnomalyFeedbackRequest struct {
	Decision            string  `json:"decision"`
	ApprovedForBaseline *bool   `json:"approved_for_baseline"`
	Reason              *string `json:"reason"`
}
