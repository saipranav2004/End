package v1

// ─────────────────────────────────────────────────────────────────────────
// GET /api/v1/identities/{id}/events — "Identity Event Graph".
//
// One endpoint, nine sections selected by ?section=. Every response carries
// "section", and data_available=false when ClickHouse (the event store) is
// disabled or failed, in which case the section is returned empty rather than
// as an error. Timestamps are UTC, formatted like "2026-10-10T14:23:01Z".
// ─────────────────────────────────────────────────────────────────────────

// IdentityEventsSection names, as accepted by ?section=.
const (
	EventsSectionTimeline     = "timeline"
	EventsSectionDetail       = "detail"
	EventsSectionCredentials  = "credentials"
	EventsSectionResources    = "resources"
	EventsSectionHeatmap      = "heatmap"
	EventsSectionSessions     = "sessions"
	EventsSectionErrors       = "errors"
	EventsSectionCrossAccount = "cross_account"
	EventsSectionIPs          = "ips"
)

// IdentityEventsQuery is the validated request.
type IdentityEventsQuery struct {
	IdentityID string
	Section    string
	TimeRange  string
	StartDate  string
	EndDate    string

	Service    string
	Action     string
	Result     string // success | error | all
	Resource   string
	SourceIP   string
	Credential string
	ReadWrite  string // read | write | all

	Page     int
	PageSize int
	EventID  string
}

type IdentityEventsIdentity struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	IdentityType string `json:"identity_type"`
	AccountID    string `json:"account_id"`
}

type IdentityEventsPagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

// IdentityEventCore holds the fields shared by timeline rows and the detail
// panel.
type IdentityEventCore struct {
	EventID          string `json:"event_id"`
	EventTime        string `json:"event_time"`
	EventName        string `json:"event_name"`
	EventSource      string `json:"event_source"`
	Service          string `json:"service"`
	AWSRegion        string `json:"aws_region"`
	ResourceARN      string `json:"resource_arn"`
	ResourceType     string `json:"resource_type"`
	ResourceName     string `json:"resource_name"`
	SourceIP         string `json:"source_ip"`
	UserAgent        string `json:"user_agent"`
	IsReadOnly       bool   `json:"is_read_only"`
	ErrorCode        string `json:"error_code"`
	ErrorMessage     string `json:"error_message"`
	Status           string `json:"status"` // success | error
	CallerARN        string `json:"caller_arn"`
	CallerType       string `json:"caller_type"`
	SessionIssuerARN string `json:"session_issuer_arn"`
	CredentialName   string `json:"credential_name"`
	CredentialType   string `json:"credential_type"`
	BindingType      string `json:"binding_type"`
	AccessKeyID      string `json:"access_key_id"`
}

type IdentityTimelineEvent struct {
	IdentityEventCore
	IsCrossAccount bool `json:"is_cross_account"`
}

type IdentityEventsTimelineResponse struct {
	Section       string                   `json:"section"`
	Identity      IdentityEventsIdentity   `json:"identity"`
	TimeRange     string                   `json:"time_range"`
	TotalEvents   int                      `json:"total_events"`
	Events        []IdentityTimelineEvent  `json:"events"`
	Pagination    IdentityEventsPagination `json:"pagination"`
	DataAvailable bool                     `json:"data_available"`
}

// IdentityEventPermissionSource says which policy statement allowed the call.
type IdentityEventPermissionSource struct {
	PolicyName      *string `json:"policy_name"`
	PolicyType      *string `json:"policy_type"`
	StatementEffect *string `json:"statement_effect"`
	ResourceScope   *string `json:"resource_scope"`
	UsedCount90d    uint64  `json:"used_count_90d"`
}

type IdentityEventsDetailResponse struct {
	Section          string                         `json:"section"`
	Event            IdentityEventCore              `json:"event"`
	PermissionSource *IdentityEventPermissionSource `json:"permission_source"`
	DataAvailable    bool                           `json:"data_available"`
}

type IdentityEventsCredentialUsed struct {
	CredentialARN        string   `json:"credential_arn"`
	CredentialName       string   `json:"credential_name"`
	CredentialType       string   `json:"credential_type"`
	BindingType          string   `json:"binding_type"`
	EventCount           uint64   `json:"event_count"`
	FirstUsed            string   `json:"first_used"`
	LastUsed             string   `json:"last_used"`
	ActionsPerformed     []string `json:"actions_performed"`
	UniqueResourcesCount uint64   `json:"unique_resources_count"`
}

type IdentityEventsCredentialsResponse struct {
	Section         string                         `json:"section"`
	TimeRange       string                         `json:"time_range"`
	CredentialsUsed []IdentityEventsCredentialUsed `json:"credentials_used"`
	DataAvailable   bool                           `json:"data_available"`
}

type IdentityEventsResource struct {
	ResourceARN    string   `json:"resource_arn"`
	ResourceName   string   `json:"resource_name"`
	ResourceType   string   `json:"resource_type"`
	AccountID      string   `json:"account_id"`
	Actions        []string `json:"actions"`
	ReadCount      uint64   `json:"read_count"`
	WriteCount     uint64   `json:"write_count"`
	ErrorCount     uint64   `json:"error_count"`
	FirstAccess    string   `json:"first_access"`
	LastAccess     string   `json:"last_access"`
	IsCrossAccount bool     `json:"is_cross_account"`
}

