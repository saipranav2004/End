package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecovererReturnsJSON500(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestRecovererPassesThrough(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
}

func TestRespondErrorUsesTheFamilyShape(t *testing.T) {
	for path, want := range map[string]string{
		"/api/v1/genome/dashboard/summary":     `"error":{"code":"UNAUTHORIZED"`,
		"/api/v1/ispm/posture/overview":        `{"error":{"code":"UNAUTHORIZED","message":"no"}}`,
		"/api/v1/credential-exposure/findings": `{"error":"UNAUTHORIZED","message":"no"}`,
		"/api/v1/identities":                   `{"success":false,"message":"no","error":"UNAUTHORIZED"}`,
	} {
		rec := httptest.NewRecorder()
		respondUnauthorized(rec, httptest.NewRequest(http.MethodGet, path, nil), "no")
		if got := rec.Body.String(); !strings.Contains(got, want) {
			t.Errorf("%s: %s, want %s", path, got, want)
		}
	}
}
