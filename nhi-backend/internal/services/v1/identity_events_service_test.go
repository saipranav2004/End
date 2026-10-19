package v1

import (
	"context"
	"errors"
	"testing"
	"time"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
)

const (
	evHome  = "111111111111"
	evOther = "222222222222"
	evRole  = "arn:aws:iam::111111111111:role/app-role"
	evKeyID = "AKIAEXAMPLEKEY12345"
)

// fakeEventStore records the subject/scope/filter each call received.
type fakeEventStore struct {
	events      []chrepo.Event
	total       uint64
	cred        []chrepo.CredentialUsageRow
	resources   []chrepo.ResourceRow
	heat        []chrepo.HeatCell
	sessionRows []chrepo.SessionEvent
	truncated   bool
	errSummary  *chrepo.ErrorSummaryResult
	cross       []chrepo.CrossAccountRow
	ips         []chrepo.IPRow
	err         error

	gotSub    chrepo.Subject
	gotScope  chrepo.Scope
	gotFilter chrepo.EventFilter
	gotHome   string
	gotLimit  int
	gotOffset int
}

func (f *fakeEventStore) rec(sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) {
	f.gotSub, f.gotScope, f.gotFilter = sub, sc, fl
}
func (f *fakeEventStore) EventsPage(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter, limit, offset int) ([]chrepo.Event, error) {
	f.rec(sub, sc, fl)
	f.gotLimit, f.gotOffset = limit, offset
	return f.events, f.err
}
func (f *fakeEventStore) CountFiltered(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) (uint64, error) {
	f.rec(sub, sc, fl)
	return f.total, f.err
}
func (f *fakeEventStore) EventByID(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, id string) (*chrepo.Event, error) {
	f.rec(sub, sc, chrepo.EventFilter{})
	if f.err != nil {
		return nil, f.err
	}
	for i := range f.events {
		if f.events[i].EventID == id {
			return &f.events[i], nil
		}
	}
	return nil, nil
}
func (f *fakeEventStore) CredentialUsage(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) ([]chrepo.CredentialUsageRow, error) {
	f.rec(sub, sc, fl)
	return f.cred, f.err
}
func (f *fakeEventStore) ResourceAccess(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) ([]chrepo.ResourceRow, uint64, error) {
	f.rec(sub, sc, fl)
	return f.resources, uint64(len(f.resources)), f.err
}
func (f *fakeEventStore) Heatmap(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) ([]chrepo.HeatCell, error) {
	f.rec(sub, sc, fl)
	return f.heat, f.err
}
func (f *fakeEventStore) SessionEvents(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) ([]chrepo.SessionEvent, bool, error) {
	f.rec(sub, sc, fl)
	return f.sessionRows, f.truncated, f.err
}
func (f *fakeEventStore) ErrorSummary(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) (*chrepo.ErrorSummaryResult, error) {
	f.rec(sub, sc, fl)
	return f.errSummary, f.err
}
func (f *fakeEventStore) CrossAccount(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter, home string) ([]chrepo.CrossAccountRow, uint64, error) {
	f.rec(sub, sc, fl)
	f.gotHome = home
	return f.cross, uint64(len(f.cross)), f.err
}
func (f *fakeEventStore) IPStats(_ context.Context, sub chrepo.Subject, sc chrepo.Scope, fl chrepo.EventFilter) ([]chrepo.IPRow, uint64, error) {
	f.rec(sub, sc, fl)
	return f.ips, uint64(len(f.ips)), f.err
}

func testEnv(store eventStore, section string) *eventsEnv {
	tr, _ := ResolveEventsRange("24h", "", "")
	return &eventsEnv{
		store: store, accounts: []string{evHome},
		ident: &repov1.EventsContext{
			ID: "id-1", ARN: "arn:aws:iam::111111111111:role/web", Name: "web", IdentityType: "ec2_instance", AccountID: evHome,
			Credentials: []repov1.EventsCredential{
				{ID: "c-role", ARN: evRole, Name: "app-role", Type: "iam_role", BindingType: "instance_profile"},
				{ID: "c-key", ARN: "arn:aws:iam::111111111111:user/svc", Name: evKeyID, Type: "access_key", BindingType: "access_key"},
			},
		},
		q:  models.IdentityEventsQuery{Section: section, Page: 1, PageSize: 100, Result: "all", ReadWrite: "all"},
		tr: tr,
	}
}

