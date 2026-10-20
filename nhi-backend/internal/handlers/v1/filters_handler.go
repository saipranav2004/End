package v1

import (
	"encoding/json"
	"log"
	"net/http"

	"nhi-backend-api/internal/database"
	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	svcv1 "nhi-backend-api/internal/services/v1"
)

type FilterHandler struct {
	filterService *svcv1.FilterService
}

func NewFilterHandler(filterService *svcv1.FilterService) *FilterHandler {
	return &FilterHandler{filterService: filterService}
}

func (h *FilterHandler) GetFilters(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{
			Success: false,
			Message: "Unauthorized",
			Error:   "UNAUTHORIZED",
		})
		return
	}

	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}

	filters, err := h.filterService.GetFilters(r.Context(), claims.UserID, claims.TenantID, claims.DBName)
	if err != nil {
		sendServerError(w, err, "Failed to retrieve filter options", "FILTERS_LOAD_FAILED")
		return
	}

	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Filter options retrieved successfully",
		Data:    filters,
	})
}

// sendServerError logs err and answers 500, or 503 TENANT_NOT_PROVISIONED
// when the tenant schema lacks a table this endpoint reads.
func sendServerError(w http.ResponseWriter, err error, message, code string) {
	log.Printf("%s: %v", code, err)
	if database.IsMissingTable(err) {
		sendJSONResponse(w, http.StatusServiceUnavailable, models.APIResponse{Success: false, Message: database.NotProvisionedMessage, Error: database.NotProvisionedCode})
		return
	}
	sendJSONResponse(w, http.StatusInternalServerError, models.APIResponse{Success: false, Message: message, Error: code})
}

func sendJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
