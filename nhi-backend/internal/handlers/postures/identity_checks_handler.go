package postures

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	models "nhi-backend-api/internal/models/postures"
	svc "nhi-backend-api/internal/services/postures"
)

type IdentityChecksHandler struct {
	service *svc.IdentityChecksService
}

func NewIdentityChecksHandler(service *svc.IdentityChecksService) *IdentityChecksHandler {
	return &IdentityChecksHandler{service: service}
}

// Get handles GET /identities/{id}/checks?status=failed|passed|all&pillar= —
// the Checks tab.
func (h *IdentityChecksHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	id, ok := identityIDOK(w, mux.Vars(r)["id"])
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow("status", "pillar")) {
		return
	}
	f := models.IdentityChecksFilter{Status: models.ChecksAll}
	if raw := strings.ToLower(strings.TrimSpace(q.Get("status"))); raw != "" {
		switch raw {
		case models.ChecksFailed, models.ChecksPassed, models.ChecksAll:
			f.Status = raw
		default:
			invalid(w, "status", "status must be failed, passed or all")
			return
		}
	}
	if raw := strings.ToLower(strings.TrimSpace(q.Get("pillar"))); raw != "" {
		if !tokenRegex.MatchString(raw) {
			invalid(w, "pillar", "pillar is not valid")
			return
		}
		f.Pillar = raw
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, id, f)
	if err != nil {
		fail(w, r, "load identity checks", err)
		return
	}
	respond(w, env, true)
}
