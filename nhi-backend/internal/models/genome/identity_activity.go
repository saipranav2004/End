package genome

import "time"

// Response shapes of GET /api/v1/identities/{source_type}/{identity_name}/activity
// — exactly backend_api_contract.md §5.3 / §10.3, in the §1.5 envelope
// (MonitoringEnvelope; the recent-calls page cursor travels in meta.next_cursor).

// IdentityActivityResponse is the data block of §5.3.
type IdentityActivityResponse struct {
	Volume      []IdentityVolumePoint `json:"volume"`
	RecentCalls []IdentityRecentCall  `json:"recent_calls"`
	TotalCalls  *uint64               `json:"total_calls"` // uniqExact(event_id) in the window; null when ClickHouse is unavailable
}

// IdentityVolumePoint is one call-volume bucket: "2026-10-01" for bucket=day,
// "2026-10-01T14:00:00Z" for bucket=hour.
type IdentityVolumePoint struct {
	Bucket string `json:"bucket"`
	Calls  uint64 `json:"calls"`
}

// IdentityRecentCall is one row of the recent-calls table (§5.3 columns only).
type IdentityRecentCall struct {
	EventID    string    `json:"event_id"`
	EventTime  time.Time `json:"event_time"`
	APICall    string    `json:"api_call"` // event_source + ':' + event_name (".amazonaws.com" suffix removed)
	Resource   string    `json:"resource"` // first ARN of the split log_events.resource_arns ("" when the event has none)
	SourceIP   string    `json:"source_ip"`
	Region     string    `json:"region"`
	RiskStatus string    `json:"risk_status"`
	RiskScore  *uint8    `json:"risk_score"` // log_events.event_risk_score; null when the event has no numeric score
}
