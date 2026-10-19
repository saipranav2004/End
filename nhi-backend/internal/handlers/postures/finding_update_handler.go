package postures

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	models "nhi-backend-api/internal/models/postures"
	svc "nhi-backend-api/internal/services/postures"
)

type FindingUpdateHandler struct {
	service *svc.FindingUpdateService
}

func NewFindingUpdateHandler(service *svc.FindingUpdateService) *FindingUpdateHandler {
	return &FindingUpdateHandler{service: service}
}

// maxPatchBody bounds the request body.
const maxPatchBody = 16 << 10

// Patch handles PATCH /findings/{id} (analyst / admin): accept, suppress,
// reopen, assign or add a note. Like the inventory APIs it needs only the
// finding id and the change: no version or scan id. Concurrent changes to one
// finding are applied one after the other (the row is locked), and every
// change is kept in the finding's history.
func (h *FindingUpdateHandler) Patch(w http.ResponseWriter, r *http.Request) {
	c, ok := begin(w, r)
	if !ok {
		return
	}
	if !c.CanWrite() { // checked before reading anything else
		fail(w, "update finding", svc.ErrForbidden)
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if !uuidRegex.MatchString(id) {
		invalid(w, "id", "id must be a valid finding UUID")
		return
	}
	if !strictQuery(w, r.URL.Query(), allow()) {
		return
	}
	var req models.FindingUpdateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPatchBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			invalid(w, "body", "a JSON body is required")
		} else {
			invalid(w, "body", "the body is not valid JSON for this request: "+err.Error())
		}
		return
	}

	env, err := h.service.Update(r.Context(), c, models.FindingUpdate{
		FindingID: strings.ToLower(id), Request: req,
	})
	if err != nil {
		fail(w, "update finding", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, env)
}
