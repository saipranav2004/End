package genome

import (
	"fmt"
	"net/http"
	"time"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// IdentityGenomeHandler serves
// GET /api/v1/identities/{source_type}/{identity_name}/genome
// (backend_api_contract.md §5.4 — behavioural fingerprint and typical
// actions). It only reads the JWT claims, validates the request and shapes
// the §1.5 envelope; the data work is in svc.IdentityGenomeService.
type IdentityGenomeHandler struct {
	service *svc.IdentityGenomeService
	now     func() time.Time
}

// NewIdentityGenomeHandler creates the handler.
func NewIdentityGenomeHandler(service *svc.IdentityGenomeService) *IdentityGenomeHandler {
	return &IdentityGenomeHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters: the §2 window the §5.4 queries are bounded by and the
// §2 limit (rows per list / values per fingerprint dimension).
var identityGenomeAllowed = map[string]struct{}{"from": {}, "to": {}, "limit": {}}

// Get handles GET /api/v1/identities/{source_type}/{identity_name}/genome.
func (h *IdentityGenomeHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	if unknown, bad := checkUnknown(q, identityGenomeAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(identityGenomeAllowed)), rid)
		return
	}
	window, err := parseIdentityWindow(q, h.now())
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}
	limit, err := parseIdentityLimit(q, "limit")
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.GetGenome(r.Context(), claims.UserID, claims.TenantID, claims.DBName, sourceType, identityName,
		svc.IdentityGenomeParams{Window: window, Limit: limit})
	if err != nil {
		writeIdentityServiceError(w, "identity genome", rid, err)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now()},
	})
}
