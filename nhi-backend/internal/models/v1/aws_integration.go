package v1

import "time"

// ─────────────────────────────────────────────────────────────────────────
// AWS account connection ("Integrations" page)
//
//   GET    /api/v1/integrations/aws                                 setup values + accounts
//   POST   /api/v1/integrations/aws/verify                          run the checks, save the account
//   PUT    /api/v1/integrations/aws/accounts/{account_id}/regions   change the regions read
//   PUT    /api/v1/integrations/aws/accounts/{account_id}/exposure  enable exposed-credential scanning
//   POST   /api/v1/integrations/aws/accounts/{account_id}/sync      queue a sync
//   DELETE /api/v1/integrations/aws/accounts/{account_id}           remove the account
//
// Tables (one nhi_db): nhi_global.tenants holds the tenant's collector values
// (migrations/global/004_tenant_collector.sql); each tenant schema holds
// aws_accounts (migrations/tenant/003_aws_accounts.sql). Connected accounts
// are also registered, one row per region, in the tenant schema's accounts
// table - the list every other screen scopes by.
// ─────────────────────────────────────────────────────────────────────────

// Connection status of a tenant, and of one AWS account (account_status).
const (
	AWSStatusNotConnected = "not_connected" // tenant only: no account connected yet
	AWSStatusPending      = "pending"       // saved, has never passed every check
	AWSStatusActionNeeded = "action_needed" // passed before, the latest check failed
	AWSStatusConnected    = "connected"     // the latest check passed every step
)

// State of one check.
const (
	AWSCheckPass    = "pass"
	AWSCheckFail    = "fail"    // the customer's setup needs a change; Fix says which
	AWSCheckSkipped = "skipped" // not run because an earlier check failed
	AWSCheckError   = "error"   // could not be run (our side); try again
)

// Connection check keys, in the order they run.
const (
	AWSCheckAssumeRole = "assume_role"
	AWSCheckReadAccess = "read_access"
	AWSCheckEvents     = "events"
)

// Exposed-credential check keys.
const (
	ExposureCheckCodeCommitList = "codecommit_list"
	ExposureCheckCodeCommitRead = "codecommit_read"
	ExposureCheckSecretRead     = "github_secret"
	ExposureCheckGitHubToken    = "github_token"
)

// Exposed-credential platforms.
const (
	ExposurePlatformGitHub     = "github"
	ExposurePlatformCodeCommit = "codecommit"
)

// Sync state of one account. The API sets queued; the discovery worker moves
// it to running and then succeeded or failed.
const (
	AWSSyncIdle      = "idle"
	AWSSyncQueued    = "queued"
	AWSSyncRunning   = "running"
	AWSSyncSucceeded = "succeeded"
	AWSSyncFailed    = "failed"
)

type AWSIntegrationResponse struct {
	// Status is the tenant's overall state: connected when any account is.
	Status string `json:"status"`
	// HasConnectedAccount is true once any account has ever connected. The
	// console opens past the setup screen from then on.
	HasConnectedAccount bool `json:"has_connected_account"`
	// ExposureEnabled is true when any account scans GitHub or CodeCommit:
	// the console shows the exposed-credential pages from then on.
	ExposureEnabled bool `json:"exposure_enabled"`
	// CanManage is true for roles allowed to connect accounts (admin). Setup
	// is null for everyone else.
	CanManage bool                  `json:"can_manage"`
	Workspace AWSWorkspace          `json:"workspace"`
	Setup     *AWSSetupValues       `json:"setup"`
	Accounts  []AWSConnectedAccount `json:"accounts"`
}

type AWSWorkspace struct {
	Domain string `json:"domain"`
}

