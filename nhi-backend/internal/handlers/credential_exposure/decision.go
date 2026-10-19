package credential_exposure

import (
	"errors"
	"net/http"
	"strings"

	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	svc "nhi-backend-api/internal/services/credential_exposure"
	"nhi-backend-api/internal/middleware"
)

type DecisionHandler struct{ service *svc.DecisionService }

func NewDecisionHandler(service *svc.DecisionService) *DecisionHandler {
	return &DecisionHandler{service: service}
}

func (h *DecisionHandler) Save(w http.ResponseWriter, r *http.Request) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, map[string]struct{}{}) {
		return
	}
	var req models.FindingDecisionRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	by := strings.TrimSpace(req.DecidedBy)
	if by == "" {
		by = "dashboard user"
	}
	err := h.service.Save(r.Context(), claims.UserID, claims.TenantID, claims.DBName, req.FindingID, req.Decision, by)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrDecisionFindingNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Finding not found")
		case errors.Is(err, repo.ErrDecisionFindingAmbiguous):
			writeError(w, http.StatusConflict, "AMBIGUOUS_FINDING", "Finding ID matched more than one platform table")
		case strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") || strings.Contains(err.Error(), "decision must") || strings.Contains(err.Error(), "exceeds"):
			writeAPIError(w, http.StatusBadRequest, err.Error(), "INVALID_DECISION")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to save analyst decision")
		}
		return
	}
	writeJSON(w, http.StatusOK, models.OKResponse{OK: true})
}
