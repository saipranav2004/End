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

// IdentityListHandler serves GET /api/v1/identities
// (backend_api_contract.md §5.1 — identity list and fleet table). It only
// reads the JWT claims, validates the request and shapes the §1.5 envelope;
// the data work is in svc.IdentityListService.
type IdentityListHandler struct {
	service *svc.IdentityListService
	now     func() time.Time
}

// NewIdentityListHandler creates the handler.
func NewIdentityListHandler(service *svc.IdentityListService) *IdentityListHandler {
	return &IdentityListHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters of §5.1, plus baseline (established | learning).
var identityListAllowed = map[string]struct{}{
	"source_type": {}, "search": {}, "account_id": {}, "region": {}, "status": {},
	"baseline": {}, "sort": {}, "limit": {}, "cursor": {},
}

// Get handles GET /api/v1/identities.
func (h *IdentityListHandler) Get(w http.ResponseWriter, r *http.Request) {
	rid := summaryRequestID(r)
	w.Header().Set("X-Request-ID", rid)

	claims := middleware.GetClaims(r)
	if claims == nil {
		writeSummaryError(w, http.StatusUnauthorized, models.MonitoringCodeUnauthorized, "authentication required", rid)
		return
	}
	q := r.URL.Query()
	if unknown, bad := checkUnknown(q, identityListAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(identityListAllowed)), rid)
		return
	}
	sortBy := strings.ToLower(strings.TrimSpace(q.Get("sort")))
	switch sortBy {
	case "":
		sortBy = svc.IdentitySortRiskDesc
	case svc.IdentitySortRiskDesc, svc.IdentitySortLastSeenDesc, svc.IdentitySortNameAsc:
	default:
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("sort must be one of %s, %s, %s", svc.IdentitySortRiskDesc, svc.IdentitySortLastSeenDesc, svc.IdentitySortNameAsc), rid)
		return
	}
	baseline := strings.ToLower(strings.TrimSpace(q.Get("baseline")))
	switch baseline {
	case "", svc.BaselineEstablished, svc.BaselineLearning:
	default:
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, "baseline must be established or learning", rid)
		return
	}
	limit, err := parseIdentityLimit(q, "limit")
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}

	data, next, err := h.service.List(r.Context(), claims.UserID, claims.TenantID, claims.DBName, svc.IdentityListParams{
		SourceType: strings.ToUpper(strings.TrimSpace(q.Get("source_type"))),
		Search:     strings.TrimSpace(q.Get("search")),
		AccountID:  strings.TrimSpace(q.Get("account_id")),
		Region:     strings.TrimSpace(q.Get("region")),
		Status:     strings.TrimSpace(q.Get("status")),
		Baseline:   baseline,
		Sort:       sortBy,
		Limit:      limit,
		Cursor:     strings.TrimSpace(q.Get("cursor")),
	})
	if errors.Is(err, svc.ErrInvalidCursor) {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, "cursor is invalid", rid)
		return
	}
	if err != nil {
		writeIdentityServiceError(w, "identity list", rid, err)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now(), NextCursor: next},
	})
}
