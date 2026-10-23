package v1

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"nhi-backend-api/internal/awsconnect"
	"nhi-backend-api/internal/githubcheck"
	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/repository"
)

const (
	awsTestTenant  = "11111111-1111-1111-1111-111111111111"
	awsTestRoleARN = "arn:aws:iam::222222222222:role/NHIPlatformCollector"
	awsTestSecret  = "arn:aws:secretsmanager:us-east-1:222222222222:secret:nhi/github-token-AbCdEf"
)

var awsTestCaller = AWSCaller{TenantID: awsTestTenant, Schema: "tenant_a", Actor: "alice@acme.test"}

// ── fakes ────────────────────────────────────────────────────────────────

type fakeAWSStore struct {
	mu       sync.Mutex
	settings *models.AWSTenantSettings
	accounts map[string]models.AWSAccountRecord // per account, single tenant
	claimed  bool
	saved    []repository.ExposureUpdate
}

func newFakeAWSStore() *fakeAWSStore {
	return &fakeAWSStore{
		settings: &models.AWSTenantSettings{
			Domain:       "acme.com",
			AWSAccountID: "999999999999",
			ExternalID:   "ext-0123456789abcdef0123456789abcdef",
			EventBusARN:  "arn:aws:events:ap-south-1:999999999999:event-bus/nhi-central-bus",
		},
		accounts: map[string]models.AWSAccountRecord{},
	}
}

func (f *fakeAWSStore) TenantSettings(context.Context, string) (*models.AWSTenantSettings, error) {
	return f.settings, nil
}

func (f *fakeAWSStore) ListAccounts(context.Context, string) ([]models.AWSAccountRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []models.AWSAccountRecord{}
	for _, rec := range f.accounts {
		out = append(out, rec)
	}
	return out, nil
}

func (f *fakeAWSStore) GetAccount(_ context.Context, _, accountID string) (*models.AWSAccountRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.accounts[accountID]
	if !ok {
		return nil, nil
	}
	return &rec, nil
}

func (f *fakeAWSStore) AccountClaimedByOther(context.Context, string, string) (bool, error) {
	return f.claimed, nil
}

func (f *fakeAWSStore) SaveVerification(_ context.Context, _, _ string, rec models.AWSAccountRecord) (*models.AWSAccountRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prev, existed := f.accounts[rec.AccountID]
	passed := rec.Status == models.AWSStatusConnected
	queue := passed && (!existed || prev.ConnectedAt == nil)
	if existed {
		rec.ConnectedAt = prev.ConnectedAt
	}
	rec.IsConnected = passed
	switch {
	case passed:
		rec.Status = models.AWSStatusConnected
		if rec.ConnectedAt == nil {
			t := rec.LastVerifiedAt
			rec.ConnectedAt = &t
		}
	case rec.ConnectedAt != nil:
		rec.Status = models.AWSStatusActionNeeded
	default:
		rec.Status = models.AWSStatusPending
	}
	rec.SyncStatus = models.AWSSyncIdle
	if queue {
		rec.SyncStatus = models.AWSSyncQueued
	}
	f.accounts[rec.AccountID] = rec
	return &rec, queue, nil
}

func (f *fakeAWSStore) UpdateRegions(_ context.Context, _, _, accountID string, regions []string) (*models.AWSAccountRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.accounts[accountID]
	if !ok {
		return nil, repository.ErrAWSAccountNotFound
	}
	rec.Regions = regions
	f.accounts[accountID] = rec
	return &rec, nil
}

func (f *fakeAWSStore) SaveExposure(_ context.Context, _, accountID string, u repository.ExposureUpdate) (*models.AWSAccountRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.accounts[accountID]
	if !ok {
		return nil, repository.ErrAWSAccountNotFound
	}
	f.saved = append(f.saved, u)
	if u.GitHub != nil {
		rec.GitHubConnected, rec.GitHubChecks, rec.GitHubSecretARN = u.GitHub.Connected, u.GitHub.Checks, u.GitHub.SecretARN
	}
	if u.CodeCommit != nil {
		rec.CodeCommitConnected, rec.CodeCommitChecks = u.CodeCommit.Connected, u.CodeCommit.Checks
	}
	f.accounts[accountID] = rec
	return &rec, nil
}

