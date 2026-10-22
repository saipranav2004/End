package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"nhi-backend-api/internal/logging"
)

const requestIDHeader = "X-Request-ID"

// RequestID gives every request an ID (the caller's X-Request-ID when it is
// well-formed, otherwise a new one), echoes it on the response and makes it
// available to log lines through the request context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := logging.WithRequest(r.Context(), &logging.RequestInfo{ID: id})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// validRequestID accepts 8-64 characters of [A-Za-z0-9._-], so a caller cannot
// inject newlines or huge values into the logs.
func validRequestID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
