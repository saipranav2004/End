package postures

import (
	"net/http"

	"github.com/gorilla/mux"

	svc "nhi-backend-api/internal/services/postures"
)

type IdentityRoleUseHandler struct {
	service *svc.IdentityRoleUseService
}

func NewIdentityRoleUseHandler(service *svc.IdentityRoleUseService) *IdentityRoleUseHandler {
	return &IdentityRoleUseHandler{service: service}
}

// Get handles GET /identities/{id}/role-use — the Role use tab. There is no
// window yet: it needs a daily rollup that arrives with the CloudTrail data.
func (h *IdentityRoleUseHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	id, ok := identityIDOK(w, mux.Vars(r)["id"])
	if !ok {
		return
	}
	if !strictQuery(w, r.URL.Query(), allow()) {
		return
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, id)
	if err != nil {
		fail(w, "load identity role use", err)
		return
	}
	respond(w, env, true)
}
