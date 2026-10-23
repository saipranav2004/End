package v1

import (
	"context"
	"errors"
	"fmt"
	"nhi-backend-api/internal/logging"
	"strings"
	"sync"
	"time"

	"nhi-backend-api/internal/awsconnect"
	"nhi-backend-api/internal/githubcheck"
	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/repository"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
)

var (
	// ErrAWSTenantNotConfigured: the tenant's row in nhi_global.tenants lacks
	// (or has inconsistent) collector values. The super admin fills them in.
	ErrAWSTenantNotConfigured = errors.New("aws integration is not configured for this tenant")
	// ErrAWSUnavailable: this process has no usable AWS configuration.
	ErrAWSUnavailable = errors.New("aws is not reachable from the api")
	// ErrAWSVerifyInProgress: a check for this tenant is already running.
	ErrAWSVerifyInProgress = errors.New("a connection check is already running")
	// ErrAWSAccountClaimed: the account belongs to another workspace.
	ErrAWSAccountClaimed = errors.New("aws account is connected to another workspace")
	// ErrRegionNotOffered: a region outside the tenant's collector_regions.
	ErrRegionNotOffered = errors.New("region not offered")
)

// What the customer is asked to set up. Kept next to the checks that prove it.
var (
	awsManagedPolicies = []string{"SecurityAudit", "ViewOnlyAccess"}
	awsEventSources    = []string{
		"aws.ec2", "aws.ecs", "aws.iam", "aws.sts",
		"aws.lambda", "aws.eks", "aws.secretsmanager", "aws.kms",
	}
	// CodeCommit: the AWS managed read-only policy, or these actions inline.
	codeCommitManagedPolicy = "AWSCodeCommitReadOnly"
	codeCommitActions       = []string{
		"codecommit:ListRepositories", "codecommit:BatchGetRepositories", "codecommit:GetRepository",
		"codecommit:ListBranches", "codecommit:GetBranch", "codecommit:GetCommit",
		"codecommit:GetDifferences", "codecommit:GetBlob", "codecommit:GetFile",
		"codecommit:GetFolder", "codecommit:GitPull",
	}
	// GitHub: read the token secret, nothing else.
	gitHubSecretActions    = []string{"secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"}
	gitHubTokenPermissions = []string{"Contents: Read-only", "Metadata: Read-only"}
)

// Time budget for one check. The HTTP server's write timeout is 15s.
const (
	awsVerifyBudget     = 12 * time.Second
	awsAssumeTimeout    = 4 * time.Second
	awsReadTimeout      = 3 * time.Second
	awsPermissionBudget = 3 * time.Second
	awsCleanupTimeout   = 5 * time.Second
	exposureStepTimeout = 5 * time.Second
)

// AWSIntegrationStore is the persistence (repository.AWSIntegrationRepository).
type AWSIntegrationStore interface {
	TenantSettings(ctx context.Context, tenantID string) (*models.AWSTenantSettings, error)
	ListAccounts(ctx context.Context, schema string) ([]models.AWSAccountRecord, error)
	GetAccount(ctx context.Context, schema, accountID string) (*models.AWSAccountRecord, error)
	AccountClaimedByOther(ctx context.Context, tenantID, accountID string) (bool, error)
	SaveVerification(ctx context.Context, tenantID, schema string, rec models.AWSAccountRecord) (*models.AWSAccountRecord, bool, error)
	UpdateRegions(ctx context.Context, tenantID, schema, accountID string, regions []string) (*models.AWSAccountRecord, error)
	SaveExposure(ctx context.Context, schema, accountID string, u repository.ExposureUpdate) (*models.AWSAccountRecord, error)
	RequestSync(ctx context.Context, schema, accountID, actor string, at time.Time) (*models.AWSAccountRecord, bool, error)
	DeleteAccount(ctx context.Context, tenantID, schema, accountID string) error
}

