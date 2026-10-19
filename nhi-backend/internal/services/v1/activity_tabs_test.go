package v1

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
)

var tabNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func tabRange(days int) *ResolvedTimeRange {
	return &ResolvedTimeRange{Days: days, Start: tabNow.AddDate(0, 0, -days), End: tabNow, Label: "30d"}
}

func sampleFetch() *activityFetch {
	t1 := tabNow.Add(-2 * time.Hour)
	return &activityFetch{
		volume: []chrepo.VolumePoint{{Date: "2026-10-11", Count: 3}, {Date: "2026-10-12", Count: 7}},
		events: []chrepo.Event{
			{EventTime: t1, EventName: "GetObject", EventSource: "s3.amazonaws.com", Region: "us-east-1",
				SourceIP: "1.2.3.4", UserAgent: "aws-cli", ResourceARN: "arn:aws:s3:::b", EventID: "evt-1"},
			{EventTime: t1, EventName: "AssumeRole", EventSource: "sts.amazonaws.com", ErrorCode: "AccessDenied", EventID: "evt-2"},
		},
		total:  10,
		errors: []chrepo.Event{{EventTime: t1, EventName: "AssumeRole", EventSource: "sts.amazonaws.com", ErrorCode: "AccessDenied", ErrorMessage: "denied", SourceIP: "1.2.3.4"}},
		rows: []chrepo.AnalyticsRow{
			{SourceIP: "1.2.3.4", Region: "us-east-1", EventSource: "s3.amazonaws.com", EventName: "GetObject", Hour: 9, Count: 9},
			{SourceIP: "1.2.3.4", Region: "us-east-1", EventSource: "sts.amazonaws.com", EventName: "AssumeRole", ErrorCode: "AccessDenied", Hour: 10, Count: 1},
		},
	}
}

func TestIdentityActivityMapping(t *testing.T) {
	resp := identityActivityFrom(sampleFetch(), tabRange(30), normalizePaging(1, 50), nil)
	if resp.Tab != "activity" || resp.TimeRange != "30d" {
		t.Fatalf("header: %+v", resp)
	}
	if len(resp.VolumeChart) != 2 || resp.VolumeChart[1].Count != 7 {
		t.Fatalf("volume: %+v", resp.VolumeChart)
	}
	ev := resp.RecentEvents[0]
	if ev.Service != "s3" || ev.SourceIP == nil || *ev.SourceIP != "1.2.3.4" || ev.RequestID == nil || *ev.RequestID != "evt-1" {
		t.Fatalf("event mapping: %+v", ev)
	}
	if e2 := resp.RecentEvents[1]; e2.SourceIP != nil || e2.ErrorCode == nil || *e2.ErrorCode != "AccessDenied" {
		t.Fatalf("blank source ip must be nil, error code kept: %+v", e2)
	}
	if len(resp.ErrorEvents) != 1 || resp.ErrorEvents[0].ErrorMessage == nil {
		t.Fatalf("errors: %+v", resp.ErrorEvents)
	}
	a := resp.Analytics
	if a.ErrorRatePct == nil || *a.ErrorRatePct != 10 || a.AccessDeniedRatePct == nil || *a.AccessDeniedRatePct != 10 {
		t.Fatalf("rates: %v %v", a.ErrorRatePct, a.AccessDeniedRatePct)
	}
	if len(a.TopServices) != 2 || a.TopServices[0].Service != "s3" || a.TopServices[0].Pct != 90 {
		t.Fatalf("services: %+v", a.TopServices)
	}
	if len(a.PeakHours) != 2 || a.PeakHours[0].Hour != 9 {
		t.Fatalf("peak hours: %+v", a.PeakHours)
	}
	if resp.EventsPagination.TotalCount != 10 || resp.EventsPagination.TotalPages != 1 || resp.EventsPagination.PageSize != 50 {
		t.Fatalf("pagination: %+v", resp.EventsPagination)
	}
	if a.UsagePattern != "Intermittent" { // 2 active days of 30
		t.Fatalf("pattern = %s", a.UsagePattern)
	}
}

func TestCredentialActivityMappingHasTheNewFields(t *testing.T) {
	resp := credentialActivityFrom(sampleFetch(), tabRange(30), normalizePaging(1, 50), nil)
	if len(resp.Analytics.TopActions) == 0 || len(resp.Analytics.PeakHours) == 0 || resp.Analytics.AccessDeniedRatePct == nil {
		t.Fatalf("credential analytics missing fields: %+v", resp.Analytics)
	}
	if len(resp.ErrorEvents) != 1 || resp.RecentEvents[0].EventCount != 1 || resp.RecentEvents[0].RequestID == nil {
		t.Fatalf("credential events: %+v", resp)
	}
}

