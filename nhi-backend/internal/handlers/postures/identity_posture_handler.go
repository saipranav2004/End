package postures

import (
	"net/http"

	"github.com/gorilla/mux"

	svc "nhi-backend-api/internal/services/postures"
)

type IdentityPostureHandler struct {
	service *svc.IdentityPostureService
}

func NewIdentityPostureHandler(service *svc.IdentityPostureService) *IdentityPostureHandler {
	return &IdentityPostureHandler{service: service}
}

// Get handles GET /identities/{id}/posture?window=30d — the Posture tab.
func (h *IdentityPostureHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	id, ok := identityIDOK(w, mux.Vars(r)["id"])
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow("window")) {
		return
	}
	win, ok := parseWindow(w, q)
	if !ok {
		return
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, id, win)
	if err != nil {
		fail(w, r, "load identity posture", err)
		return
	}
	respond(w, env, true)
}
