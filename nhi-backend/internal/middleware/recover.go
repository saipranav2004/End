package middleware

import (
	"net/http"
	"runtime/debug"

	"nhi-backend-api/internal/logging"
)

// Recoverer turns a handler panic into a logged JSON 500 instead of a dropped
// connection. http.ErrAbortHandler is re-panicked so net/http can abort quietly.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			logging.FromContext(r.Context()).Error("panic serving request", "method", r.Method, "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
			respondError(w, r, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
		}()
		next.ServeHTTP(w, r)
	})
}