func (f *fakeAWSStore) RequestSync(_ context.Context, _, accountID, actor string, at time.Time) (*models.AWSAccountRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.accounts[accountID]
	switch {
	case !ok:
		return nil, false, repository.ErrAWSAccountNotFound
	case !rec.IsConnected:
		return &rec, false, repository.ErrAWSAccountNotConnected
	case rec.SyncStatus == models.AWSSyncQueued || rec.SyncStatus == models.AWSSyncRunning:
		return &rec, true, nil
	}
	rec.SyncStatus, rec.SyncRequestedBy, rec.SyncRequestedAt = models.AWSSyncQueued, actor, &at
	f.accounts[accountID] = rec
	return &rec, false, nil
}

func (f *fakeAWSStore) DeleteAccount(_ context.Context, _, _, accountID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.accounts[accountID]; !ok {
		return repository.ErrAWSAccountNotFound
	}
	delete(f.accounts, accountID)
	return nil
}

type fakeVerifier struct {
	mu          sync.Mutex
	assumeErr   error
	readErr     error
	busErr      error
	revoked     []string
	ccList      awsconnect.CodeCommitResult
	ccListErr   error
	ccReadErr   error
	ccRegions   []string
	secretToken string
	secretErr   error
	assumeGate  chan struct{} // when set, AssumeRole waits on it
}

func (v *fakeVerifier) AssumeRole(ctx context.Context, _ awsconnect.Platform, _ awsconnect.RoleARN, _ string) (*awsconnect.Session, error) {
	if v.assumeGate != nil {
		select {
		case <-v.assumeGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if v.assumeErr != nil {
		return nil, v.assumeErr
	}
	return &awsconnect.Session{}, nil
}
func (v *fakeVerifier) TestRead(context.Context, *awsconnect.Session) error { return v.readErr }
func (v *fakeVerifier) AllowAccountOnBus(context.Context, awsconnect.Platform, string) error {
	return v.busErr
}
func (v *fakeVerifier) RevokeAccountOnBus(_ context.Context, _ awsconnect.Platform, id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.revoked = append(v.revoked, id)
	return nil
}
func (v *fakeVerifier) ListCodeCommit(_ context.Context, _ *awsconnect.Session, regions []string) (awsconnect.CodeCommitResult, error) {
	v.ccRegions = regions
	return v.ccList, v.ccListErr
}
func (v *fakeVerifier) ProbeCodeCommit(_ context.Context, _ *awsconnect.Session, region, repo string) awsconnect.RepoProbe {
	p := awsconnect.RepoProbe{Region: region, Repository: repo, Branch: "main", Files: 2}
	if v.ccReadErr != nil {
		p.Denied = []string{"codecommit:GetBlob"}
	}
	return p
}
func (v *fakeVerifier) ReadSecretToken(context.Context, *awsconnect.Session, awsconnect.SecretARN) (string, error) {
	return v.secretToken, v.secretErr
}

type fakeEvents struct {
	found bool
	err   error
}

func (e fakeEvents) HasEventsFrom(context.Context, awsconnect.Platform, string, time.Time) (bool, error) {
	return e.found, e.err
}

type fakeGitHub struct {
	res   githubcheck.Result
	err   error
	token string
}

func (g *fakeGitHub) Check(_ context.Context, token string) (githubcheck.Result, error) {
	g.token = token
	return g.res, g.err
}

func newTestService(store *fakeAWSStore, v *fakeVerifier, gh *fakeGitHub, events AWSEventSource) *AWSIntegrationService {
	var verifier AWSVerifier
	if v != nil {
		verifier = v
	}
	var github GitHubChecker
	if gh != nil {
		github = gh
	}
	return NewAWSIntegrationService(store, verifier, github, events, AWSIntegrationOptions{})
}

func states(checks []models.AWSCheck) string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.State
	}
	return strings.Join(out, ",")
}

