package v1

import "time"

// ─────────────────────────────────────────────────────────────────────────
// GET /api/v1/dashboard/overview ("ENDPOINT 3: Dashboard Overview")
// No query params — all KPIs + all charts in one response.
// ─────────────────────────────────────────────────────────────────────────

type DashboardOverviewResponse struct {
	KPIs   DashboardKPIs   `json:"kpis"`
	Charts DashboardCharts `json:"charts"`
	// ActivityDataAvailable is false when CloudTrail data could not be read;
	// stale counts then fall back to creation dates only.
	ActivityDataAvailable bool `json:"activity_data_available"`
	// RecentChanges is what the latest discovery scan changed, within the
	// caller's scope; null before the first scan.
	RecentChanges *RecentChanges `json:"recent_changes"`
}

type DashboardKPIs struct {
	TotalIdentities          int     `json:"total_identities"`
	TotalCredentials         int     `json:"total_credentials"`
	OrphanedCredentials      int     `json:"orphaned_credentials"`
	StaleIdentities          int     `json:"stale_identities"`
	StaleCredentials         int     `json:"stale_credentials"`
	PubliclyAccessible       int     `json:"publicly_accessible"`
	ShadowIdentities         int     `json:"shadow_identities"`
	AccountsScanned          int     `json:"accounts_scanned"`
	PermissionUtilizationPct float64 `json:"permission_utilization_pct"`
}

type DashboardCharts struct {
	IdentitiesByType            []TypeCountFilter        `json:"identities_by_type"`
	IdentitiesByFunctionalClass []ClassCountFilter       `json:"identities_by_functional_class"`
	IdentitiesByAccount         []AccountCountItem       `json:"identities_by_account"`
	IdentitiesByRegion          []RegionFilter           `json:"identities_by_region"`
	IdentitiesByEnvironment     []EnvironmentCountFilter `json:"identities_by_environment"`
	IdentitiesByStatus          []StatusCountFilter      `json:"identities_by_status"`
	IdentityGrowth              []GrowthPoint            `json:"identity_growth"`
	CredentialGrowth            []GrowthPoint            `json:"credential_growth"`
	StaleVsActive               StaleVsActive            `json:"stale_vs_active"`
	CredentialTypes             []TypeCountFilter        `json:"credential_types"`
	TopOwners                   []OwnerCountItem         `json:"top_owners"`
	CredentialsExpiringSoon     []ExpiringCredentialItem `json:"credentials_expiring_soon"`
}

type AccountCountItem struct {
	AccountID string `json:"account_id"`
	Count     int    `json:"count"`
}

type GrowthPoint struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type StaleVsActive struct {
	Active int `json:"active"`
	Stale  int `json:"stale"`
}

type OwnerCountItem struct {
	Owner string `json:"owner"`
	Count int    `json:"count"`
}

type ExpiringCredentialItem struct {
	CredentialName string    `json:"credential_name"`
	ExpiresAt      time.Time `json:"expires_at"`
	DaysRemaining  int       `json:"days_remaining"`
}
