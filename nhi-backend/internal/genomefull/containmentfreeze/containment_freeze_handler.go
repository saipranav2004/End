package containmentfreeze

import (
	"errors"
	"net/http"
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Handler serves
// POST /api/v1/genome/identities/{source_type}/{identity_name}/containment/freeze.
// The action must not run until a containment audit log and worker exist:
// the handler validates the body, Service resolves the identity (404 when
// unknown) and the request is answered 501 NOT_IMPLEMENTED. No AWS call is
// made and nothing is written.
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

// Post handles POST /api/v1/genome/identities/{source_type}/{identity_name}/containment/freeze.
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	rid := common.RequestID(r)
	w.Header().Set("X-Request-ID", rid)
	defer common.Recover(w, r, "identity containment freeze", rid)
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

	err = h.service.Freeze(ctx, claims.UserID, claims.TenantID, claims.DBName, sourceType, identityName, body)
	if errors.Is(err, common.ErrNotImplemented) {
		common.WriteError(w, http.StatusNotImplemented, common.CodeNotImplemented, err.Error(), rid)
		return
	}
	if err != nil {
		common.WriteServiceError(w, r, "identity containment freeze", rid, err)
		return
	}
	// Unreachable until containment is implemented; kept so a future
	// implementation returns the success envelope from here.
	common.WriteJSON(w, http.StatusOK, common.Envelope{
		Data: nil,
		Meta: common.Meta{RequestID: rid, GeneratedAt: h.now()},
	})
}
