package v1

import "time"

// ─────────────────────────────────────────────────────────────────────────
// GET /api/v1/identities/:id?tab= ("ENDPOINT 5: Identity Detail")
// Six tabs: overview, permissions, activity, relationships, credentials, raw.
//
// Shared types already defined in credential_detail.go (same package) are
// reused wherever the shape matches exactly: RiskSignal, TrustPrincipal,
// AssumableRole, VolumePoint, EventsPagination, IPPct, RegionCnt,
// ServicePct, and the entire tab=relationships shape (RelationshipsResponse
// / RelationshipGraph / GraphNode / GraphEdge / RelationshipRow).
// ─────────────────────────────────────────────────────────────────────────

// EventPrincipal says who made a CloudTrail call, resolved to the Postgres
// inventory where possible. It is embedded in the event shapes, so its fields
// appear flat in the JSON. Unmatched callers keep the raw caller_arn and have
// null names; terminated marks identities/credentials deleted since discovery.
type EventPrincipal struct {
	CallerARN      string  `json:"caller_arn"`
	CallerType     *string `json:"caller_type"`
	CredentialID   *string `json:"credential_id"`
	CredentialName *string `json:"credential_name"`
	IdentityID     *string `json:"identity_id"`
	IdentityName   *string `json:"identity_name"`
	Terminated     bool    `json:"terminated"`
}

// IdentityDetailFilter holds the parsed & validated query parameters.
type IdentityDetailFilter struct {
	IdentityID string
	Tab        string // overview | permissions | activity | relationships | credentials | raw
	TimeRange  string // activity tab — default 90d, or "custom" with StartDate/EndDate
	StartDate  string // time_range=custom only (YYYY-MM-DD or RFC3339)
	EndDate    string // time_range=custom only (YYYY-MM-DD or RFC3339)
	Page       int    // activity tab — events page, default 1
	PageSize   int    // activity tab — events per page, default 50, max 100
	Service    string // activity/permissions tab — optional service filter
	UsedOnly   *bool  // permissions tab — optional
	Depth      int    // relationships tab — default 1
	EdgeType   string // relationships tab — optional comma-separated filter
}

// ── tab=overview ────────────────────────────────────────────────────────

type IdentityOverviewResponse struct {
	Tab              string                    `json:"tab"`
	Identity         IdentityOverviewCore      `json:"identity"`
	Discovery        IdentityDiscovery         `json:"discovery"`
	TypeAttributes   interface{}               `json:"type_attributes"`
	BoundCredentials []IdentityBoundCredential `json:"bound_credentials"`
	Tags             interface{}               `json:"tags"`
	MissingTags      []string                  `json:"missing_tags"`
	RiskSignals      []RiskSignal              `json:"risk_signals"`
}

type IdentityOverviewCore struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	ARN                  string      `json:"arn"`
	IdentityType         string      `json:"identity_type"`
	FunctionalClass      *string     `json:"functional_class"`
	Category             string      `json:"category"`
	AccountID            string      `json:"account_id"`
	Region               string      `json:"region"`
	VPCID                *string     `json:"vpc_id"`
	SubnetID             *string     `json:"subnet_id"`
	SecurityGroupIDs     interface{} `json:"security_group_ids"`
	Environment          *string     `json:"environment"`
	Owner                *string     `json:"owner"`
	Status               string      `json:"status"`
	IsPubliclyAccessible bool        `json:"is_publicly_accessible"`
	CreatedAtAWS         *time.Time  `json:"created_at_aws"`
	AgeDays              *int        `json:"age_days"`
	FirstSeenAt          *time.Time  `json:"first_seen_at"`
	LastSeenAt           *time.Time  `json:"last_seen_at"`
	LastActivityAt       *time.Time  `json:"last_activity_at"`
	IdleDays             *int        `json:"idle_days"`
	ParentARN            *string     `json:"parent_arn"`
	CreatedBy            *string     `json:"created_by"`
}

type IdentityDiscovery struct {
	DiscoverySource      *string `json:"discovery_source"`
	CredentialResolution string  `json:"credential_resolution"`
	DataSources          string  `json:"data_sources"`
}

type IdentityBoundCredential struct {
	CredentialID          string  `json:"credential_id"`
	Name                  string  `json:"name"`
	CredentialType        string  `json:"credential_type"`
	BindingType           string  `json:"binding_type"`
	IsPrimary             bool    `json:"is_primary"`
	TrustPrincipal        *string `json:"trust_principal"`
	AttachedPoliciesCount int     `json:"attached_policies_count"`
	InlinePoliciesCount   int     `json:"inline_policies_count"`
	GrantedActionsCount   int     `json:"granted_actions_count"`
	UsedActionsCount      int     `json:"used_actions_count"`
	UtilizationPct        *int    `json:"utilization_pct"`
}

// ── tab=permissions ─────────────────────────────────────────────────────

type IdentityPermissionsResponse struct {
	Tab             string                        `json:"tab"`
	Summary         IdentityPermissionsSummary    `json:"summary"`
	Permissions     []IdentityPermissionItem      `json:"permissions"`
	PolicyBreakdown []IdentityPolicyBreakdownItem `json:"policy_breakdown"`
}

type IdentityPermissionsSummary struct {
	GrantedCount   int `json:"granted_count"`
	UsedCount      int `json:"used_count"`
	UnusedCount    int `json:"unused_count"`
	UtilizationPct int `json:"utilization_pct"`
}

