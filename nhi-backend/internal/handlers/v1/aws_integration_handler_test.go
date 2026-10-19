package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/middleware"
	svcv1 "nhi-backend-api/internal/services/v1"
	"nhi-backend-api/pkg/jwt"
)

func awsRequest(method, url, body, role string, vars map[string]string) *http.Request {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	claims := &jwt.CustomClaims{UserID: "u@acme.test", Email: "u@acme.test", TenantID: "11111111-1111-1111-1111-111111111111", DBName: "tenant_a", Role: role}
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserClaimsKey, claims))
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	return r
}

func awsErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %q", rec.Body.String())
	}
	code, _ := body["error"].(string)
	return code
}

// The service is never reached on these paths.
func newTestAWSHandler() *AWSIntegrationHandler {
	return NewAWSIntegrationHandler(svcv1.NewAWSIntegrationService(nil, nil, nil, nil, svcv1.AWSIntegrationOptions{}))
}

func TestAWSWritesRequireAdmin(t *testing.T) {
	h := newTestAWSHandler()
	acct := map[string]string{"account_id": "222222222222"}
	writes := []struct {
		name string
		fn   http.HandlerFunc
		req  func(role string) *http.Request
	}{
		{"verify", h.Verify, func(role string) *http.Request {
			return awsRequest(http.MethodPost, "/api/v1/integrations/aws/verify", `{"role_arn":"x","regions":["us-east-1"]}`, role, nil)
		}},
		{"regions", h.UpdateRegions, func(role string) *http.Request {
			return awsRequest(http.MethodPut, "/x", `{"regions":["us-east-1"]}`, role, acct)
		}},
		{"exposure", h.Exposure, func(role string) *http.Request {
			return awsRequest(http.MethodPut, "/x", `{"codecommit":true}`, role, acct)
		}},
		{"sync", h.Sync, func(role string) *http.Request { return awsRequest(http.MethodPost, "/x", ``, role, acct) }},
		{"remove", h.Remove, func(role string) *http.Request { return awsRequest(http.MethodDelete, "/x", ``, role, acct) }},
	}
	for _, w := range writes {
		for _, role := range []string{"developer", "auditor", "team_lead", ""} {
			rec := httptest.NewRecorder()
			w.fn(rec, w.req(role))
			if rec.Code != http.StatusForbidden || awsErrorCode(t, rec) != "FORBIDDEN" {
				t.Fatalf("%s as %q: %d %s", w.name, role, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestAWSAccountIDValidated(t *testing.T) {
	h := newTestAWSHandler()
	for _, id := range []string{"", "12345", "abcdefghijkl", "1234567890123", "22222222222x"} {
		for name, fn := range map[string]http.HandlerFunc{"regions": h.UpdateRegions, "exposure": h.Exposure, "sync": h.Sync, "remove": h.Remove} {
			rec := httptest.NewRecorder()
			fn(rec, awsRequest(http.MethodPost, "/x", `{"regions":["us-east-1"]}`, "admin", map[string]string{"account_id": id}))
			if rec.Code != http.StatusBadRequest || awsErrorCode(t, rec) != "INVALID_ACCOUNT_ID" {
				t.Fatalf("%s(%q): %d %s", name, id, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestAWSRejectsMalformedBodies(t *testing.T) {
	h := newTestAWSHandler()
	for _, body := range []string{``, `not json`, `{"role_arn":"x","extra":1}`, `{"role_arn":"x"}{"role_arn":"y"}`, `{"role_arn":` + strings.Repeat(`"x`, 20000) + `"}`, `{"regions":"us-east-1"}`} {
		rec := httptest.NewRecorder()
		h.Verify(rec, awsRequest(http.MethodPost, "/x", body, "admin", nil))
		if rec.Code != http.StatusBadRequest || awsErrorCode(t, rec) != "INVALID_REQUEST" {
			t.Fatalf("verify body %.40q: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.UpdateRegions(rec, awsRequest(http.MethodPut, "/x", `{"regions":["us-east-1"],"x":1}`, "admin", map[string]string{"account_id": "222222222222"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("regions with unknown field: %d", rec.Code)
	}
	for _, body := range []string{`{"github":"yes"}`, `{"codecommit":true,"token":"ghp_x"}`, ``} {
		rec = httptest.NewRecorder()
		h.Exposure(rec, awsRequest(http.MethodPut, "/x", body, "admin", map[string]string{"account_id": "222222222222"}))
		if rec.Code != http.StatusBadRequest || awsErrorCode(t, rec) != "INVALID_REQUEST" {
			t.Fatalf("exposure body %q: %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestAWSGetRequiresClaimsAndNoQuery(t *testing.T) {
	h := newTestAWSHandler()
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/aws", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no claims: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.Get(rec, awsRequest(http.MethodGet, "/api/v1/integrations/aws?x=1", "", "admin", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown query: %d", rec.Code)
	}
}