func TestEmptyActivityIsWellFormedJSONShape(t *testing.T) {
	resp := emptyIdentityActivity(tabRange(7), normalizePaging(1, 50))
	if resp.VolumeChart == nil || resp.RecentEvents == nil || resp.ErrorEvents == nil ||
		resp.Analytics.TopServices == nil || resp.Analytics.PeakHours == nil || resp.Analytics.UsagePattern != "Dormant" {
		t.Fatalf("empty tab must use empty slices, not nil: %+v", resp)
	}
	cred := emptyCredentialActivity(tabRange(7), normalizePaging(1, 50))
	if cred.IdentitiesUsingThisCredential == nil || cred.ErrorEvents == nil {
		t.Fatalf("empty credential tab must use empty slices: %+v", cred)
	}
}

func TestUsagePatternForAllRangeStartsAtFirstActiveDay(t *testing.T) {
	all := &ResolvedTimeRange{Days: maxRangeDays, Start: tabNow.AddDate(0, 0, -maxRangeDays), End: tabNow, Label: "all"}
	vol := []chrepo.VolumePoint{{Date: "2026-10-10", Count: 1}, {Date: "2026-10-11", Count: 1}, {Date: "2026-10-12", Count: 1}, {Date: "2026-10-13", Count: 1}}
	if got := usagePatternFor(vol, all); got != "Continuous" {
		t.Fatalf("4 of 4 days since first activity = %s, want Continuous", got)
	}
	if got := usagePatternFor(nil, all); got != "Dormant" {
		t.Fatalf("no activity = %s", got)
	}
}

func TestUsingIdentityFromMatchedAndUnmatched(t *testing.T) {
	byID := map[string]repov1.LookupIdentity{"id-1": {ID: "id-1", Name: "web-1", Type: "ec2_instance"}}
	c := chrepo.CallerUsage{CallerARN: "arn:aws:sts::1:assumed-role/r/i-0aaa", SourceIP: "10.0.0.5", LastAction: "GetObject", LastActionTime: tabNow}

	u := usingIdentityFrom(c, Match{IdentityID: "id-1", IdentityTerminated: true}, byID)
	if u.IdentityID != "id-1" || u.Name != "web-1" || u.IdentityType != "ec2_instance" || !u.Terminated || u.CallerARN != c.CallerARN {
		t.Fatalf("matched: %+v", u)
	}
	u = usingIdentityFrom(c, Match{}, byID)
	if u.IdentityID != "" || u.Name != "i-0aaa" || u.IdentityType != "unknown" || u.LastAction == nil || *u.LastAction != "GetObject" {
		t.Fatalf("unmatched caller must be kept with its raw name: %+v", u)
	}
	svc := usingIdentityFrom(chrepo.CallerUsage{CallerARN: "resource-explorer-2.amazonaws.com"}, Match{}, byID)
	if svc.Name != "resource-explorer-2.amazonaws.com" || svc.SourceIP != nil {
		t.Fatalf("service caller: %+v", svc)
	}
}