type IdentityPermissionItem struct {
	Service      string     `json:"service"`
	Action       string     `json:"action"`
	ResourceARN  string     `json:"resource_arn"`
	Effect       string     `json:"effect"`
	PolicySource string     `json:"policy_source"`
	PolicyType   string     `json:"policy_type"`
	Used         bool       `json:"used"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	SourceIPs    []string   `json:"source_ips"` // always empty — permission_usage has no source_ip column
}

type IdentityPolicyBreakdownItem struct {
	PolicyName       string `json:"policy_name"`
	PolicyType       string `json:"policy_type"`
	TotalPermissions int    `json:"total_permissions"`
	UsedPermissions  int    `json:"used_permissions"`
}

// ── tab=activity ────────────────────────────────────────────────────────

type IdentityActivityResponse struct {
	Tab              string                    `json:"tab"`
	TimeRange        string                    `json:"time_range"`
	VolumeChart      []VolumePoint             `json:"volume_chart"`
	RecentEvents     []IdentityRecentEvent     `json:"recent_events"`
	Analytics        IdentityActivityAnalytics `json:"analytics"`
	ErrorEvents      []IdentityErrorEvent      `json:"error_events"`
	EventsPagination EventsPagination          `json:"events_pagination"`
	// ActivityDataAvailable is false when the CloudTrail store could not be
	// read, so empty activity means "unknown" rather than "none".
	ActivityDataAvailable bool `json:"activity_data_available"`
}

type IdentityRecentEvent struct {
	EventPrincipal
	EventTime   *time.Time `json:"event_time"`
	EventName   string     `json:"event_name"`
	Service     string     `json:"service"`
	ResourceARN *string    `json:"resource_arn"`
	SourceIP    *string    `json:"source_ip"`
	Region      *string    `json:"region"`
	UserAgent   *string    `json:"user_agent"`
	ErrorCode   *string    `json:"error_code"`
	RequestID   *string    `json:"request_id"` // CloudTrail event ID
}

type IdentityActivityAnalytics struct {
	SourceIPs           []IPPct               `json:"source_ips"`
	ActiveRegions       []RegionCnt           `json:"active_regions"`
	TopServices         []ServicePct          `json:"top_services"`
	TopActions          []IdentityActionCount `json:"top_actions"`
	ErrorRatePct        *float64              `json:"error_rate_pct"`
	AccessDeniedRatePct *float64              `json:"access_denied_rate_pct"`
	PeakHours           []IdentityHourCount   `json:"peak_hours"`
	UsagePattern        string                `json:"usage_pattern"`
}

type IdentityActionCount struct {
	Action string `json:"action"`
	Count  int    `json:"count"`
}

type IdentityHourCount struct {
	Hour  int `json:"hour"`
	Count int `json:"count"`
}

type IdentityErrorEvent struct {
	EventPrincipal
	EventTime    *time.Time `json:"event_time"`
	EventName    string     `json:"event_name"`
	Service      string     `json:"service"`
	ResourceARN  *string    `json:"resource_arn"`
	SourceIP     *string    `json:"source_ip"`
	ErrorCode    *string    `json:"error_code"`
	ErrorMessage *string    `json:"error_message"`
}

// ── tab=relationships ───────────────────────────────────────────────────
// Reuses RelationshipsResponse and friends from credential_detail.go
// unchanged — identical shape in the spec for both endpoints.

// ── tab=credentials ─────────────────────────────────────────────────────

type IdentityCredentialsTabResponse struct {
	Tab         string                         `json:"tab"`
	Credentials []IdentityCredentialDetailItem `json:"credentials"`
}

type IdentityCredentialDetailItem struct {
	CredentialID     string                `json:"credential_id"`
	Name             string                `json:"name"`
	ARN              string                `json:"arn"`
	CredentialType   string                `json:"credential_type"`
	BindingType      string                `json:"binding_type"`
	BindingSource    *string               `json:"binding_source"`
	IsPrimary        bool                  `json:"is_primary"`
	TrustPolicy      interface{}           `json:"trust_policy"`
	TrustPrincipals  []TrustPrincipal      `json:"trust_principals"`
	AttachedPolicies interface{}           `json:"attached_policies"`
	InlinePolicies   interface{}           `json:"inline_policies"`
	Lifecycle        IdentityCredLifecycle `json:"lifecycle"`
	AssumableRoles   []AssumableRole       `json:"assumable_roles"`
}

type IdentityCredLifecycle struct {
	CreatedAtAWS          *time.Time `json:"created_at_aws"`
	LastUsedAt            *time.Time `json:"last_used_at"`
	LastRotatedAt         *time.Time `json:"last_rotated_at"`
	AgeDays               *int       `json:"age_days"`
	DaysSinceRotation     *int       `json:"days_since_rotation"`
	RotationEnabled       bool       `json:"rotation_enabled"`
	RotationIntervalDays  *int       `json:"rotation_interval_days"`
	RotationOverdue       bool       `json:"rotation_overdue"`
	ExpiresAt             *time.Time `json:"expires_at"`
	MaxSessionDuration    *int       `json:"max_session_duration"`
	PermissionBoundaryARN *string    `json:"permission_boundary_arn"`
}

// ── tab=raw ─────────────────────────────────────────────────────────────

type IdentityRawResponse struct {
	Tab                string                 `json:"tab"`
	IdentityRecord     map[string]interface{} `json:"identity_record"`
	RawAPIResponse     interface{}            `json:"raw_api_response"`
	ResolvedCredential interface{}            `json:"resolved_credential"`
	TypeAttributes     interface{}            `json:"type_attributes"`
}