func sampleEvent(id string, t time.Time) chrepo.Event {
	return chrepo.Event{
		EventID: id, EventTime: t, EventName: "GetObject", EventSource: "s3.amazonaws.com", Region: "us-east-1",
		SourceIP: "10.0.1.42", ResourceARN: "arn:aws:s3:::web-assets/logo.png", ResourceType: "AWS::S3::Object",
		IsReadOnly: true, CallerARN: "arn:aws:sts::111111111111:assumed-role/app-role/i-0abc", CallerType: "AssumedRole",
		SessionIssuerARN: evRole,
	}
}

func TestSubjectAndScope(t *testing.T) {
	e := testEnv(&fakeEventStore{}, "timeline")
	e.index()
	keys := e.subject().Keys
	for _, want := range []string{e.ident.ARN, evRole, evKeyID} {
		found := false
		for _, k := range keys {
			found = found || k == want
		}
		if !found {
			t.Errorf("subject keys %v missing %q", keys, want)
		}
	}
	// A credential filter that is not this identity's must match nothing.
	e.q.Credential = "arn:aws:iam::999999999999:role/stranger"
	if len(e.subject().Keys) != 0 {
		t.Fatal("foreign credential filter must yield an empty subject")
	}
	e.q.Credential = evRole
	if len(e.subject().Keys) == 0 {
		t.Fatal("own credential filter must keep the subject")
	}
}

func TestTimeline(t *testing.T) {
	ts := time.Date(2026, 10, 10, 14, 23, 1, 0, time.UTC)
	fake := &fakeEventStore{events: []chrepo.Event{sampleEvent("e1", ts)}, total: 1240}
	e := testEnv(fake, "timeline")
	e.q.Page, e.q.PageSize = 3, 100
	e.q.Result, e.q.ReadWrite, e.q.Service = "error", "write", "s3"
	resp, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := resp.(*models.IdentityEventsTimelineResponse)
	if !r.DataAvailable || r.TotalEvents != 1240 || r.Pagination.TotalPages != 13 || r.Pagination.Page != 3 {
		t.Fatalf("unexpected timeline: %+v", r)
	}
	ev := r.Events[0]
	if ev.EventTime != "2026-10-10T14:23:01Z" || ev.Service != "s3" || ev.ResourceName != "web-assets/logo.png" ||
		ev.Status != "success" || ev.CredentialName != "app-role" || ev.BindingType != "instance_profile" || ev.IsCrossAccount {
		t.Fatalf("unexpected event enrichment: %+v", ev)
	}
	if fake.gotOffset != 200 || fake.gotLimit != 100 {
		t.Fatalf("paging offset/limit = %d/%d, want 200/100", fake.gotOffset, fake.gotLimit)
	}
	if fake.gotScope.Service != "s3" || len(fake.gotScope.Accounts) != 1 || fake.gotScope.Accounts[0] != evHome {
		t.Fatalf("scope = %+v", fake.gotScope)
	}
	if fake.gotFilter.Result != "error" || fake.gotFilter.ReadWrite != "write" {
		t.Fatalf("filter = %+v", fake.gotFilter)
	}
}

func TestTimelineDegraded(t *testing.T) {
	// ClickHouse disabled.
	resp, _ := testEnv(nil, "timeline").run(context.Background())
	r := resp.(*models.IdentityEventsTimelineResponse)
	if r.DataAvailable || r.Events == nil || len(r.Events) != 0 {
		t.Fatalf("disabled store: %+v", r)
	}
	// ClickHouse failing.
	resp, err := testEnv(&fakeEventStore{err: errors.New("boom")}, "timeline").run(context.Background())
	if err != nil {
		t.Fatalf("a ClickHouse failure must not be an error: %v", err)
	}
	if r := resp.(*models.IdentityEventsTimelineResponse); r.DataAvailable || len(r.Events) != 0 {
		t.Fatalf("failing store: %+v", r)
	}
}