// connectedAccount puts a connected account in the store.
func connectedAccount(store *fakeAWSStore) {
	at := time.Now().UTC()
	store.accounts["222222222222"] = models.AWSAccountRecord{
		AccountID: "222222222222", RoleARN: awsTestRoleARN, Regions: []string{"us-east-1", "eu-west-1"},
		Status: models.AWSStatusConnected, IsConnected: true, ConnectedAt: &at,
	}
}

// ── verify ───────────────────────────────────────────────────────────────

func TestVerifyConnectsAndQueuesFirstSync(t *testing.T) {
	store := newFakeAWSStore()
	svc := newTestService(store, &fakeVerifier{}, nil, fakeEvents{found: true})
	res, err := svc.Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"ap-south-1", "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != models.AWSStatusConnected || !res.Saved || !res.SyncQueued || states(res.Checks) != "pass,pass,pass" {
		t.Fatalf("got %+v", res)
	}
	if got := strings.Join(res.Regions, ","); got != "us-east-1,ap-south-1" {
		t.Fatalf("regions in catalogue order: %s", got)
	}
	// Verified again: connected, no second queue.
	res, _ = svc.Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"})
	if res.SyncQueued {
		t.Fatal("second verify queued again")
	}
}

func TestVerifyOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		v      *fakeVerifier
		events fakeEvents
		want   string
		saved  bool
		fix    string
	}{
		{"assume denied", &fakeVerifier{assumeErr: awsconnect.ErrAccessDenied}, fakeEvents{found: true}, "fail,skipped,skipped", false, "Check the External ID matches"},
		{"assume unavailable", &fakeVerifier{assumeErr: errors.New("dial")}, fakeEvents{found: true}, "error,skipped,skipped", false, ""},
		{"no read", &fakeVerifier{readErr: awsconnect.ErrAccessDenied}, fakeEvents{found: true}, "pass,fail,pass", true, "Attach SecurityAudit + ViewOnlyAccess"},
		{"no events", &fakeVerifier{}, fakeEvents{}, "pass,pass,fail", true, "Check the EventBridge rule target ARN"},
		{"bus error", &fakeVerifier{busErr: errors.New("throttled")}, fakeEvents{found: true}, "pass,pass,error", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeAWSStore()
			res, err := newTestService(store, tc.v, nil, tc.events).Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			if states(res.Checks) != tc.want || res.Saved != tc.saved || res.Status == models.AWSStatusConnected {
				t.Fatalf("got %s saved=%v status=%s", states(res.Checks), res.Saved, res.Status)
			}
			if tc.saved && res.Status != models.AWSStatusPending {
				t.Fatalf("never connected should be pending, got %s", res.Status)
			}
			if tc.fix != "" && !strings.Contains(fmtChecks(res.Checks), tc.fix) {
				t.Fatalf("missing fix %q in %+v", tc.fix, res.Checks)
			}
			if !tc.saved && len(store.accounts) != 0 {
				t.Fatal("saved an account whose role could not be assumed")
			}
		})
	}
}

func fmtChecks(c []models.AWSCheck) string {
	var b strings.Builder
	for _, x := range c {
		b.WriteString(x.Fix + "|")
	}
	return b.String()
}

func TestVerifyFailingAfterConnectIsActionNeeded(t *testing.T) {
	store := newFakeAWSStore()
	connectedAccount(store)
	res, err := newTestService(store, &fakeVerifier{}, nil, fakeEvents{}).Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != models.AWSStatusActionNeeded {
		t.Fatalf("got %s", res.Status)
	}
}

func TestVerifyRejectsBadInput(t *testing.T) {
	store := newFakeAWSStore()
	svc := newTestService(store, &fakeVerifier{}, nil, fakeEvents{found: true})
	var arnErr *awsconnect.ErrInvalidRoleARN
	if _, err := svc.Verify(context.Background(), awsTestCaller, "arn:aws:iam::999999999999:role/X", []string{"us-east-1"}); !errors.As(err, &arnErr) {
		t.Fatalf("our own account: %v", err)
	}
	var regErr *awsconnect.ErrInvalidRegions
	if _, err := svc.Verify(context.Background(), awsTestCaller, awsTestRoleARN, nil); !errors.As(err, &regErr) {
		t.Fatalf("no regions: %v", err)
	}
	store.settings.Regions = []string{"us-east-1"}
	if _, err := svc.Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"eu-west-1"}); !errors.As(err, &regErr) {
		t.Fatalf("region outside collector_regions: %v", err)
	}
	store.claimed = true
	store.settings.Regions = nil
	if _, err := svc.Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"}); !errors.Is(err, ErrAWSAccountClaimed) {
		t.Fatalf("claimed: %v", err)
	}
}

