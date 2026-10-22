package genome

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// IdentityTimelineHandler serves
// GET /api/v1/identities/{source_type}/{identity_name}/timeline
// (backend_api_contract.md §5.6 — baseline and anomaly lifecycle). It only
// reads the JWT claims, validates the request and shapes the §1.5 envelope;
// the data work is in svc.IdentityTimelineService.
type IdentityTimelineHandler struct {
	service *svc.IdentityTimelineService
	now     func() time.Time
}

// NewIdentityTimelineHandler creates the handler.
func NewIdentityTimelineHandler(service *svc.IdentityTimelineService) *IdentityTimelineHandler {
	return &IdentityTimelineHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters of §5.6: the §2 page controls.
var identityTimelineAllowed = map[string]struct{}{"limit": {}, "cursor": {}}

// Get handles GET /api/v1/identities/{source_type}/{identity_name}/timeline.
func (h *IdentityTimelineHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	if unknown, bad := checkUnknown(q, identityTimelineAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(identityTimelineAllowed)), rid)
		return
	}
	limit, err := parseIdentityLimit(q, "limit")
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}

	data, next, err := h.service.GetTimeline(r.Context(), claims.UserID, claims.TenantID, claims.DBName, sourceType, identityName,
		svc.IdentityTimelineParams{Limit: limit, Cursor: strings.TrimSpace(q.Get("cursor"))})
	if errors.Is(err, svc.ErrInvalidCursor) {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, "cursor is invalid", rid)
		return
	}
	if err != nil {
		writeIdentityServiceError(r.Context(), w, "identity timeline", rid, err)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now(), NextCursor: next},
	})
}
