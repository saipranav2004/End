package jwt

import (
	"errors"
	"strings"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

const issuer = "nhi-backend-api"

type CustomClaims struct {
	UserID   string `json:"user_id"` // Canonical email; this is also the user_settings key.
	Email    string `json:"email"`
	TenantID string `json:"tenant_id"`
	DBName   string `json:"db_name"`
	Role     string `json:"role"`
	jwtv5.RegisteredClaims
}

func GenerateToken(email, tenantID, dbName, role, sessionID, secret string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(dbName) == "" {
		return "", errors.New("JWT requires user_id, tenant_id and db_name")
	}
	if len(secret) < 32 {
		return "", errors.New("JWT signing secret must be at least 32 characters")
	}
	if ttl <= 0 {
		return "", errors.New("JWT expiry must be positive")
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("JWT requires a session id")
	}
	email = strings.TrimSpace(email)
	claims := CustomClaims{
		UserID:   email,
		Email:    email,
		TenantID: strings.TrimSpace(tenantID),
		DBName:   strings.TrimSpace(dbName),
		Role:     strings.TrimSpace(role),
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    issuer,
			Subject:   email,
			ID:        strings.TrimSpace(sessionID),
			IssuedAt:  jwtv5.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwtv5.NewNumericDate(time.Now().UTC().Add(ttl)),
		},
	}
	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateToken(tokenString, secret string) (*CustomClaims, error) {
	if strings.TrimSpace(tokenString) == "" || len(secret) < 32 {
		return nil, errors.New("invalid token or server key")
	}
	claims := &CustomClaims{}
	token, err := jwtv5.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwtv5.Token) (interface{}, error) {
			if token.Method != jwtv5.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		},
		jwtv5.WithValidMethods([]string{jwtv5.SigningMethodHS256.Alg()}),
		jwtv5.WithIssuer(issuer),
	)
	if err != nil {
		return nil, err
	}
	if token == nil || !token.Valid || claims.ExpiresAt == nil || claims.ExpiresAt.Time.IsZero() {
		return nil, errors.New("invalid or non-expiring token")
	}
	if strings.TrimSpace(claims.ID) == "" {
		return nil, errors.New("token has no session id")
	}
	return claims, nil
}
