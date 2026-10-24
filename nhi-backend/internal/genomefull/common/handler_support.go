package common

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/pkg/jwt"
)

// Helpers shared by every genome handler: request parsing, the envelope of
// envelope.go and the error handling every endpoint goes through.
//
// Error handling contract of this package
//
//   - Every response — success or failure — is the envelope, also echoed in
//     the X-Request-ID header. Clients and the server log can be correlated
//     through that id.
//   - Client mistakes are answered before any store is touched: 400
//     VALIDATION_ERROR (path, query, body), 401 UNAUTHORIZED, 413
//     PAYLOAD_TOO_LARGE, 415 UNSUPPORTED_MEDIA_TYPE.
//   - Service errors are mapped in exactly one place, WriteServiceError: 404
//     IDENTITY_NOT_FOUND / ANOMALY_NOT_FOUND, 501 NOT_IMPLEMENTED, 503
//     STORE_UNAVAILABLE, 504 STORE_TIMEOUT, otherwise 500 INTERNAL_ERROR.
//   - Each request runs under RequestBudget (RequestContext), so a slow
//     store yields a clean 504 instead of a dropped connection.
//   - A panic inside a handler is caught by Recover (in addition to the
//     global middleware.Recoverer) and answered as 500 in the same envelope;
//     the stack goes to the server log only.
//   - Messages sent to the client never contain SQL, host names, driver
//     errors or tenant data; those go to the log line
//     "genome <endpoint> rid=<id> tenant=<tenant>: …".

// RequestBudget bounds the work done for one request (all PostgreSQL and
// ClickHouse reads together). It is deliberately below the 15s
// http.Server WriteTimeout set in cmd/api-server/main.go, so an over-long
// request is answered with 504 STORE_TIMEOUT instead of the server closing
// the connection mid-response.
const RequestBudget = 12 * time.Second

// Protective input limits (not business rules): a request beyond them is
// rejected with 400 before any store is touched.
const (
	MaxBodyBytes          = 1 << 20 // the feedback/freeze bodies are a few hundred bytes
	maxCursorLength       = 2048    // cursors issued by this API are well under 200 characters
	maxSearchLength       = 200     // list / search query parameters
	maxIdentityNameLength = 2048    // AWS ARNs are at most 2048 characters
	maxSourceTypeLength   = 64      // compact upper-case family, e.g. IAM_ROLE
)

// §2 paging bounds shared by every page-size parameter (limit default 25,
// maximum 100).
const (
	IdentityPageDefault = 25
	IdentityPageMax     = 100
)

// RequestContext derives the per-request context every service call must
// use. The CancelFunc must be deferred by the caller.
func RequestContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), RequestBudget)
}

// ParseTime parses an ISO-8601 timestamp or date, as the API's §2 accepts.
func ParseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("must be an ISO-8601 timestamp such as 2026-10-01T00:00:00Z")
}

// ---- envelope helpers -------------------------------------------------------

var requestIDRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// RequestID echoes a well-formed client X-Request-ID or mints one.
func RequestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestIDRE.MatchString(id) {
		return id
	}
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_unavailable"
	}
	return "req_" + hex.EncodeToString(b[:])
}

// WriteJSON writes body as the response. It is encoded in memory first so
// an encoding failure becomes a proper 500 envelope instead of a truncated
// 200, and so Content-Length can be set.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	buf, err := json.Marshal(body)
	if err != nil {
		log.Printf("genome: response encoding failed: %v", err)
		rid := w.Header().Get("X-Request-ID")
		buf, _ = json.Marshal(ErrorEnvelope{Error: ErrorDetail{Code: CodeInternal, Message: "response could not be encoded", RequestID: rid}})
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)+1))
	w.WriteHeader(status)
	_, _ = w.Write(buf)
	_, _ = w.Write([]byte{'\n'})
}