// AWSSetupValues is everything the customer copies into the AWS console, and
// the regions they may choose from.
type AWSSetupValues struct {
	AccountID       string           `json:"account_id"`
	ExternalID      string           `json:"external_id"`
	EventBusARN     string           `json:"event_bus_arn"`
	Region          string           `json:"region"`
	AssumerRoleARN  string           `json:"assumer_role_arn"`
	RoleName        string           `json:"role_name"`
	RuleName        string           `json:"rule_name"`
	ManagedPolicies []string         `json:"managed_policies"`
	EventSources    []string         `json:"event_sources"`
	Regions         []AWSRegion      `json:"regions"`
	Exposure        AWSExposureSetup `json:"exposure"`
}

// AWSExposureSetup is what the customer adds to the same role to let the
// secret scanner read their code.
type AWSExposureSetup struct {
	CodeCommit CodeCommitSetup `json:"codecommit"`
	GitHub     GitHubSetup     `json:"github"`
}

type CodeCommitSetup struct {
	// ManagedPolicy is the AWS managed policy that grants exactly the read
	// access the scanner needs.
	ManagedPolicy string `json:"managed_policy"`
	// Actions is the least-privilege alternative, for an inline policy.
	Actions []string `json:"actions"`
}

type GitHubSetup struct {
	// SecretPrefix is the name the token secret must start with; the read
	// policy is scoped to it.
	SecretPrefix string `json:"secret_prefix"`
	// Actions are granted on that secret only.
	Actions []string `json:"actions"`
	// TokenPermissions are what the fine-grained GitHub token needs.
	TokenPermissions []string `json:"token_permissions"`
}

type AWSRegion struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Group string `json:"group"`
	OptIn bool   `json:"opt_in"`
}

type AWSConnectedAccount struct {
	AccountID      string      `json:"account_id"`
	RoleARN        string      `json:"role_arn"`
	Regions        []string    `json:"regions"`
	Status         string      `json:"status"` // pending | connected | action_needed
	IsConnected    bool        `json:"is_connected"`
	Checks         []AWSCheck  `json:"checks"`
	LastVerifiedAt time.Time   `json:"last_verified_at"`
	LastVerifiedBy string      `json:"last_verified_by"`
	ConnectedAt    *time.Time  `json:"connected_at"`
	Exposure       AWSExposure `json:"exposure"`
	Sync           AWSSync     `json:"sync"`
}

// AWSExposure is the exposed-credential scanning state of one account.
type AWSExposure struct {
	GitHub     ExposurePlatformState `json:"github"`
	CodeCommit ExposurePlatformState `json:"codecommit"`
	UpdatedBy  string                `json:"updated_by"`
}

type ExposurePlatformState struct {
	// Connected: enabled and the latest check passed (github_connected /
	// codecommit_connected).
	Connected  bool       `json:"connected"`
	Checks     []AWSCheck `json:"checks"`
	VerifiedAt *time.Time `json:"verified_at"`
	// SecretARN is the GitHub token secret (GitHub only).
	SecretARN string `json:"secret_arn,omitempty"`
}

type AWSSync struct {
	Status      string     `json:"status"`
	RequestedAt *time.Time `json:"requested_at"`
	RequestedBy string     `json:"requested_by"`
	StartedAt   *time.Time `json:"started_at"`
	LastSyncAt  *time.Time `json:"last_synced_at"`
	LastError   string     `json:"last_error,omitempty"`
}

type AWSCheck struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Message string `json:"message"`
	// Fix is the one change that makes a failed check pass. Empty unless
	// State is "fail".
	Fix string `json:"fix,omitempty"`
}

type AWSVerifyRequest struct {
	RoleARN string   `json:"role_arn"`
	Regions []string `json:"regions"`
}

type AWSRegionsRequest struct {
	Regions []string `json:"regions"`
}

// AWSExposureRequest turns scanning on or off per platform. Each platform
// turned on is verified now; GitHub needs the token secret's ARN.
type AWSExposureRequest struct {
	GitHub          bool   `json:"github"`
	CodeCommit      bool   `json:"codecommit"`
	GitHubSecretARN string `json:"github_secret_arn"`
}

