package postures

import (
	"net/http"

	svc "nhi-backend-api/internal/services/postures"
)

type IdentitiesListHandler struct {
	service *svc.IdentitiesListService
}

func NewIdentitiesListHandler(service *svc.IdentitiesListService) *IdentitiesListHandler {
	return &IdentitiesListHandler{service: service}
}

// List handles GET /identities — the identity table with filters, sorting and
// keyset pagination (limit, cursor).
func (h *IdentitiesListHandler) List(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow(append([]string{"sort", "limit", "cursor"}, identityFilterParams...)...)) {
		return
	}
	filter, ok := parseIdentityFilter(w, q, true)
	if !ok {
		return
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.List(r.Context(), c, filter)
	if err != nil {
		fail(w, "list identities", err)
		return
	}
	respond(w, env, true)
}