// AWSVerifier is the AWS side (awsconnect.Client in production).
type AWSVerifier interface {
	AssumeRole(ctx context.Context, p awsconnect.Platform, role awsconnect.RoleARN, externalID string) (*awsconnect.Session, error)
	TestRead(ctx context.Context, s *awsconnect.Session) error
	AllowAccountOnBus(ctx context.Context, p awsconnect.Platform, accountID string) error
	RevokeAccountOnBus(ctx context.Context, p awsconnect.Platform, accountID string) error
	ListCodeCommit(ctx context.Context, s *awsconnect.Session, regions []string) (awsconnect.CodeCommitResult, error)
	ReadCodeCommit(ctx context.Context, s *awsconnect.Session, region, repository string) error
	ReadSecretToken(ctx context.Context, s *awsconnect.Session, arn awsconnect.SecretARN) (string, error)
}

// GitHubChecker validates a GitHub token (githubcheck.Checker in production).
type GitHubChecker interface {
	Check(ctx context.Context, token string) (githubcheck.Result, error)
}

// AWSEventSource answers "has an event from this account reached us?".
type AWSEventSource interface {
	HasEventsFrom(ctx context.Context, p awsconnect.Platform, accountID string, since time.Time) (bool, error)
}

// ClickHouseEventSource checks the CloudTrail event store the pipeline fills
// from the central bus.
type ClickHouseEventSource struct{ Repo *chrepo.Repository }

func (s ClickHouseEventSource) HasEventsFrom(ctx context.Context, _ awsconnect.Platform, accountID string, since time.Time) (bool, error) {
	return s.Repo.AccountHasEventsSince(ctx, accountID, since)
}

// FallbackEventSource asks Primary (the SQS queue) first and, when it finds
// nothing or fails, Secondary (the event store). Either may be nil. An account
// is connected as soon as either one has seen its events.
type FallbackEventSource struct{ Primary, Secondary AWSEventSource }

func (s FallbackEventSource) HasEventsFrom(ctx context.Context, p awsconnect.Platform, accountID string, since time.Time) (bool, error) {
	var firstErr error
	if s.Primary != nil {
		found, err := s.Primary.HasEventsFrom(ctx, p, accountID, since)
		if err == nil && found {
			return true, nil
		}
		if err != nil {
			firstErr = err
			logging.FromContext(ctx).Warn("aws integration: event queue check failed, trying the event store", "err", err)
		}
	}
	if s.Secondary != nil {
		return s.Secondary.HasEventsFrom(ctx, p, accountID, since)
	}
	return false, firstErr
}

// QueueEventSource peeks at the central SQS queue.
type QueueEventSource struct{ Client *awsconnect.Client }

func (s QueueEventSource) HasEventsFrom(ctx context.Context, p awsconnect.Platform, accountID string, _ time.Time) (bool, error) {
	return s.Client.PeekEventsFrom(ctx, p, accountID)
}

// AWSIntegrationOptions are the deployment's names and limits.
type AWSIntegrationOptions struct {
	AssumerRoleName    string
	CollectorRoleName  string
	ForwardRuleName    string
	GitHubSecretPrefix string
	EventLookback      time.Duration
}

// AWSCaller is who is asking, from the signed token.
type AWSCaller struct {
	TenantID string
	Schema   string
	Actor    string
}

type AWSIntegrationService struct {
	store    AWSIntegrationStore
	verifier AWSVerifier
	github   GitHubChecker
	events   AWSEventSource
	opts     AWSIntegrationOptions
	now      func() time.Time

	running sync.Map // tenantID -> struct{}: one check per tenant at a time
}

// NewAWSIntegrationService wires the service. verifier may be nil when the
// process has no AWS configuration (the page still loads; checks answer
// ErrAWSUnavailable), and events may be nil when there is no event store (the
// events check then reports it cannot run).
func NewAWSIntegrationService(store AWSIntegrationStore, verifier AWSVerifier, github GitHubChecker, events AWSEventSource, opts AWSIntegrationOptions) *AWSIntegrationService {
	if opts.EventLookback <= 0 {
		opts.EventLookback = 24 * time.Hour
	}
	opts.AssumerRoleName = orDefault(opts.AssumerRoleName, "NHIPlatformAssumer")
	opts.CollectorRoleName = orDefault(opts.CollectorRoleName, "NHIPlatformCollector")
	opts.ForwardRuleName = orDefault(opts.ForwardRuleName, "nhi-forward-events")
	opts.GitHubSecretPrefix = orDefault(opts.GitHubSecretPrefix, "nhi/github")
	return &AWSIntegrationService{store: store, verifier: verifier, github: github, events: events, opts: opts, now: time.Now}
}

