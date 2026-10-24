package anomalydetail

import (
	"net/http"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves GET /api/v1/genome/anomalies/{alert_id} (anomaly detail).
// It only reads the JWT claims, validates the request and shapes the
// response; the data work is in Service.
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

// Get handles GET /api/v1/genome/anomalies/{alert_id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "anomaly detail", rid)
	ctx, cancel := common.RequestContext(r)
	defer cancel()

	claims, ok := common.RequireClaims(w, r, rid)
	if !ok {
		return
	}
	alertID, err := common.AlertIDPath(r)
	if err != nil {
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

	data, err := h.service.GetDetail(ctx, claims.UserID, claims.TenantID, claims.DBName, alertID)
	if err != nil {
		common.WriteServiceError(w, r, "anomaly detail", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