// WriteError writes the error envelope.
func WriteError(w http.ResponseWriter, status int, code, msg, rid string) {
	WriteJSON(w, status, ErrorEnvelope{Error: ErrorDetail{Code: code, Message: msg, RequestID: rid}})
}

// RequireClaims returns the JWT claims the auth middleware stored on the
// request, or writes 401 and returns ok=false. The route is behind
// middleware.AuthMiddleware, so a nil result means the middleware chain was
// bypassed or misconfigured — it is still answered, never dereferenced.
func RequireClaims(w http.ResponseWriter, r *http.Request, rid string) (claims *jwt.CustomClaims, ok bool) {
	c := middleware.GetClaims(r)
	if c == nil {
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "authentication required", rid)
		return nil, false
	}
	if strings.TrimSpace(c.UserID) == "" || strings.TrimSpace(c.TenantID) == "" || strings.TrimSpace(c.DBName) == "" {
		// The token is valid but carries no usable tenant scope: no query
		// could be scoped, so refuse instead of failing later with a 503.
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "token does not carry a tenant scope (user id, tenant id and database name are required)", rid)
		return nil, false
	}
	return c, true
}

// Log writes one server-side log line of a failed request: endpoint,
// request id, tenant and the full error (never sent to clients).
func Log(r *http.Request, endpoint, rid, format string, args ...any) {
	tenant := "-"
	if c := middleware.GetClaims(r); c != nil && c.TenantID != "" {
		tenant = c.TenantID
	}
	log.Printf("genome %s rid=%s tenant=%s: %s", endpoint, rid, tenant, fmt.Sprintf(format, args...))
}

// WriteServiceError maps every error a genome service can return onto the
// error envelope. The order matters: a client that has already gone away
// gets no response at all; the request budget wins over whatever the store
// reported; then the typed service errors; anything unknown is 500.
func WriteServiceError(w http.ResponseWriter, r *http.Request, endpoint, rid string, err error) {
	var (
		invalid     *InvalidInputError
		unavailable *StoreUnavailableError
	)
	switch {
	case errors.Is(r.Context().Err(), context.Canceled):
		// The client closed the connection; nothing can be delivered.
		Log(r, endpoint, rid, "client closed the connection before the response was ready")
	case errors.Is(err, ErrInvalidCursor):
		WriteError(w, http.StatusBadRequest, CodeValidation, "cursor is invalid", rid)
	case errors.As(err, &invalid):
		WriteError(w, http.StatusBadRequest, CodeValidation, invalid.Msg, rid)
	case errors.Is(err, ErrIdentityNotFound):
		WriteError(w, http.StatusNotFound, CodeIdentityNotFound, "The requested identity was not found", rid)
	case errors.Is(err, ErrAnomalyNotFound):
		WriteError(w, http.StatusNotFound, CodeAnomalyNotFound, "The requested anomaly was not found", rid)
	case errors.Is(err, ErrNotImplemented):
		WriteError(w, http.StatusNotImplemented, CodeNotImplemented, "this operation is not available yet", rid)
	case IsStoreTimeout(err):
		Log(r, endpoint, rid, "timed out (budget %s): %v", RequestBudget, err)
		WriteError(w, http.StatusGatewayTimeout, CodeStoreTimeout,
			fmt.Sprintf("the request did not complete within %s; narrow the time window or retry", RequestBudget), rid)
	case errors.As(err, &unavailable):
		Log(r, endpoint, rid, "store unavailable: %v", err)
		WriteError(w, http.StatusServiceUnavailable, CodeStoreUnavailable, unavailable.Reason(), rid)
	default:
		Log(r, endpoint, rid, "unexpected error: %v", err)
		WriteError(w, http.StatusInternalServerError, CodeInternal, "unexpected error", rid)
	}
}