func TestVerifyNeedsTenantValuesAndAWS(t *testing.T) {
	store := newFakeAWSStore()
	store.settings.EventBusARN = ""
	if _, err := newTestService(store, &fakeVerifier{}, nil, nil).Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"}); !errors.Is(err, ErrAWSTenantNotConfigured) {
		t.Fatalf("got %v", err)
	}
	if _, err := newTestService(newFakeAWSStore(), nil, nil, nil).Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"}); !errors.Is(err, ErrAWSUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestOneCheckPerTenantAtATime(t *testing.T) {
	gate := make(chan struct{})
	store := newFakeAWSStore()
	svc := newTestService(store, &fakeVerifier{assumeGate: gate}, nil, fakeEvents{found: true})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, busy := svc.running.Load(awsTestTenant); busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first check never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := svc.Verify(context.Background(), awsTestCaller, awsTestRoleARN, []string{"us-east-1"}); !errors.Is(err, ErrAWSVerifyInProgress) {
		t.Fatalf("second check: %v", err)
	}
	close(gate)
	<-done
}

// ── get ──────────────────────────────────────────────────────────────────

func TestGetFlagsAndSetup(t *testing.T) {
	store := newFakeAWSStore()
	connectedAccount(store)
	store.settings.Regions = []string{"us-east-1", "eu-west-1"}
	svc := newTestService(store, &fakeVerifier{}, nil, nil)
	res, err := svc.Get(context.Background(), awsTestCaller, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasConnectedAccount || res.ExposureEnabled || res.Status != models.AWSStatusConnected || res.Workspace.Domain != "acme.com" {
		t.Fatalf("got %+v", res)
	}
	if res.Setup == nil || len(res.Setup.Regions) != 2 || len(res.Setup.Exposure.CodeCommit.Actions) != 4 || res.Setup.Exposure.GitHub.SecretPrefix != "nhi/github" {
		t.Fatalf("setup %+v", res.Setup)
	}
	rec := store.accounts["222222222222"]
	rec.CodeCommitConnected = true
	store.accounts["222222222222"] = rec
	res, _ = svc.Get(context.Background(), awsTestCaller, false)
	if !res.ExposureEnabled || res.Setup != nil {
		t.Fatalf("non-admin: %+v", res)
	}
}

// ── exposure ─────────────────────────────────────────────────────────────

func TestExposureCodeCommit(t *testing.T) {
	cases := []struct {
		name      string
		v         *fakeVerifier
		connected bool
		want      string
		fix       string
	}{
		{"enabled", &fakeVerifier{ccList: awsconnect.CodeCommitResult{Repositories: 3, Regions: []string{"us-east-1"}, SampleRepository: "api", SampleRegion: "us-east-1", Repos: []awsconnect.RepoRef{{Region: "us-east-1", Name: "api"}}}}, true, "pass,pass", ""},
		{"no repositories yet", &fakeVerifier{ccList: awsconnect.CodeCommitResult{Regions: []string{"us-east-1"}}}, true, "pass,skipped", ""},
		{"cannot list", &fakeVerifier{ccListErr: awsconnect.ErrAccessDenied}, false, "fail,skipped", "Keep ViewOnlyAccess attached to the role: it allows codecommit:ListRepositories"},
		{"list only", &fakeVerifier{ccList: awsconnect.CodeCommitResult{Repositories: 1, Regions: []string{"us-east-1"}, SampleRepository: "api", SampleRegion: "us-east-1", Repos: []awsconnect.RepoRef{{Region: "us-east-1", Name: "api"}}}, ccReadErr: awsconnect.ErrAccessDenied}, false, "pass,fail", "Add codecommit:GetBlob for those repositories (the policy is on the setup page)"},
		{"role gone", &fakeVerifier{assumeErr: awsconnect.ErrAccessDenied}, false, "fail,skipped", "Re-check the account connection"},
		{"aws error", &fakeVerifier{ccListErr: errors.New("timeout")}, false, "error,skipped", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeAWSStore()
			connectedAccount(store)
			res, err := newTestService(store, tc.v, nil, nil).SetExposure(context.Background(), awsTestCaller, "222222222222", models.AWSExposureRequest{CodeCommit: true})
			if err != nil {
				t.Fatal(err)
			}
			cc := res.Account.Exposure.CodeCommit
			if cc.Connected != tc.connected || states(cc.Checks) != tc.want || res.ExposureEnabled != tc.connected {
				t.Fatalf("got connected=%v %s enabled=%v", cc.Connected, states(cc.Checks), res.ExposureEnabled)
			}
			if tc.fix != "" && !strings.Contains(fmtChecks(cc.Checks), tc.fix) {
				t.Fatalf("missing fix %q: %+v", tc.fix, cc.Checks)
			}
			if tc.name == "enabled" && strings.Join(tc.v.ccRegions, ",") != "us-east-1,eu-west-1" {
				t.Fatalf("checked regions %v, want the account's", tc.v.ccRegions)
			}
		})
	}
}

