// Package logging configures the process-wide structured logger and carries
// per-request fields (request id, tenant, user) through the context.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"sync"
)

// Setup installs a slog handler as the default logger. format is "json"
// (default) or "text"; level is debug, info (default), warn or error. Because
// slog.SetDefault also routes the standard log package through the handler,
// existing log.Printf calls are emitted as structured INFO records.
func Setup(level, format string) *slog.Logger {
	return setup(os.Stdout, level, format)
}

func setup(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: ParseLevel(level)}
	var h slog.Handler
	if strings.EqualFold(strings.TrimSpace(format), "text") {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

// ParseLevel maps a level name to a slog level; unknown values mean info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// RequestInfo is the per-request identity that log lines carry. The request-id
// middleware creates it and the auth middleware fills in the tenant and user,
// so it is guarded by a mutex.
type RequestInfo struct {
	ID string

	mu       sync.Mutex
	tenantID string
	userID   string
}

// SetActor records who the request is for once authentication has succeeded.
func (ri *RequestInfo) SetActor(tenantID, userID string) {
	if ri == nil {
		return
	}
	ri.mu.Lock()
	ri.tenantID, ri.userID = tenantID, userID
	ri.mu.Unlock()
}

// Actor returns the tenant and user recorded by SetActor.
func (ri *RequestInfo) Actor() (tenantID, userID string) {
	if ri == nil {
		return "", ""
	}
	ri.mu.Lock()
	defer ri.mu.Unlock()
	return ri.tenantID, ri.userID
}

type ctxKey struct{}

// WithRequest stores ri in the context.
func WithRequest(ctx context.Context, ri *RequestInfo) context.Context {
	return context.WithValue(ctx, ctxKey{}, ri)
}

// RequestFrom returns the request info in ctx, or nil.
func RequestFrom(ctx context.Context) *RequestInfo {
	ri, _ := ctx.Value(ctxKey{}).(*RequestInfo)
	return ri
}

// FromContext returns the default logger with request_id, tenant_id and
// user_id attached when the context belongs to a request.
func FromContext(ctx context.Context) *slog.Logger {
	l := slog.Default()
	ri := RequestFrom(ctx)
	if ri == nil {
		return l
	}
	if ri.ID != "" {
		l = l.With("request_id", ri.ID)
	}
	if t, u := ri.Actor(); t != "" || u != "" {
		l = l.With("tenant_id", t, "user_id", u)
	}
	return l
}

// Audit logs a security-relevant action. actor and tenant may be empty (a
// failed login has neither). Never pass secrets in target or outcome.
func Audit(ctx context.Context, action, actor, tenant, target, outcome string) {
	FromContext(ctx).Info("audit", "audit", true, "action", action, "actor", actor,
		"tenant", tenant, "target", target, "outcome", outcome)
}

// Recover is deferred at the top of a goroutine started during a request. The
// Recoverer middleware only protects the handler's own goroutine, so a panic
// anywhere else would take the whole process down. It logs the panic with the
// request id and a stack trace and swallows it; the goroutine then returns
// normally, and callers that track failure should record it themselves.
// It reports whether a panic was recovered.
//
//	defer logging.Recover(ctx, "identity events: sessions")
func Recover(ctx context.Context, name string, onPanic ...func()) {
	rec := recover()
	if rec == nil {
		return
	}
	FromContext(ctx).Error("panic in goroutine", "goroutine", name, "panic", rec, "stack", string(debug.Stack()))
	for _, f := range onPanic {
		f()
	}
}

// WarnIf logs err at WARN level when it is non-nil and returns whether it was.
// It is for errors that are deliberately tolerated (the response is served
// without the data) but must not be invisible, such as a stored JSON column
// that no longer parses.
func WarnIf(ctx context.Context, what string, err error, attrs ...any) bool {
	if err == nil {
		return false
	}
	FromContext(ctx).Warn(what+" failed, continuing without it", append([]any{"err", err}, attrs...)...)
	return true
}
