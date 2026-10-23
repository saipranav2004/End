// Package ispmworker is the client of the separately deployed ISPM rule engine
// (the ispm-worker) and the scan endpoints that call it. The backend never
// scans itself: it starts a scan on the worker and relays the job status.
package ispmworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Errors the client reports; the handler turns each into an HTTP answer.
var (
	// ErrWorkerNotConfigured: WORKER_URL / WORKER_SCAN_TOKEN are not set.
	ErrWorkerNotConfigured = errors.New("scan worker is not configured")
	// ErrWorkerUnavailable: the worker could not be reached or answered with
	// something unusable (network error, 5xx, bad JSON).
	ErrWorkerUnavailable = errors.New("scan worker is unavailable")
	// ErrWorkerAuth: the worker answered 401/403, so the token in this
	// backend's configuration does not match the worker's SCAN_API_TOKEN.
	ErrWorkerAuth = errors.New("scan worker rejected the API token")
	// ErrWorkerRejected: the worker answered 400 (the request was malformed).
	ErrWorkerRejected = errors.New("scan worker rejected the request")
	// ErrTenantUnknown: the worker answered 404 to a start request, so the
	// tenant is not in nhi_global.tenants.
	ErrTenantUnknown = errors.New("tenant is not registered with the scan worker")
	// ErrJobNotFound: the worker does not know the job id.
	ErrJobNotFound = errors.New("scan job not found")
)

const (
	defaultTimeout = 10 * time.Second
	maxBody        = 1 << 20
)

// Client talks to the ispm-worker. It is safe for concurrent use.
type Client struct {
	base  string
	token string
	hc    *http.Client
}

// NewClient builds a client. A zero Timeout means 10 seconds. Redirects are
// not followed, so the bearer token can never be forwarded to another host.
func NewClient(cfg WorkerConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		base:  strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		token: strings.TrimSpace(cfg.Token),
		hc: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Enabled reports whether the client has an address and a token.
func (c *Client) Enabled() bool { return c != nil && c.base != "" && c.token != "" }

// Configured reports whether the worker address is set. The public (API key)
// endpoints need only this: the key comes from the caller, not from the config.
func (c *Client) Configured() bool { return c != nil && c.base != "" }

// Start asks the worker to scan a tenant: POST /api/v1/scans with the tenant
// id (and who asked) in the JSON body and the token in the Authorization
// header. 202 starts a new scan; 200 means one is already running.
func (c *Client) Start(ctx context.Context, tenantID, requestedBy string) (*StartResult, error) {
	if !c.Enabled() {
		return nil, ErrWorkerNotConfigured
	}
	return c.StartAs(ctx, c.token, tenantID, requestedBy)
}

// StartAs is Start with an API key chosen by the caller. The worker is the one
// that validates it: a wrong key comes back as ErrWorkerAuth.
func (c *Client) StartAs(ctx context.Context, key, tenantID, requestedBy string) (*StartResult, error) {
	if !c.Configured() {
		return nil, ErrWorkerNotConfigured
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrWorkerAuth
	}
	// The worker lower-cases the id before looking it up; send it that way.
	tenantID = strings.ToLower(strings.TrimSpace(tenantID))
	body, err := json.Marshal(map[string]string{"tenant_id": tenantID, "requested_by": requestedBy})
	if err != nil {
		return nil, fmt.Errorf("encode scan request: %w", err)
	}
	status, raw, err := c.do(ctx, key, http.MethodPost, "/api/v1/scans", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusAccepted, http.StatusOK:
		obj := unwrap(raw)
		jobID, _ := obj["job_id"].(string)
		already, _ := obj["already_running"].(bool)
		if strings.TrimSpace(jobID) == "" {
			return nil, fmt.Errorf("%w: worker answered %d without a job_id", ErrWorkerUnavailable, status)
		}
		return &StartResult{JobID: strings.TrimSpace(jobID), AlreadyRunning: status == http.StatusOK || already}, nil
	case http.StatusBadRequest:
		return nil, fmt.Errorf("%w: %s", ErrWorkerRejected, snippet(raw))
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrWorkerAuth
	case http.StatusNotFound:
		if !isWorkerAnswer(raw) {
			return nil, fmt.Errorf("%w: 404 from %s%s is not the worker's answer (wrong WORKER_URL? it must be the ispm-worker, not this backend): %s",
				ErrWorkerUnavailable, c.base, "/api/v1/scans", snippet(raw))
		}
		return nil, fmt.Errorf("%w: worker said: %s", ErrTenantUnknown, snippet(raw))
	}
	return nil, fmt.Errorf("%w: worker answered %d: %s", ErrWorkerUnavailable, status, snippet(raw))
}

// Job reads one scan job: GET /api/v1/scans/{job_id}. The worker's JSON object
// is returned as it is.
func (c *Client) Job(ctx context.Context, jobID string) (map[string]any, error) {
	if !c.Enabled() {
		return nil, ErrWorkerNotConfigured
	}
	return c.JobAs(ctx, c.token, jobID)
}

// JobAs is Job with an API key chosen by the caller (validated by the worker).
func (c *Client) JobAs(ctx context.Context, key, jobID string) (map[string]any, error) {
	if !c.Configured() {
		return nil, ErrWorkerNotConfigured
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrWorkerAuth
	}
	status, raw, err := c.do(ctx, key, http.MethodGet, "/api/v1/scans/"+url.PathEscape(jobID), nil)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		out := unwrap(raw)
		if out == nil {
			return nil, fmt.Errorf("%w: worker answered 200 with a body that is not a JSON object", ErrWorkerUnavailable)
		}
		return out, nil
	case http.StatusNotFound:
		if !isWorkerAnswer(raw) {
			return nil, fmt.Errorf("%w: 404 is not the worker's answer (wrong WORKER_URL?): %s", ErrWorkerUnavailable, snippet(raw))
		}
		return nil, ErrJobNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrWorkerAuth
	}
	return nil, fmt.Errorf("%w: worker answered %d: %s", ErrWorkerUnavailable, status, snippet(raw))
}

// isWorkerAnswer tells a 404 that the ispm-worker itself sent (a JSON object
// with an "error" member: unknown tenant / unknown job) from a 404 of some
// other server or a missing route (plain text, or this backend's own
// {"success":false,...} body). Only the first is a real "not found".
func isWorkerAnswer(raw []byte) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, ok := m["error"]
	return ok
}

func (c *Client) do(ctx context.Context, key, method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: build request: %v", ErrWorkerUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrWorkerUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		return 0, nil, fmt.Errorf("%w: reading the worker's answer failed", ErrWorkerUnavailable)
	}
	return resp.StatusCode, raw, nil
}

// snippet shortens an upstream body for logs. The text is untrusted: it is
// logged, never echoed to the browser.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// unwrap reads the worker's JSON. The worker wraps its answer as
// {"data": {...job...}, "already_running": bool}; a flat object is accepted
// too. The result is the job object, with already_running copied into it when
// the worker put it next to "data". It returns nil if the body is not an object.
func unwrap(raw []byte) map[string]any {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return nil
	}
	data, ok := top["data"].(map[string]any)
	if !ok {
		return top
	}
	if v, ok := top["already_running"]; ok {
		if _, has := data["already_running"]; !has {
			data["already_running"] = v
		}
	}
	return data
}
