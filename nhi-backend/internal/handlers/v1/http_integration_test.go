package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/gorilla/mux"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	chclient "nhi-backend-api/internal/clickhouse"
	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/middleware"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
	svcv1 "nhi-backend-api/internal/services/v1"
	jwtpkg "nhi-backend-api/pkg/jwt"
)

type singleDB struct{ db *sql.DB }

func (s singleDB) GetTenantDB(string) (*sql.DB, error) { return s.db, nil }

// httpStack drives the real handlers over a real tenant database. Claims are
// injected into the request context directly (no token is minted).
func httpStack(t *testing.T, withClickHouse bool) http.Handler {
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

	var chr *chrepo.Repository
	if withClickHouse {
		port, _ := strconv.Atoi(os.Getenv("NHI_CLICKHOUSE_PORT"))
		chCfg := config.ClickHouseConfig{
			Host: os.Getenv("NHI_CLICKHOUSE_HOST"), Port: port, Database: os.Getenv("NHI_CLICKHOUSE_DATABASE"),
			User: os.Getenv("NHI_CLICKHOUSE_USER"), Password: os.Getenv("NHI_CLICKHOUSE_PASSWORD"),
			Table: "cloudtrail_events", TimeoutSeconds: 10, Protocol: os.Getenv("NHI_CLICKHOUSE_PROTOCOL"),
		}
		if chCfg.Host == "" {
			t.Skip("NHI_CLICKHOUSE_HOST not set")
		}
		if chCfg.Protocol == "" {
			chCfg.Protocol = "native"
		}
		c, err := chclient.New(context.Background(), chCfg)
		if err != nil {
			t.Skipf("clickhouse unreachable: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		chr = chrepo.New(c)
	}

	resolver := scope.NewResolver(singleDB{db})
	activity := svcv1.NewActivityService(chr, repov1.NewIdentityLookupRepository())

	r := mux.NewRouter()
	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/dashboard/overview", NewDashboardOverviewHandler(svcv1.NewDashboardOverviewService(repov1.NewDashboardOverviewRepository(), nil, resolver, activity)).Get)
	api.HandleFunc("/identities", NewIdentitiesListHandler(svcv1.NewIdentitiesListService(repov1.NewIdentitiesListRepository(), nil, resolver, activity)).List)
	api.HandleFunc("/identities/{id}/events", NewIdentityEventsHandler(svcv1.NewIdentityEventsService(repov1.NewIdentityEventsRepository(), resolver, activity)).Get)
	api.HandleFunc("/identities/{id}", NewIdentityDetailHandler(svcv1.NewIdentityDetailService(repov1.NewIdentityDetailRepository(), nil, resolver, activity)).Get)
	api.HandleFunc("/credentials", NewCredentialsHandler(svcv1.NewCredentialsService(repov1.NewCredentialsRepository(), nil, resolver, activity)).List)
	api.HandleFunc("/permission-source", NewPermissionSourceHandler(svcv1.NewPermissionSourceService(repov1.NewPermissionSourceRepository(), resolver)).Get)
	api.HandleFunc("/credentials/{id}", NewCredentialDetailHandler(svcv1.NewCredentialDetailService(repov1.NewCredentialDetailRepository(), nil, resolver, activity)).Get)

	claims := &jwtpkg.CustomClaims{UserID: userID, Email: userID, TenantID: tenantID, DBName: dbName, Role: "admin"}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), middleware.UserClaimsKey, claims)))
	})
}

type envelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

func get(t *testing.T, h http.Handler, url string, wantStatus int) envelope {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != wantStatus {
		t.Fatalf("GET %s = %d, want %d: %s", url, rec.Code, wantStatus, rec.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("GET %s: bad JSON: %v", url, err)
	}
	return env
}

func dataMap(t *testing.T, env envelope) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("data is not an object: %v (%s)", err, string(env.Data))
	}
	return m
}

func requireKeys(t *testing.T, label string, m map[string]json.RawMessage, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: response is missing key %q", label, k)
		}
	}
}

