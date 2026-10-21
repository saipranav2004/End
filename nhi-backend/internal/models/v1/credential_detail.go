package v1

import "time"

// ─────────────────────────────────────────────────────────────────────────
// GET /api/v1/credentials/:id?tab= ("ENDPOINT 7: Credential Detail")
// Six tabs: overview, trust, activity, rotation, relationships, raw.
// ─────────────────────────────────────────────────────────────────────────

// DetailFilter holds the parsed & validated query parameters.
type DetailFilter struct {
	CredentialID string
	Tab          string // overview | trust | activity | rotation | relationships | raw
	TimeRange    string // activity tab, e.g. "7d","30d","90d" — default 90d, or "custom"
	StartDate    string // time_range=custom only (YYYY-MM-DD or RFC3339)
	EndDate      string // time_range=custom only (YYYY-MM-DD or RFC3339)
	Page         int    // activity tab — events page, default 1
	PageSize     int    // activity tab — events per page, default 50, max 100
	Depth        int    // relationships tab — default 1
	EdgeType     string // relationships tab — optional comma-separated filter
}

// ── tab=overview ────────────────────────────────────────────────────────

type OverviewResponse struct {
	Tab             string             `json:"tab"`
	Credential      OverviewCredential `json:"credential"`
	BoundIdentities []BoundIdentity    `json:"bound_identities"`
	RiskSignals     []RiskSignal       `json:"risk_signals"`
	TypeAttributes  interface{}        `json:"type_attributes"`
}

type OverviewCredential struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	ARN                   string     `json:"arn"`
	CredentialType        string     `json:"credential_type"`
	Category              string     `json:"category"`
	AccountID             string     `json:"account_id"`
	Region                string     `json:"region"`
	Status                string     `json:"status"`
	OwnerPrincipalARN     *string    `json:"owner_principal_arn"`
	CreatedAtAWS          *time.Time `json:"created_at_aws"`
	AgeDays               *int       `json:"age_days"`
	LastUsedAt            *time.Time `json:"last_used_at"`
	IdleDays              *int       `json:"idle_days"`
	LastRotatedAt         *time.Time `json:"last_rotated_at"`
	DaysSinceRotation     *int       `json:"days_since_rotation"`
	RotationEnabled       bool       `json:"rotation_enabled"`
	RotationIntervalDays  *int       `json:"rotation_interval_days"`
	RotationStatus        string     `json:"rotation_status"`
	ExpiresAt             *time.Time `json:"expires_at"`
	DaysUntilExpiry       *int       `json:"days_until_expiry"`
	MaxSessionDuration    *int       `json:"max_session_duration"`
	PermissionBoundaryARN *string    `json:"permission_boundary_arn"`
	// The list's flags, so a credential opened by link shows them too.
	RotationOverdue  bool `json:"rotation_overdue"`
	IsOrphaned       bool `json:"is_orphaned"`        // bound to no identity
	HasExternalTrust bool `json:"has_external_trust"` // trust policy admits another account
}

