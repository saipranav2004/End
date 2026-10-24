package anomalyfeedback

import (
	"net/http"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves POST /api/v1/genome/anomalies/{alert_id}/feedback
// (reviewer feedback, the only write of the genome API). It reads the JWT
// claims (the reviewer is the token's user, never the body), validates the
// body and shapes the response; the upsert is in Service.
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

// maxReasonLength is a protective cap on the free-text reason
// (alert_feedback.reason is unbounded TEXT; the body itself is capped at
// common.MaxBodyBytes).
const maxReasonLength = 4000

// Allowed alert_feedback.decision values (schema guide §10).
var decisions = map[string]struct{}{
	DecisionLegitimate: {}, DecisionMalicious: {}, DecisionUnknown: {},
}

// Post handles POST /api/v1/genome/anomalies/{alert_id}/feedback.
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "anomaly feedback", rid)
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
	var body Request
	if be := common.DecodeJSONBody(w, r, &body); be != nil {
		common.WriteError(w, be.Status, be.Code, be.Message, rid)
		return
	}
	in := Input{Decision: strings.ToLower(strings.TrimSpace(body.Decision)), Reviewer: strings.TrimSpace(claims.UserID)}
	if _, ok := decisions[in.Decision]; !ok {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, "decision must be one of legitimate, malicious, unknown", rid)
		return
	}
	if body.ApprovedForBaseline != nil {
		in.ApprovedForBaseline = *body.ApprovedForBaseline
	}
	if body.Reason != nil {
		if reason := strings.TrimSpace(*body.Reason); reason != "" {
			if err := common.CheckLength("reason", reason, maxReasonLength); err != nil {
				common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
				return
			}
			in.Reason = &reason
		}
	}
	if in.Reviewer == "" {
		in.Reviewer = strings.TrimSpace(claims.Email)
	}

	data, err := h.service.Save(ctx, claims.UserID, claims.TenantID, claims.DBName, alertID, in)
	if err != nil {
		common.WriteServiceError(w, r, "anomaly feedback", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