func TestArnLastSegment(t *testing.T) {
	for in, want := range map[string]string{
		"arn:aws:iam::1:user/path/bob":  "bob",
		"arn:aws:sns:us-east-1:1:topic": "topic",
		"plain":                         "plain",
		"trailing/":                     "trailing/",
		"":                              "",
	} {
		if got := arnLastSegment(in); got != want {
			t.Errorf("arnLastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomTimeRange(t *testing.T) {
	r, err := ResolveCustomTimeRange("2026-01-01", "2026-01-10")
	if err != nil {
		t.Fatal(err)
	}
	if r.Label != "custom" || r.Days != 10 || r.Start.Format("2006-01-02") != "2026-01-01" {
		t.Fatalf("got %+v", r)
	}
	if r.End.Format("2006-01-02") != "2026-01-10" || r.End.Hour() != 23 {
		t.Fatalf("a bare end date must cover the whole day: %v", r.End)
	}
	if r, err = ResolveCustomTimeRange("2026-01-01T00:00:00Z", "2099-01-01"); err != nil || r.End.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("future end must clamp to now: %+v, %v", r, err)
	}
	for _, bad := range [][2]string{{"2026-02-01", "2026-01-01"}, {"nope", "2026-01-01"}, {"2026-01-01", ""}} {
		var tre *TimeRangeError
		if _, err := ResolveCustomTimeRange(bad[0], bad[1]); err == nil {
			t.Errorf("%v should fail", bad)
		} else if e, ok := err.(*TimeRangeError); !ok {
			t.Errorf("%v: want *TimeRangeError, got %T", bad, err)
		} else {
			tre = e
			_ = tre
		}
	}
	if r, err := resolveRange("custom", "2026-01-01", "2026-01-02"); err != nil || r.Label != "custom" {
		t.Fatalf("resolveRange custom: %+v %v", r, err)
	}
	if r, err := resolveRange("30d", "", ""); err != nil || r.Label != "30d" {
		t.Fatalf("resolveRange 30d: %+v %v", r, err)
	}
}

func TestTTLCacheExpiresAndResets(t *testing.T) {
	now := tabNow
	c := newTTLCache[int](time.Minute, 2, func() time.Time { return now })
	c.set("a", 1)
	if v, ok := c.get("a"); !ok || v != 1 {
		t.Fatal("fresh entry must hit")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.get("a"); ok {
		t.Fatal("expired entry must miss")
	}
	c.set("x", 1)
	c.set("y", 2)
	c.set("z", 3) // over capacity: cache resets, newest survives
	if _, ok := c.get("x"); ok {
		t.Fatal("reset must drop old entries")
	}
	if v, ok := c.get("z"); !ok || v != 3 {
		t.Fatal("newest entry must survive the reset")
	}
}

func TestRequestActivityWithoutClickHouseIsInert(t *testing.T) {
	a := NewActivityService(nil, repov1.NewIdentityLookupRepository())
	r := a.ForRequest(nil)
	if r.Available() || r.Degraded() {
		t.Fatal("no ClickHouse: unavailable but not degraded")
	}
	if len(r.LastSeen(nil, []string{"x"})) != 0 || len(r.Creators(nil, []string{"x"})) != 0 ||
		r.UsedActionSets(nil, []string{"x"}, time.Time{}) != nil || r.UsedPermissions(nil, []string{"x"}, time.Time{}) != nil || r.AccessEdges(nil, []string{"x"}, time.Time{}, time.Time{}) != nil {
		t.Fatal("inert source must return nothing")
	}
}

func enrichLookup() *repov1.IdentityLookup {
	return &repov1.IdentityLookup{
		Identities: []repov1.LookupIdentity{
			{ID: "i-a", ARN: "arn:aws:ec2:us-east-1:111:instance/i-0aaa", Name: "web-1"},
			{ID: "i-b", ARN: "arn:aws:ec2:us-east-1:111:instance/i-0bbb", Name: "web-2"},
			{ID: "i-gone", ARN: "arn:aws:ec2:us-east-1:111:instance/i-0ccc", Name: "old", Terminated: true},
			{ID: "usr", ARN: "arn:aws:iam::111:user/ispm-user", Name: "ispm-user"},
		},
		Credentials: []repov1.LookupCredential{
			{ID: "c-app", ARN: "arn:aws:iam::111:role/app", Name: "app"},
			{ID: "c-old", ARN: "arn:aws:iam::111:role/old", Name: "old-role", Terminated: true},
			{ID: "c-key", Name: "AKIAKEY", Type: "access_key"},
		},
		Bindings: []repov1.LookupBinding{
			{IdentityID: "i-a", CredentialID: "c-app"}, {IdentityID: "i-b", CredentialID: "c-app"},
			{IdentityID: "i-gone", CredentialID: "c-old"}, {IdentityID: "usr", CredentialID: "c-key"},
		},
	}
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestEventEnricherNamesTheCallerAndKeepsRawARN(t *testing.T) {
	enr := newEventEnricher(enrichLookup())

	// A role session shared by two instances: the session name picks the right one.
	p := enr.principal(chrepo.Event{
		CallerARN: "arn:aws:sts::111:assumed-role/app/i-0bbb", SessionIssuerARN: "arn:aws:iam::111:role/app", CallerType: "AssumedRole",
	})
	if str(p.IdentityID) != "i-b" || str(p.IdentityName) != "web-2" || str(p.CredentialID) != "c-app" || str(p.CredentialName) != "app" || p.Terminated {
		t.Fatalf("role session: %+v", p)
	}
	if p.CallerARN != "arn:aws:sts::111:assumed-role/app/i-0bbb" || str(p.CallerType) != "AssumedRole" {
		t.Fatalf("raw caller must be kept: %+v", p)
	}

	// An IAM user acting through its access key.
	p = enr.principal(chrepo.Event{CallerARN: "arn:aws:iam::111:user/ispm-user", AccessKeyID: "AKIAKEY", CallerType: "IAMUser"})
	if str(p.IdentityName) != "ispm-user" || str(p.CredentialName) != "AKIAKEY" {
		t.Fatalf("user: %+v", p)
	}

	// Terminated identities are still named, and flagged.
	p = enr.principal(chrepo.Event{SessionIssuerARN: "arn:aws:iam::111:role/old"})
	if str(p.IdentityName) != "old" || !p.Terminated {
		t.Fatalf("terminated: %+v", p)
	}

	// Nothing matches (AWS service caller, foreign user): kept, names null.
	for _, ev := range []chrepo.Event{
		{CallerARN: "resource-explorer-2.amazonaws.com", CallerType: "AWSService", SourceIP: "resource-explorer-2.amazonaws.com"},
		{CallerARN: "arn:aws:iam::999:user/stranger", AccessKeyID: "AKIASTRANGER"},
	} {
		p = enr.principal(ev)
		if p.CallerARN != ev.CallerARN || p.IdentityID != nil || p.IdentityName != nil || p.CredentialID != nil || p.Terminated {
			t.Fatalf("unmatched event must keep the raw caller only: %+v", p)
		}
	}
}

func TestEventEnricherNilStillReportsRawCaller(t *testing.T) {
	var enr *eventEnricher = newEventEnricher(nil)
	p := enr.principal(chrepo.Event{CallerARN: "arn:aws:iam::1:user/a", CallerType: "IAMUser"})
	if p.CallerARN != "arn:aws:iam::1:user/a" || str(p.CallerType) != "IAMUser" || p.IdentityID != nil {
		t.Fatalf("got %+v", p)
	}
}

func TestActivityMappingCarriesThePrincipalOnEveryEventShape(t *testing.T) {
	f := sampleFetch()
	f.events[0].CallerARN = "arn:aws:iam::111:user/ispm-user"
	f.events[0].AccessKeyID = "AKIAKEY"
	f.errors[0].CallerARN = "arn:aws:iam::111:user/ispm-user"
	enr := newEventEnricher(enrichLookup())

	id := identityActivityFrom(f, tabRange(30), normalizePaging(1, 50), enr)
	if str(id.RecentEvents[0].IdentityName) != "ispm-user" || str(id.ErrorEvents[0].IdentityName) != "ispm-user" {
		t.Fatalf("identity tab: %+v / %+v", id.RecentEvents[0].EventPrincipal, id.ErrorEvents[0].EventPrincipal)
	}
	cr := credentialActivityFrom(f, tabRange(30), normalizePaging(1, 50), enr)
	if str(cr.RecentEvents[0].IdentityName) != "ispm-user" || str(cr.ErrorEvents[0].IdentityName) != "ispm-user" {
		t.Fatalf("credential tab: %+v", cr.RecentEvents[0].EventPrincipal)
	}
	// And the flat JSON keys exist.
	b, _ := json.Marshal(id.RecentEvents[0])
	for _, k := range []string{`"caller_arn"`, `"credential_name"`, `"identity_name"`, `"terminated"`, `"caller_type"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("event JSON missing %s: %s", k, b)
		}
	}
}

func TestPagingBoundsAndPaginationBlock(t *testing.T) {
	for _, tc := range []struct{ page, size, wantPage, wantSize int }{
		{0, 0, 1, 50}, {-3, -1, 1, 50}, {2, 20, 2, 20}, {1, 500, 1, 100},
	} {
		if p := normalizePaging(tc.page, tc.size); p.page != tc.wantPage || p.size != tc.wantSize {
			t.Errorf("normalizePaging(%d,%d) = %+v", tc.page, tc.size, p)
		}
	}
	if o := normalizePaging(3, 20).offset(); o != 40 {
		t.Fatalf("offset = %d, want 40", o)
	}
	pg := pagination(101, normalizePaging(2, 25))
	if pg.Page != 2 || pg.PageSize != 25 || pg.TotalCount != 101 || pg.TotalPages != 5 {
		t.Fatalf("pagination = %+v", pg)
	}
	resp := identityActivityFrom(sampleFetch(), tabRange(30), normalizePaging(2, 5), nil)
	if resp.EventsPagination.Page != 2 || resp.EventsPagination.PageSize != 5 || resp.EventsPagination.TotalPages != 2 {
		t.Fatalf("mapped pagination = %+v", resp.EventsPagination)
	}
}

func TestResolveCallersUsesTheSharedLookup(t *testing.T) {
	callers := []chrepo.CallerUsage{
		{CallerARN: "arn:aws:sts::111:assumed-role/app/i-0aaa", SourceIP: "1.2.3.4", LastAction: "GetObject", LastActionTime: tabNow},
		{CallerARN: "arn:aws:sts::111:assumed-role/app/unknown-session", LastAction: "PutObject", LastActionTime: tabNow},
	}
	got := resolveCallers(callers, "arn:aws:iam::111:role/app", enrichLookup())
	if len(got) != 2 {
		t.Fatalf("got %d callers", len(got))
	}
	if got[0].IdentityID != "i-a" || got[0].Name != "web-1" {
		t.Fatalf("session i-0aaa should resolve to web-1: %+v", got[0])
	}
	if got[1].IdentityID != "" || got[1].Name != "unknown-session" || got[1].CallerARN == "" {
		t.Fatalf("unmatched session must be kept by its raw name: %+v", got[1])
	}
	if raw := resolveCallers(callers, "arn:aws:iam::111:role/app", nil); len(raw) != 2 || raw[0].IdentityID != "" {
		t.Fatalf("without a lookup callers stay raw: %+v", raw)
	}
}