// platform reads the tenant's collector values and checks them against each other.
func (s *AWSIntegrationService) platform(ctx context.Context, tenantID string) (awsconnect.Platform, *models.AWSTenantSettings, error) {
	settings, err := s.store.TenantSettings(ctx, tenantID)
	if err != nil {
		return awsconnect.Platform{}, nil, err
	}
	if settings == nil || settings.AWSAccountID == "" || settings.EventBusARN == "" || settings.ExternalID == "" {
		logging.FromContext(ctx).Warn("aws integration: tenant has no collector_aws_account_id / collector_event_bridge_arn / collector_external_id in nhi_global.tenants", "tenant_id", tenantID)
		return awsconnect.Platform{}, settings, ErrAWSTenantNotConfigured
	}
	p, err := awsconnect.PlatformForTenant(settings.AWSAccountID, settings.EventBusARN, s.opts.AssumerRoleName)
	if err != nil {
		logging.FromContext(ctx).Warn("aws integration: platform lookup failed", "tenant_id", tenantID, "err", err)
		return awsconnect.Platform{}, settings, ErrAWSTenantNotConfigured
	}
	return p, settings, nil
}

// offeredRegions is the catalogue limited to the tenant's collector_regions
// (all regions when that is empty).
func offeredRegions(settings *models.AWSTenantSettings) []models.AWSRegion {
	allowed := map[string]bool{}
	if settings != nil {
		for _, r := range settings.Regions {
			allowed[r] = true
		}
	}
	out := make([]models.AWSRegion, 0, len(awsconnect.Regions))
	for _, r := range awsconnect.Regions {
		if len(allowed) == 0 || allowed[r.Code] {
			out = append(out, models.AWSRegion{Code: r.Code, Name: r.Name, Group: r.Group, OptIn: r.OptIn})
		}
	}
	return out
}

// normalizeRegions validates the codes and that the tenant is offered them.
func normalizeRegions(raw []string, settings *models.AWSTenantSettings) ([]string, error) {
	regions, err := awsconnect.NormalizeRegions(raw)
	if err != nil {
		return nil, err
	}
	if settings == nil || len(settings.Regions) == 0 {
		return regions, nil
	}
	allowed := map[string]bool{}
	for _, r := range settings.Regions {
		allowed[r] = true
	}
	for _, r := range regions {
		if !allowed[r] {
			return nil, &awsconnect.ErrInvalidRegions{Message: "Region " + r + " is not available for your workspace."}
		}
	}
	return regions, nil
}

