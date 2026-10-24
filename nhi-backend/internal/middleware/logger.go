package middleware

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/logging"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
	// errBody keeps the start of a 5xx body, so the request line can show what
	// the client was told even when no handler logged the underlying error.
	errBody []byte
}

const maxErrBody = 512

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	if rw.statusCode >= 500 && len(rw.errBody) < maxErrBody {
		room := maxErrBody - len(rw.errBody)
		if room > n {
			room = n
		}
		rw.errBody = append(rw.errBody, b[:room]...)
	}
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer (Flush, Hijack).
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// Flush keeps streaming responses (such as the posture export) working.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return h.Hijack()
}

// RequestLogger writes one structured line per request. It logs the route
// template rather than only the raw path, and never the query string or any
// header, so tokens cannot reach the logs. 5xx is ERROR, 4xx WARN, the rest
// INFO; health checks are DEBUG so they do not flood the output.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		level := slog.LevelInfo
		switch {
		case rw.statusCode >= 500:
			level = slog.LevelError
		case rw.statusCode >= 400:
			level = slog.LevelWarn
		case r.URL.Path == "/api/v1/health":
			level = slog.LevelDebug
		}
		ctx := r.Context()
		if !slog.Default().Enabled(ctx, level) {
			return
		}

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.statusCode),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.Int("bytes", rw.bytes),
			slog.String("remote_ip", remoteIP(r)),
			slog.String("user_agent", r.UserAgent()),
		}
		if len(rw.errBody) > 0 {
			attrs = append(attrs, slog.String("response_body", string(rw.errBody)))
		}
		if route := mux.CurrentRoute(r); route != nil {
			if tpl, err := route.GetPathTemplate(); err == nil {
				attrs = append(attrs, slog.String("route", tpl))
			}
		}
		if ri := logging.RequestFrom(ctx); ri != nil {
			attrs = append(attrs, slog.String("request_id", ri.ID))
			if tenant, user := ri.Actor(); tenant != "" || user != "" {
				attrs = append(attrs, slog.String("tenant_id", tenant), slog.String("user_id", user))
			}
		}
		slog.LogAttrs(ctx, level, "http request", attrs...)
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
