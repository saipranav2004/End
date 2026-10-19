package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/middleware"
	modelsv1 "nhi-backend-api/internal/models/v1"
	jwtpkg "nhi-backend-api/pkg/jwt"
)

const evTestID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

func evQuery(t *testing.T, raw string) (modelsv1.IdentityEventsQuery, int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	q, _ := url.ParseQuery(raw)
	out, ok := parseIdentityEventsQuery(rec, evTestID, q)
	var body map[string]any
	if !ok {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return out, rec.Code, body
	}
	return out, http.StatusOK, nil
}

func TestParseIdentityEventsQueryDefaults(t *testing.T) {
	q, code, _ := evQuery(t, "")
	if code != http.StatusOK || q.Section != "timeline" || q.TimeRange != "24h" || q.Result != "all" ||
		q.ReadWrite != "all" || q.Page != 1 || q.PageSize != 100 || q.IdentityID != evTestID {
		t.Fatalf("defaults = %+v (code %d)", q, code)
	}
}

func TestParseIdentityEventsQueryAccepts(t *testing.T) {
	for _, raw := range []string{
		"section=detail&event_id=b063de15-f18f-4c97-b8d4-4fbbd9128e4b",
		"section=CREDENTIALS&time_range=7D",
		"time_range=custom&start_date=2026-10-01&end_date=2026-10-05",
		"service=S3&action=GetObject&result=error&read_write=write&source_ip=10.0.1.42&resource=bucket&credential=arn:aws:iam::1:role/x",
		"source_ip=2001:db8::1", "page=2&page_size=500", "page=200&page_size=500",
		"section=heatmap&section=ips", // first value is used; duplicates are not an error
	} {
		if _, code, body := evQuery(t, raw); code != http.StatusOK {
			t.Errorf("%q rejected: %d %v", raw, code, body)
		}
	}
	if q, _, _ := evQuery(t, "section=CREDENTIALS&service=S3&time_range=7D"); q.Section != "credentials" || q.Service != "s3" || q.TimeRange != "7d" {
		t.Errorf("values must be normalised: %+v", q)
	}
}

func TestParseIdentityEventsQueryRejects(t *testing.T) {
	long := strings.Repeat("a", 300)
	cases := map[string]string{
		"section=nope":                                         "INVALID_SECTION",
		"section=detail":                                       "MISSING_EVENT_ID",
		"section=detail&event_id=a%3Bdrop":                     "INVALID_EVENT_ID",
		"section=detail&event_id=" + long:                      "INVALID_EVENT_ID",
		"time_range=custom":                                    "INVALID_TIME_RANGE",
		"time_range=custom&start_date=2026-10-01":              "INVALID_TIME_RANGE",
		"start_date=2026-10-01&end_date=2026-10-05":            "INVALID_TIME_RANGE",
		"start_date=garbage":                                   "INVALID_DATE",
		"time_range=custom&start_date=2026-10-01&end_date=bad": "INVALID_DATE",
		"result=maybe":                                         "INVALID_RESULT",
		"read_write=both":                                      "INVALID_READ_WRITE",
		"service=s3%3Bdrop":                                    "INVALID_SERVICE",
		"service=has space":                                    "INVALID_SERVICE",
		"action=Get'Object":                                    "INVALID_ACTION",
		"action=" + long:                                       "INVALID_ACTION",
		"source_ip=999.1.1.1":                                  "INVALID_SOURCE_IP",
		"source_ip=10.0.0.1'--":                                "INVALID_SOURCE_IP",
		"resource=" + long:                                     "INVALID_FILTER",
		"resource=a%00b":                                       "INVALID_FILTER",
		"credential=a%0Ab":                                     "INVALID_FILTER",
		"page=0":                                               "INVALID_PAGINATION",
		"page=abc":                                             "INVALID_PAGINATION",
		"page_size=0":                                          "INVALID_PAGINATION",
		"page_size=501":                                        "INVALID_PAGINATION",
		"page_size=-1":                                         "INVALID_PAGINATION",
		"page=201&page_size=500":                               "INVALID_PAGINATION",
	}
	for raw, code := range cases {
		_, status, body := evQuery(t, raw)
		if status != http.StatusBadRequest || body["error"] != code {
			t.Errorf("%q → %d %v, want 400 %s", raw, status, body, code)
		}
	}
}

// routerFor serves the handler on the real path, optionally with claims.
func routerFor(withClaims bool) http.Handler {
	h := NewIdentityEventsHandler(nil) // validation fails before the service is used in these tests
	r := mux.NewRouter()
	r.HandleFunc("/api/v1/identities/{id}/events", func(w http.ResponseWriter, req *http.Request) {
		if withClaims {
			ctx := context.WithValue(req.Context(), middleware.UserClaimsKey,
				&jwtpkg.CustomClaims{UserID: "u@example.com", TenantID: "11111111-1111-1111-1111-111111111111", DBName: "tenant_a"})
			req = req.WithContext(ctx)
		}
		h.Get(w, req)
	}).Methods(http.MethodGet)
	return r
}

func TestIdentityEventsHandlerRouting(t *testing.T) {
	get := func(h http.Handler, path string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
	base := "/api/v1/identities/" + evTestID + "/events"

	if code, _ := get(routerFor(false), base); code != http.StatusUnauthorized {
		t.Errorf("no claims → %d, want 401", code)
	}
	h := routerFor(true)
	if code, body := get(h, "/api/v1/identities/not-a-uuid/events"); code != http.StatusBadRequest || body["message"] != "Invalid identity ID" {
		t.Errorf("bad uuid → %d %v", code, body)
	}
	if code, body := get(h, base+"?sectoin=timeline"); code != http.StatusBadRequest || body["error"] != "INVALID_QUERY_PARAM" {
		t.Errorf("unknown param → %d %v", code, body)
	}
	// The identity detail endpoint's own params must not be accepted here, and
	// vice versa (separate allow-lists).
	if code, body := get(h, base+"?tab=activity"); code != http.StatusBadRequest || body["error"] != "INVALID_QUERY_PARAM" {
		t.Errorf("tab on events → %d %v", code, body)
	}
	if code, body := get(h, base+"?section=banana"); code != http.StatusBadRequest || body["error"] != "INVALID_SECTION" {
		t.Errorf("bad section → %d %v", code, body)
	}
	// 404 for the route's siblings stays JSON-free of panics: wrong method.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, base, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST → %d, want 405", rec.Code)
	}
}

func TestIdentityDetailAllowListUnchanged(t *testing.T) {
	// The identity detail endpoint must keep rejecting events-only parameters.
	for _, k := range []string{"section", "event_id", "action", "result", "source_ip", "read_write", "resource", "credential"} {
		if _, ok := identityDetailAllowed[k]; ok {
			t.Errorf("identityDetailAllowed unexpectedly accepts %q", k)
		}
	}
	for _, k := range []string{"tab", "used_only", "depth", "edge_type"} {
		if _, ok := identityEventsAllowed[k]; ok {
			t.Errorf("identityEventsAllowed unexpectedly accepts %q", k)
		}
	}
}
