package genome

import "time"

// Response shapes of GET /api/v1/identities (backend_api_contract.md §5.1
// with the field sources of §10.2) in the §1.5 envelope
// (MonitoringEnvelope); the page cursor travels in meta.next_cursor.

// IdentityListResponse is the data block of §5.1: the page of fleet-table
// rows (the contract's list endpoints wrap their rows in "items").
type IdentityListResponse struct {
	Items []IdentityListItem `json:"items"`
}

// IdentityListItem is the §5.1 response item. Discovery attributes are
// normalized from the latest snapshot of the tenant's identities table; baseline, risk
// and open_anomaly_count come from the tenant's monitoring tables, last_seen
// from ClickHouse log_events. A section whose store cannot be read is null.
type IdentityListItem struct {
	ID               string                  `json:"id"` // source_type + ':' + identity_name
	SourceType       string                  `json:"source_type"`
	IdentityName     string                  `json:"identity_name"`
	DisplayName      string                  `json:"display_name"`
	WorkloadKey      string                  `json:"workload_key"`
	Status           *string                 `json:"status"` // raw identities.status (lifecycle values NEED CONFIRMATION)
	AccountID        string                  `json:"account_id"`
	Region           *string                 `json:"region"`
	ServiceName      *string                 `json:"service_name"`
	RoleType         *string                 `json:"role_type"`
	RoleARN          *string                 `json:"role_arn"`
	Baseline         IdentityBaselineSummary `json:"baseline"`
	Risk             IdentityRiskSnapshot    `json:"risk"`
	LastSeen         *time.Time              `json:"last_seen"`
	OpenAnomalyCount *int                    `json:"open_anomaly_count"`
	// ProvenanceConfidence is always null: §10.2 leaves the selection rule
	// (latest or average log_events.provenance_confidence) NEEDS CONFIRMATION.
	ProvenanceConfidence *float64 `json:"provenance_confidence"`
}
