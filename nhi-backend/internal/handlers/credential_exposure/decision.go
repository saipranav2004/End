package credential_exposure

import (
	"errors"
	"net/http"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	svc "nhi-backend-api/internal/services/credential_exposure"
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
	// The signed-in user decided; a decided_by in the body is ignored.
	err := h.service.Save(r.Context(), claims.UserID, claims.TenantID, claims.DBName, req.FindingID, req.Decision, claims.UserID)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrDecisionFindingNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Finding not found")
		case errors.Is(err, repo.ErrDecisionFindingAmbiguous):
			writeError(w, http.StatusConflict, "AMBIGUOUS_FINDING", "Finding ID matched more than one platform table")
		default:
			writeInvalidOr(w, err, "INVALID_DECISION", "Unable to save analyst decision")
		}
		return
	}
	writeJSON(w, http.StatusOK, models.OKResponse{OK: true})
}
