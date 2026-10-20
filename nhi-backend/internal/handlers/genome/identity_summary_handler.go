package genome

import (
	"fmt"
	"net/http"
	"time"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// IdentitySummaryHandler serves GET /api/v1/genome/identities/{source_type}/{identity_name}
// (backend_api_contract.md §5.2 — identity header and summary). Like the
// dashboard summary it only reads the JWT claims, validates the request and
// shapes the §1.5 envelope; the data work is in svc.IdentitySummaryService.
type IdentitySummaryHandler struct {
	service *svc.IdentitySummaryService
	now     func() time.Time
}

// NewIdentitySummaryHandler creates the handler.
func NewIdentitySummaryHandler(service *svc.IdentitySummaryService) *IdentitySummaryHandler {
	return &IdentitySummaryHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters accepted by §5.2: the §2 window of calls_in_window.
var identitySummaryAllowed = map[string]struct{}{"from": {}, "to": {}}

// Get handles GET /api/v1/genome/identities/{source_type}/{identity_name}.
func (h *IdentitySummaryHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	if unknown, bad := checkUnknown(q, identitySummaryAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(identitySummaryAllowed)), rid)
		return
	}
	window, err := parseIdentityWindow(q, h.now())
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.GetIdentity(r.Context(), claims.UserID, claims.TenantID, claims.DBName, sourceType, identityName,
		svc.IdentitySummaryParams{Window: window})
	if err != nil {
		writeIdentityServiceError(w, "identity summary", rid, err)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now()},
	})
}
