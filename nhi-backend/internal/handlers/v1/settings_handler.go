package v1

import (
	"encoding/json"
	"errors"
	"net/http"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	modelsv1 "nhi-backend-api/internal/models/v1"
	repov1 "nhi-backend-api/internal/repository/v1"
	svcv1 "nhi-backend-api/internal/services/v1"
)

type SettingsHandler struct{ service *svcv1.SettingsService }

func NewSettingsHandler(service *svcv1.SettingsService) *SettingsHandler {
	return &SettingsHandler{service: service}
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}

	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}

	result, err := h.service.GetSettings(r.Context(), claims.UserID, claims.TenantID, claims.DBName)
	if err != nil {
		sendServerError(w, r, err, "Failed to retrieve settings", "SETTINGS_LOAD_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Settings retrieved successfully", Data: result})
}

func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}

	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}

	var req modelsv1.UpdateSettingsRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid JSON request body", Error: "INVALID_REQUEST"})
		return
	}
	result, err := h.service.UpdateSettings(r.Context(), claims.UserID, claims.TenantID, claims.DBName, req)
	if err != nil {
		if errors.Is(err, repov1.ErrInvalidTenantSettings) {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "One or more selected filters do not belong to this tenant", Error: "INVALID_SETTINGS"})
			return
		}
		sendServerError(w, r, err, "Failed to update settings", "SETTINGS_UPDATE_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Settings updated successfully", Data: result})
}
