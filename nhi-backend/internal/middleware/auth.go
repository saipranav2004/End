package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"nhi-backend-api/internal/config"
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
			respondUnauthorized(w, "Missing or invalid Bearer token")
			return
		}
		claims, err := jwt.ValidateToken(parts[1], m.jwtCfg.Secret)
		if err != nil {
			respondUnauthorized(w, "Invalid or expired token")
			return
		}
		claims.UserID = strings.TrimSpace(claims.UserID)
		claims.TenantID = strings.TrimSpace(claims.TenantID)
		claims.DBName = strings.TrimSpace(claims.DBName)
		claims.ID = strings.TrimSpace(claims.ID)
		if !sessionIDRE.MatchString(claims.ID) {
			respondUnauthorized(w, "Token is missing a valid session id")
			return
		}
		if claims.UserID == "" || claims.UserID != strings.TrimSpace(claims.Email) || claims.UserID != strings.TrimSpace(claims.Subject) || !tenantIDRE.MatchString(claims.TenantID) || config.ValidateSchemaName(claims.DBName) != nil {
			respondUnauthorized(w, "Token is missing or has invalid tenant claims")
			return
		}
		if m.validator == nil {
			respondError(w, http.StatusServiceUnavailable, "Authentication state validation is unavailable", "AUTH_STATE_UNAVAILABLE")
			return
		}
		valid, role, err := m.validator.ValidateAuthContext(r.Context(), claims.UserID, claims.TenantID, claims.DBName, claims.ID)
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, "Unable to verify account and tenant status", "AUTH_STATE_CHECK_FAILED")
			return
		}
		if !valid {
			respondUnauthorized(w, "Session has ended or account is no longer active")
			return
		}
		claims.Role = role // the database's current role, not the token's
		ctx := context.WithValue(r.Context(), UserClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func respondUnauthorized(w http.ResponseWriter, message string) {
	respondError(w, http.StatusUnauthorized, message, "UNAUTHORIZED")
}

func respondError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(models.APIResponse{Success: false, Message: message, Error: code})
}

func GetClaims(r *http.Request) *jwt.CustomClaims {
	if r == nil {
		return nil
	}
	claims, _ := r.Context().Value(UserClaimsKey).(*jwt.CustomClaims)
	return claims
}

func GetDBName(r *http.Request) string {
	if claims := GetClaims(r); claims != nil {
		return claims.DBName
	}
	return ""
}

func GetTenantID(r *http.Request) string {
	if claims := GetClaims(r); claims != nil {
		return claims.TenantID
	}
	return ""
}
