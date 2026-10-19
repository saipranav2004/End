package middleware

import (
	"net/http"

	"github.com/rs/cors"

	"nhi-backend-api/internal/config"
)

// SetupCORS configures cross-origin resource sharing for the React frontend
func SetupCORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	origins := cfg.AllowedOrigins
	if len(origins) == 0 {
		origins = []string{"http://localhost:3000", "http://localhost:5173"}
	}

	c := cors.New(cors.Options{
		AllowedOrigins: origins,
		// PATCH and If-None-Match serve the posture API (PATCH
		// /findings/{id}, optional ETag revalidation on GETs).
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type", "Accept", "If-Match", "If-None-Match"},
		// ETag must be readable by the browser to send it back as If-None-Match.
		ExposedHeaders:   []string{"ETag", "Retry-After", "Content-Disposition"},
		AllowCredentials: true,
	})

	return c.Handler
}
