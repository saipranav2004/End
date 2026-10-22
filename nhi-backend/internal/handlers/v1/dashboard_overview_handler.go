package v1

import (
	"net/http"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	svcv1 "nhi-backend-api/internal/services/v1"
)

type DashboardOverviewHandler struct {
	service *svcv1.DashboardOverviewService
}

func NewDashboardOverviewHandler(service *svcv1.DashboardOverviewService) *DashboardOverviewHandler {
	return &DashboardOverviewHandler{service: service}
}

// Get handles GET /api/v1/dashboard/overview ("ENDPOINT 3: Dashboard Overview").
// No query parameters — returns all KPIs and all charts in one response.
func (h *DashboardOverviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}

	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}

	result, err := h.service.GetOverview(r.Context(), claims.UserID, claims.TenantID, claims.DBName)
	if err != nil {
		sendServerError(w, r, err, "Failed to retrieve dashboard overview", "DASHBOARD_LOAD_FAILED")
		return
	}

	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Dashboard overview retrieved successfully",
		Data:    result,
	})
}
