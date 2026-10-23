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
	// ActiveCredentials is total_credentials minus stale_credentials.
	ActiveCredentials int `json:"active_credentials"`
	// TrustEdges counts active trust edges in the selected accounts;
	// CrossAccountTrustEdges those of edge_type cross_account.
	TrustEdges             int `json:"trust_edges"`
	CrossAccountTrustEdges int `json:"cross_account_trust_edges"`
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
	// TrustEdgesByType: active trust edges per edge_type, largest first.
	TrustEdgesByType []EdgeTypeCount `json:"trust_edges_by_type"`
	// EventVolume: CloudTrail events per UTC day over the last 30 days in the
	// selected accounts (empty when CloudTrail cannot be read).
	EventVolume []GrowthPoint `json:"event_volume"`
	// CredentialUtilization: credentials by last use, in the fixed bucket
	// order used_7d, used_30d, used_90d, idle_90d, never_used.
	CredentialUtilization []BucketCount `json:"credential_utilization"`
}

type EdgeTypeCount struct {
	EdgeType string `json:"edge_type"`
	Count    int    `json:"count"`
}

type BucketCount struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
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
	CredentialID   string    `json:"credential_id"`
	CredentialName string    `json:"credential_name"`
	ExpiresAt      time.Time `json:"expires_at"`
	DaysRemaining  int       `json:"days_remaining"`
}

// DashboardTrendPoint is one point of GET /dashboard/trends. Aggregated points
// (the default) carry no scan_id/account_id and say how many accounts they sum.
type DashboardTrendPoint struct {
	ScanID           string `json:"scan_id,omitempty"`
	AccountID        string `json:"account_id,omitempty"`
	ScanDate         string `json:"scan_date"`
	TotalIdentities  int    `json:"total_identities"`
	TotalCredentials int    `json:"total_credentials"`
	Accounts         int    `json:"accounts"`
}

type DashboardTrendsResponse struct {
	Trends []DashboardTrendPoint `json:"trends"`
}
