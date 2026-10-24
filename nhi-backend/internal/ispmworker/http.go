package ispmworker

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/logging"
	"nhi-backend-api/internal/middleware"
)

// Two ways in, one worker behind them:
//
//	POST /api/v1/ispm-scans       public: {"tenant_id","requested_by"} + the worker's API key
//	GET  /api/v1/ispm-scans/{job_id}  public: status of that job
//	POST /api/v1/ispm/scans       login: the tenant comes from the JWT (the Sync button)
//	GET  /api/v1/ispm/scans/{id}  login: status of that job
//
// The public pair holds no login: the caller's key is forwarded to the worker,
// which checks it against its SCAN_API_TOKEN. The login pair uses
// WORKER_SCAN_TOKEN from this backend's environment.
// Errors use the posture API's body: { "error": { code, message, field } }.

const (
	codeInvalid     = "invalid_parameter"
	codeUnauthorize = "unauthorized"
	codeForbidden   = "forbidden"
	codeNotFound    = "not_found"
	codeRateLimited = "rate_limited"
	codeUnavailable = "scan_unavailable"

	maxKeyLen      = 256
	maxRequestedBy = 100
	publicRate     = 30 // requests per client address per minute
	publicBodyMax  = 4 << 10
)

var (
	uuidRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	tokenRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

type Handler struct {
	client  *Client
	limiter *addrLimiter
}

func NewHandler(cfg WorkerConfig) *Handler {
	return &Handler{client: NewClient(cfg), limiter: newAddrLimiter(publicRate, time.Minute)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg, field string) {
	body := map[string]string{"code": code, "message": msg}
	if field != "" {
		body["field"] = field
	}
	writeJSON(w, status, map[string]any{"error": body})
}

type startRequest struct {
	TenantID    string `json:"tenant_id"`
	APIKey      string `json:"api_key"`
	RequestedBy string `json:"requested_by"`
}

// PublicStart handles POST /api/v1/ispm-scans.
func (h *Handler) PublicStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.client.Configured() {
		h.fail(w, r, "start scan", ErrWorkerNotConfigured)
		return
	}
	if !h.allow(w, r) || !noQuery(w, r) {
		return
	}
	var req startRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, publicBodyMax))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, codeInvalid, `body must be JSON: {"tenant_id":"<uuid>"}`, "body")
		return
	}
	key, ok := apiKey(r, req.APIKey)
	if !ok {
		unauthorized(w)
		return
	}
	tenantID := strings.ToLower(strings.TrimSpace(req.TenantID))
	switch {
	case tenantID == "":
		writeErr(w, http.StatusBadRequest, codeInvalid, "tenant_id is required", "tenant_id")
		return
	case !uuidRE.MatchString(tenantID):
		writeErr(w, http.StatusBadRequest, codeInvalid, "tenant_id must be a UUID", "tenant_id")
		return
	}
	h.respondStart(w, r, func() (*StartResult, error) {
		return h.client.StartAs(r.Context(), key, tenantID, label(req.RequestedBy))
	})
}

// PublicJob handles GET /api/v1/ispm-scans/{job_id}.
func (h *Handler) PublicJob(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.client.Configured() {
		h.fail(w, r, "load scan job", ErrWorkerNotConfigured)
		return
	}
	if !h.allow(w, r) || !noQuery(w, r) {
		return
	}
	key, ok := apiKey(r, "")
	if !ok {
		unauthorized(w)
		return
	}
	jobID := strings.TrimSpace(mux.Vars(r)["job_id"])
	if !tokenRE.MatchString(jobID) {
		writeErr(w, http.StatusBadRequest, codeInvalid, "job_id is not valid", "job_id")
		return
	}
	job, err := h.client.JobAs(r.Context(), key, jobID)
	if err != nil {
		h.fail(w, r, "load scan job", err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: job})
}

// canScan: the roles that may start a scan from the console (the console's
// super admin is stored as super_admin or superadmin).
func canScan(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "analyst", "manager", "admin", "super_admin", "superadmin":
		return true
	}
	return false
}

// Start handles POST /api/v1/ispm/scans (login): the tenant is the caller's.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	claims := middleware.GetClaims(r)
	if claims == nil {
		writeErr(w, http.StatusUnauthorized, codeUnauthorize, "missing or invalid token", "")
		return
	}
	if !canScan(claims.Role) {
		writeErr(w, http.StatusForbidden, codeForbidden, "your role may not start a scan", "")
		return
	}
	if !rejectKeyParams(w, r) {
		return
	}
	var none struct{}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&none); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, codeInvalid, "this request takes no fields: the tenant is taken from your login", "body")
		return
	}
	h.respondStart(w, r, func() (*StartResult, error) {
		return h.client.Start(r.Context(), claims.TenantID, claims.UserID)
	})
}

// Job handles GET /api/v1/ispm/scans/{job_id} (login).
func (h *Handler) Job(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if middleware.GetClaims(r) == nil {
		writeErr(w, http.StatusUnauthorized, codeUnauthorize, "missing or invalid token", "")
		return
	}
	jobID := strings.TrimSpace(mux.Vars(r)["job_id"])
	if !tokenRE.MatchString(jobID) {
		writeErr(w, http.StatusBadRequest, codeInvalid, "job_id is not valid", "job_id")
		return
	}
	if !rejectKeyParams(w, r) {
		return
	}
	job, err := h.client.Job(r.Context(), jobID)
	if err != nil {
		h.fail(w, r, "load scan job", err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: job})
}