func TestExposureGitHub(t *testing.T) {
	cases := []struct {
		name      string
		v         *fakeVerifier
		gh        *fakeGitHub
		connected bool
		want      string
		fix       string
	}{
		{"enabled", &fakeVerifier{secretToken: "ghp_T0kenValue9f8e"}, &fakeGitHub{res: githubcheck.Result{Login: "bot", Repositories: 4}}, true, "pass,pass", ""},
		{"secret missing", &fakeVerifier{secretErr: awsconnect.ErrSecretNotFound}, &fakeGitHub{}, false, "fail,skipped", "Check the secret ARN"},
		{"secret denied", &fakeVerifier{secretErr: awsconnect.ErrAccessDenied}, &fakeGitHub{}, false, "fail,skipped", "Add the secret read policy to the role"},
		{"kms denied", &fakeVerifier{secretErr: awsconnect.ErrKMSDenied}, &fakeGitHub{}, false, "fail,skipped", "Allow kms:Decrypt on the secret's key"},
		{"empty secret", &fakeVerifier{secretErr: awsconnect.ErrSecretEmpty}, &fakeGitHub{}, false, "fail,skipped", "Store the GitHub token as the secret value"},
		{"token rejected", &fakeVerifier{secretToken: "ghp_T0kenValue9f8e"}, &fakeGitHub{err: githubcheck.ErrTokenRejected}, false, "pass,fail", "Create a new token and update the secret"},
		{"no repositories", &fakeVerifier{secretToken: "ghp_T0kenValue9f8e"}, &fakeGitHub{err: githubcheck.ErrNoRepositories}, false, "pass,fail", "Give the token access to your repositories"},
		{"no contents", &fakeVerifier{secretToken: "ghp_T0kenValue9f8e"}, &fakeGitHub{err: githubcheck.ErrNoContents}, false, "pass,fail", "Give the token Contents: Read-only"},
		{"github down", &fakeVerifier{secretToken: "ghp_T0kenValue9f8e"}, &fakeGitHub{err: githubcheck.ErrUnavailable}, false, "pass,error", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeAWSStore()
			connectedAccount(store)
			res, err := newTestService(store, tc.v, tc.gh, nil).SetExposure(context.Background(), awsTestCaller, "222222222222",
				models.AWSExposureRequest{GitHub: true, GitHubSecretARN: awsTestSecret})
			if err != nil {
				t.Fatal(err)
			}
			gh := res.Account.Exposure.GitHub
			if gh.Connected != tc.connected || states(gh.Checks) != tc.want || gh.SecretARN != awsTestSecret {
				t.Fatalf("got connected=%v %s arn=%q", gh.Connected, states(gh.Checks), gh.SecretARN)
			}
			if tc.fix != "" && !strings.Contains(fmtChecks(gh.Checks), tc.fix) {
				t.Fatalf("missing fix %q: %+v", tc.fix, gh.Checks)
			}
			for _, c := range gh.Checks {
				if strings.Contains(c.Message, tc.v.secretToken) && tc.v.secretToken != "" {
					t.Fatalf("token leaked into a message: %q", c.Message)
				}
			}
		})
	}
}

