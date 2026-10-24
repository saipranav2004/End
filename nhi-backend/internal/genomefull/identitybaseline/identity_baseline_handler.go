package identitybaseline

import (
	"net/http"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves
// GET /api/v1/genome/identities/{source_type}/{identity_name}/baseline
// (baseline profile and provenance). It only reads the JWT claims,
// validates the request and shapes the response; the data work is in
// Service.
type Handler struct {
	service *Service
	now     func() time.Time
}

// NewHandler creates the handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters: the limit bounds the versions list (newest first).
var allowed = map[string]struct{}{"limit": {}}

// Get handles GET /api/v1/genome/identities/{source_type}/{identity_name}/baseline.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "identity baseline", rid)
	ctx, cancel := common.RequestContext(r)
	defer cancel()

	claims, ok := common.RequireClaims(w, r, rid)
	if !ok {
		return
	}
	sourceType, identityName, err := common.IdentityPath(r)
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
	limit, err := common.ParseIdentityLimit(q, "limit")
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, common.CodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.GetBaseline(ctx, claims.UserID, claims.TenantID, claims.DBName, sourceType, identityName,
		Params{VersionsLimit: limit})
	if err != nil {
		common.WriteServiceError(w, r, "identity baseline", rid, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: data,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