// Get returns the tenant's connection status and accounts and, for those who
// may manage them, the values to copy and the regions to choose from.
func (s *AWSIntegrationService) Get(ctx context.Context, caller AWSCaller, canManage bool) (*models.AWSIntegrationResponse, error) {
	records, err := s.store.ListAccounts(ctx, caller.Schema)
	if err != nil {
		return nil, err
	}
	out := &models.AWSIntegrationResponse{
		Status:    tenantStatus(records),
		CanManage: canManage,
		Accounts:  make([]models.AWSConnectedAccount, 0, len(records)),
	}
	for _, rec := range records {
		out.Accounts = append(out.Accounts, rec.ToAPI())
		if rec.ConnectedAt != nil {
			out.HasConnectedAccount = true
		}
		if rec.GitHubConnected || rec.CodeCommitConnected {
			out.ExposureEnabled = true
		}
	}

	p, settings, err := s.platform(ctx, caller.TenantID)
	if settings != nil {
		out.Workspace.Domain = settings.Domain
	}
	if !canManage {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.Setup = &models.AWSSetupValues{
		AccountID:       p.AccountID,
		ExternalID:      settings.ExternalID,
		EventBusARN:     p.EventBusARN,
		Region:          p.Region,
		AssumerRoleARN:  p.AssumerRoleARN,
		RoleName:        s.opts.CollectorRoleName,
		RuleName:        s.opts.ForwardRuleName,
		ManagedPolicies: append([]string(nil), awsManagedPolicies...),
		EventSources:    append([]string(nil), awsEventSources...),
		Regions:         offeredRegions(settings),
		Exposure: models.AWSExposureSetup{
			CodeCommit: models.CodeCommitSetup{
				ManagedPolicy: codeCommitManagedPolicy,
				Actions:       append([]string(nil), codeCommitActions...),
			},
			GitHub: models.GitHubSetup{
				SecretPrefix:     s.opts.GitHubSecretPrefix,
				Actions:          append([]string(nil), gitHubSecretActions...),
				TokenPermissions: append([]string(nil), gitHubTokenPermissions...),
			},
		},
	}
	return out, nil
}

func tenantStatus(records []models.AWSAccountRecord) string {
	if len(records) == 0 {
		return models.AWSStatusNotConnected
	}
	for _, rec := range records {
		if rec.IsConnected {
			return models.AWSStatusConnected
		}
	}
	return models.AWSStatusActionNeeded
}

// begin claims the tenant's single check slot.
func (s *AWSIntegrationService) begin(tenantID string) (func(), error) {
	if _, busy := s.running.LoadOrStore(tenantID, struct{}{}); busy {
		return nil, ErrAWSVerifyInProgress
	}
	return func() { s.running.Delete(tenantID) }, nil
}

// Verify runs the three checks for a pasted role ARN and the regions the
// customer allows, in order:
//
//  1. sts:AssumeRole with the ARN and the tenant's External ID
//  2. iam:ListRoles with the assumed role (one test read)
//  3. an event from the account has reached us
//
// Before (3) the account is allowed to put events on the central bus. Nothing
// is stored or granted until (1) passes, because only then has the tenant
// shown it controls the account. The first time all three pass, a sync is
// queued and the account joins the tenant's scope.
func (s *AWSIntegrationService) Verify(ctx context.Context, caller AWSCaller, rawRoleARN string, rawRegions []string) (*models.AWSVerifyResponse, error) {
	p, settings, err := s.platform(ctx, caller.TenantID)
	if err != nil {
		return nil, err
	}
	role, err := awsconnect.ParseRoleARN(rawRoleARN)
	if err != nil {
		return nil, err
	}
	if role.AccountID == p.AccountID {
		return nil, &awsconnect.ErrInvalidRoleARN{Message: "That ARN is in our account (" + p.AccountID + "). Paste the ARN of the role you created in your own AWS account."}
	}
	regions, err := normalizeRegions(rawRegions, settings)
	if err != nil {
		return nil, err
	}
	if s.verifier == nil {
		return nil, ErrAWSUnavailable
	}
	done, err := s.begin(caller.TenantID)
	if err != nil {
		return nil, err
	}
	defer done()

	ctx, cancel := context.WithTimeout(ctx, awsVerifyBudget)
	defer cancel()

	checks := newChecks()
	result := &models.AWSVerifyResponse{
		Status:     models.AWSStatusPending,
		AccountID:  role.AccountID,
		RoleARN:    role.ARN,
		Regions:    regions,
		Checks:     checks,
		VerifiedAt: s.now().UTC(),
	}

	// 1. AssumeRole
	stepCtx, stepCancel := context.WithTimeout(ctx, awsAssumeTimeout)
	session, err := s.verifier.AssumeRole(stepCtx, p, role, settings.ExternalID)
	stepCancel()
	if err != nil {
		if errors.Is(err, awsconnect.ErrAccessDenied) {
			fail(&checks[0], "AWS would not let us use this role.", "Check the External ID matches")
		} else {
			logCheckError(ctx, caller.TenantID, role.AccountID, checks[0].Key, err)
			unavailable(&checks[0])
		}
		skip(&checks[1])
		skip(&checks[2])
		return result, nil
	}
	pass(&checks[0], "Signed in to your account with your External ID.")

	// The tenant controls this account. It may still belong to another tenant.
	claimed, err := s.store.AccountClaimedByOther(ctx, caller.TenantID, role.AccountID)
	if err != nil {
		return nil, err
	}
	if claimed {
		return nil, ErrAWSAccountClaimed
	}

	// 2. One test read
	stepCtx, stepCancel = context.WithTimeout(ctx, awsReadTimeout)
	err = s.verifier.TestRead(stepCtx, session)
	stepCancel()
	switch {
	case err == nil:
		pass(&checks[1], "Read access works.")
	case errors.Is(err, awsconnect.ErrAccessDenied), errors.Is(err, awsconnect.ErrNothingRead):
		fail(&checks[1], "The role cannot read your account.", "Attach SecurityAudit + ViewOnlyAccess")
	default:
		logCheckError(ctx, caller.TenantID, role.AccountID, checks[1].Key, err)
		unavailable(&checks[1])
	}

	// 3. Events. Allow the account on the bus first (idempotent); EventBridge
	// retries refused deliveries, so events sent before this still arrive.
	stepCtx, stepCancel = context.WithTimeout(ctx, awsPermissionBudget)
	err = s.verifier.AllowAccountOnBus(stepCtx, p, role.AccountID)
	stepCancel()
	switch {
	case err != nil:
		logCheckError(ctx, caller.TenantID, role.AccountID, "bus_permission", err)
		unavailable(&checks[2])
	case s.events == nil:
		logCheckError(ctx, caller.TenantID, role.AccountID, checks[2].Key, errors.New("no event source: configure ClickHouse or aws_integration.event_log_group"))
		unavailable(&checks[2])
	default:
		found, err := s.events.HasEventsFrom(ctx, p, role.AccountID, s.now().Add(-s.opts.EventLookback))
		switch {
		case err != nil:
			logCheckError(ctx, caller.TenantID, role.AccountID, checks[2].Key, err)
			unavailable(&checks[2])
		case found:
			pass(&checks[2], "Events from your account are arriving.")
		default:
			fail(&checks[2], fmt.Sprintf("No events from this account in the last %s.", humanHours(s.opts.EventLookback)), "Check the EventBridge rule target ARN")
		}
	}

	allPassed := checks[0].State == models.AWSCheckPass && checks[1].State == models.AWSCheckPass && checks[2].State == models.AWSCheckPass
	if allPassed {
		result.Status = models.AWSStatusConnected
	}

	// Saved even if the caller has gone: the checks really ran.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	saved, queued, err := s.store.SaveVerification(saveCtx, caller.TenantID, caller.Schema, models.AWSAccountRecord{
		AccountID:      role.AccountID,
		RoleARN:        role.ARN,
		Regions:        regions,
		ExternalID:     settings.ExternalID,
		Status:         result.Status,
		Checks:         checks,
		LastVerifiedAt: result.VerifiedAt,
		LastVerifiedBy: caller.Actor,
	})
	if errors.Is(err, repository.ErrAWSAccountClaimed) {
		return nil, ErrAWSAccountClaimed
	}
	if err != nil {
		return nil, err
	}
	result.Status = saved.Status
	result.Saved = true
	result.SyncQueued = queued
	return result, nil
}

