package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	chclient "nhi-backend-api/internal/clickhouse"
	"nhi-backend-api/internal/config"
	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

// This test runs every dashboard / list / detail service against a real tenant
// database and the real ClickHouse, read-only. It needs:
//
//	NHI_TEST_TENANT_DB, NHI_TEST_TENANT_ID, NHI_TEST_USER_ID  (and the usual
//	.env with NHI_CLICKHOUSE_* for the ClickHouse half)
type singleDB struct{ db *sql.DB }

func (s singleDB) GetTenantDB(string) (*sql.DB, error) { return s.db, nil }

type stack struct {
	dashboard  *DashboardOverviewService
	identities *IdentitiesListService
	identity   *IdentityDetailService
	creds      *CredentialsService
	cred       *CredentialDetailService
}

func buildStack(resolver *scope.Resolver, activity *ActivityService) stack {
	return stack{
		dashboard:  NewDashboardOverviewService(repov1.NewDashboardOverviewRepository(), nil, resolver, activity),
		identities: NewIdentitiesListService(repov1.NewIdentitiesListRepository(), nil, resolver, activity),
		identity:   NewIdentityDetailService(repov1.NewIdentityDetailRepository(), nil, resolver, activity),
		creds:      NewCredentialsService(repov1.NewCredentialsRepository(), nil, resolver, activity),
		cred:       NewCredentialDetailService(repov1.NewCredentialDetailRepository(), nil, resolver, activity),
	}
}

func mustJSON(t *testing.T, label string, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: not serialisable: %v", label, err)
	}
	return b
}

func openTenant(t *testing.T) (*sql.DB, string, string, string) {
	t.Helper()
	dbName, tenantID, userID := os.Getenv("NHI_TEST_TENANT_DB"), os.Getenv("NHI_TEST_TENANT_ID"), os.Getenv("NHI_TEST_USER_ID")
	if dbName == "" || tenantID == "" || userID == "" {
		t.Skip("NHI_TEST_TENANT_DB / NHI_TEST_TENANT_ID / NHI_TEST_USER_ID not set")
	}
	_ = godotenv.Load("../../../.env")
	t.Setenv("NHI_CONFIG_FILE", "../../../configs/config.yaml")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	db, err := sql.Open("pgx", cfg.Database.GetConnectionString())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dbName, tenantID, userID
}