func (h *Handler) respondStart(w http.ResponseWriter, r *http.Request, start func() (*StartResult, error)) {
	res, err := start()
	if err != nil {
		h.fail(w, r, "start scan", err)
		return
	}
	status := http.StatusAccepted
	if res.AlreadyRunning {
		status = http.StatusOK
	}
	writeJSON(w, status, Response{Data: res})
}

// fail maps worker errors to the documented body. Upstream text is logged,
// never returned.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log := logging.FromContext(r.Context())
	switch {
	case errors.Is(err, ErrWorkerNotConfigured):
		writeErr(w, http.StatusServiceUnavailable, codeUnavailable, "scanning is not configured on this server", "")
	case errors.Is(err, ErrWorkerAuth):
		if r.Header.Get("Authorization") != "" && middleware.GetClaims(r) != nil {
			// Console route: the key is ours, so this is a setup problem, not the caller's.
			log.Error("scan api: the worker rejected WORKER_SCAN_TOKEN", "what", what)
			writeErr(w, http.StatusBadGateway, codeUnavailable, "the scan service could not be reached; try again shortly", "")
			return
		}
		unauthorized(w)
	case errors.Is(err, ErrTenantUnknown), errors.Is(err, ErrJobNotFound):
		log.Warn("scan api: not found", "what", what, "err", err)
		writeErr(w, http.StatusNotFound, codeNotFound, "tenant or job not found", "")
	case errors.Is(err, ErrWorkerRejected):
		log.Warn("scan api: worker rejected the request", "what", what, "err", err)
		writeErr(w, http.StatusBadRequest, codeInvalid, "the scan service rejected the request", "")
	default:
		log.Error("scan api: worker unavailable", "what", what, "err", err)
		writeErr(w, http.StatusBadGateway, codeUnavailable, "the scan service could not be reached; try again shortly", "")
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="ispm-scan"`)
	writeErr(w, http.StatusUnauthorized, codeUnauthorize, "missing or invalid API key: send it in the X-API-Key header (or as api_key in the JSON body)", "")
}

// apiKey finds the caller's key: X-API-Key, then "Authorization: Bearer", then
// the body field. Printable ASCII, sane length (it becomes a header value).
func apiKey(r *http.Request, fromBody string) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if key == "" {
		if scheme, rest, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " "); ok && strings.EqualFold(scheme, "Bearer") {
			key = strings.TrimSpace(rest)
		}
	}
	if key == "" {
		key = strings.TrimSpace(fromBody)
	}
	if key == "" || len(key) > maxKeyLen {
		return "", false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return "", false
		}
	}
	return key, true
}

// noQuery refuses any query string; a key in the URL gets its own message.
func noQuery(w http.ResponseWriter, r *http.Request) bool {
	for k := range r.URL.Query() {
		msg := "this endpoint takes no query parameters"
		if isKeyParam(k) {
			msg = "never put the API key in the URL: send it in the X-API-Key header (or as api_key in the JSON body)"
		}
		writeErr(w, http.StatusBadRequest, codeInvalid, msg, k)
		return false
	}
	return true
}

// rejectKeyParams: console callers send only their login token, no query.
func rejectKeyParams(w http.ResponseWriter, r *http.Request) bool {
	for k := range r.URL.Query() {
		msg := "this endpoint takes no query parameters"
		if isKeyParam(k) {
			msg = "do not send an API key: the worker key is configured on the backend (WORKER_SCAN_TOKEN). Send only your login token"
		}
		writeErr(w, http.StatusBadRequest, codeInvalid, msg, k)
		return false
	}
	return true
}

func isKeyParam(name string) bool {
	switch strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name)) {
	case "apikey", "key", "token", "accesstoken", "scantoken", "workertoken":
		return true
	}
	return false
}

// label cleans the optional requested_by text: trimmed, printable, short.
func label(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if len(s) > maxRequestedBy {
		s = s[:maxRequestedBy]
	}
	if s == "" {
		return "api-key"
	}
	return s
}

func (h *Handler) allow(w http.ResponseWriter, r *http.Request) bool {
	ok, wait := h.limiter.allow(clientAddr(r), time.Now())
	if ok {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeErr(w, http.StatusTooManyRequests, codeRateLimited, "too many requests; slow down", "")
	return false
}

// clientAddr is the TCP peer. X-Forwarded-For is deliberately not trusted
// here: anyone could forge it to dodge the limit.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// addrLimiter is a per-address token bucket: it stops the key being guessed or
// the worker flooded through the open endpoint.
type addrLimiter struct {
	mu      sync.Mutex
	perMin  float64
	burst   float64
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newAddrLimiter(perPeriod int, period time.Duration) *addrLimiter {
	return &addrLimiter{perMin: float64(perPeriod) / period.Minutes(), burst: float64(perPeriod), buckets: map[string]*bucket{}}
}

func (l *addrLimiter) allow(addr string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.at) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b := l.buckets[addr]
	if b == nil {
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[addr] = b
	}
	b.tokens += now.Sub(b.at).Minutes() * l.perMin
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.perMin * float64(time.Minute))
}
