package genome

import (
	"fmt"
	"net/http"
	"time"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// IdentityBaselineHandler serves
// GET /api/v1/identities/{source_type}/{identity_name}/baseline
// (backend_api_contract.md §5.5 — baseline profile and provenance). It only
// reads the JWT claims, validates the request and shapes the §1.5 envelope;
// the data work is in svc.IdentityBaselineService.
type IdentityBaselineHandler struct {
	service *svc.IdentityBaselineService
	now     func() time.Time
}

// NewIdentityBaselineHandler creates the handler.
func NewIdentityBaselineHandler(service *svc.IdentityBaselineService) *IdentityBaselineHandler {
	return &IdentityBaselineHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters: the §2 limit bounds the versions list (newest first).
var identityBaselineAllowed = map[string]struct{}{"limit": {}}

// Get handles GET /api/v1/identities/{source_type}/{identity_name}/baseline.
func (h *IdentityBaselineHandler) Get(w http.ResponseWriter, r *http.Request) {
	rid := summaryRequestID(r)
	w.Header().Set("X-Request-ID", rid)

	claims := middleware.GetClaims(r)
	if claims == nil {
		writeSummaryError(w, http.StatusUnauthorized, models.MonitoringCodeUnauthorized, "authentication required", rid)
		return
	}
	sourceType, identityName, err := identityPath(r)
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}
	q := r.URL.Query()
	if unknown, bad := checkUnknown(q, identityBaselineAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(identityBaselineAllowed)), rid)
		return
	}
	limit, err := parseIdentityLimit(q, "limit")
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.GetBaseline(r.Context(), claims.UserID, claims.TenantID, claims.DBName, sourceType, identityName,
		svc.IdentityBaselineParams{VersionsLimit: limit})
	if err != nil {
		writeIdentityServiceError(w, "identity baseline", rid, err)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now()},
	})
}