func TestExposurePreconditionsAndTurningOff(t *testing.T) {
	store := newFakeAWSStore()
	svc := newTestService(store, &fakeVerifier{}, &fakeGitHub{}, nil)
	if _, err := svc.SetExposure(context.Background(), awsTestCaller, "222222222222", models.AWSExposureRequest{CodeCommit: true}); !errors.Is(err, repository.ErrAWSAccountNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
	store.accounts["222222222222"] = models.AWSAccountRecord{AccountID: "222222222222", RoleARN: awsTestRoleARN, Status: models.AWSStatusPending}
	if _, err := svc.SetExposure(context.Background(), awsTestCaller, "222222222222", models.AWSExposureRequest{CodeCommit: true}); !errors.Is(err, repository.ErrAWSAccountNotConnected) {
		t.Fatalf("never connected: %v", err)
	}
	connectedAccount(store)
	var secretErr *awsconnect.ErrInvalidSecretARN
	for _, arn := range []string{"", "nope", "arn:aws:secretsmanager:us-east-1:333333333333:secret:nhi/github-x", "arn:aws:secretsmanager:us-east-1:222222222222:secret:other-x"} {
		if _, err := svc.SetExposure(context.Background(), awsTestCaller, "222222222222", models.AWSExposureRequest{GitHub: true, GitHubSecretARN: arn}); !errors.As(err, &secretErr) {
			t.Fatalf("secret %q: %v", arn, err)
		}
	}
	// Turning everything off needs no AWS call and clears both platforms.
	rec := store.accounts["222222222222"]
	rec.GitHubConnected, rec.CodeCommitConnected = true, true
	store.accounts["222222222222"] = rec
	off := newTestService(store, nil, nil, nil)
	res, err := off.SetExposure(context.Background(), awsTestCaller, "222222222222", models.AWSExposureRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExposureEnabled || res.Account.Exposure.GitHub.Connected || res.Account.Exposure.CodeCommit.Connected {
		t.Fatalf("got %+v", res)
	}
}

// ── regions, sync, remove ────────────────────────────────────────────────

func TestUpdateRegionsSyncRemove(t *testing.T) {
	store := newFakeAWSStore()
	connectedAccount(store)
	v := &fakeVerifier{}
	svc := newTestService(store, v, nil, nil)
	acc, err := svc.UpdateRegions(context.Background(), awsTestCaller, "222222222222", []string{"eu-west-1", "us-east-1", "us-east-1"})
	if err != nil || strings.Join(acc.Regions, ",") != "us-east-1,eu-west-1" {
		t.Fatalf("regions: %v %v", acc, err)
	}
	res, err := svc.RequestSync(context.Background(), awsTestCaller, "222222222222")
	if err != nil || res.AlreadyQueued || res.Account.Sync.Status != models.AWSSyncQueued || res.Account.Sync.RequestedBy != "alice@acme.test" {
		t.Fatalf("sync: %+v %v", res, err)
	}
	if res, _ := svc.RequestSync(context.Background(), awsTestCaller, "222222222222"); !res.AlreadyQueued {
		t.Fatal("second sync should report already queued")
	}
	if err := svc.RemoveAccount(context.Background(), awsTestCaller, "222222222222"); err != nil {
		t.Fatal(err)
	}
	if len(v.revoked) != 1 || v.revoked[0] != "222222222222" {
		t.Fatalf("bus permission not revoked: %v", v.revoked)
	}
	if err := svc.RemoveAccount(context.Background(), awsTestCaller, "222222222222"); !errors.Is(err, repository.ErrAWSAccountNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
}
