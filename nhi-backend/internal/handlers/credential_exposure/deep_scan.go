package credential_exposure

import (
	"errors"
	"net/http"
	"strings"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/credential_exposure"
	svc "nhi-backend-api/internal/services/credential_exposure"
)

var deepScanStatusAllowed = map[string]struct{}{
	"client_id": {}, "repository": {}, "platform": {},
}

type DeepScanHandler struct{ service *svc.DeepScanService }

func NewDeepScanHandler(service *svc.DeepScanService) *DeepScanHandler {
	return &DeepScanHandler{service: service}
}

func (h *DeepScanHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, map[string]struct{}{}) {
		return
	}
	var req models.DeepScanRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.Repository = strings.TrimSpace(req.Repository)
	if req.ClientID == "" || len(req.ClientID) > 200 || req.Repository == "" || len(req.Repository) > 500 {
		writeAPIError(w, http.StatusBadRequest, "client_id and repository are required and must fit their database columns", "INVALID_REQUEST")
		return
	}
	req.Platform = strings.ToLower(strings.TrimSpace(req.Platform))
	if req.Platform != "" && req.Platform != "codecommit" && req.Platform != "github" {
		writeAPIError(w, http.StatusBadRequest, "Invalid platform: "+req.Platform+". Allowed: codecommit, github", "INVALID_PLATFORM")
		return
	}
	accepted, err := h.service.Trigger(r.Context(), claims.UserID, claims.TenantID, claims.DBName, req)
	switch {
	case errors.Is(err, svc.ErrDeepScanClientNotFound):
		writeError(w, http.StatusNotFound, "CLIENT_NOT_FOUND", "This client has no scanned repositories on that platform in your workspace")
		return
	case err != nil:
		writeInvalidOr(w, r, err, "INVALID_REPOSITORY", "Unable to request the deep scan")
		return
	}
	writeJSON(w, http.StatusAccepted, accepted)
}

func (h *DeepScanHandler) Status(w http.ResponseWriter, r *http.Request) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, deepScanStatusAllowed) {
		return
	}
	q := r.URL.Query()
	clientID := strings.TrimSpace(q.Get("client_id"))
	repoKey := strings.TrimSpace(q.Get("repository"))
	platform := strings.ToLower(strings.TrimSpace(q.Get("platform")))
	if clientID == "" || len(clientID) > 200 || repoKey == "" || len(repoKey) > 500 {
		writeAPIError(w, http.StatusBadRequest, "client_id and repository are required and must fit their database columns", "INVALID_QUERY")
		return
	}
	if platform != "" && platform != "codecommit" && platform != "github" {
		writeAPIError(w, http.StatusBadRequest, "Invalid platform: "+platform+". Allowed: codecommit, github", "INVALID_PLATFORM")
		return
	}
	items, err := h.service.Status(r.Context(), claims.UserID, claims.TenantID, claims.DBName, clientID, repoKey, platform)
	if err != nil {
		writeServerError(w, r, err, "Unable to load repository scan status")
		return
	}
	if len(items) == 0 {
		// Same shape as a started scan, so clients read one type.
		writeJSON(w, http.StatusOK, models.DeepScanResult{ClientID: clientID, RepoKey: repoKey, Platform: platform, Status: "not_started"})
		return
	}
	if len(items) > 1 {
		writeError(w, http.StatusConflict, "PLATFORM_REQUIRED", "More than one platform matched; provide the platform query parameter")
		return
	}
	writeJSON(w, http.StatusOK, items[0])
}
