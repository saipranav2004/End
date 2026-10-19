package postures

import (
	"net/http"

	"github.com/gorilla/mux"

	svc "nhi-backend-api/internal/services/postures"
)

type IdentityHeaderHandler struct {
	service *svc.IdentityHeaderService
}

func NewIdentityHeaderHandler(service *svc.IdentityHeaderService) *IdentityHeaderHandler {
	return &IdentityHeaderHandler{service: service}
}

// Get handles GET /identities/{id} — the identity page header.
func (h *IdentityHeaderHandler) Get(w http.ResponseWriter, r *http.Request) {
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
		fail(w, "load identity", err)
		return
	}
	respond(w, env, true)
}
