package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	"nhi-backend-api/internal/ratelimit"
	"nhi-backend-api/internal/services"
)

// Sign-in guessing limits: per client address and per account.
var (
	loginByIP    = ratelimit.New(10.0/60, 10) // 10 a minute per address
	loginByEmail = ratelimit.New(5.0/300, 5)  // 5, then 1 every minute per account
	refreshByIP  = ratelimit.New(1, 60)       // tabs refresh about hourly
)

// clientIP is the caller's address. X-Real-IP (set by the console's nginx) is
// trusted only when the direct peer is a private or loopback address, i.e.
// the request came through our own proxy; otherwise anyone could forge it.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if peer, err := netip.ParseAddr(host); err == nil && (peer.IsLoopback() || peer.IsPrivate()) {
		if real, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
			return real.String()
		}
	}
	return host
}

// tooManyAttempts answers 429 with Retry-After when any limiter is empty.
func tooManyAttempts(w http.ResponseWriter, what string, checks ...func() (bool, time.Duration)) bool {
	for _, check := range checks {
		if ok, wait := check(); !ok {
			secs := int(wait.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			sendJSON(w, http.StatusTooManyRequests, models.APIResponse{
				Success: false,
				Message: fmt.Sprintf("Too many %s attempts. Wait %d seconds and try again.", what, secs),
				Error:   "RATE_LIMITED",
			})
			return true
		}
	}
	return false
}

type AuthHandler struct {
	authService *services.AuthService
}

func NewAuthHandler(authService *services.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// Login handles POST /api/v1/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}

	// Prevent large payloads / slowloris
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	defer r.Body.Close()

	var req models.LoginRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Invalid request payload",
			Error:   err.Error(),
		})
		return
	}

	if req.Login() == "" || req.Password == "" {
		sendJSON(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Email or username and password are required",
		})
		return
	}

	now := time.Now()
	if tooManyAttempts(w, "sign-in",
		func() (bool, time.Duration) { return loginByIP.Allow(clientIP(r), now) },
		func() (bool, time.Duration) { return loginByEmail.Allow(strings.ToLower(req.Login()), now) },
	) {
		return
	}

	resp, err := h.authService.Login(r.Context(), req)
	if errors.Is(err, services.ErrAccountDisabled) {
		// Only reachable with the right password, so saying so leaks nothing.
		sendJSON(w, http.StatusForbidden, models.APIResponse{
			Success: false,
			Message: "This account is disabled. Ask an administrator to re-enable it.",
			Error:   "ACCOUNT_DISABLED",
		})
		return
	}
	if err != nil {
		log.Printf("login failed: %v", err)
		sendJSON(w, http.StatusUnauthorized, models.APIResponse{
			Success: false,
			Message: "Invalid email/username or password",
			Error:   "AUTHENTICATION_FAILED",
		})
		return
	}

	sendJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Login successful",
		Data:    resp,
	})
}

// decodeRefreshRequest reads and validates the {"refresh_token"} body.
func decodeRefreshRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req models.RefreshRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.RefreshToken == "" {
		sendJSON(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "refresh_token is required",
		})
		return "", false
	}
	return req.RefreshToken, true
}

// Refresh handles POST /api/v1/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	token, ok := decodeRefreshRequest(w, r)
	if !ok {
		return
	}
	if tooManyAttempts(w, "session renewal", func() (bool, time.Duration) { return refreshByIP.Allow(clientIP(r), time.Now()) }) {
		return
	}
	resp, err := h.authService.Refresh(r.Context(), token)
	if err != nil {
		log.Printf("refresh failed: %v", err)
		sendJSON(w, http.StatusUnauthorized, models.APIResponse{
			Success: false,
			Message: "Invalid or expired refresh token",
			Error:   "AUTHENTICATION_FAILED",
		})
		return
	}
	sendJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Token refreshed",
		Data:    resp,
	})
}

// Logout handles POST /api/v1/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token, ok := decodeRefreshRequest(w, r)
	if !ok {
		return
	}
	if err := h.authService.Logout(r.Context(), token); err != nil {
		log.Printf("logout failed: %v", err)
		sendJSON(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Logout failed",
		})
		return
	}
	sendJSON(w, http.StatusOK, models.APIResponse{Success: true, Message: "Logged out"})
}

// Me handles GET /api/v1/me
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSON(w, http.StatusUnauthorized, models.APIResponse{
			Success: false,
			Message: "Unauthorized",
		})
		return
	}

	sendJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "User profile retrieved",
		Data: map[string]interface{}{
			"user_id":   claims.UserID,
			"tenant_id": claims.TenantID,
			"db_name":   claims.DBName,
			"role":      claims.Role,
		},
	})
}

// Helper utility for sending JSON responses
func sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