func openClickHouse(t *testing.T, table string) *chrepo.Repository {
	t.Helper()
	port, _ := strconv.Atoi(os.Getenv("NHI_CLICKHOUSE_PORT"))
	cfg := config.ClickHouseConfig{
		Host: os.Getenv("NHI_CLICKHOUSE_HOST"), Port: port, Database: os.Getenv("NHI_CLICKHOUSE_DATABASE"),
		User: os.Getenv("NHI_CLICKHOUSE_USER"), Password: os.Getenv("NHI_CLICKHOUSE_PASSWORD"),
		Table: table, TimeoutSeconds: 10, Protocol: os.Getenv("NHI_CLICKHOUSE_PROTOCOL"),
	}
	if cfg.Host == "" {
		t.Skip("NHI_CLICKHOUSE_HOST not set")
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "native"
	}
	c, err := chclient.New(context.Background(), cfg)
	if err != nil {
		t.Skipf("clickhouse unreachable: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return chrepo.New(c)
}

// exercise calls every endpoint and tab; it returns the activity flags seen.
func exercise(t *testing.T, s stack, userID, tenantID, dbName string, wantAvailable bool) {
	t.Helper()
	ctx := context.Background()

	dash, err := s.dashboard.GetOverview(ctx, userID, tenantID, dbName)
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	mustJSON(t, "dashboard", dash)
	if dash.ActivityDataAvailable != wantAvailable {
		t.Fatalf("dashboard activity_data_available = %v, want %v", dash.ActivityDataAvailable, wantAvailable)
	}
	t.Logf("dashboard: identities=%d credentials=%d stale_i=%d stale_c=%d shadow=%d util=%.1f orphaned=%d",
		dash.KPIs.TotalIdentities, dash.KPIs.TotalCredentials, dash.KPIs.StaleIdentities, dash.KPIs.StaleCredentials,
		dash.KPIs.ShadowIdentities, dash.KPIs.PermissionUtilizationPct, dash.KPIs.OrphanedCredentials)
	if dash.Charts.StaleVsActive.Active+dash.Charts.StaleVsActive.Stale != dash.KPIs.TotalIdentities {
		t.Fatalf("stale_vs_active %+v does not add up to %d identities", dash.Charts.StaleVsActive, dash.KPIs.TotalIdentities)
	}

	// identities list: default sort, derived sort, min idle, plain sort
	var idList *models.IdentityListResponse
	for _, f := range []models.IdentityListFilter{
		{Page: 1, PageSize: 25},
		{Page: 1, PageSize: 2, SortBy: "idle_days", SortOrder: "asc"},
		{Page: 1, PageSize: 25, SortBy: "permission_utilization_pct"},
		{Page: 1, PageSize: 25, SortBy: "name", SortOrder: "asc"},
		{Page: 2, PageSize: 2, SortBy: "name", SortOrder: "asc"},
		func() models.IdentityListFilter {
			n := 0
			return models.IdentityListFilter{Page: 1, PageSize: 25, MinIdleDays: &n}
		}(),
	} {
		resp, err := s.identities.GetIdentities(ctx, userID, tenantID, dbName, f)
		if err != nil {
			t.Fatalf("identities list %+v: %v", f, err)
		}
		mustJSON(t, "identities list", resp)
		if resp.ActivityDataAvailable != wantAvailable {
			t.Fatalf("identities list activity_data_available = %v", resp.ActivityDataAvailable)
		}
		if f.PageSize == 25 && f.SortBy == "" && f.MinIdleDays == nil {
			idList = resp
		}
		if f.SortBy == "name" && f.Page == 1 && resp.Pagination.TotalCount != len(idList.Data) {
			t.Fatalf("paged total %d != default list %d", resp.Pagination.TotalCount, len(idList.Data))
		}
		if f.MinIdleDays != nil && !wantAvailable && len(resp.Data) != 0 {
			t.Fatalf("min_idle_days without CloudTrail must return nothing, got %d", len(resp.Data))
		}
	}
	t.Logf("identities: %d", len(idList.Data))

	for _, it := range idList.Data {
		for _, tab := range []string{"overview", "permissions", "activity", "relationships", "credentials", "raw"} {
			res, err := s.identity.GetDetail(ctx, userID, tenantID, dbName, models.IdentityDetailFilter{IdentityID: it.ID, Tab: tab, TimeRange: "30d", Depth: 2})
			if err != nil {
				t.Fatalf("identity %s tab %s: %v", it.Name, tab, err)
			}
			mustJSON(t, "identity "+tab, res)
			if a, ok := res.(*models.IdentityActivityResponse); ok && a.ActivityDataAvailable != wantAvailable {
				t.Fatalf("identity activity flag = %v", a.ActivityDataAvailable)
			}
			if o, ok := res.(*models.IdentityOverviewResponse); ok && wantAvailable {
				t.Logf("identity %-14s overview: last_activity=%v idle=%v created_by=%v", it.Name, o.Identity.LastActivityAt, o.Identity.IdleDays, o.Identity.CreatedBy)
			}
		}
	}
	// custom range and service filter on the activity tab
	if len(idList.Data) > 0 {
		id := idList.Data[0].ID
		if _, err := s.identity.GetDetail(ctx, userID, tenantID, dbName, models.IdentityDetailFilter{IdentityID: id, Tab: "activity", TimeRange: "custom", StartDate: "2026-10-01", EndDate: "2026-10-13", Service: "sts"}); err != nil {
			t.Fatalf("custom range activity: %v", err)
		}
		if _, err := s.identity.GetDetail(ctx, userID, tenantID, dbName, models.IdentityDetailFilter{IdentityID: id, Tab: "activity", TimeRange: "custom"}); err == nil {
			t.Fatal("custom range without dates must be rejected")
		}
	}

	// credentials list
	var credList *models.CredentialListResponse
	for _, f := range []models.CredentialListFilter{
		{Page: 1, PageSize: 200},
		{Page: 1, PageSize: 5, SortBy: "idle_days", SortOrder: "desc"},
		{Page: 1, PageSize: 200, SortBy: "name", SortOrder: "asc"},
		func() models.CredentialListFilter {
			n := 0
			return models.CredentialListFilter{Page: 1, PageSize: 200, MinIdleDays: &n}
		}(),
	} {
		resp, err := s.creds.GetCredentials(ctx, userID, tenantID, dbName, f)
		if err != nil {
			t.Fatalf("credentials list %+v: %v", f, err)
		}
		mustJSON(t, "credentials list", resp)
		if resp.ActivityDataAvailable != wantAvailable {
			t.Fatalf("credentials list activity_data_available = %v", resp.ActivityDataAvailable)
		}
		if f.PageSize == 200 && f.SortBy == "" && f.MinIdleDays == nil {
			credList = resp
		}
	}
	t.Logf("credentials: %d", len(credList.Data))

	// one credential of each type through every tab
	seenType := map[string]bool{}
	for _, c := range credList.Data {
		if seenType[c.CredentialType] {
			continue
		}
		seenType[c.CredentialType] = true
		for _, tab := range []string{"overview", "trust", "activity", "rotation", "relationships", "raw"} {
			res, err := s.cred.GetDetail(ctx, userID, tenantID, dbName, models.DetailFilter{CredentialID: c.ID, Tab: tab, TimeRange: "30d", Depth: 2})
			if err != nil {
				t.Fatalf("credential %s (%s) tab %s: %v", c.Name, c.CredentialType, tab, err)
			}
			mustJSON(t, "credential "+tab, res)
			if a, ok := res.(*models.ActivityResponse); ok && a.ActivityDataAvailable != wantAvailable {
				t.Fatalf("credential activity flag = %v", a.ActivityDataAvailable)
			}
		}
	}
}

func TestIntegrationAllEndpointsWithClickHouse(t *testing.T) {
	db, dbName, tenantID, userID := openTenant(t)
	chr := openClickHouse(t, "cloudtrail_events")
	resolver := scope.NewResolver(singleDB{db})
	activity := NewActivityService(chr, repov1.NewIdentityLookupRepository())
	exercise(t, buildStack(resolver, activity), userID, tenantID, dbName, true)
}

func TestIntegrationAllEndpointsWithoutClickHouse(t *testing.T) {
	db, dbName, tenantID, userID := openTenant(t)
	resolver := scope.NewResolver(singleDB{db})
	activity := NewActivityService(nil, repov1.NewIdentityLookupRepository())
	exercise(t, buildStack(resolver, activity), userID, tenantID, dbName, false)
}

// ClickHouse answers the ping but every query fails (the table does not exist):
// every endpoint must still return Postgres data with the flag off.
func TestIntegrationAllEndpointsWhenClickHouseQueriesFail(t *testing.T) {
	db, dbName, tenantID, userID := openTenant(t)
	chr := openClickHouse(t, "table_that_does_not_exist")
	resolver := scope.NewResolver(singleDB{db})
	activity := NewActivityService(chr, repov1.NewIdentityLookupRepository())
	exercise(t, buildStack(resolver, activity), userID, tenantID, dbName, false)
}

// The dev ClickHouse has events for one IAM user whose calls were all denied
// (not one of the tenant's three identities), so this checks the real-event
// path of the activity tab and the cached last-seen lookups directly.
func TestIntegrationActivityTabWithRealEvents(t *testing.T) {
	db, dbName, tenantID, userID := openTenant(t)
	chr := openClickHouse(t, "cloudtrail_events")
	resolver := scope.NewResolver(singleDB{db})
	resolved, err := resolver.Resolve(context.Background(), userID, tenantID, dbName)
	if err != nil {
		t.Fatalf("resolve scope: %v", err)
	}
	activity := NewActivityService(chr, repov1.NewIdentityLookupRepository())
	ctx := context.Background()

	devUser, devKey := ctFixture(t)
	tr, _ := ResolveTimeRange("90d")
	resp := activity.IdentityActivity(ctx, resolved, &repov1.ActivitySubject{Keys: []string{devUser, devKey}}, tr, "", 1, 50)
	mustJSON(t, "activity", resp)

	if !resp.ActivityDataAvailable {
		t.Fatal("activity must be available")
	}
	if len(resp.RecentEvents) == 0 || resp.EventsPagination.TotalCount < len(resp.RecentEvents) {
		t.Fatalf("expected real events: %d events, pagination %+v", len(resp.RecentEvents), resp.EventsPagination)
	}
	if len(resp.ErrorEvents) == 0 {
		t.Fatal("this user's calls were all denied: expected error events")
	}
	a := resp.Analytics
	if a.ErrorRatePct == nil || *a.ErrorRatePct != 100 || a.AccessDeniedRatePct == nil || *a.AccessDeniedRatePct != 100 {
		t.Fatalf("every call was AccessDenied, got error=%v denied=%v", a.ErrorRatePct, a.AccessDeniedRatePct)
	}
	if len(a.TopServices) != 1 || a.TopServices[0].Service != "sts" || a.TopServices[0].Pct != 100 {
		t.Fatalf("top services = %+v", a.TopServices)
	}
	if len(a.SourceIPs) == 0 || len(a.ActiveRegions) == 0 || len(a.PeakHours) == 0 || len(resp.VolumeChart) == 0 {
		t.Fatalf("analytics incomplete: %+v volume=%v", a, resp.VolumeChart)
	}
	if ev := resp.RecentEvents[0]; ev.Service != "sts" || ev.RequestID == nil || ev.Region == nil {
		t.Fatalf("event fields: %+v", ev)
	}
	t.Logf("real activity: %d events, error rate %.0f%%, regions %d, ips %d, pattern %s",
		resp.EventsPagination.TotalCount, *a.ErrorRatePct, len(a.ActiveRegions), len(a.SourceIPs), a.UsagePattern)

	// service filter and a window that predates the data
	if r := activity.IdentityActivity(ctx, resolved, &repov1.ActivitySubject{Keys: []string{devUser}}, tr, "s3", 1, 50); len(r.RecentEvents) != 0 || r.EventsPagination.TotalCount != 0 {
		t.Fatalf("s3 filter must be empty: %+v", r.EventsPagination)
	}
	old, err := ResolveCustomTimeRange("2020-01-01", "2020-01-31")
	if err != nil {
		t.Fatal(err)
	}
	if r := activity.IdentityActivity(ctx, resolved, &repov1.ActivitySubject{Keys: []string{devUser}}, old, "", 1, 50); len(r.RecentEvents) != 0 || !r.ActivityDataAvailable || r.Analytics.UsagePattern != "Dormant" {
		t.Fatalf("2020 window must be empty but available: %+v", r)
	}

	// last-seen lookups: found, cached on repeat, absent for unknown keys
	req := activity.ForRequest(resolved)
	keys := []string{devUser, devKey, quietARN}
	first := req.LastSeen(ctx, keys)
	second := req.LastSeen(ctx, keys)
	if _, ok := first[devUser]; !ok {
		t.Fatalf("user missing from last seen: %v", first)
	}
	if _, ok := first[devKey]; !ok {
		t.Fatalf("access key missing from last seen: %v", first)
	}
	if _, ok := first[quietARN]; ok {
		t.Fatal("ispm-user has no events, must be absent")
	}
	if len(second) != len(first) || !second[devUser].Equal(first[devUser]) {
		t.Fatalf("cached answer differs: %v vs %v", second, first)
	}
	if req.Degraded() {
		t.Fatal("no query failed")
	}
}

// Paging and event enrichment against the real events of the dev IAM user.
func TestIntegrationActivityPagingAndEnrichment(t *testing.T) {
	db, dbName, tenantID, userID := openTenant(t)
	chr := openClickHouse(t, "cloudtrail_events")
	resolver := scope.NewResolver(singleDB{db})
	resolved, err := resolver.Resolve(context.Background(), userID, tenantID, dbName)
	if err != nil {
		t.Fatalf("resolve scope: %v", err)
	}
	activity := NewActivityService(chr, repov1.NewIdentityLookupRepository())
	ctx := context.Background()
	devUser, _ := ctFixture(t)
	subj := &repov1.ActivitySubject{Keys: []string{devUser}}
	tr, _ := ResolveTimeRange("90d")

	p1 := activity.IdentityActivity(ctx, resolved, subj, tr, "", 1, 3)
	p2 := activity.IdentityActivity(ctx, resolved, subj, tr, "", 2, 3)
	p3 := activity.IdentityActivity(ctx, resolved, subj, tr, "", 3, 3)
	p4 := activity.IdentityActivity(ctx, resolved, subj, tr, "", 4, 3)
	if len(p1.RecentEvents) != 3 || len(p2.RecentEvents) != 3 || len(p3.RecentEvents) != 1 || len(p4.RecentEvents) != 0 {
		t.Fatalf("7 events in pages of 3 should be 3/3/1/0, got %d/%d/%d/%d",
			len(p1.RecentEvents), len(p2.RecentEvents), len(p3.RecentEvents), len(p4.RecentEvents))
	}
	if p1.EventsPagination.TotalCount != 7 || p1.EventsPagination.TotalPages != 3 || p2.EventsPagination.Page != 2 || p2.EventsPagination.PageSize != 3 {
		t.Fatalf("pagination: %+v / %+v", p1.EventsPagination, p2.EventsPagination)
	}
	seen := map[string]bool{}
	for _, p := range []*models.IdentityActivityResponse{p1, p2, p3} {
		for _, e := range p.RecentEvents {
			if e.RequestID == nil || seen[*e.RequestID] {
				t.Fatalf("pages overlap or an event has no id: %+v", e)
			}
			seen[*e.RequestID] = true
		}
	}
	// Every event names its caller; this user is not in the tenant inventory,
	// so the names are null but the raw ARN and type are there.
	for _, e := range p1.RecentEvents {
		if e.CallerARN != devUser || e.CallerType == nil || *e.CallerType != "IAMUser" {
			t.Fatalf("caller fields: %+v", e.EventPrincipal)
		}
		if e.IdentityName != nil || e.CredentialName != nil {
			t.Fatalf("unmatched caller must have null names: %+v", e.EventPrincipal)
		}
	}
	if p4.ActivityDataAvailable != true {
		t.Fatal("an empty page is still available data")
	}

	// The IAM service principal callers show up unmatched but intact.
	svc := activity.IdentityActivity(ctx, resolved, &repov1.ActivitySubject{Keys: []string{"resource-explorer-2.amazonaws.com"}}, tr, "", 1, 5)
	if len(svc.RecentEvents) == 0 || svc.RecentEvents[0].CallerARN != "resource-explorer-2.amazonaws.com" || *svc.RecentEvents[0].CallerType != "AWSService" {
		t.Fatalf("service principal events: %+v", svc.RecentEvents)
	}
}

// quietARN is an identity with no CloudTrail events.
const quietARN = "arn:aws:iam::000000000000:user/no-events"

// ctFixture names an IAM user with CloudTrail events and its access key ID,
// from NHI_TEST_CT_USER_ARN / NHI_TEST_CT_ACCESS_KEY (kept out of the code:
// they identify a real person in the dev account).
func ctFixture(t *testing.T) (userARN, accessKey string) {
	t.Helper()
	userARN, accessKey = os.Getenv("NHI_TEST_CT_USER_ARN"), os.Getenv("NHI_TEST_CT_ACCESS_KEY")
	if userARN == "" || accessKey == "" {
		t.Skip("NHI_TEST_CT_USER_ARN and NHI_TEST_CT_ACCESS_KEY not set")
	}
	return userARN, accessKey
}
