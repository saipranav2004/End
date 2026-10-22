package postures

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	models "nhi-backend-api/internal/models/postures"
	svc "nhi-backend-api/internal/services/postures"
)

type IdentityGraphHandler struct {
	service *svc.IdentityGraphService
}

func NewIdentityGraphHandler(service *svc.IdentityGraphService) *IdentityGraphHandler {
	return &IdentityGraphHandler{service: service}
}

// Get handles GET /identities/{id}/graph?depth=2&edge_types=&max_nodes=200 —
// the Access graph as nodes and edges.
func (h *IdentityGraphHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	id, ok := identityIDOK(w, mux.Vars(r)["id"])
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow("depth", "edge_types", "max_nodes")) {
		return
	}
	f := models.IdentityGraphFilter{Depth: models.DefaultGraphDepth, MaxNodes: models.DefaultGraphMaxNodes}
	if raw := strings.TrimSpace(q.Get("depth")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > models.MaxGraphDepth {
			invalid(w, "depth", fmt.Sprintf("depth must be an integer between 1 and %d", models.MaxGraphDepth))
			return
		}
		f.Depth = n
	}
	if raw := strings.TrimSpace(q.Get("max_nodes")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > models.MaxGraphMaxNodes {
			invalid(w, "max_nodes", fmt.Sprintf("max_nodes must be an integer between 1 and %d", models.MaxGraphMaxNodes))
			return
		}
		f.MaxNodes = n
	}
	types, ok := csvEnum(w, q, "edge_types", models.GraphEdgeTypes)
	if !ok {
		return
	}
	f.EdgeTypes = types
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, id, f)
	if err != nil {
		fail(w, r, "load identity graph", err)
		return
	}
	respond(w, env, true)
}
