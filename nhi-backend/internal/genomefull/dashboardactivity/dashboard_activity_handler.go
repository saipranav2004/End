package dashboardactivity

import (
	"net/http"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves GET /api/v1/genome/dashboard/activity (fleet call volume
// and anomalies per day). It only reads the JWT claims, validates the
// request and shapes the response; the data work is in Service.
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
	"from": {}, "to": {}, "source_type": {}, "account_id": {}, "region": {},
}

// Get handles GET /api/v1/genome/dashboard/activity.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "dashboard activity", rid)
	ctx, cancel := common.RequestContext(r)
	defer cancel()

	claims, ok := common.RequireClaims(w, r, rid)
	if !ok {
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
	window, err := common.ParseIdentityWindow(q, h.now()) // from/to, default last 14 days
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.GetActivity(ctx, claims.UserID, claims.TenantID, claims.DBName, Params{
		Window:     window,
		SourceType: strings.ToUpper(strings.TrimSpace(q.Get("source_type"))),
		AccountID:  strings.TrimSpace(q.Get("account_id")),
		Region:     strings.TrimSpace(q.Get("region")),
	})
	if err != nil {
		common.WriteServiceError(w, r, "dashboard activity", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
