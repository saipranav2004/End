package v1

import "time"

// CredentialListFilter holds the parsed & validated query parameters for
// GET /api/v1/credentials ("ENDPOINT 6: Credentials List" in the AWS-NHI
// Updated API spec). It is built by the handler and consumed by the
// repository layer.
type CredentialListFilter struct {
	Page     int
	PageSize int

	Search            string
	CredentialTypes   []string // parsed from a comma-separated query value
	Statuses          []string // parsed from a comma-separated query value
	OwnerPrincipalARN string

	RotationEnabled  *bool
	RotationOverdue  *bool
	IsOrphaned       *bool
	HasExternalTrust *bool

	MinIdleDays        *int
	MaxDaysUntilExpiry *int

	SortBy    string
	SortOrder string
}

// CredentialListItem is a single row of the GET /api/v1/credentials response.
type CredentialListItem struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	ARN                   string     `json:"arn"`
	CredentialType        string     `json:"credential_type"`
	AccountID             string     `json:"account_id"`
	Region                string     `json:"region"`
	Status                string     `json:"status"`
	OwnerPrincipalARN     *string    `json:"owner_principal_arn"`
	BoundIdentitiesCount  int        `json:"bound_identities_count"`
	IsOrphaned            bool       `json:"is_orphaned"`
	LastUsedAt            *time.Time `json:"last_used_at"`
	LastRotatedAt         *time.Time `json:"last_rotated_at"`
	RotationEnabled       bool       `json:"rotation_enabled"`
	RotationOverdue       bool       `json:"rotation_overdue"`
	AgeDays               *int       `json:"age_days"`
	IdleDays              *int       `json:"idle_days"`
	ExpiresAt             *time.Time `json:"expires_at"`
	DaysUntilExpiry       *int       `json:"days_until_expiry"`
	HasExternalTrust      bool       `json:"has_external_trust"`
	PermissionBoundaryARN *string    `json:"permission_boundary_arn"`
	// RecentChange is what the latest discovery scan did to this row:
	// "CREATED", "MODIFIED" (it or its permissions, bindings or trust), or null.
	RecentChange *string `json:"recent_change"`
}

// CredentialListPagination mirrors the spec's "pagination" block exactly.
type CredentialListPagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

// CredentialListResponse is the payload returned by GET /api/v1/credentials.
type CredentialListResponse struct {
	Pagination CredentialListPagination `json:"pagination"`
	Data       []CredentialListItem     `json:"data"`
	// ActivityDataAvailable is false when CloudTrail data could not be read:
	// last_used_at and idle_days are then unknown, not zero.
	ActivityDataAvailable bool `json:"activity_data_available"`
}