// UpdateRegions changes the regions an account is read in.
func (s *AWSIntegrationService) UpdateRegions(ctx context.Context, caller AWSCaller, accountID string, rawRegions []string) (*models.AWSConnectedAccount, error) {
	settings, err := s.store.TenantSettings(ctx, caller.TenantID)
	if err != nil {
		return nil, err
	}
	regions, err := normalizeRegions(rawRegions, settings)
	if err != nil {
		return nil, err
	}
	rec, err := s.store.UpdateRegions(ctx, caller.TenantID, caller.Schema, accountID, regions)
	if err != nil {
		return nil, err
	}
	account := rec.ToAPI()
	return &account, nil
}

// SetExposure turns exposed-credential scanning on or off for one connected
// account. Each platform turned on is checked now with the account's role;
// it is marked connected only when every check passes. Turning a platform off
// needs no check.
func (s *AWSIntegrationService) SetExposure(ctx context.Context, caller AWSCaller, accountID string, req models.AWSExposureRequest) (*models.AWSExposureResponse, error) {
	rec, err := s.store.GetAccount(ctx, caller.Schema, accountID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, repository.ErrAWSAccountNotFound
	}
	if rec.ConnectedAt == nil {
		return nil, repository.ErrAWSAccountNotConnected
	}
	var secret awsconnect.SecretARN
	if req.GitHub {
		secret, err = awsconnect.ParseSecretARN(req.GitHubSecretARN, accountID, s.opts.GitHubSecretPrefix)
		if err != nil {
			return nil, err
		}
	}

	update := repository.ExposureUpdate{Actor: caller.Actor, At: s.now().UTC()}
	if !req.GitHub {
		update.GitHub = &repository.ExposurePlatformUpdate{}
	}
	if !req.CodeCommit {
		update.CodeCommit = &repository.ExposurePlatformUpdate{}
	}

	if req.GitHub || req.CodeCommit {
		p, settings, err := s.platform(ctx, caller.TenantID)
		if err != nil {
			return nil, err
		}
		if s.verifier == nil {
			return nil, ErrAWSUnavailable
		}
		role, err := awsconnect.ParseRoleARN(rec.RoleARN)
		if err != nil {
			return nil, err
		}
		done, err := s.begin(caller.TenantID)
		if err != nil {
			return nil, err
		}
		defer done()
		ctx, cancel := context.WithTimeout(ctx, awsVerifyBudget)
		defer cancel()

		stepCtx, stepCancel := context.WithTimeout(ctx, awsAssumeTimeout)
		session, assumeErr := s.verifier.AssumeRole(stepCtx, p, role, settings.ExternalID)
		stepCancel()

		if req.CodeCommit {
			checks := s.checkCodeCommit(ctx, caller, session, assumeErr, rec)
			update.CodeCommit = &repository.ExposurePlatformUpdate{Connected: allPass(checks), Checks: checks}
		}
		if req.GitHub {
			checks := s.checkGitHub(ctx, caller, session, assumeErr, rec, secret)
			update.GitHub = &repository.ExposurePlatformUpdate{Connected: allPass(checks), Checks: checks, SecretARN: secret.ARN}
		}
	}

	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	saved, err := s.store.SaveExposure(saveCtx, caller.Schema, accountID, update)
	if err != nil {
		return nil, err
	}
	all, err := s.store.ListAccounts(saveCtx, caller.Schema)
	if err != nil {
		return nil, err
	}
	resp := &models.AWSExposureResponse{Account: saved.ToAPI()}
	for _, a := range all {
		if a.GitHubConnected || a.CodeCommitConnected {
			resp.ExposureEnabled = true
		}
	}
	return resp, nil
}

