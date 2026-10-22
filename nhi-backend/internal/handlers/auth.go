package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"nhi-backend-api/internal/logging"
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
	// Per address it only stops a single source spraying many accounts: an
	// office behind one NAT signs in from one address, so it is generous; the
	// per-account limit is what stops guessing one password.
	loginByIP    = ratelimit.New(1, 60)      // 60 a minute per address
	loginByEmail = ratelimit.New(5.0/300, 5) // 5, then 1 every minute per account
	refreshByIP  = ratelimit.New(1, 60)      // tabs refresh about hourly
)

// clientIP is the caller's address. Forwarding headers are trusted only when
// the direct peer is a private or loopback address (our own proxies);
// otherwise anyone could forge them. Behind proxies the client is the
// right-most public address in X-Forwarded-For: each proxy appends the peer it
// saw, so entries left of it may be forged but that one was not. A load
// balancer in front of the console's nginx would otherwise make every user
// share the balancer's address (and its rate limit).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !trustedPeer(host) {
		return host
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		if ip, err := netip.ParseAddr(strings.TrimSpace(hops[i])); err == nil && !trustedPeer(ip.String()) {
			return ip.String()
		}
	}
	if real, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return real.String()
	}
	return host
}

func trustedPeer(addr string) bool {
	ip, err := netip.ParseAddr(addr)
	return err == nil && (ip.IsLoopback() || ip.IsPrivate())
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
			Error:   "INVALID_REQUEST",
		})
		return
	}

	if req.Login() == "" || req.Password == "" {
		sendJSON(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Email or username and password are required",
			Error:   "INVALID_REQUEST",
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
		logging.Audit(r.Context(), "login", req.Login(), "", "", "account_disabled")
		// Only reachable with the right password, so saying so leaks nothing.
		sendJSON(w, http.StatusForbidden, models.APIResponse{
			Success: false,
			Message: "This account is disabled. Ask an administrator to re-enable it.",
			Error:   "ACCOUNT_DISABLED",
		})
		return
	}
	if err != nil {
		logging.FromContext(r.Context()).Warn("login failed", "err", err)
		logging.Audit(r.Context(), "login", req.Login(), "", "", "failure")
		sendJSON(w, http.StatusUnauthorized, models.APIResponse{
			Success: false,
			Message: "Invalid email/username or password",
			Error:   "AUTHENTICATION_FAILED",
		})
		return
	}

	logging.Audit(r.Context(), "login", req.Login(), "", "", "success")
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
			Error:   "INVALID_REQUEST",
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
		logging.FromContext(r.Context()).Warn("refresh failed", "err", err)
		logging.Audit(r.Context(), "refresh", "", "", "", "failure")
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
		logging.FromContext(r.Context()).Error("logout failed", "err", err)
		sendJSON(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Logout failed",
			Error:   "INTERNAL_ERROR",
		})
		return
	}
	logging.Audit(r.Context(), "logout", "", "", "", "success")
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
			Error:   "UNAUTHORIZED",
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
