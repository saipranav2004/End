package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/logging"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func lastRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, buf.String())
	}
	return rec
}

func newTestRouter(status int) *mux.Router {
	r := mux.NewRouter()
	r.Use(RequestID)
	r.Use(RequestLogger)
	r.HandleFunc("/api/v1/things/{id}", func(w http.ResponseWriter, req *http.Request) {
		logging.RequestFrom(req.Context()).SetActor("tenant-1", "user@example.com")
		w.WriteHeader(status)
		_, _ = w.Write([]byte("hello"))
	})
	r.HandleFunc("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

func TestRequestLoggerFieldsAndNoSecrets(t *testing.T) {
	buf := captureLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/things/42?token=supersecret", nil)
	req.Header.Set("Authorization", "Bearer abc.def.ghi")
	req.Header.Set(requestIDHeader, "req-12345678")
	rr := httptest.NewRecorder()
	newTestRouter(http.StatusOK).ServeHTTP(rr, req)

	rec := lastRecord(t, buf)
	want := map[string]any{
		"msg": "http request", "level": "INFO", "method": "GET", "path": "/api/v1/things/42",
		"route": "/api/v1/things/{id}", "status": float64(200), "bytes": float64(5),
		"request_id": "req-12345678", "tenant_id": "tenant-1", "user_id": "user@example.com",
	}
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("%s = %v, want %v", k, rec[k], v)
		}
	}
	if _, ok := rec["duration_ms"]; !ok {
		t.Error("duration_ms missing")
	}
	if out := buf.String(); strings.Contains(out, "supersecret") || strings.Contains(out, "abc.def.ghi") {
		t.Errorf("query string or Authorization leaked into logs: %s", out)
	}
	if got := rr.Header().Get(requestIDHeader); got != "req-12345678" {
		t.Errorf("response X-Request-ID = %q", got)
	}
}

func TestRequestLoggerLevelByStatus(t *testing.T) {
	cases := []struct {
		status int
		level  string
	}{{200, "INFO"}, {404, "WARN"}, {500, "ERROR"}}
	for _, tc := range cases {
		buf := captureLogs(t)
		newTestRouter(tc.status).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/things/1", nil))
		if got := lastRecord(t, buf)["level"]; got != tc.level {
			t.Errorf("status %d: level = %v, want %s", tc.status, got, tc.level)
		}
	}
}

func TestRequestLoggerHealthIsDebug(t *testing.T) {
	buf := captureLogs(t)
	newTestRouter(200).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if got := lastRecord(t, buf)["level"]; got != "DEBUG" {
		t.Errorf("/health level = %v, want DEBUG", got)
	}
}

func TestRequestIDGeneratedWhenMissingOrInvalid(t *testing.T) {
	for _, in := range []string{"", "short", "has space in it!", strings.Repeat("a", 100), "bad\nid-123456"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		if in != "" {
			req.Header.Set(requestIDHeader, in)
		}
		RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rr, req)
		got := rr.Header().Get(requestIDHeader)
		if got == in || len(got) != 16 {
			t.Errorf("input %q: got request id %q, want a generated 16-char id", in, got)
		}
	}
}

func TestResponseWriterFlushPassesThrough(t *testing.T) {
	rr := httptest.NewRecorder()
	h := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("wrapped writer does not implement http.Flusher")
		}
		_, _ = w.Write([]byte("x"))
		f.Flush()
	}))
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if !rr.Flushed {
		t.Error("Flush did not reach the underlying writer")
	}
}

func TestRequestLoggerCapturesServerErrorBodyOnly(t *testing.T) {
	buf := captureLogs(t)
	r := mux.NewRouter()
	r.Use(RequestLogger)
	r.HandleFunc("/boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"INTERNAL_ERROR","message":"Unable to load findings"}`))
	})
	r.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	})
	r.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("fine")) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	rec := lastRecord(t, buf)
	if rec["level"] != "ERROR" || !strings.Contains(rec["response_body"].(string), "Unable to load findings") {
		t.Errorf("5xx body not captured: %v", rec)
	}

	buf.Reset()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/big", nil))
	if got := len(lastRecord(t, buf)["response_body"].(string)); got != maxErrBody {
		t.Errorf("response_body length = %d, want %d", got, maxErrBody)
	}

	buf.Reset()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	if _, ok := lastRecord(t, buf)["response_body"]; ok {
		t.Error("response_body must only be logged for 5xx")
	}
}
