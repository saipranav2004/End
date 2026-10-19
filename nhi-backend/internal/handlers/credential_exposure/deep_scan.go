package credential_exposure

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	models "nhi-backend-api/internal/models/credential_exposure"
	svc "nhi-backend-api/internal/services/credential_exposure"
	"nhi-backend-api/internal/middleware"
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
	status, body, err := h.service.Trigger(r.Context(), claims.UserID, claims.TenantID, claims.DBName, req)
	if err != nil {
		if errors.Is(err, svc.ErrScannerProxyUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "SCANNER_UNAVAILABLE", "Deep-scan proxy is not configured")
		} else {
			writeError(w, http.StatusBadGateway, "SCANNER_UNAVAILABLE", "Unable to reach the scanner aggregator")
		}
		return
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		if status == http.StatusBadRequest || status == http.StatusNotFound {
			code := "SCANNER_REJECTED"
			// The upstream body is untrusted free text: log it, never echo it.
			if raw := strings.TrimSpace(string(body)); raw != "" {
				if len(raw) > 300 {
					raw = raw[:300]
				}
				log.Printf("deep-scan: scanner returned %d: %s", status, raw)
			}
			message := "Scanner rejected the deep-scan request"
			if status == http.StatusNotFound {
				code = "CLIENT_NOT_FOUND"
				message = "Unknown client_id"
			}
			writeError(w, status, code, message)
			return
		}
		writeError(w, http.StatusBadGateway, "INVALID_SCANNER_RESPONSE", "Scanner aggregator returned invalid JSON")
		return
	}
	writeJSON(w, status, payload)
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
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to load repository scan status")
		return
	}
	if len(items) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_started"})
		return
	}
	if len(items) > 1 {
		writeError(w, http.StatusConflict, "PLATFORM_REQUIRED", "More than one platform matched; provide the platform query parameter")
		return
	}
	writeJSON(w, http.StatusOK, items[0])
}
