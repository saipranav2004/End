package postures

import (
	"net/http"

	"github.com/gorilla/mux"

	svc "nhi-backend-api/internal/services/postures"
)

type QuickWinIdentitiesHandler struct {
	service *svc.QuickWinIdentitiesService
}

func NewQuickWinIdentitiesHandler(service *svc.QuickWinIdentitiesService) *QuickWinIdentitiesHandler {
	return &QuickWinIdentitiesHandler{service: service}
}

// Get handles GET /posture/quick-wins/{rule_id}/identities?limit=&cursor= —
// the identities failing one check.
func (h *QuickWinIdentitiesHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	ruleID, ok := ruleIDOK(w, mux.Vars(r)["rule_id"])
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow("limit", "cursor")) {
		return
	}
	limit, ok := parseLimit(w, q)
	if !ok {
		return
	}
	cursor, ok := parseCursor(w, q)
	if !ok {
		return
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, ruleID, limit, cursor)
	if err != nil {
		fail(w, r, "load quick-win identities", err)
		return
	}
	respond(w, env, true)
}