func TestDetail(t *testing.T) {
	ts := time.Date(2026, 10, 10, 14, 23, 1, 0, time.UTC)
	fake := &fakeEventStore{events: []chrepo.Event{sampleEvent("e1", ts)}, total: 180}
	e := testEnv(fake, "detail")
	e.q.EventID = "e1"
	var gotCred, gotSvc, gotName string
	e.grant = func(_ context.Context, credID, svc, name string) (*models.PermissionSourceResponse, error) {
		gotCred, gotSvc, gotName = credID, svc, name
		p, ty, ef, rs := "AmazonS3FullAccess", "managed", "Allow", "*"
		return &models.PermissionSourceResponse{Matched: true, PolicySource: &p, PolicyType: &ty, Effect: &ef, ResourceARN: &rs}, nil
	}
	resp, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := resp.(*models.IdentityEventsDetailResponse)
	if r.Event.EventID != "e1" || r.PermissionSource == nil || *r.PermissionSource.PolicyName != "AmazonS3FullAccess" ||
		r.PermissionSource.UsedCount90d != 180 {
		t.Fatalf("detail = %+v perm=%+v", r, r.PermissionSource)
	}
	if gotCred != "c-role" || gotSvc != "s3" || gotName != "GetObject" {
		t.Fatalf("grant lookup got %q %q %q", gotCred, gotSvc, gotName)
	}

	// Unknown / foreign event id → not found, and no permission lookup.
	e2 := testEnv(fake, "detail")
	e2.q.EventID = "someone-elses"
	called := false
	e2.grant = func(context.Context, string, string, string) (*models.PermissionSourceResponse, error) {
		called = true
		return nil, nil
	}
	if _, err := e2.run(context.Background()); !errors.Is(err, ErrEventNotFound) || called {
		t.Fatalf("foreign event id: err=%v grantCalled=%v", err, called)
	}

	// Unmatched credential → permission_source null, not an error.
	ev := sampleEvent("e2", ts)
	ev.SessionIssuerARN, ev.CallerARN = "arn:aws:iam::333333333333:role/unknown", "arn:aws:sts::333333333333:assumed-role/unknown/s"
	e3 := testEnv(&fakeEventStore{events: []chrepo.Event{ev}}, "detail")
	e3.q.EventID = "e2"
	resp, err = e3.run(context.Background())
	if err != nil || resp.(*models.IdentityEventsDetailResponse).PermissionSource != nil {
		t.Fatalf("unmatched credential: err=%v resp=%+v", err, resp)
	}
}

func TestCredentialsIncludesUnknownIssuers(t *testing.T) {
	ts := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	fake := &fakeEventStore{cred: []chrepo.CredentialUsageRow{
		{Key: evRole, Kind: "issuer", Count: 1240, First: ts, Last: ts.Add(time.Hour), Actions: []string{"GetObject"}, UniqueResources: 5},
		{Key: evKeyID, Kind: "access_key", Count: 3, First: ts, Last: ts, Actions: nil},
		{Key: "arn:aws:iam::987654321098:role/cross-account-reader", Kind: "issuer", Count: 45, First: ts, Last: ts},
	}}
	resp, _ := testEnv(fake, "credentials").run(context.Background())
	r := resp.(*models.IdentityEventsCredentialsResponse)
	if len(r.CredentialsUsed) != 3 {
		t.Fatalf("got %d credentials, want 3: %+v", len(r.CredentialsUsed), r.CredentialsUsed)
	}
	known, key, unknown := r.CredentialsUsed[0], r.CredentialsUsed[1], r.CredentialsUsed[2]
	if known.CredentialName != "app-role" || known.BindingType != "instance_profile" || known.FirstUsed != "2026-10-01T08:00:00Z" {
		t.Fatalf("known credential = %+v", known)
	}
	if key.CredentialType != "access_key" || key.CredentialARN != "arn:aws:iam::111111111111:user/svc" || key.ActionsPerformed == nil {
		t.Fatalf("access key credential = %+v", key)
	}
	if unknown.CredentialARN != "arn:aws:iam::987654321098:role/cross-account-reader" || unknown.CredentialName != "" ||
		unknown.CredentialType != "" || unknown.BindingType != "" {
		t.Fatalf("unknown issuer must be listed with blank name/type/binding: %+v", unknown)
	}
}

