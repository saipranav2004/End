package anomalylist

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves GET /api/v1/genome/anomalies (the anomaly list). It only
// reads the JWT claims, validates the request and shapes the response; the
// data work is in Service.
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
	"severity": {}, "dimension": {}, "state": {}, "source_type": {}, "search": {},
	"from": {}, "to": {}, "limit": {}, "cursor": {},
}

// Severity values, as stored in log_events.event_risk_severity.
var severities = map[string]string{"critical": "Critical", "high": "High", "medium": "Medium", "low": "Low"}

// Get handles GET /api/v1/genome/anomalies.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "anomaly list", rid)
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
	params := Params{
		Dimension:  strings.TrimSpace(q.Get("dimension")),
		State:      strings.TrimSpace(q.Get("state")),
		SourceType: strings.ToUpper(strings.TrimSpace(q.Get("source_type"))), // compact upper-case family
		Search:     strings.TrimSpace(q.Get("search")),
		Cursor:     strings.TrimSpace(q.Get("cursor")),
	}
	if sv := strings.TrimSpace(q.Get("severity")); sv != "" {
		canonical, ok := severities[strings.ToLower(sv)]
		if !ok {
			common.WriteError(w, http.StatusBadRequest, common.CodeValidation, "severity must be one of Critical, High, Medium, Low", rid)
			return
		}
		params.Severity = canonical
	}
	var err error
	if params.From, params.To, err = common.ParseOptionalWindow(q); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}
	if params.Limit, err = common.ParseIdentityLimit(q, "limit"); err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}

	data, next, err := h.service.List(ctx, claims.UserID, claims.TenantID, claims.DBName, params)
	if errors.Is(err, common.ErrInvalidCursor) {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, "cursor is invalid", rid)
		return
	}
	if err != nil {
		common.WriteServiceError(w, r, "anomaly list", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now(), NextCursor: next},
	})
}