// roleUnusable fills checks when the account's role could not be assumed:
// the connection itself is broken, so nothing else can be tried.
func roleUnusable(ctx context.Context, checks []models.AWSCheck, caller AWSCaller, accountID string, err error) {
	if errors.Is(err, awsconnect.ErrAccessDenied) {
		fail(&checks[0], "AWS would not let us use this account's role any more.", "Re-check the account connection")
	} else {
		logCheckError(ctx, caller.TenantID, accountID, checks[0].Key, err)
		unavailable(&checks[0])
	}
	for i := 1; i < len(checks); i++ {
		skip(&checks[i])
	}
}

// checkCodeCommit: list repositories in every allowed region, then read one.
func (s *AWSIntegrationService) checkCodeCommit(ctx context.Context, caller AWSCaller, session *awsconnect.Session, assumeErr error, rec *models.AWSAccountRecord) []models.AWSCheck {
	checks := []models.AWSCheck{
		{Key: models.ExposureCheckCodeCommitList, Label: "List your repositories"},
		{Key: models.ExposureCheckCodeCommitRead, Label: "Read repository contents"},
	}
	if assumeErr != nil {
		roleUnusable(ctx, checks, caller, rec.AccountID, assumeErr)
		return checks
	}
	stepCtx, cancel := context.WithTimeout(ctx, exposureStepTimeout)
	found, err := s.verifier.ListCodeCommit(stepCtx, session, rec.Regions)
	cancel()
	if err != nil {
		if errors.Is(err, awsconnect.ErrAccessDenied) {
			fail(&checks[0], "The role cannot list CodeCommit repositories.", "Attach "+codeCommitManagedPolicy+" to the role")
		} else {
			logCheckError(ctx, caller.TenantID, rec.AccountID, checks[0].Key, err)
			unavailable(&checks[0])
		}
		skip(&checks[1])
		return checks
	}
	msg := fmt.Sprintf("%s in %s.", plural(found.Repositories, "repository", "repositories"), plural(len(found.Regions), "region", "regions"))
	if found.Repositories == 0 {
		msg = "No repositories yet in your selected regions. New ones are scanned as they appear."
	}
	if len(found.Unavailable) > 0 {
		msg += " CodeCommit is not available in " + strings.Join(found.Unavailable, ", ") + "."
	}
	pass(&checks[0], msg)

	if found.SampleRepository == "" {
		checks[1].State, checks[1].Message = models.AWSCheckSkipped, "Nothing to read yet."
		return checks
	}
	stepCtx, cancel = context.WithTimeout(ctx, exposureStepTimeout)
	err = s.verifier.ReadCodeCommit(stepCtx, session, found.SampleRegion, found.SampleRepository)
	cancel()
	switch {
	case err == nil:
		pass(&checks[1], "Read "+found.SampleRepository+".")
	case errors.Is(err, awsconnect.ErrAccessDenied):
		fail(&checks[1], "The role can list repositories but not read their files.", "Attach "+codeCommitManagedPolicy+" to the role")
	default:
		logCheckError(ctx, caller.TenantID, rec.AccountID, checks[1].Key, err)
		unavailable(&checks[1])
	}
	return checks
}

