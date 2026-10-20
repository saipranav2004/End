package middleware

import (
	"log"
	"net/http"

	"github.com/rs/cors"

	"nhi-backend-api/internal/config"
)

// SetupCORS configures cross-origin resource sharing for the React frontend
func SetupCORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	origins := cfg.AllowedOrigins
	if len(origins) == 0 {
		// Only matters when the console is served from another origin; behind
		// the console's nginx (same origin) CORS is not used at all.
		origins = []string{"http://localhost:3000", "http://localhost:5173"}
		log.Printf("cors: no allowed origins configured; allowing only %v (set NHI_CORS_ALLOWED_ORIGINS)", origins)
	}

	c := cors.New(cors.Options{
		AllowedOrigins: origins,
		// PATCH and If-None-Match serve the posture API (PATCH
		// /findings/{id}, optional ETag revalidation on GETs).
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type", "Accept", "If-Match", "If-None-Match", "X-Request-ID"},
		// ETag must be readable by the browser to send it back as If-None-Match.
		ExposedHeaders:   []string{"ETag", "Retry-After", "Content-Disposition", "X-Request-ID"},
		AllowCredentials: true,
	})

	return c.Handler
}
