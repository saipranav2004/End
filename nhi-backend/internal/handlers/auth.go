package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	"nhi-backend-api/internal/services"
)

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

	resp, err := h.authService.Login(r.Context(), req)
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
