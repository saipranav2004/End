package genome

import "time"

// Response shapes of GET /api/v1/identities/{source_type}/{identity_name}
// — exactly the sections of backend_api_contract.md §5.2 with the field
// sources of §10.2, in the §1.5 envelope (MonitoringEnvelope).

// MonitoringCodeIdentityNotFound is the §1.5 error code (HTTP 404) for an
// unknown {source_type}/{identity_name} inside the caller's scope.
const MonitoringCodeIdentityNotFound = "IDENTITY_NOT_FOUND"

// IdentitySummaryResponse is the data block of §5.2.
type IdentitySummaryResponse struct {
	Identity        IdentityRecord          `json:"identity"`
	Risk            IdentityRiskSnapshot    `json:"risk"`
	BaselineSummary IdentityBaselineSummary `json:"baseline_summary"`
	HeadlineMetrics IdentityHeadlineMetrics `json:"headline_metrics"`
	Links           IdentityLinks           `json:"links"`
}

// IdentityRecord is the §10.2 identity block, normalized from the latest
// snapshot of the tenant's identities table. Optional attributes are null when the
// discovery row does not carry them.
type IdentityRecord struct {
	ID              string  `json:"id"`                // source_type + ':' + identity_name
	SourceType      string  `json:"source_type"`       // compact family, e.g. ECS
	NHIIdentityType string  `json:"nhi_identity_type"` // registry subtype, e.g. ecs_task
	IdentityName    string  `json:"identity_name"`     // canonical ARN or typed fallback key
	DisplayName     string  `json:"display_name"`
	WorkloadKey     string  `json:"workload_key"`
	Status          *string `json:"status"` // raw identities.status (lifecycle values NEED CONFIRMATION)
	AccountID       string  `json:"account_id"`
	Region          *string `json:"region"`
	ServiceName     *string `json:"service_name"`
	RoleType        *string `json:"role_type"`
	RoleARN         *string `json:"role_arn"`
}

// IdentityRiskSnapshot is §5.2 risk: the latest identity_risk_scores
// snapshot; all null when the identity has not been scored yet.
type IdentityRiskSnapshot struct {
	Score    *int    `json:"score"`
	Severity *string `json:"severity"`
	Status   *string `json:"status"` // identity_risk_scores.score_status
}

// IdentityBaselineSummary is §5.2 baseline_summary from baselines; all null
// when no baseline row exists for the identity or its workload key.
type IdentityBaselineSummary struct {
	Status       *string `json:"status"`
	BuiltDays    *int    `json:"built_days"`
	RequiredDays *int    `json:"required_days"`
}

// IdentityHeadlineMetrics is §5.2 headline_metrics. calls_in_window and
// last_seen come from ClickHouse log_events and are null when it is not
// configured or unavailable; open_anomalies comes from alerts.
type IdentityHeadlineMetrics struct {
	CallsInWindow *uint64    `json:"calls_in_window"`
	OpenAnomalies *int       `json:"open_anomalies"`
	LastSeen      *time.Time `json:"last_seen"`
}

// IdentityLinks are the §5.2 relative paths of the identity's sub-resources.
type IdentityLinks struct {
	Activity string `json:"activity"`
	Genome   string `json:"genome"`
	Baseline string `json:"baseline"`
	Timeline string `json:"timeline"`
}
