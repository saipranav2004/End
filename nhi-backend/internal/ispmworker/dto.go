package ispmworker

import "time"

// POST /ispm/scans      — start a scan of the caller's tenant
// GET  /ispm/scans/{id} — status of a scan job
//
// The dashboard backend is only a client of the ispm-worker: it sends one HTTP
// request to the worker and relays the answer. The tenant always comes from
// the caller's JWT, never from the request.

// WorkerConfig tells the backend where the ispm-worker listens and which API
// key the worker expects (the worker's SCAN_API_TOKEN).
type WorkerConfig struct {
	BaseURL string        // e.g. http://ispm-worker:8080 (nginx host also works)
	Token   string        // sent as "Authorization: Bearer <Token>"
	Timeout time.Duration // per request; defaults to 10s
}

// Enabled reports whether the worker address and token are both configured.
func (c WorkerConfig) Enabled() bool { return c.BaseURL != "" && c.Token != "" }

// StartResult is the answer of POST /ispm/scans.
type StartResult struct {
	JobID string `json:"job_id"`
	// AlreadyRunning is true when the worker answered 200: a scan of this
	// tenant is already in progress and JobID is that scan's job.
	AlreadyRunning bool `json:"already_running"`
}

// Response is the success body of both endpoints: { "data": ... }.
type Response struct {
	Data any `json:"data"`
}
