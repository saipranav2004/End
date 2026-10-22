package genome

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// IdentityActivityHandler serves
// GET /api/v1/identities/{source_type}/{identity_name}/activity
// (backend_api_contract.md §5.3 — call volume and recent calls). It only
// reads the JWT claims, validates the request and shapes the §1.5 envelope;
// the data work is in svc.IdentityActivityService.
type IdentityActivityHandler struct {
	service *svc.IdentityActivityService
	now     func() time.Time
}

// NewIdentityActivityHandler creates the handler.
func NewIdentityActivityHandler(service *svc.IdentityActivityService) *IdentityActivityHandler {
	return &IdentityActivityHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters of §5.3.
var identityActivityAllowed = map[string]struct{}{"from": {}, "to": {}, "bucket": {}, "recent_limit": {}, "recent_cursor": {}}

// §2 paging bounds (limit default 25, maximum 100).
const (
	identityPageDefault = 25
	identityPageMax     = 100
)

// Get handles GET /api/v1/identities/{source_type}/{identity_name}/activity.
func (h *IdentityActivityHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	if unknown, bad := checkUnknown(q, identityActivityAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(identityActivityAllowed)), rid)
		return
	}
	window, err := parseIdentityWindow(q, h.now())
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}
	bucket := strings.ToLower(strings.TrimSpace(q.Get("bucket")))
	switch bucket {
	case "":
		bucket = svc.ActivityBucketDay
	case svc.ActivityBucketDay, svc.ActivityBucketHour:
	default:
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, "bucket must be day or hour", rid)
		return
	}
	limit, err := parseIdentityLimit(q, "recent_limit")
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}

	data, next, err := h.service.GetActivity(r.Context(), claims.UserID, claims.TenantID, claims.DBName, sourceType, identityName,
		svc.IdentityActivityParams{Window: window, Bucket: bucket, RecentLimit: limit, RecentCursor: strings.TrimSpace(q.Get("recent_cursor"))})
	if errors.Is(err, svc.ErrInvalidCursor) {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, "recent_cursor is invalid", rid)
		return
	}
	if err != nil {
		writeIdentityServiceError(r.Context(), w, "identity activity", rid, err)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now(), NextCursor: next},
	})
}

// parseIdentityLimit reads a §2 page-size parameter: default 25, 1..100.
func parseIdentityLimit(q url.Values, name string) (int, error) {
	s := strings.TrimSpace(q.Get(name))
	if s == "" {
		return identityPageDefault, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > identityPageMax {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", name, identityPageMax)
	}
	return n, nil
}
