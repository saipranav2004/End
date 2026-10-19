package middleware

import (
	"fmt"
	"net/http"
	"time"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// RequestLogger logs incoming HTTP requests with latency and status code
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		statusColor := "🟢"
		if rw.statusCode >= 400 && rw.statusCode < 500 {
			statusColor = "🟡"
		} else if rw.statusCode >= 500 {
			statusColor = "🔴"
		}

		fmt.Printf("%s [%s] %s %s - Status: %d (%v)\n",
			statusColor,
			time.Now().Format("15:04:05"),
			r.Method,
			r.URL.Path,
			rw.statusCode,
			duration,
		)
	})
}
