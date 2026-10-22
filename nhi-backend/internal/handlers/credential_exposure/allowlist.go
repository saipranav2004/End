package credential_exposure

import (
	"net/http"
	"strings"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/credential_exposure"
	svc "nhi-backend-api/internal/services/credential_exposure"
)

var allowlistListAllowed = map[string]struct{}{"client_id": {}}

type AllowlistHandler struct{ service *svc.AllowlistService }

func NewAllowlistHandler(service *svc.AllowlistService) *AllowlistHandler {
	return &AllowlistHandler{service: service}
}

func (h *AllowlistHandler) List(w http.ResponseWriter, r *http.Request) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, allowlistListAllowed) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("client_id"))
	if len(id) > 200 {
		writeAPIError(w, http.StatusBadRequest, "client_id exceeds 200 characters", "INVALID_QUERY")
		return
	}
	entries, err := h.service.List(r.Context(), claims.UserID, claims.TenantID, claims.DBName, id)
	if err != nil {
		writeServerError(w, r, err, "Unable to load allowlist")
		return
	}
	writeJSON(w, http.StatusOK, models.AllowlistListResponse{Entries: entries})
}

func (h *AllowlistHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, map[string]struct{}{}) {
		return
	}
	var req models.AllowlistRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	req.DismissedBy = claims.UserID // the signed-in user, never the client's claim
	entry, err := h.service.Upsert(r.Context(), claims.UserID, claims.TenantID, claims.DBName, req)
	if err != nil {
		writeAllowlistError(w, r, "upsert", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "entry": entry})
}

func (h *AllowlistHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, map[string]struct{}{}) {
		return
	}
	var req models.AllowlistRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	ok, err := h.service.Delete(r.Context(), claims.UserID, claims.TenantID, claims.DBName, req)
	if err != nil {
		writeAllowlistError(w, r, "delete", err)
		return
	}
	writeJSON(w, http.StatusOK, models.OKResponse{OK: ok})
}

// writeAllowlistError returns validation errors (safe messages) as 400 and
// hides everything else (database/resolver detail) behind a generic 500.
func writeAllowlistError(w http.ResponseWriter, r *http.Request, op string, err error) {
	writeInvalidOr(w, r, err, "INVALID_ALLOWLIST_ENTRY", "Unable to update the allowlist")
}
