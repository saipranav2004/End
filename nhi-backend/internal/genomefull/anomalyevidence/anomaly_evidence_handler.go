package anomalyevidence

import (
	"net/http"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves GET /api/v1/genome/anomalies/{alert_id}/evidence
// (evidence for an anomaly). It only reads the JWT claims, validates the
// request and shapes the response; the data work is in Service.
type Handler struct {
	service *Service
	now     func() time.Time
}

// NewHandler creates the handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters (both booleans, default false).
var allowed = map[string]struct{}{"include_request_parameters": {}, "include_raw_risk": {}}

// Get handles GET /api/v1/genome/anomalies/{alert_id}/evidence.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "anomaly evidence", rid)
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
	q := r.URL.Query()
	if unknown, bad := common.CheckUnknown(q, allowed); bad {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, common.UnknownParamMessage(q, unknown, allowed), rid)
		return
	}
	if err := common.CheckParamLimits(q); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}
	var params Params
	if params.IncludeRequestParameters, err = common.ParseBoolParam(q, "include_request_parameters"); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}
	if params.IncludeRawRisk, err = common.ParseBoolParam(q, "include_raw_risk"); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.GetEvidence(ctx, claims.UserID, claims.TenantID, claims.DBName, alertID, params)
	if err != nil {
		common.WriteServiceError(w, r, "anomaly evidence", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
