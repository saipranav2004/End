package postures

import (
	"net/http"

	"github.com/gorilla/mux"

	svc "nhi-backend-api/internal/services/postures"
)

type CheckDetailHandler struct {
	service *svc.CheckDetailService
}

func NewCheckDetailHandler(service *svc.CheckDetailService) *CheckDetailHandler {
	return &CheckDetailHandler{service: service}
}

// Get handles GET /checks/{rule_id} — the details behind the Remediate button.
func (h *CheckDetailHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	ruleID, ok := ruleIDOK(w, mux.Vars(r)["rule_id"])
	if !ok {
		return
	}
	if !strictQuery(w, r.URL.Query(), allow()) {
		return
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, ruleID)
	if err != nil {
		fail(w, r, "load check", err)
		return
	}
	respond(w, env, true)
}
