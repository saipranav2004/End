package containmentlist

import (
	"net/http"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves GET /api/v1/genome/containment. The current pipeline
// persists no containment actions, so after validating the parameters the
// handler returns the empty, "available: false" list from Service.
type Handler struct {
	service *Service
	now     func() time.Time
}

// NewHandler creates the handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters (nothing else is accepted).
var allowed = map[string]struct{}{
	"state": {}, "source_type": {}, "identity_name": {}, "from": {}, "to": {}, "limit": {}, "cursor": {},
}

// Get handles GET /api/v1/genome/containment.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "containment list", rid)

	if _, ok := common.RequireClaims(w, r, rid); !ok {
		return
	}
	q := r.URL.Query()
	if unknown, bad := common.CheckUnknown(q, allowed); bad {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, common.UnknownParamMessage(q, unknown, allowed), rid)
		return
	}
	if err := common.CheckParamLimits(q); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}
	if _, _, err := common.ParseOptionalWindow(q); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}
	if _, err := common.ParseIdentityLimit(q, "limit"); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: h.service.List(),
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