// Recover is deferred first thing by every handler. A panic is turned into
// a 500 envelope with the request id (the stack goes to the log), so a bug
// in one endpoint never produces an empty reply or leaks internals.
// http.ErrAbortHandler is re-raised: it is net/http's own "abort quietly".
func Recover(w http.ResponseWriter, r *http.Request, endpoint, rid string) {
	rec := recover()
	if rec == nil {
		return
	}
	if rec == http.ErrAbortHandler {
		panic(rec)
	}
	Log(r, endpoint, rid, "PANIC: %v\n%s", rec, debug.Stack())
	WriteError(w, http.StatusInternalServerError, CodeInternal, "unexpected error", rid)
}

// ---- query-parameter helpers ------------------------------------------------

// CheckUnknown reports the first query parameter that is not in allowed or
// that was given more than once (ambiguous: the API would silently use one).
func CheckUnknown(q url.Values, allowed map[string]struct{}) (string, bool) {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic message when several are wrong
	for _, k := range keys {
		if _, ok := allowed[k]; !ok {
			return k, true
		}
		if len(q[k]) > 1 {
			return k, true
		}
	}
	return "", false
}

// UnknownParamMessage is the 400 text for CheckUnknown's result.
func UnknownParamMessage(q url.Values, name string, allowed map[string]struct{}) string {
	if _, ok := allowed[name]; ok && len(q[name]) > 1 {
		return fmt.Sprintf("query parameter %q must be given only once", name)
	}
	return fmt.Sprintf("unknown query parameter %q; allowed: %s", name, AllowedList(allowed))
}

// AllowedList renders the allowed parameter names for error messages.
func AllowedList(allowed map[string]struct{}) string {
	if len(allowed) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(allowed))
	for k := range allowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// CheckLength enforces a protective maximum on a free-text parameter.
func CheckLength(name, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s must be at most %d characters", name, max)
	}
	return nil
}

// CheckParamLimits applies the protective limits to the free-text query
// parameters an endpoint may carry (whichever of them are present; the
// endpoint's allowed set has already been enforced by CheckUnknown).
func CheckParamLimits(q url.Values) error {
	if err := CheckSourceType(strings.ToUpper(strings.TrimSpace(q.Get("source_type")))); err != nil {
		return err
	}
	for name, max := range map[string]int{
		"search":        maxSearchLength,
		"cursor":        maxCursorLength,
		"recent_cursor": maxCursorLength,
		"account_id":    maxSourceTypeLength,
		"region":        maxSourceTypeLength,
	} {
		if err := CheckLength(name, q.Get(name), max); err != nil {
			return err
		}
	}
	return nil
}

// sourceTypeRE is the shape of a §2 source_type family: compact upper-case
// identifier such as IAM_ROLE or K8S_SERVICE_ACCOUNT.
var sourceTypeRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// CheckSourceType validates an (already upper-cased) source_type value.
func CheckSourceType(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxSourceTypeLength || !sourceTypeRE.MatchString(value) {
		return fmt.Errorf("source_type must be a compact upper-case family such as IAM_ROLE")
	}
	return nil
}

// ParseIdentityLimit reads a §2 page-size parameter: default 25, 1..100.
func ParseIdentityLimit(q url.Values, name string) (int, error) {
	s := strings.TrimSpace(q.Get(name))
	if s == "" {
		return IdentityPageDefault, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > IdentityPageMax {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", name, IdentityPageMax)
	}
	return n, nil
}

// ParseBoolParam reads an optional true/false query parameter (default false).
func ParseBoolParam(q url.Values, name string) (bool, error) {
	s := strings.TrimSpace(q.Get(name))
	if s == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return v, nil
}

// ---- request-body helper ----------------------------------------------------

// BodyError is a request-body problem together with the HTTP status and
// error code it maps to (400 VALIDATION_ERROR, 413 PAYLOAD_TOO_LARGE or 415
// UNSUPPORTED_MEDIA_TYPE).
type BodyError struct {
	Status  int
	Code    string
	Message string
}

// DecodeJSONBody reads a request body into dst: application/json only, at
// most MaxBodyBytes, exactly one JSON object, unknown fields rejected. It
// returns nil when the body is fine; otherwise the caller writes the
// returned status/code/message as the error envelope.
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) *BodyError {
	if ct := strings.TrimSpace(r.Header.Get("Content-Type")); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			return &BodyError{http.StatusUnsupportedMediaType, CodeUnsupportedMediaType,
				"request body must be sent as Content-Type: application/json"}
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return classifyBodyError(err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return &BodyError{http.StatusBadRequest, CodeValidation, "request body must contain exactly one JSON object"}
	}
	return nil
}