type IdentityEventsResourcesResponse struct {
	Section           string                   `json:"section"`
	TimeRange         string                   `json:"time_range"`
	TotalResources    uint64                   `json:"total_resources"`
	ResourcesAccessed []IdentityEventsResource `json:"resources_accessed"`
	DataAvailable     bool                     `json:"data_available"`
}

type IdentityEventsHeatCell struct {
	Action string `json:"action"`
	Hour   int    `json:"hour"`
	Count  uint64 `json:"count"`
}

type IdentityEventsHeatmapResponse struct {
	Section       string                   `json:"section"`
	TimeRange     string                   `json:"time_range"`
	Heatmap       []IdentityEventsHeatCell `json:"heatmap"`
	DataAvailable bool                     `json:"data_available"`
}

type IdentityEventsSession struct {
	SessionName     string   `json:"session_name"`
	CredentialName  string   `json:"credential_name"`
	CredentialARN   string   `json:"credential_arn"`
	StartTime       string   `json:"start_time"`
	EndTime         string   `json:"end_time"`
	DurationMinutes int      `json:"duration_minutes"`
	EventCount      int      `json:"event_count"`
	Services        []string `json:"services"`
	Actions         []string `json:"actions"`
	SourceIPs       []string `json:"source_ips"`
}

type IdentityEventsSessionsResponse struct {
	Section       string                  `json:"section"`
	TimeRange     string                  `json:"time_range"`
	TotalSessions int                     `json:"total_sessions"`
	Sessions      []IdentityEventsSession `json:"sessions"`
	// Truncated is true when the window held more events than are scanned for
	// session building, so the oldest sessions may be incomplete.
	Truncated     bool `json:"truncated"`
	DataAvailable bool `json:"data_available"`
}

type IdentityEventsDeniedAction struct {
	Action string `json:"action"`
	Count  uint64 `json:"count"`
}

type IdentityEventsDeniedResource struct {
	ResourceARN  string `json:"resource_arn"`
	ResourceName string `json:"resource_name"`
	Count        uint64 `json:"count"`
}

type IdentityEventsErrorSummary struct {
	TotalErrors        uint64                        `json:"total_errors"`
	ByErrorCode        map[string]uint64             `json:"by_error_code"`
	MostDeniedAction   *IdentityEventsDeniedAction   `json:"most_denied_action"`
	MostDeniedResource *IdentityEventsDeniedResource `json:"most_denied_resource"`
}

type IdentityEventsErrorEvent struct {
	EventTime      string `json:"event_time"`
	EventName      string `json:"event_name"`
	Service        string `json:"service"`
	ResourceARN    string `json:"resource_arn"`
	ResourceName   string `json:"resource_name"`
	ErrorCode      string `json:"error_code"`
	ErrorMessage   string `json:"error_message"`
	CredentialName string `json:"credential_name"`
	SourceIP       string `json:"source_ip"`
	AWSRegion      string `json:"aws_region"`
}

type IdentityEventsErrorsResponse struct {
	Section       string                     `json:"section"`
	TimeRange     string                     `json:"time_range"`
	Summary       IdentityEventsErrorSummary `json:"summary"`
	Events        []IdentityEventsErrorEvent `json:"events"`
	Pagination    IdentityEventsPagination   `json:"pagination"`
	DataAvailable bool                       `json:"data_available"`
}

type IdentityEventsCrossAccountItem struct {
	TargetAccountID    string   `json:"target_account_id"`
	TargetResourceARN  string   `json:"target_resource_arn"`
	TargetResourceName string   `json:"target_resource_name"`
	Actions            []string `json:"actions"`
	EventCount         uint64   `json:"event_count"`
	LastSeen           string   `json:"last_seen"`
}

type IdentityEventsCrossAccountResponse struct {
	Section                 string                           `json:"section"`
	TimeRange               string                           `json:"time_range"`
	IdentityAccount         string                           `json:"identity_account"`
	TotalCrossAccountEvents uint64                           `json:"total_cross_account_events"`
	CrossAccountActivity    []IdentityEventsCrossAccountItem `json:"cross_account_activity"`
	DataAvailable           bool                             `json:"data_available"`
}

type IdentityEventsIP struct {
	SourceIP    string  `json:"source_ip"`
	EventCount  uint64  `json:"event_count"`
	FirstSeen   string  `json:"first_seen"`
	LastSeen    string  `json:"last_seen"`
	IsInternal  bool    `json:"is_internal"`
	GeoLocation *string `json:"geo_location"`
	// Kind refines is_internal: "private" (RFC 1918), "public", "aws_service"
	// (a service principal such as resource-explorer-2.amazonaws.com) or
	// "unknown" (not an IP address).
	Kind string `json:"kind"`
}

type IdentityEventsIPsResponse struct {
	Section        string             `json:"section"`
	TimeRange      string             `json:"time_range"`
	TotalUniqueIPs uint64             `json:"total_unique_ips"`
	IPs            []IdentityEventsIP `json:"ips"`
	DataAvailable  bool               `json:"data_available"`
}
