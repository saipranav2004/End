package postures

import (
	"net/http"

	svc "nhi-backend-api/internal/services/postures"
)

type PostureOverviewHandler struct {
	service *svc.PostureOverviewService
}

func NewPostureOverviewHandler(service *svc.PostureOverviewService) *PostureOverviewHandler {
	return &PostureOverviewHandler{service: service}
}

// Get handles GET /posture/overview?window=30d — the whole Posture page.
func (h *PostureOverviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
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
	env, err := h.service.Get(r.Context(), c, win)
	if err != nil {
		fail(w, "load posture overview", err)
		return
	}
	respond(w, env, true)
}
