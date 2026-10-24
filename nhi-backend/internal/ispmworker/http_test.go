package ispmworker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// fakeWorker answers like the ispm-worker: it accepts only the key "good".
func fakeWorker(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/scans":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"data":{"job_id":"job-1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/scans/job-1":
			_, _ = w.Write([]byte(`{"data":{"job_id":"job-1","status":"RUNNING"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func publicRouter(h *Handler) *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/api/v1/ispm-scans", h.PublicStart).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/ispm-scans/{job_id}", h.PublicJob).Methods(http.MethodGet)
	return r
}

func do(r http.Handler, method, target, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestPublicScanAPI(t *testing.T) {
	worker := fakeWorker(t)
	h := NewHandler(WorkerConfig{BaseURL: worker.URL, Timeout: 2 * time.Second})
	r := publicRouter(h)
	const tenant = `{"tenant_id":"de300000-0000-4000-8000-000000000001"}`

	if rec := do(r, "POST", "/api/v1/ispm-scans", "good", tenant); rec.Code != 202 || !strings.Contains(rec.Body.String(), `"job_id":"job-1"`) {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if rec := do(r, "POST", "/api/v1/ispm-scans", "bad", tenant); rec.Code != 401 {
		t.Fatalf("wrong key must be 401 from the worker's answer: %d", rec.Code)
	}
	if rec := do(r, "POST", "/api/v1/ispm-scans", "", tenant); rec.Code != 401 {
		t.Fatalf("no key: %d", rec.Code)
	}
	if rec := do(r, "POST", "/api/v1/ispm-scans", "good", `{"tenant_id":"nope"}`); rec.Code != 400 {
		t.Fatalf("tenant_id must be a UUID: %d", rec.Code)
	}
	if rec := do(r, "POST", "/api/v1/ispm-scans?api_key=good", "good", tenant); rec.Code != 400 || !strings.Contains(rec.Body.String(), "never put the API key in the URL") {
		t.Fatalf("key in URL: %d %s", rec.Code, rec.Body)
	}
	if rec := do(r, "GET", "/api/v1/ispm-scans/job-1", "good", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "RUNNING") {
		t.Fatalf("job: %d %s", rec.Code, rec.Body)
	}
	if rec := do(r, "GET", "/api/v1/ispm-scans/job-9", "good", ""); rec.Code != 404 {
		t.Fatalf("unknown job: %d", rec.Code)
	}
}

func TestScanAPINotConfigured(t *testing.T) {
	r := publicRouter(NewHandler(WorkerConfig{}))
	if rec := do(r, "POST", "/api/v1/ispm-scans", "good", `{"tenant_id":"de300000-0000-4000-8000-000000000001"}`); rec.Code != 503 {
		t.Fatalf("no WORKER_URL: %d", rec.Code)
	}
}

func TestCanScanRoles(t *testing.T) {
	for _, role := range []string{"analyst", "Manager", "admin", "super_admin", "superadmin"} {
		if !canScan(role) {
			t.Errorf("%s should be allowed to start a scan", role)
		}
	}
	for _, role := range []string{"viewer", "", "guest"} {
		if canScan(role) {
			t.Errorf("%q should not be allowed to start a scan", role)
		}
	}
}
