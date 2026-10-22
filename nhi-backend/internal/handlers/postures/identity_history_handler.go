package postures

import (
	"net/http"

	"github.com/gorilla/mux"

	svc "nhi-backend-api/internal/services/postures"
)

type IdentityHistoryHandler struct {
	service *svc.IdentityHistoryService
}

func NewIdentityHistoryHandler(service *svc.IdentityHistoryService) *IdentityHistoryHandler {
	return &IdentityHistoryHandler{service: service}
}

// Get handles GET /identities/{id}/history?window=30d — the History tab.
func (h *IdentityHistoryHandler) Get(w http.ResponseWriter, r *http.Request) {
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
		fail(w, r, "load identity history", err)
		return
	}
	respond(w, env, true)
}
