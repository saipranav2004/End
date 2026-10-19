package v1

import (
	"encoding/json"
	"log"
	"net/http"

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
		})
		return
	}

	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}

	filters, err := h.filterService.GetFilters(r.Context(), claims.UserID, claims.TenantID, claims.DBName)
	if err != nil {
		log.Printf("filter options failed: %v", err)
		sendJSONResponse(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to retrieve filter options",
			Error:   "FILTERS_LOAD_FAILED",
		})
		return
	}

	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Filter options retrieved successfully",
		Data:    filters,
	})
}

func sendJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
