package genome

import "time"

// Response shape of GET /api/v1/genome/identities/{source_type}/{identity_name}
// — the identity page header, in the §1.5 envelope (MonitoringEnvelope).
// Only what the stores actually populate: per-identity values that are absent
// (no baseline row yet, never seen, no role type) are omitted, not null.
// The contract's risk block is left out because identity_risk_scores never
// carries a score, and its links because they are fixed paths every client
// already builds.

// IdentitySummaryResponse is the data block.
type IdentitySummaryResponse struct {
	Identity        IdentityRecord           `json:"identity"`
	BaselineSummary *IdentityBaselineSummary `json:"baseline_summary,omitempty"`
	HeadlineMetrics IdentityHeadlineMetrics  `json:"headline_metrics"`
}

// IdentityRecord is the identity normalized from the latest snapshot of the
// tenant's identities table.
type IdentityRecord struct {
	SourceType      string `json:"source_type"`       // compact family, e.g. ECS
	NHIIdentityType string `json:"nhi_identity_type"` // registry subtype, e.g. ecs_task
	IdentityName    string `json:"identity_name"`     // canonical ARN or typed fallback key
	DisplayName     string `json:"display_name"`
	WorkloadKey     string `json:"workload_key"`
	Status          string `json:"status,omitempty"`
	AccountID       string `json:"account_id"`
	Region          string `json:"region,omitempty"`
	ServiceName     string `json:"service_name,omitempty"`
	RoleType        string `json:"role_type,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
}

// IdentityBaselineSummary is the baselines row of the identity (or of its
// shared workload key).
type IdentityBaselineSummary struct {
	Status       string `json:"status"`
	BuiltDays    int    `json:"built_days"`
	RequiredDays int    `json:"required_days"`
}

// IdentityHeadlineMetrics: calls_in_window and last_seen come from ClickHouse
// log_events (omitted when it is not configured or unavailable),
// open_anomalies from alerts.
type IdentityHeadlineMetrics struct {
	CallsInWindow *uint64    `json:"calls_in_window,omitempty"`
	OpenAnomalies *int       `json:"open_anomalies,omitempty"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
}