// checkGitHub: read the token from the customer's secret with the role, then
// ask GitHub what it can reach. The token is never stored or logged.
func (s *AWSIntegrationService) checkGitHub(ctx context.Context, caller AWSCaller, session *awsconnect.Session, assumeErr error, rec *models.AWSAccountRecord, secret awsconnect.SecretARN) []models.AWSCheck {
	checks := []models.AWSCheck{
		{Key: models.ExposureCheckSecretRead, Label: "Read the token secret"},
		{Key: models.ExposureCheckGitHubToken, Label: "GitHub accepts the token"},
	}
	if assumeErr != nil {
		roleUnusable(ctx, checks, caller, rec.AccountID, assumeErr)
		return checks
	}
	stepCtx, cancel := context.WithTimeout(ctx, exposureStepTimeout)
	token, err := s.verifier.ReadSecretToken(stepCtx, session, secret)
	cancel()
	switch {
	case err == nil:
		pass(&checks[0], "Read the secret with your role.")
	case errors.Is(err, awsconnect.ErrSecretNotFound):
		fail(&checks[0], "No secret with that ARN in "+secret.Region+".", "Check the secret ARN")
	case errors.Is(err, awsconnect.ErrKMSDenied):
		fail(&checks[0], "The secret's KMS key does not let the role decrypt it.", "Allow kms:Decrypt on the secret's key")
	case errors.Is(err, awsconnect.ErrSecretEmpty):
		fail(&checks[0], "The secret has no token in it.", "Store the GitHub token as the secret value")
	case errors.Is(err, awsconnect.ErrAccessDenied):
		fail(&checks[0], "The role may not read this secret.", "Add the secret read policy to the role")
	default:
		logCheckError(ctx, caller.TenantID, rec.AccountID, checks[0].Key, err)
		unavailable(&checks[0])
	}
	if checks[0].State != models.AWSCheckPass {
		skip(&checks[1])
		return checks
	}
	if s.github == nil {
		unavailable(&checks[1])
		return checks
	}
	stepCtx, cancel = context.WithTimeout(ctx, exposureStepTimeout)
	res, err := s.github.Check(stepCtx, token)
	cancel()
	token = ""
	switch {
	case err == nil:
		count := plural(res.Repositories, "repository", "repositories")
		if res.More {
			count = fmt.Sprintf("More than %d repositories", res.Repositories)
		}
		who := ""
		if res.Login != "" {
			who = " as " + res.Login
		}
		pass(&checks[1], count+" visible"+who+".")
	case errors.Is(err, githubcheck.ErrTokenRejected):
		fail(&checks[1], "GitHub does not accept this token. It may be wrong, expired or revoked.", "Create a new token and update the secret")
	case errors.Is(err, githubcheck.ErrNoRepositories):
		fail(&checks[1], "The token works but cannot see any repository.", "Give the token access to your repositories")
	case errors.Is(err, githubcheck.ErrNoContents):
		fail(&checks[1], "The token can see repositories but not read their files.", "Give the token Contents: Read-only")
	default:
		logCheckError(ctx, caller.TenantID, rec.AccountID, checks[1].Key, err)
		unavailable(&checks[1])
	}
	return checks
}

