package anomalydisposition

import (
	"net/http"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves POST /api/v1/genome/anomalies/{alert_id}/disposition. No
// disposition history table exists in the current pipeline, so the
// endpoint validates the request and returns the "available: false" body;
// nothing is written and the request body is not interpreted.
type Handler struct {
	service *Service
	now     func() time.Time
}

// NewHandler creates the handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// No query parameters are accepted.
var allowed = map[string]struct{}{}

// Post handles POST /api/v1/genome/anomalies/{alert_id}/disposition.
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "anomaly disposition", rid)

	if _, ok := common.RequireClaims(w, r, rid); !ok {
		return
	}
	if _, err := common.AlertIDPath(r); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
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
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: h.service.Record(),
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