func runHTTPChecks(t *testing.T, h http.Handler, wantAvailable bool) {
	t.Helper()
	flag := func(label string, m map[string]json.RawMessage) {
		var got bool
		if err := json.Unmarshal(m["activity_data_available"], &got); err != nil || got != wantAvailable {
			t.Errorf("%s: activity_data_available = %s, want %v", label, string(m["activity_data_available"]), wantAvailable)
		}
	}

	dash := dataMap(t, get(t, h, "/api/v1/dashboard/overview", 200))
	requireKeys(t, "dashboard", dash, "kpis", "charts", "activity_data_available")
	flag("dashboard", dash)
	var kpis map[string]json.RawMessage
	_ = json.Unmarshal(dash["kpis"], &kpis)
	requireKeys(t, "kpis", kpis, "total_identities", "total_credentials", "orphaned_credentials", "stale_identities",
		"stale_credentials", "publicly_accessible", "shadow_identities", "accounts_scanned", "permission_utilization_pct")
	get(t, h, "/api/v1/dashboard/overview?bogus=1", 400)

	list := dataMap(t, get(t, h, "/api/v1/identities?page_size=10", 200))
	requireKeys(t, "identities list", list, "pagination", "data", "activity_data_available")
	flag("identities list", list)
	var rows []map[string]json.RawMessage
	_ = json.Unmarshal(list["data"], &rows)
	if len(rows) == 0 {
		t.Fatal("expected identities")
	}
	requireKeys(t, "identity row", rows[0], "id", "name", "arn", "identity_type", "functional_class", "account_id", "region",
		"environment", "owner", "status", "is_publicly_accessible", "vpc_id", "bound_credentials_count", "last_activity_at",
		"created_at_aws", "age_days", "idle_days", "created_by", "permission_utilization_pct")
	get(t, h, "/api/v1/identities?sort_by=idle_days&sort_order=asc&min_idle_days=0", 200)
	get(t, h, "/api/v1/identities?sort_by=permission_utilization_pct", 200)
	get(t, h, "/api/v1/identities?sort_by=not_a_column", 400)

	var identityID string
	_ = json.Unmarshal(rows[0]["id"], &identityID)

	// activity paging: valid values pass, bad ones are 400s
	get(t, h, "/api/v1/identities/"+identityID+"?tab=activity&page=2&page_size=10", 200)
	for _, bad := range []string{"page=0", "page=abc", "page_size=0", "page_size=101", "page=11&page_size=100", "page=-1"} {
		if e := get(t, h, "/api/v1/identities/"+identityID+"?tab=activity&"+bad, 400); e.Error != "INVALID_PAGINATION" {
			t.Errorf("%s: error = %q, want INVALID_PAGINATION", bad, e.Error)
		}
	}
	pg := dataMap(t, get(t, h, "/api/v1/identities/"+identityID+"?tab=activity&page=3&page_size=7", 200))
	var ep struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	_ = json.Unmarshal(pg["events_pagination"], &ep)
	if ep.Page != 3 || ep.PageSize != 7 {
		t.Errorf("events_pagination = %+v, want page 3 size 7", ep)
	}
	for _, tab := range []string{"overview", "permissions", "activity", "relationships", "credentials", "raw"} {
		m := dataMap(t, get(t, h, "/api/v1/identities/"+identityID+"?tab="+tab+"&time_range=30d", 200))
		requireKeys(t, "identity "+tab, m, "tab")
		switch tab {
		case "activity":
			requireKeys(t, "identity activity", m, "time_range", "volume_chart", "recent_events", "analytics", "error_events", "events_pagination", "activity_data_available")
			flag("identity activity", m)
		case "overview":
			requireKeys(t, "identity overview", m, "identity", "discovery", "type_attributes", "bound_credentials", "tags", "missing_tags", "risk_signals")
		case "relationships":
			requireKeys(t, "identity relationships", m, "graph", "relationship_table")
		}
	}
	// custom time range now works; bad ranges are 400s with the right code
	get(t, h, "/api/v1/identities/"+identityID+"?tab=activity&time_range=custom&start_date=2026-10-01&end_date=2026-10-13", 200)
	if e := get(t, h, "/api/v1/identities/"+identityID+"?tab=activity&time_range=custom", 400); e.Error != "INVALID_TIME_RANGE" {
		t.Errorf("custom without dates: error = %q", e.Error)
	}
	if e := get(t, h, "/api/v1/identities/"+identityID+"?tab=activity&time_range=custom&start_date=2026-10-13&end_date=2026-10-01", 400); e.Error != "INVALID_TIME_RANGE" {
		t.Errorf("reversed custom range: error = %q", e.Error)
	}
	get(t, h, "/api/v1/identities/"+identityID+"?tab=activity&time_range=bogus", 400)
	// edge types stored in trust_edges, and the CloudTrail ones, are now accepted
	for _, et := range []string{"service_trust", "cross_account", "federated_oidc", "assume_role", "accesses", "created_by"} {
		get(t, h, "/api/v1/identities/"+identityID+"?tab=relationships&edge_type="+et, 200)
	}
	get(t, h, "/api/v1/identities/"+identityID+"?tab=relationships&edge_type=bogus", 400)
	get(t, h, "/api/v1/identities/not-a-uuid?tab=overview", 400)
	get(t, h, "/api/v1/identities/00000000-0000-0000-0000-000000000000?tab=overview", 404)

	clist := dataMap(t, get(t, h, "/api/v1/credentials?page_size=200", 200))
	requireKeys(t, "credentials list", clist, "pagination", "data", "activity_data_available")
	flag("credentials list", clist)
	var crows []map[string]json.RawMessage
	_ = json.Unmarshal(clist["data"], &crows)
	if len(crows) == 0 {
		t.Fatal("expected credentials")
	}
	requireKeys(t, "credential row", crows[0], "id", "name", "arn", "credential_type", "account_id", "region", "status",
		"owner_principal_arn", "bound_identities_count", "is_orphaned", "last_used_at", "last_rotated_at", "rotation_enabled",
		"rotation_overdue", "age_days", "idle_days", "expires_at", "days_until_expiry", "has_external_trust", "permission_boundary_arn")
	get(t, h, "/api/v1/credentials?sort_by=idle_days&min_idle_days=0", 200)
	get(t, h, "/api/v1/credentials?sort_by=last_used_at&sort_order=asc&page=2&page_size=5", 200)

	var credID string
	_ = json.Unmarshal(crows[0]["id"], &credID)
	get(t, h, "/api/v1/credentials/"+credID+"?tab=activity&page=2&page_size=25", 200)
	if e := get(t, h, "/api/v1/credentials/"+credID+"?tab=activity&page_size=500", 400); e.Error != "INVALID_PAGINATION" {
		t.Errorf("credential page_size=500: error = %q", e.Error)
	}

	// permission source: validation, tenant scoping and a real grant lookup
	get(t, h, "/api/v1/permission-source", 400)
	get(t, h, "/api/v1/permission-source?credential_id=not-a-uuid&event_source=ec2.amazonaws.com&event_name=DescribeInstances", 400)
	get(t, h, "/api/v1/permission-source?credential_id="+credID+"&event_source=&event_name=X", 400)
	get(t, h, "/api/v1/permission-source?credential_id="+credID+"&event_source=ec2.amazonaws.com&event_name=Describe%20Instances", 400)
	get(t, h, "/api/v1/permission-source?credential_id="+credID+"&event_source=ec2.amazonaws.com&event_name=X&bogus=1", 400)
	get(t, h, "/api/v1/permission-source?credential_id=00000000-0000-0000-0000-000000000000&event_source=ec2.amazonaws.com&event_name=DescribeInstances", 404)
	ranGrantCheck := false
	for _, row := range crows {
		var name string
		_ = json.Unmarshal(row["name"], &name)
		if name != "AWSServiceRoleForECS" { // a role whose real grants include ec2:Describe*
			continue
		}
		ranGrantCheck = true
		var rid string
		_ = json.Unmarshal(row["id"], &rid)
		hit := dataMap(t, get(t, h, "/api/v1/permission-source?credential_id="+rid+"&event_source=ec2.amazonaws.com&event_name=DescribeInstances", 200))
		var matched bool
		_ = json.Unmarshal(hit["matched"], &matched)
		if !matched || string(hit["policy_source"]) == "null" || string(hit["policy_type"]) == "null" || string(hit["note"]) != "null" {
			t.Errorf("AWSServiceRoleForECS ec2:DescribeInstances should match a grant: %v", hit)
		}
		miss := dataMap(t, get(t, h, "/api/v1/permission-source?credential_id="+rid+"&event_source=nosuchservice.amazonaws.com&event_name=Frobnicate", 200))
		_ = json.Unmarshal(miss["matched"], &matched)
		if matched || string(miss["policy_source"]) != "null" || string(miss["note"]) == "null" {
			t.Errorf("ungranted event must be unmatched with a note: %v", miss)
		}
	}
	if !ranGrantCheck {
		t.Fatal("the dev tenant is expected to contain the AWSServiceRoleForECS role; the real-grant check did not run")
	}
	for _, tab := range []string{"overview", "trust", "activity", "rotation", "relationships", "raw"} {
		m := dataMap(t, get(t, h, "/api/v1/credentials/"+credID+"?tab="+tab+"&time_range=7d", 200))
		requireKeys(t, "credential "+tab, m, "tab")
		if tab == "activity" {
			requireKeys(t, "credential activity", m, "time_range", "volume_chart", "recent_events", "identities_using_this_credential",
				"analytics", "error_events", "events_pagination", "activity_data_available")
			flag("credential activity", m)
		}
		if tab == "overview" {
			requireKeys(t, "credential overview", m, "credential", "bound_identities", "risk_signals", "type_attributes")
		}
	}
}

func TestHTTPWithClickHouse(t *testing.T)    { runHTTPChecks(t, httpStack(t, true), true) }
func TestHTTPWithoutClickHouse(t *testing.T) { runHTTPChecks(t, httpStack(t, false), false) }