func TestResourcesAndCrossAccount(t *testing.T) {
	ts := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	fake := &fakeEventStore{resources: []chrepo.ResourceRow{
		{ARN: "arn:aws:s3:::web-assets-prod", Type: "AWS::S3::Bucket", Reads: 890, Errors: 2, First: ts, Last: ts},
		{ARN: "arn:aws:dynamodb:us-east-1:222222222222:table/customer-data", Type: "AWS::DynamoDB::Table", Reads: 56, First: ts, Last: ts},
	}}
	resp, _ := testEnv(fake, "resources").run(context.Background())
	r := resp.(*models.IdentityEventsResourcesResponse)
	if r.TotalResources != 2 || r.ResourcesAccessed[0].IsCrossAccount || r.ResourcesAccessed[0].AccountID != "" {
		t.Fatalf("S3 bucket must not be cross-account: %+v", r.ResourcesAccessed[0])
	}
	d := r.ResourcesAccessed[1]
	if !d.IsCrossAccount || d.AccountID != evOther || d.ResourceName != "customer-data" {
		t.Fatalf("dynamodb resource = %+v", d)
	}

	fake2 := &fakeEventStore{cross: []chrepo.CrossAccountRow{{TargetAccount: evOther, ResourceARN: "arn:aws:s3:::shared", Count: 45, LastSeen: ts}}}
	resp, _ = testEnv(fake2, "cross_account").run(context.Background())
	c := resp.(*models.IdentityEventsCrossAccountResponse)
	if fake2.gotHome != evHome || c.IdentityAccount != evHome || c.TotalCrossAccountEvents != 1 || c.CrossAccountActivity[0].TargetResourceName != "shared" {
		t.Fatalf("cross account = %+v (home %q)", c, fake2.gotHome)
	}
}

