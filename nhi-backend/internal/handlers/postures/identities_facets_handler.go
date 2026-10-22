package postures

import (
	"net/http"

	svc "nhi-backend-api/internal/services/postures"
)

type IdentitiesFacetsHandler struct {
	service *svc.IdentitiesFacetsService
}

func NewIdentitiesFacetsHandler(service *svc.IdentitiesFacetsService) *IdentitiesFacetsHandler {
	return &IdentitiesFacetsHandler{service: service}
}

// Get handles GET /identities/facets?<same filters> — the counts of the filter
// panel. Each facet ignores its own filter.
func (h *IdentitiesFacetsHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow(identityFilterParams...)) {
		return
	}
	filter, ok := parseIdentityFilter(w, q, false)
	if !ok {
		return
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, filter)
	if err != nil {
		fail(w, r, "load identity facets", err)
		return
	}
	respond(w, env, true)
}