// classifyBodyError turns encoding/json's errors into precise, user-safe
// messages (field, offset, expected type) instead of the raw error text.
func classifyBodyError(err error) *BodyError {
	var (
		maxBytes    *http.MaxBytesError
		syntaxErr   *json.SyntaxError
		typeErr     *json.UnmarshalTypeError
		unknownName = "json: unknown field "
	)
	switch {
	case errors.As(err, &maxBytes):
		return &BodyError{http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			fmt.Sprintf("request body must be at most %d bytes", maxBytes.Limit)}
	case errors.Is(err, io.EOF):
		return &BodyError{http.StatusBadRequest, CodeValidation, "request body is required and must be a JSON object"}
	case errors.Is(err, io.ErrUnexpectedEOF):
		return &BodyError{http.StatusBadRequest, CodeValidation, "request body is incomplete (unexpected end of JSON)"}
	case errors.As(err, &syntaxErr):
		return &BodyError{http.StatusBadRequest, CodeValidation,
			fmt.Sprintf("request body is not valid JSON (at byte offset %d)", syntaxErr.Offset)}
	case errors.As(err, &typeErr):
		field := typeErr.Field
		if field == "" {
			return &BodyError{http.StatusBadRequest, CodeValidation, "request body must be a JSON object"}
		}
		return &BodyError{http.StatusBadRequest, CodeValidation,
			fmt.Sprintf("field %q must be of type %s", field, jsonTypeName(typeErr.Type.Kind().String(), typeErr.Type.String()))}
	case strings.HasPrefix(err.Error(), unknownName):
		field := strings.Trim(strings.TrimPrefix(err.Error(), unknownName), `"`)
		return &BodyError{http.StatusBadRequest, CodeValidation, fmt.Sprintf("unknown field %q in request body", field)}
	}
	return &BodyError{http.StatusBadRequest, CodeValidation, "request body must be a JSON object with the documented fields"}
}

// jsonTypeName renders a Go type as the JSON type a client understands.
func jsonTypeName(kind, goType string) string {
	switch {
	case kind == "string" || strings.HasSuffix(goType, "string"):
		return "string"
	case kind == "bool" || strings.HasSuffix(goType, "bool"):
		return "boolean"
	case strings.HasPrefix(kind, "int") || strings.HasPrefix(kind, "uint") || strings.HasPrefix(kind, "float"):
		return "number"
	case kind == "slice" || kind == "array":
		return "array"
	case kind == "struct" || kind == "map":
		return "object"
	}
	return goType
}

// ParseOptionalWindow reads the §2 from/to parameters when a window is
// optional: either may be absent (nil); when both are present from must be
// before to.
func ParseOptionalWindow(q url.Values) (from, to *time.Time, err error) {
	if s := strings.TrimSpace(q.Get("from")); s != "" {
		t, err := ParseTime(s)
		if err != nil {
			return nil, nil, fmt.Errorf("from: %w", err)
		}
		from = &t
	}
	if s := strings.TrimSpace(q.Get("to")); s != "" {
		t, err := ParseTime(s)
		if err != nil {
			return nil, nil, fmt.Errorf("to: %w", err)
		}
		to = &t
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, fmt.Errorf("from must be before to")
	}
	return from, to, nil
}