type BoundIdentity struct {
	IdentityID     string     `json:"identity_id"`
	Name           string     `json:"name"`
	IdentityType   string     `json:"identity_type"`
	AccountID      string     `json:"account_id"`
	Status         string     `json:"status"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	BindingType    string     `json:"binding_type"`
}

type RiskSignal struct {
	Signal   string `json:"signal"`
	Status   bool   `json:"status"`
	Severity string `json:"severity"`
}

// ── tab=trust ───────────────────────────────────────────────────────────

type TrustResponse struct {
	Tab                         string                 `json:"tab"`
	TrustPolicy                 TrustPolicyBlock       `json:"trust_policy"`
	AttachedPolicies            interface{}            `json:"attached_policies"`
	InlinePolicies              interface{}            `json:"inline_policies"`
	EffectivePermissionsSummary []EffectivePermSummary `json:"effective_permissions_summary"`
	AssumableRoles              []AssumableRole        `json:"assumable_roles"`
	PermissionBoundary          PermissionBoundary     `json:"permission_boundary"`
}

type TrustPolicyBlock struct {
	Raw        interface{}      `json:"raw"`
	Principals []TrustPrincipal `json:"principals"`
	Warnings   []string         `json:"warnings"`
}

type TrustPrincipal struct {
	Principal  string  `json:"principal"`
	Type       string  `json:"type"`
	Condition  *string `json:"condition"`
	IsExternal bool    `json:"is_external"`
}

type EffectivePermSummary struct {
	Service        string `json:"service"`
	AllowedActions int    `json:"allowed_actions"`
	DeniedActions  int    `json:"denied_actions"`
	ResourceScope  string `json:"resource_scope"`
}

type AssumableRole struct {
	TargetRoleARN string     `json:"target_role_arn"`
	TargetAccount string     `json:"target_account"`
	Conditions    *string    `json:"conditions"`
	LastAssumed   *time.Time `json:"last_assumed"`
}

type PermissionBoundary struct {
	BoundaryARN          *string  `json:"boundary_arn"`
	EffectiveRestriction *string  `json:"effective_restriction"`
	ActionsBlocked       []string `json:"actions_blocked"`
}

// ── tab=activity ────────────────────────────────────────────────────────

type ActivityResponse struct {
	Tab                           string               `json:"tab"`
	TimeRange                     string               `json:"time_range"`
	VolumeChart                   []VolumePoint        `json:"volume_chart"`
	RecentEvents                  []RecentEvent        `json:"recent_events"`
	IdentitiesUsingThisCredential []UsingIdentity      `json:"identities_using_this_credential"`
	Analytics                     ActivityAnalytics    `json:"analytics"`
	ErrorEvents                   []IdentityErrorEvent `json:"error_events"`
	EventsPagination              EventsPagination     `json:"events_pagination"`
	// ActivityDataAvailable is false when the CloudTrail store could not be
	// read, so empty activity means "unknown" rather than "none".
	ActivityDataAvailable bool `json:"activity_data_available"`
}

type VolumePoint struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type RecentEvent struct {
	EventPrincipal
	EventTime   *time.Time `json:"event_time"`
	EventName   string     `json:"event_name"`
	Service     string     `json:"service"`
	ResourceARN *string    `json:"resource_arn"`
	SourceIP    *string    `json:"source_ip"`
	Region      *string    `json:"region"`
	UserAgent   *string    `json:"user_agent"`
	ErrorCode   *string    `json:"error_code"`
	EventCount  int        `json:"event_count"`
	RequestID   *string    `json:"request_id"`
}

type UsingIdentity struct {
	IdentityID     string     `json:"identity_id"`
	Name           string     `json:"name"`
	IdentityType   string     `json:"identity_type"`
	SourceIP       *string    `json:"source_ip"`
	LastAction     *string    `json:"last_action"`
	LastActionTime *time.Time `json:"last_action_time"`
	// CallerARN is the raw CloudTrail caller; set so unmatched callers can be
	// shown by ARN. Terminated marks identities deleted since the last discovery.
	CallerARN  string `json:"caller_arn"`
	Terminated bool   `json:"terminated"`
}

type ActivityAnalytics struct {
	SourceIPs     []IPPct      `json:"source_ips"`
	ActiveRegions []RegionCnt  `json:"active_regions"`
	TopServices   []ServicePct `json:"top_services"`
	UsagePattern  string       `json:"usage_pattern"`
	ErrorRatePct  *float64     `json:"error_rate_pct"`
	// Added with the ClickHouse activity source (same shape as the identity tab).
	TopActions          []IdentityActionCount `json:"top_actions"`
	AccessDeniedRatePct *float64              `json:"access_denied_rate_pct"`
	PeakHours           []IdentityHourCount   `json:"peak_hours"`
}

type IPPct struct {
	IP  string  `json:"ip"`
	Pct float64 `json:"pct"`
}

type RegionCnt struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

type ServicePct struct {
	Service string  `json:"service"`
	Pct     float64 `json:"pct"`
	Count   int     `json:"count"`
}

type EventsPagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

// ── tab=rotation ────────────────────────────────────────────────────────

type RotationResponse struct {
	Tab           string            `json:"tab"`
	Timeline      []RotationEvent   `json:"timeline"`
	Configuration RotationConfig    `json:"configuration"`
	Lifecycle     RotationLifecycle `json:"lifecycle"`
}

type RotationEvent struct {
	Date    time.Time `json:"date"`
	Event   string    `json:"event"`
	Details string    `json:"details"`
}

type RotationConfig struct {
	AutoRotation         bool       `json:"auto_rotation"`
	RotationIntervalDays *int       `json:"rotation_interval_days"`
	LastRotation         *time.Time `json:"last_rotation"`
	NextExpected         *time.Time `json:"next_expected"`
	Status               string     `json:"status"`
	RotationLambdaARN    *string    `json:"rotation_lambda_arn"` // not tracked in this schema — always null
}

type RotationLifecycle struct {
	CreatedAtAWS      *time.Time `json:"created_at_aws"`
	AgeDays           *int       `json:"age_days"`
	DaysSinceRotation *int       `json:"days_since_rotation"`
	ExpiresAt         *time.Time `json:"expires_at"`
	DaysUntilExpiry   *int       `json:"days_until_expiry"`
}

// ── tab=relationships ───────────────────────────────────────────────────

type RelationshipsResponse struct {
	Tab               string            `json:"tab"`
	Graph             RelationshipGraph `json:"graph"`
	RelationshipTable []RelationshipRow `json:"relationship_table"`
	// ActivityDataAvailable is false when CloudTrail could not be read, so the
	// "accesses" and "created_by" edges may be missing - not "no calls".
	ActivityDataAvailable bool `json:"activity_data_available"`
}

type RelationshipGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ARN       string `json:"arn"`
	Name      string `json:"name"`
	Type      string `json:"type"` // credential | identity | resource | unknown
	Subtype   string `json:"subtype"`
	IsCurrent bool   `json:"is_current"`
}

type GraphEdge struct {
	Source         string     `json:"source"`
	Target         string     `json:"target"`
	EdgeType       string     `json:"edge_type"`
	LastObservedAt *time.Time `json:"last_observed_at"`
	// CallCount is the number of successful calls behind a CloudTrail-derived
	// "accesses" edge in the requested window; null for Postgres edges.
	CallCount *int `json:"call_count"`
}

type RelationshipRow struct {
	Relationship string     `json:"relationship"`
	Direction    string     `json:"direction"` // inbound | outbound
	TargetARN    string     `json:"target_arn"`
	TargetName   string     `json:"target_name"`
	TargetType   string     `json:"target_type"`
	LastActive   *time.Time `json:"last_active"`
	CallCount    *int       `json:"call_count"` // accesses edges only
}

// ── tab=raw ─────────────────────────────────────────────────────────────

type RawResponse struct {
	Tab                  string                 `json:"tab"`
	CredentialRecord     map[string]interface{} `json:"credential_record"`
	RawAPIResponse       interface{}            `json:"raw_api_response"`
	TrustPolicy          interface{}            `json:"trust_policy"`
	AttachedPolicies     interface{}            `json:"attached_policies"`
	InlinePolicies       interface{}            `json:"inline_policies"`
	TypeAttributes       interface{}            `json:"type_attributes"`
	EffectivePermissions interface{}            `json:"effective_permissions"`
}
