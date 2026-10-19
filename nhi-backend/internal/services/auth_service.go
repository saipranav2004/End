package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/models"
	"nhi-backend-api/internal/repository"
	"nhi-backend-api/pkg/jwt"
)

var (
	ErrAccountDisabled     = errors.New("account is not active")
	ErrTenantNotConfigured = errors.New("tenant database is not configured for this account")
	ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")
)

type AuthService struct {
	userRepo *repository.UserRepository
	jwtCfg   config.JWTConfig
}

func NewAuthService(userRepo *repository.UserRepository, jwtCfg config.JWTConfig) *AuthService {
	return &AuthService{userRepo: userRepo, jwtCfg: jwtCfg}
}

// Login authenticates against the global database, resolves the tenant schema
// from the trusted registry, and issues a token with the PDF's claim contract.
func (s *AuthService) Login(ctx context.Context, req models.LoginRequest) (*models.LoginResponse, error) {
	email := strings.TrimSpace(req.Email)
	if email == "" || req.Password == "" {
		return nil, errors.New("email and password are required")
	}
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authentication lookup failed: %w", err)
	}
	if user == nil {
		return nil, errors.New("invalid email or password")
	}
	if !strings.EqualFold(strings.TrimSpace(user.Status), "active") {
		return nil, ErrAccountDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, errors.New("invalid email or password")
	}

	// Do not issue a token if the audit update failed: the documented login
	// process requires last_login_at to be recorded for a successful login.
	if err := s.userRepo.UpdateLastLogin(ctx, user.ID); err != nil {
		return nil, fmt.Errorf("record last login: %w", err)
	}
	now := time.Now().UTC()
	user.LastLoginAt = &now
	sessionID, err := newSessionID()
	if err != nil {
		return nil, fmt.Errorf("issue session id: %w", err)
	}
	return s.issueTokens(ctx, user, sessionID)
}

// issueTokens resolves the tenant schema and returns a fresh access token plus
// a newly stored refresh token for an already-authenticated, active user.
func (s *AuthService) issueTokens(ctx context.Context, user *models.User, sessionID string) (*models.LoginResponse, error) {
	dbName, err := s.userRepo.GetTenantDBName(ctx, user.TenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant resolution failed: %w", err)
	}
	if err := config.ValidateSchemaName(dbName); err != nil {
		return nil, ErrTenantNotConfigured
	}
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))

	accessTTL := time.Duration(s.jwtCfg.AccessTTLMinutes) * time.Minute
	refreshTTL := time.Duration(s.jwtCfg.RefreshTTLHours) * time.Hour

	token, err := jwt.GenerateToken(user.Email, user.TenantID, dbName, user.Role, sessionID, s.jwtCfg.Secret, accessTTL)
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}

	refresh, err := newRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("issue refresh token: %w", err)
	}
	if err := s.userRepo.CreateRefreshToken(ctx, user.ID, sessionID, hashRefreshToken(refresh), time.Now().UTC().Add(refreshTTL)); err != nil {
		return nil, err
	}

	user.Password = ""
	return &models.LoginResponse{
		Token:            token,
		RefreshToken:     refresh,
		User:             *user,
		ExpiresIn:        int(accessTTL.Seconds()),
		RefreshExpiresIn: int(refreshTTL.Seconds()),
	}, nil
}

// Refresh exchanges a valid refresh token for a new access + refresh pair. The
// presented token is revoked (single use). Re-presenting an already-used token
// revokes all of that user's refresh tokens.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*models.LoginResponse, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, ErrInvalidRefreshToken
	}
	userID, sessionID, found, reused, err := s.userRepo.ConsumeRefreshToken(ctx, hashRefreshToken(refreshToken))
	if err != nil {
		return nil, err
	}
	if reused {
		if err := s.userRepo.RevokeAllRefreshTokens(ctx, userID); err != nil {
			return nil, err
		}
		return nil, ErrInvalidRefreshToken
	}
	if !found {
		return nil, ErrInvalidRefreshToken
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !strings.EqualFold(strings.TrimSpace(user.Status), "active") {
		return nil, ErrAccountDisabled
	}
	return s.issueTokens(ctx, user, sessionID)
}

// Logout ends the session the refresh token belongs to, so both its refresh
// tokens and its access tokens stop working. Unknown tokens are ignored.
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return ErrInvalidRefreshToken
	}
	return s.userRepo.RevokeRefreshSession(ctx, hashRefreshToken(refreshToken))
}

// newSessionID returns a random RFC 4122 version-4 UUID.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func newRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AuthService) GetUserByID(ctx context.Context, id int) (*models.User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user != nil {
		user.Password = ""
	}
	return user, nil
}
