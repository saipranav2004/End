package postures

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	models "nhi-backend-api/internal/models/postures"
	svc "nhi-backend-api/internal/services/postures"
)

type IdentityActivityHandler struct {
	service *svc.IdentityActivityService
}

func NewIdentityActivityHandler(service *svc.IdentityActivityService) *IdentityActivityHandler {
	return &IdentityActivityHandler{service: service}
}

// Get handles GET /identities/{id}/activity?window=30d&mutating=&service=&limit=&cursor=
// — the Activity tab: CloudTrail calls, newest first.
func (h *IdentityActivityHandler) Get(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	id, ok := identityIDOK(w, mux.Vars(r)["id"])
	if !ok {
		return
	}
	q := r.URL.Query()
	if !strictQuery(w, q, allow("window", "mutating", "service", "limit", "cursor")) {
		return
	}
	var f models.IdentityActivityFilter
	if f.Window, ok = parseWindow(w, q); !ok {
		return
	}
	if raw := strings.TrimSpace(q.Get("mutating")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			invalid(w, "mutating", "mutating must be true or false")
			return
		}
		f.Mutating = &v
	}
	if raw := strings.TrimSpace(q.Get("service")); raw != "" {
		if !tokenRegex.MatchString(raw) {
			invalid(w, "service", "service is not valid")
			return
		}
		f.Service = raw
	}
	if f.Limit, ok = parseLimit(w, q); !ok {
		return
	}
	if f.Cursor, ok = parseCursor(w, q); !ok {
		return
	}
	if notModified(w, r, c, h.service.ScanMeta) {
		return
	}
	env, err := h.service.Get(r.Context(), c, id, f)
	if err != nil {
		fail(w, "load identity activity", err)
		return
	}
	respond(w, env, true)
}