func TestHeatmapErrorsAndIPs(t *testing.T) {
	resp, _ := testEnv(&fakeEventStore{heat: []chrepo.HeatCell{{Action: "GetObject", Hour: 9, Count: 180}}}, "heatmap").run(context.Background())
	if h := resp.(*models.IdentityEventsHeatmapResponse); len(h.Heatmap) != 1 || h.Heatmap[0].Hour != 9 || h.Heatmap[0].Count != 180 {
		t.Fatalf("heatmap = %+v", h)
	}

	ts := time.Date(2026, 10, 10, 14, 20, 12, 0, time.UTC)
	bad := sampleEvent("x", ts)
	bad.ErrorCode, bad.ErrorMessage, bad.EventName = "AccessDenied", "nope", "DeleteBucket"
	fake := &fakeEventStore{
		events: []chrepo.Event{bad},
		errSummary: &chrepo.ErrorSummaryResult{
			TotalErrors: 15, ByCode: []chrepo.ErrorCount{{Code: "AccessDenied", Count: 12}, {Code: "Throttling", Count: 2}},
			MostDeniedAct: &chrepo.DeniedAction{EventSource: "s3.amazonaws.com", EventName: "DeleteObject", Count: 5},
			MostDeniedRes: &chrepo.DeniedResource{ARN: "arn:aws:s3:::restricted-bucket", Count: 8},
		},
	}
	e := testEnv(fake, "errors")
	e.q.Result, e.q.ReadWrite = "success", "read" // not applied to the errors section
	resp, _ = e.run(context.Background())
	er := resp.(*models.IdentityEventsErrorsResponse)
	if er.Summary.TotalErrors != 15 || er.Summary.ByErrorCode["AccessDenied"] != 12 ||
		er.Summary.MostDeniedAction.Action != "s3:DeleteObject" || er.Summary.MostDeniedResource.ResourceName != "restricted-bucket" ||
		er.Pagination.TotalCount != 15 || len(er.Events) != 1 || er.Events[0].ErrorCode != "AccessDenied" {
		t.Fatalf("errors = %+v", er)
	}
	if fake.gotFilter.Result != "error" || fake.gotFilter.ReadWrite != "" {
		t.Fatalf("errors section must force result=error and ignore read_write: %+v", fake.gotFilter)
	}

	ips := &fakeEventStore{ips: []chrepo.IPRow{
		{IP: "10.0.1.42", Count: 12450}, {IP: "203.0.113.5", Count: 3}, {IP: "resource-explorer-2.amazonaws.com", Count: 9},
	}}
	resp, _ = testEnv(ips, "ips").run(context.Background())
	ir := resp.(*models.IdentityEventsIPsResponse)
	kinds := []string{ir.IPs[0].Kind, ir.IPs[1].Kind, ir.IPs[2].Kind}
	if !ir.IPs[0].IsInternal || ir.IPs[1].IsInternal || ir.IPs[2].IsInternal || kinds[0] != "private" || kinds[1] != "public" || kinds[2] != "aws_service" {
		t.Fatalf("ips = %+v", ir.IPs)
	}
	if ir.IPs[0].GeoLocation != nil {
		t.Fatal("geo_location must stay null")
	}
}

func TestBuildSessions(t *testing.T) {
	base := time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC)
	caller := "arn:aws:sts::111111111111:assumed-role/app-role/i-0abc"
	other := "arn:aws:sts::111111111111:assumed-role/app-role/i-0def"
	rows := []chrepo.SessionEvent{ // ordered by caller, then time
		{CallerARN: caller, SessionIssuerARN: evRole, EventTime: base, EventName: "GetObject", EventSource: "s3.amazonaws.com", SourceIP: "10.0.1.42"},
		{CallerARN: caller, SessionIssuerARN: evRole, EventTime: base.Add(30 * time.Minute), EventName: "PutObject", EventSource: "s3.amazonaws.com", SourceIP: "10.0.1.42"},
		{CallerARN: caller, SessionIssuerARN: evRole, EventTime: base.Add(30*time.Minute + 31*time.Minute), EventName: "GetParameter", EventSource: "ssm.amazonaws.com", SourceIP: "10.0.1.43"},
		{CallerARN: other, EventTime: base.Add(time.Hour), EventName: "Decrypt", EventSource: "kms.amazonaws.com"},
	}
	e := testEnv(nil, "sessions")
	e.index()
	got := buildSessions(rows, e)
	if len(got) != 3 {
		t.Fatalf("got %d sessions, want 3 (exactly 30 min stays in one session, 31 min splits): %+v", len(got), got)
	}
	// Newest first.
	if got[0].StartTime != "2026-10-10T15:01:00Z" || got[1].SessionName != "i-0def" || got[2].StartTime != "2026-10-10T14:00:00Z" {
		t.Fatalf("order/names = %+v", got)
	}
	first := got[2]
	if first.EventCount != 2 || first.DurationMinutes != 30 || first.CredentialName != "app-role" ||
		len(first.Actions) != 2 || first.Services[0] != "s3" || first.SourceIPs[0] != "10.0.1.42" {
		t.Fatalf("first session = %+v", first)
	}

	fake := &fakeEventStore{sessionRows: rows, truncated: true}
	resp, _ := testEnv(fake, "sessions").run(context.Background())
	if sr := resp.(*models.IdentityEventsSessionsResponse); !sr.Truncated || sr.TotalSessions != 3 {
		t.Fatalf("sessions = %+v", sr)
	}
}

