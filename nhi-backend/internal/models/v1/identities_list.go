package v1

import "time"

// ─────────────────────────────────────────────────────────────────────────
// GET /api/v1/identities ("ENDPOINT 4: Identities List")
// ─────────────────────────────────────────────────────────────────────────

// IdentityListFilter holds the parsed & validated query parameters.
type IdentityListFilter struct {
	Page     int
	PageSize int

	Search            string
	IdentityTypes     []string // comma-separated
	FunctionalClasses []string // comma-separated
	Environments      []string // comma-separated
	Statuses          []string // comma-separated
	Owner             string
	VPCID             string

	IsPubliclyAccessible *bool
	MinIdleDays          *int

	SortBy    string
	SortOrder string
}

// IdentityListItem is a single row of the GET /api/v1/identities response.
type IdentityListItem struct {
	ID                       string     `json:"id"`
	Name                     string     `json:"name"`
	ARN                      string     `json:"arn"`
	IdentityType             string     `json:"identity_type"`
	FunctionalClass          *string    `json:"functional_class"`
	AccountID                string     `json:"account_id"`
	Region                   string     `json:"region"`
	Environment              *string    `json:"environment"`
	Owner                    *string    `json:"owner"`
	Status                   string     `json:"status"`
	IsPubliclyAccessible     bool       `json:"is_publicly_accessible"`
	VPCID                    *string    `json:"vpc_id"`
	BoundCredentialsCount    int        `json:"bound_credentials_count"`
	LastActivityAt           *time.Time `json:"last_activity_at"`
	CreatedAtAWS             *time.Time `json:"created_at_aws"`
	AgeDays                  *int       `json:"age_days"`
	IdleDays                 *int       `json:"idle_days"`
	CreatedBy                *string    `json:"created_by"`
	PermissionUtilizationPct *int       `json:"permission_utilization_pct"`
	// RecentChange is what the latest discovery scan did to this row:
	// "CREATED", "MODIFIED" (it or its permissions, bindings or trust), or null.
	RecentChange *string `json:"recent_change"`
}

// IdentityListPagination mirrors the spec's "pagination" block exactly.
type IdentityListPagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

// IdentityListResponse is the payload returned by GET /api/v1/identities.
type IdentityListResponse struct {
	Pagination IdentityListPagination `json:"pagination"`
	Data       []IdentityListItem     `json:"data"`
	// ActivityDataAvailable is false when CloudTrail data could not be read:
	// last_activity_at, idle_days, created_by and permission utilisation are
	// then unknown, not zero.
	ActivityDataAvailable bool `json:"activity_data_available"`
}
