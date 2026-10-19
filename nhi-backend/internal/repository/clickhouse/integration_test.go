package clickhouse

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/joho/godotenv"

	chclient "nhi-backend-api/internal/clickhouse"
	"nhi-backend-api/internal/config"
)

// Integration tests run read-only queries against the real ClickHouse named in
// .env (NHI_CLICKHOUSE_*). They skip when it is not configured or reachable.
// Expectations match the dev data: account 548593215839, IAM user
// abhishek.tiwari whose AssumeRole calls were all AccessDenied, and AWS
// service callers such as resource-explorer-2.amazonaws.com.
const (
	devAccount = "548593215839"
	devUser    = "arn:aws:iam::884264984984:user/abhishek.tiwari"
	devKey     = "AKIA43YSM4GMD3N4DTTS"
	devService = "resource-explorer-2.amazonaws.com"
)

func integrationClient(t *testing.T) *Repository {
	t.Helper()
	_ = godotenv.Load("../../../.env")
	host := os.Getenv("NHI_CLICKHOUSE_HOST")
	if host == "" {
		t.Skip("NHI_CLICKHOUSE_HOST not set")
	}
	port, _ := strconv.Atoi(os.Getenv("NHI_CLICKHOUSE_PORT"))
	cfg := config.ClickHouseConfig{
		Host: host, Port: port, Database: os.Getenv("NHI_CLICKHOUSE_DATABASE"),
		User: os.Getenv("NHI_CLICKHOUSE_USER"), Password: os.Getenv("NHI_CLICKHOUSE_PASSWORD"),
		Table: "cloudtrail_events", TimeoutSeconds: 10, Protocol: os.Getenv("NHI_CLICKHOUSE_PROTOCOL"),
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "native"
	}
	client, err := chclient.New(context.Background(), cfg)
	if err != nil {
		t.Skipf("clickhouse unreachable: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return New(client)
}

func TestIntegrationQueries(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	sc := Scope{Accounts: []string{devAccount}}
	user := NewSubject(devUser)

	events, err := c.ActivityEvents(ctx, user, sc, 50, 0)
	if err != nil {
		t.Fatalf("ActivityEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events for the dev user")
	}
	total, err := c.CountEvents(ctx, user, sc)
	if err != nil || total < uint64(len(events)) {
		t.Fatalf("CountEvents = %d, err %v", total, err)
	}

	errs, err := c.ErrorEvents(ctx, user, sc, 50)
	if err != nil {
		t.Fatalf("ErrorEvents: %v", err)
	}
	for _, e := range errs {
		if e.ErrorCode == "" {
			t.Fatal("ErrorEvents returned a successful call")
		}
	}

	vol, err := c.VolumeChart(ctx, user, sc)
	if err != nil {
		t.Fatalf("VolumeChart: %v", err)
	}
	var sum uint64
	for _, p := range vol {
		sum += p.Count
	}
	if sum != total {
		t.Fatalf("volume sum %d != count %d", sum, total)
	}

	rows, err := c.AnalyticsRows(ctx, user, sc)
	if err != nil {
		t.Fatalf("AnalyticsRows: %v", err)
	}
	var calls uint64
	for _, r := range rows {
		calls += r.Count
	}
	if calls != total {
		t.Fatalf("analytics total %d != count %d", calls, total)
	}

	if _, err := c.UsedPermissions(ctx, user, sc); err != nil {
		t.Fatalf("UsedPermissions: %v", err)
	}

	// Every one of this user's calls failed, so there must be no access edges.
	edges, err := c.AccessEdges(ctx, user, sc)
	if err != nil || len(edges) != 0 {
		t.Fatalf("AccessEdges = %d edges, err %v; failed calls must not create edges", len(edges), err)
	}

	if _, err := c.IdentitiesUsingCredential(ctx, "arn:aws:iam::1:role/none", sc); err != nil {
		t.Fatalf("IdentitiesUsingCredential: %v", err)
	}
	if creator, err := c.CreatorARN(ctx, "arn:aws:iam::1:role/none", sc.Accounts); err != nil || creator != "" {
		t.Fatalf("CreatorARN = %q, err %v", creator, err)
	}
}

func TestIntegrationSafetyRules(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// Empty subject: no query, no rows.
	if ev, err := c.ActivityEvents(ctx, Subject{}, Scope{Accounts: []string{devAccount}}, 10, 0); err != nil || ev != nil {
		t.Fatalf("empty subject returned %d events, err %v", len(ev), err)
	}
	// A credential with no ARN must not match every row with an empty issuer.
	if ev, _ := c.ActivityEvents(ctx, NewSubject("arn:aws:iam::1:user/nobody", ""),
		Scope{Accounts: []string{devAccount}}, 10, 0); len(ev) != 0 {
		t.Fatalf("empty credential leaked %d events", len(ev))
	}
	// Tenant boundary: a different account sees nothing.
	if ev, err := c.ActivityEvents(ctx, NewSubject(devUser), Scope{Accounts: []string{"000000000000"}}, 10, 0); err != nil || len(ev) != 0 {
		t.Fatalf("other account saw %d events, err %v", len(ev), err)
	}
	// No accounts at all: refuse.
	if ev, err := c.ActivityEvents(ctx, NewSubject(devUser), Scope{}, 10, 0); err != nil || ev != nil {
		t.Fatalf("no accounts returned %d events, err %v", len(ev), err)
	}
	// AWS service callers (not ARNs) match by exact caller value.
	if ev, err := c.ActivityEvents(ctx, NewSubject(devService), Scope{Accounts: []string{devAccount}}, 10, 0); err != nil || len(ev) == 0 {
		t.Fatalf("service caller: %d events, err %v", len(ev), err)
	}
}

func TestIntegrationLastSeen(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	sc := Scope{Accounts: []string{devAccount}, End: time.Now()}

	last, err := c.LastSeenAt(ctx, NewSubject(devUser), sc)
	if err != nil || last == nil || last.Year() < 2026 {
		t.Fatalf("LastSeenAt = %v, err %v", last, err)
	}
	if none, err := c.LastSeenAt(ctx, NewSubject("arn:aws:iam::1:user/nobody"), sc); err != nil || none != nil {
		t.Fatalf("unknown subject LastSeenAt = %v, err %v", none, err)
	}

	m, err := c.LastSeenByKeys(ctx, []string{devUser, devKey, "arn:aws:iam::1:user/nobody"}, sc)
	if err != nil {
		t.Fatalf("LastSeenByKeys: %v", err)
	}
	if _, ok := m[devUser]; !ok {
		t.Fatal("user missing from bulk result")
	}
	if _, ok := m[devKey]; !ok {
		t.Fatal("access key missing from bulk result")
	}
	if _, ok := m["arn:aws:iam::1:user/nobody"]; ok {
		t.Fatal("unknown key must be absent")
	}

	// As-of an instant before any data, nothing is seen.
	early := Scope{Accounts: []string{devAccount}, End: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	if m, err := c.LastSeenByKeys(ctx, []string{devUser}, early); err != nil || len(m) != 0 {
		t.Fatalf("early as-of returned %v, err %v", m, err)
	}
}

func TestIntegrationBulkCreatorsAndUsedActions(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	accounts := []string{devAccount}

	// No create events exist in the dev data: the call must succeed and be empty.
	creators, err := c.CreatorsByARNs(ctx, []string{"arn:aws:iam::1:role/none", ""}, accounts)
	if err != nil || len(creators) != 0 {
		t.Fatalf("CreatorsByARNs = %v, err %v", creators, err)
	}
	if m, err := c.CreatorsByARNs(ctx, nil, accounts); err != nil || len(m) != 0 {
		t.Fatalf("empty input: %v, %v", m, err)
	}

	// The dev user's calls all failed, so it has no used actions; the bulk call
	// must not error and must not invent entries.
	used, err := c.UsedActionsByKeys(ctx, []string{devUser, devKey}, accounts, time.Time{})
	if err != nil {
		t.Fatalf("UsedActionsByKeys: %v", err)
	}
	if len(used[devUser]) != 0 {
		t.Fatalf("failed calls must not count as used: %v", used[devUser])
	}
	// A successful AssumeRole by an AWS service principal does exist.
	svc, err := c.UsedActionsByKeys(ctx, []string{devService}, accounts, time.Time{})
	if err != nil || len(svc[devService]) == 0 {
		t.Fatalf("service principal used actions = %v, err %v", svc, err)
	}
	if got := svc[devService][0]; got.Service != "sts" || got.Action != "AssumeRole" {
		t.Fatalf("got %+v", got)
	}
	// Tenant boundary.
	if m, err := c.UsedActionsByKeys(ctx, []string{devService}, []string{"000000000000"}, time.Time{}); err != nil || len(m) != 0 {
		t.Fatalf("other account: %v, %v", m, err)
	}
}

func TestIntegrationServiceFilter(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	user := NewSubject(devUser)
	all, _ := c.CountEvents(ctx, user, Scope{Accounts: []string{devAccount}})
	sts, err := c.CountEvents(ctx, user, Scope{Accounts: []string{devAccount}, Service: "sts"})
	if err != nil || sts != all || all == 0 {
		t.Fatalf("sts count %d vs all %d, err %v", sts, all, err)
	}
	s3, err := c.CountEvents(ctx, user, Scope{Accounts: []string{devAccount}, Service: "s3"})
	if err != nil || s3 != 0 {
		t.Fatalf("s3 count = %d, err %v", s3, err)
	}
}

func TestIntegrationUsedActionsHonourTheSinceBound(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	accounts := []string{devAccount}

	// The service principal's events are from Oct 11-12 2026.
	before := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	in, err := c.UsedActionsByKeys(ctx, []string{devService}, accounts, before)
	if err != nil || len(in[devService]) == 0 {
		t.Fatalf("window that covers the data: %v, err %v", in, err)
	}
	out, err := c.UsedActionsByKeys(ctx, []string{devService}, accounts, after)
	if err != nil || len(out[devService]) != 0 {
		t.Fatalf("window after the data must be empty: %v, err %v", out, err)
	}

	// Same bound through the permissions and access-edge queries.
	sub := NewSubject(devService)
	perms, err := c.UsedPermissions(ctx, sub, Scope{Accounts: accounts, Start: before})
	if err != nil || len(perms) == 0 {
		t.Fatalf("permissions in window: %v, err %v", perms, err)
	}
	perms, err = c.UsedPermissions(ctx, sub, Scope{Accounts: accounts, Start: after})
	if err != nil || len(perms) != 0 {
		t.Fatalf("permissions after window: %v, err %v", perms, err)
	}
	edges, err := c.AccessEdges(ctx, sub, Scope{Accounts: accounts, Start: before})
	if err != nil || len(edges) == 0 {
		t.Fatalf("edges in window: %v, err %v", edges, err)
	}
	for _, e := range edges {
		if e.CallCount == 0 {
			t.Fatalf("edge without a call count: %+v", e)
		}
	}
	if edges, err = c.AccessEdges(ctx, sub, Scope{Accounts: accounts, Start: after}); err != nil || len(edges) != 0 {
		t.Fatalf("edges after window: %v, err %v", edges, err)
	}
}
