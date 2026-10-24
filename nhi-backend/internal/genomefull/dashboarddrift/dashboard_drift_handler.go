package dashboarddrift

import (
	"net/http"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves GET /api/v1/genome/dashboard/drift.
type Handler struct {
	service *Service
	now     func() time.Time
}

// NewHandler creates the handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// The v3 drift endpoint defines no query parameters.
var allowed = map[string]struct{}{}

// Get handles the fleet drift request.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "dashboard drift", rid)
	ctx, cancel := common.RequestContext(r)
	defer cancel()

	claims, ok := common.RequireClaims(w, r, rid)
	if !ok {
		return
	}
	if unknown, bad := common.CheckUnknown(r.URL.Query(), allowed); bad {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, common.UnknownParamMessage(r.URL.Query(), unknown, allowed), rid)
		return
	}
	if err := common.CheckParamLimits(r.URL.Query()); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.Get(ctx, claims.UserID, claims.TenantID, claims.DBName)
	if err != nil {
		common.WriteServiceError(w, r, "dashboard drift", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
