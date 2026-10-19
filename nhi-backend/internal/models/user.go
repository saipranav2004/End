package models

import (
	"strings"
	"time"
)

type User struct {
	ID          int        `json:"id"`
	Email       string     `json:"email"`
	Password    string     `json:"-"` // Omit password in JSON responses
	FullName    string     `json:"full_name"`
	Team        string     `json:"team"` // Maps to department/team
	Role        string     `json:"role"`
	TenantID    string     `json:"tenant_id"`               // 🆕 Added for multi-tenancy
	Status      string     `json:"status"`                  // 🆕 Added (active / inactive / suspended)
	LastLoginAt *time.Time `json:"last_login_at,omitempty"` // 🆕 Added
	CreatedAt   time.Time  `json:"created_at"`
}

// LoginRequest identifies the user by email or username. Both are the
// nhi_users.email value (a username such as cirm@bimasugam is stored there);
// "username" is accepted as an alias of "email".
type LoginRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login is the identifier the user typed.
func (r LoginRequest) Login() string {
	if e := strings.TrimSpace(r.Email); e != "" {
		return e
	}
	return strings.TrimSpace(r.Username)
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LoginResponse is returned by login and refresh. Token is the short-lived
// access token; RefreshToken is opaque and single-use (rotated on refresh).
type LoginResponse struct {
	Token            string `json:"token"`
	RefreshToken     string `json:"refresh_token"`
	User             User   `json:"user"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}