type AWSExposureResponse struct {
	Account AWSConnectedAccount `json:"account"`
	// ExposureEnabled is the tenant-wide flag after this change.
	ExposureEnabled bool `json:"exposure_enabled"`
}

type AWSVerifyResponse struct {
	Status     string     `json:"status"` // connected | action_needed | pending
	AccountID  string     `json:"account_id"`
	RoleARN    string     `json:"role_arn"`
	Regions    []string   `json:"regions"`
	Checks     []AWSCheck `json:"checks"`
	VerifiedAt time.Time  `json:"verified_at"`
	// Saved is false when the role could not be assumed: nothing about an
	// account is stored until the tenant has proved it controls it.
	Saved bool `json:"saved"`
	// SyncQueued is true when this verification connected the account for
	// the first time and its first sync was queued.
	SyncQueued bool `json:"sync_queued"`
}

type AWSSyncResponse struct {
	Account AWSConnectedAccount `json:"account"`
	// AlreadyQueued: a sync was already queued or running; nothing changed.
	AlreadyQueued bool `json:"already_queued"`
}

// AWSTenantSettings is the tenant's row in nhi_global.tenants, as the
// integration reads it.
type AWSTenantSettings struct {
	Domain       string
	AWSAccountID string
	ExternalID   string
	EventBusARN  string
	// Regions limits what customers may choose; empty means every region.
	Regions []string
}

// AWSAccountRecord is one row of <tenant schema>.aws_accounts.
type AWSAccountRecord struct {
	AccountID           string
	RoleARN             string
	Regions             []string
	ExternalID          string
	Status              string
	IsConnected         bool
	Checks              []AWSCheck
	LastVerifiedAt      time.Time
	LastVerifiedBy      string
	ConnectedAt         *time.Time
	GitHubConnected     bool
	GitHubSecretARN     string
	GitHubChecks        []AWSCheck
	GitHubVerified      *time.Time
	CodeCommitConnected bool
	CodeCommitChecks    []AWSCheck
	CodeCommitVerified  *time.Time
	ExposureUpdatedBy   string
	SyncStatus          string
	SyncRequestedAt     *time.Time
	SyncRequestedBy     string
	SyncStartedAt       *time.Time
	LastSyncedAt        *time.Time
	LastSyncError       string
}

func nonNilChecks(c []AWSCheck) []AWSCheck {
	if c == nil {
		return []AWSCheck{}
	}
	return c
}

// ToAPI is the account as the API returns it.
func (r AWSAccountRecord) ToAPI() AWSConnectedAccount {
	regions := r.Regions
	if regions == nil {
		regions = []string{}
	}
	return AWSConnectedAccount{
		AccountID:      r.AccountID,
		RoleARN:        r.RoleARN,
		Regions:        regions,
		Status:         r.Status,
		IsConnected:    r.IsConnected,
		Checks:         nonNilChecks(r.Checks),
		LastVerifiedAt: r.LastVerifiedAt,
		LastVerifiedBy: r.LastVerifiedBy,
		ConnectedAt:    r.ConnectedAt,
		Exposure: AWSExposure{
			GitHub: ExposurePlatformState{
				Connected:  r.GitHubConnected,
				Checks:     nonNilChecks(r.GitHubChecks),
				VerifiedAt: r.GitHubVerified,
				SecretARN:  r.GitHubSecretARN,
			},
			CodeCommit: ExposurePlatformState{
				Connected:  r.CodeCommitConnected,
				Checks:     nonNilChecks(r.CodeCommitChecks),
				VerifiedAt: r.CodeCommitVerified,
			},
			UpdatedBy: r.ExposureUpdatedBy,
		},
		Sync: AWSSync{
			Status:      r.SyncStatus,
			RequestedAt: r.SyncRequestedAt,
			RequestedBy: r.SyncRequestedBy,
			StartedAt:   r.SyncStartedAt,
			LastSyncAt:  r.LastSyncedAt,
			LastError:   r.LastSyncError,
		},
	}
}
