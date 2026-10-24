// Package identitylist serves GET /api/v1/genome/identities: the fleet
// table of every monitored identity in the caller's tenant scope.
package identitylist

import "time"

// Response is the data block: the page of fleet-table rows.
type Response struct {
	Items []Item `json:"items"`
}

// Item is one response row. Discovery attributes are normalized from the
// latest snapshot of the tenant's identities table; baseline, risk and
// open_anomaly_count come from the tenant's monitoring tables, last_seen
// from ClickHouse log_events. A section whose store cannot be read is null.
type Item struct {
	ID               string          `json:"id"` // source_type + ':' + identity_name
	SourceType       string          `json:"source_type"`
	NHIIdentityType  string          `json:"nhi_identity_type"`
	IdentityName     string          `json:"identity_name"`
	DisplayName      string          `json:"display_name"`
	WorkloadKey      string          `json:"workload_key"`
	Status           *string         `json:"status"` // raw identities.status (lifecycle values NEED CONFIRMATION)
	AccountID        string          `json:"account_id"`
	Region           *string         `json:"region"`
	ServiceName      *string         `json:"service_name"`
	RoleType         *string         `json:"role_type"`
	RoleARN          *string         `json:"role_arn"`
	Baseline         BaselineSummary `json:"baseline"`
	Risk             RiskSnapshot    `json:"risk"`
	LastSeen         *time.Time      `json:"last_seen"`
	OpenAnomalyCount *int            `json:"open_anomaly_count"`
	// ProvenanceConfidence is always null: the selection rule (latest or
	// average log_events.provenance_confidence) NEEDS CONFIRMATION.
	ProvenanceConfidence *float64 `json:"provenance_confidence"`
}

// BaselineSummary is the item's baseline block; all null when no baseline
// row exists for the identity or its workload key.
type BaselineSummary struct {
	Status       *string `json:"status"`
	BuiltDays    *int    `json:"built_days"`
	RequiredDays *int    `json:"required_days"`
}

// RiskSnapshot is the item's risk block; all null when the identity has not
// been scored yet.
type RiskSnapshot struct {
	Score    *int    `json:"score"`
	Severity *string `json:"severity"`
	Status   *string `json:"status"`
}