func allPass(checks []models.AWSCheck) bool {
	for _, c := range checks {
		if c.State != models.AWSCheckPass && c.State != models.AWSCheckSkipped {
			return false
		}
	}
	// A skip only counts when nothing failed before it and something passed.
	return len(checks) > 0 && checks[0].State == models.AWSCheckPass
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// RequestSync queues a sync of one connected account, in its allowed
// regions. Asking again while one is queued or running changes nothing.
func (s *AWSIntegrationService) RequestSync(ctx context.Context, caller AWSCaller, accountID string) (*models.AWSSyncResponse, error) {
	rec, already, err := s.store.RequestSync(ctx, caller.Schema, accountID, caller.Actor, s.now().UTC())
	if err != nil {
		return nil, err
	}
	return &models.AWSSyncResponse{Account: rec.ToAPI(), AlreadyQueued: already}, nil
}

// RemoveAccount disconnects an account: its row and its scope rows are
// deleted in one transaction (its scanned records drop out of every scope),
// and its permission on the central bus is revoked so its events are refused.
// Nothing changes in the customer's AWS account; the page tells them to
// delete the role and rule themselves.
func (s *AWSIntegrationService) RemoveAccount(ctx context.Context, caller AWSCaller, accountID string) error {
	if err := s.store.DeleteAccount(ctx, caller.TenantID, caller.Schema, accountID); err != nil {
		return err
	}
	// Best effort: the account is already removed; a permission left behind
	// only lets events in that no tenant scope includes.
	if s.verifier != nil {
		if p, _, err := s.platform(ctx, caller.TenantID); err == nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), awsCleanupTimeout)
			defer cancel()
			if err := s.verifier.RevokeAccountOnBus(cleanupCtx, p, accountID); err != nil {
				logging.FromContext(ctx).Warn("aws integration: revoke bus permission failed, remove the statement by hand",
					"tenant_id", caller.TenantID, "account_id", accountID, "statement", awsconnect.BusStatementID(accountID), "err", err)
			}
		}
	}
	return nil
}

func newChecks() []models.AWSCheck {
	return []models.AWSCheck{
		{Key: models.AWSCheckAssumeRole, Label: "Assume your role"},
		{Key: models.AWSCheckReadAccess, Label: "Read your account"},
		{Key: models.AWSCheckEvents, Label: "Receive your events"},
	}
}

func pass(c *models.AWSCheck, message string) {
	c.State, c.Message, c.Fix = models.AWSCheckPass, message, ""
}

func fail(c *models.AWSCheck, message, fix string) {
	c.State, c.Message, c.Fix = models.AWSCheckFail, message, fix
}

func skip(c *models.AWSCheck) {
	c.State, c.Message, c.Fix = models.AWSCheckSkipped, "Runs once the check above passes.", ""
}

func unavailable(c *models.AWSCheck) {
	c.State, c.Message, c.Fix = models.AWSCheckError, "We could not run this check just now. Try again in a minute.", ""
}

// logCheckError keeps AWS's own error text in the server log only.
func logCheckError(ctx context.Context, tenantID, accountID, check string, err error) {
	logging.FromContext(ctx).Warn("aws integration check failed", "tenant_id", tenantID, "account_id", accountID, "check", check, "err", err)
}

func humanHours(d time.Duration) string {
	hours := int(d.Round(time.Hour) / time.Hour)
	switch {
	case hours <= 1:
		return "hour"
	case hours%24 == 0 && hours > 24:
		return fmt.Sprintf("%d days", hours/24)
	default:
		return fmt.Sprintf("%d hours", hours)
	}
}

func orDefault(value, fallback string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return fallback
}
