package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	chclient "nhi-backend-api/internal/clickhouse"
	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/logging"
	"nhi-backend-api/internal/models"
	"nhi-backend-api/pkg/jwt"
)

type contextKey string

const UserClaimsKey contextKey = "userClaims"

var tenantIDRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var sessionIDRE = tenantIDRE

type AuthContextValidator interface {
	ValidateAuthContext(ctx context.Context, email, tenantID, dbName, sessionID string) (valid bool, role string, err error)
}

type AuthMiddleware struct {
	jwtCfg    config.JWTConfig
	validator AuthContextValidator
}

func NewAuthMiddleware(jwtCfg config.JWTConfig, validator AuthContextValidator) *AuthMiddleware {
	return &AuthMiddleware{jwtCfg: jwtCfg, validator: validator}
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			respondUnauthorized(w, r, "Missing or invalid Bearer token")
			return
		}
		claims, err := jwt.ValidateToken(parts[1], m.jwtCfg.Secret)
		if err != nil {
			respondUnauthorized(w, r, "Invalid or expired token")
			return
		}
		claims.UserID = strings.TrimSpace(claims.UserID)
		claims.TenantID = strings.TrimSpace(claims.TenantID)
		claims.DBName = strings.TrimSpace(claims.DBName)
		claims.ID = strings.TrimSpace(claims.ID)
		if !sessionIDRE.MatchString(claims.ID) {
			respondUnauthorized(w, r, "Token is missing a valid session id")
			return
		}
		if claims.UserID == "" || claims.UserID != strings.TrimSpace(claims.Email) || claims.UserID != strings.TrimSpace(claims.Subject) || !tenantIDRE.MatchString(claims.TenantID) || config.ValidateSchemaName(claims.DBName) != nil {
			respondUnauthorized(w, r, "Token is missing or has invalid tenant claims")
			return
		}
		if m.validator == nil {
			logging.FromContext(r.Context()).Error("auth state validator is not configured")
			respondError(w, r, http.StatusServiceUnavailable, "Authentication state validation is unavailable", "AUTH_STATE_UNAVAILABLE")
			return
		}
		valid, role, err := m.validator.ValidateAuthContext(r.Context(), claims.UserID, claims.TenantID, claims.DBName, claims.ID)
		if err != nil {
			logging.FromContext(r.Context()).Error("auth state check failed", "err", err, "tenant_id", claims.TenantID)
			respondError(w, r, http.StatusServiceUnavailable, "Unable to verify account and tenant status", "AUTH_STATE_CHECK_FAILED")
			return
		}
		if !valid {
			respondUnauthorized(w, r, "Session has ended or account is no longer active")
			return
		}
		claims.Role = role                                                 // the database's current role, not the token's
		r = r.WithContext(chclient.WithTenant(r.Context(), claims.DBName)) // per-tenant ClickHouse database
		logging.RequestFrom(r.Context()).SetActor(claims.TenantID, claims.UserID)
		ctx := context.WithValue(r.Context(), UserClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func respondUnauthorized(w http.ResponseWriter, r *http.Request, message string) {
	logging.FromContext(r.Context()).Warn("authentication rejected", "reason", message)
	respondError(w, r, http.StatusUnauthorized, message, "UNAUTHORIZED")
}

// respondError answers in the error shape of the API family the path belongs
// to, so a client of one family parses every error it can receive:
//
//	/genome/               {error: {code, message, request_id}}
//	/ispm/                 {error: {code, message}}
//	/credential-exposure/  {error: code, message}
//	everything else        {success: false, message, error: code}
func respondError(w http.ResponseWriter, r *http.Request, status int, message, code string) {
	var body any = models.APIResponse{Success: false, Message: message, Error: code}
	path := r.URL.Path
	switch {
	case strings.Contains(path, "/genome/"):
		rid := requestID(r)
		w.Header().Set("X-Request-ID", rid)
		body = map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": rid}}
	case strings.Contains(path, "/ispm/"):
		body = map[string]any{"error": map[string]string{"code": code, "message": message}}
	case strings.Contains(path, "/credential-exposure/"):
		body = map[string]string{"error": code, "message": message}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

var requestIDRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// requestID echoes a well-formed client X-Request-ID or mints one (the
// genome handlers do the same).
func requestID(r *http.Request) string {
	if ri := logging.RequestFrom(r.Context()); ri != nil && ri.ID != "" {
		return ri.ID // the id the request logger and the response header already carry
	}
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestIDRE.MatchString(id) {
		return id
	}
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "req_" + hex.EncodeToString(b[:])
}

func GetClaims(r *http.Request) *jwt.CustomClaims {
	if r == nil {
		return nil
	}
	claims, _ := r.Context().Value(UserClaimsKey).(*jwt.CustomClaims)
	return claims
}
