package postures

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	models "nhi-backend-api/internal/models/postures"
	svc "nhi-backend-api/internal/services/postures"
)

type IdentityAlertsHandler struct {
	service *svc.IdentityAlertsService
}

func NewIdentityAlertsHandler(service *svc.IdentityAlertsService) *IdentityAlertsHandler {
	return &IdentityAlertsHandler{service: service}
}

// Get handles GET /identities/{id}/alerts?window=30d&status=open|closed|all&limit=&cursor=
// — the Alerts tab. Alerts change when an analyst accepts or suppresses one,
// not only at scan time, so this endpoint is not served from the scan-id ETag.
func (h *IdentityAlertsHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	id, ok := identityIDOK(w, mux.Vars(r)["id"])
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow("window", "status", "limit", "cursor")) {
		return
	}
	f := models.IdentityAlertsFilter{Status: models.AlertsAll}
	if f.Window, ok = parseWindow(w, q); !ok {
		return
	}
	if raw := strings.ToLower(strings.TrimSpace(q.Get("status"))); raw != "" {
		switch raw {
		case models.AlertsOpen, models.AlertsClosed, models.AlertsAll:
			f.Status = raw
		default:
			invalid(w, "status", "status must be open, closed or all")
			return
		}
	}
	if f.Limit, ok = parseLimit(w, q); !ok {
		return
	}
	if f.Cursor, ok = parseCursor(w, q); !ok {
		return
	}
	env, err := h.service.Get(r.Context(), c, id, f)
	if err != nil {
		fail(w, "load identity alerts", err)
		return
	}
	respond(w, env, false)
}
