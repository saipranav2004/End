package postures

import (
	"net/http"

	"github.com/gorilla/mux"

	svc "nhi-backend-api/internal/services/postures"
)

type IdentityCredentialsHandler struct {
	service *svc.IdentityCredentialsService
}

func NewIdentityCredentialsHandler(service *svc.IdentityCredentialsService) *IdentityCredentialsHandler {
	return &IdentityCredentialsHandler{service: service}
}

// Get handles GET /identities/{id}/credentials — the Credentials tab.
func (h *IdentityCredentialsHandler) Get(w http.ResponseWriter, r *http.Request) {
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
		fail(w, r, "load identity credentials", err)
		return
	}
	respond(w, env, true)
}