func TestEventHelpers(t *testing.T) {
	for arn, want := range map[string]string{
		"arn:aws:iam::123456789012:role/x":         "123456789012",
		"arn:aws:s3:::bucket/key":                  "",
		"arn:aws:dynamodb:us-east-1:12345:table/t": "",
		"resource-explorer-2.amazonaws.com":        "",
		"":                                         "",
		"arn:aws:iam::12345678901a:role/x":         "",
	} {
		if got := arnAccount(arn); got != want {
			t.Errorf("arnAccount(%q) = %q, want %q", arn, got, want)
		}
	}
	for arn, want := range map[string]string{
		"arn:aws:s3:::web-assets-prod/logo.png":                       "web-assets-prod/logo.png",
		"arn:aws:dynamodb:us-east-1:987654321098:table/customer-data": "customer-data",
		"arn:aws:iam::1:role/aws-service-role/x.amazonaws.com/MyRole": "MyRole",
		"arn:aws:lambda:us-east-1:111111111111:function:my-fn":        "my-fn",
		"not-an-arn": "not-an-arn", "": "",
	} {
		if got := eventsResourceName(arn); got != want {
			t.Errorf("eventsResourceName(%q) = %q, want %q", arn, got, want)
		}
	}
	for ip, want := range map[string]struct {
		internal bool
		kind     string
	}{
		"10.0.0.1": {true, "private"}, "10.255.255.255": {true, "private"}, "172.16.0.1": {true, "private"},
		"172.31.255.255": {true, "private"}, "172.15.0.1": {false, "public"}, "172.32.0.1": {false, "public"},
		"192.168.1.1": {true, "private"}, "192.169.0.1": {false, "public"}, "203.0.113.5": {false, "public"},
		"fd00::1": {true, "private"}, "2001:db8::1": {false, "public"},
		"foo.amazonaws.com": {false, "aws_service"}, "garbage": {false, "unknown"},
	} {
		if in, kind := classifyIP(ip); in != want.internal || kind != want.kind {
			t.Errorf("classifyIP(%q) = %v,%q want %v,%q", ip, in, kind, want.internal, want.kind)
		}
	}
	if eventsSessionName("arn:aws:sts::1:assumed-role/role/SESSION") != "SESSION" || eventsSessionName("svc.amazonaws.com") != "svc.amazonaws.com" {
		t.Error("eventsSessionName")
	}
}

func TestResolveEventsRange(t *testing.T) {
	for in, label := range map[string]string{"": "24h", "1h": "1h", "6H": "6h", "24h": "24h", "7d": "7d", "30d": "30d", "90d": "90d"} {
		tr, err := ResolveEventsRange(in, "", "")
		if err != nil || tr.Label != label || !tr.Start.Before(tr.End) {
			t.Errorf("ResolveEventsRange(%q) = %+v, %v", in, tr, err)
		}
	}
	tr, _ := ResolveEventsRange("1h", "", "")
	if d := tr.End.Sub(tr.Start); d != time.Hour {
		t.Errorf("1h window = %v", d)
	}
	for _, bad := range []string{"banana", "2h", "0", "100d", "all", "1"} {
		var terr *TimeRangeError
		if _, err := ResolveEventsRange(bad, "", ""); !errors.As(err, &terr) {
			t.Errorf("ResolveEventsRange(%q) err = %v, want TimeRangeError", bad, err)
		}
	}
	if tr, err := ResolveEventsRange("custom", "2026-10-01", "2026-10-05"); err != nil || tr.Label != "custom" {
		t.Errorf("custom = %+v, %v", tr, err)
	}
	if _, err := ResolveEventsRange("custom", "", ""); err == nil {
		t.Error("custom without dates must fail")
	}
	// The shared resolver must keep rejecting hour ranges (other endpoints rely on it).
	if _, err := ResolveTimeRange("24h"); err == nil {
		t.Error("ResolveTimeRange must remain day-based")
	}
}
