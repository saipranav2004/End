package v1

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	modelsv1 "nhi-backend-api/internal/models/v1"
	repov1 "nhi-backend-api/internal/repository/v1"
	svcv1 "nhi-backend-api/internal/services/v1"
)

// ScanChangesHandler serves scan change tracking:
//
//	GET /scans                        newest completed scans (?limit=, default 20, max 50)
//	GET /scans/{scan_id}/changes      one scan's changes (scan_id may be "latest")
//	    ?change_type=CREATED|DELETED|MODIFIED   (absent or "all": every type)
//	    ?entity_type=identity|credential|permission|binding|trust_edge
//	    ?page=1&limit=25 (max 100)
type ScanChangesHandler struct {
	service *svcv1.ScanChangesService
}

func NewScanChangesHandler(service *svcv1.ScanChangesService) *ScanChangesHandler {
	return &ScanChangesHandler{service: service}
}

var (
	scanListAllowed    = map[string]struct{}{"limit": {}}
	scanChangesAllowed = map[string]struct{}{"change_type": {}, "entity_type": {}, "page": {}, "limit": {}}
)

const (
	scanListDefaultLimit    = 20
	scanListMaxLimit        = 50
	scanChangesDefaultLimit = 25
	scanChangesMaxLimit     = 100
)

// List handles GET /scans.
func (h *ScanChangesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}
	if rejectUnknownQuery(w, r, scanListAllowed) {
		return
	}
	limit, ok := positiveInt(w, r.URL.Query().Get("limit"), "limit", scanListDefaultLimit, scanListMaxLimit)
	if !ok {
		return
	}
	result, err := h.service.ListScans(r.Context(), claims.UserID, claims.TenantID, claims.DBName, limit)
	if err != nil {
		sendServerError(w, r, err, "Failed to list scans", "SCANS_LOAD_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Scans retrieved successfully", Data: result})
}

// scanIDRegex is the shape of nhi_scans_aws.scan_id (varchar(200)), e.g.
// "scan-20261016-201642"; the scanner's ids are not UUIDs.
var scanIDRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$`)

// Changes handles GET /scans/{scan_id}/changes.
func (h *ScanChangesHandler) Changes(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}
	scanID := strings.TrimSpace(mux.Vars(r)["scan_id"])
	if strings.EqualFold(scanID, repov1.LatestScanID) {
		scanID = repov1.LatestScanID
	}
	if !scanIDRegex.MatchString(scanID) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid scan ID", Error: "INVALID_SCAN_ID"})
		return
	}
	if rejectUnknownQuery(w, r, scanChangesAllowed) {
		return
	}
	q := r.URL.Query()

	changeType := strings.ToUpper(strings.TrimSpace(q.Get("change_type")))
	if changeType == "ALL" {
		changeType = ""
	}
	if changeType != "" && !contains(modelsv1.ChangeTypes, changeType) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false,
			Message: "Invalid change_type: " + q.Get("change_type") + ". Allowed: " + strings.Join(modelsv1.ChangeTypes, ", "), Error: "INVALID_CHANGE_TYPE"})
		return
	}
	entityType := strings.ToLower(strings.TrimSpace(q.Get("entity_type")))
	if entityType == "all" {
		entityType = ""
	}
	if entityType != "" && !contains(modelsv1.EntityTypes, entityType) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false,
			Message: "Invalid entity_type: " + q.Get("entity_type") + ". Allowed: " + strings.Join(modelsv1.EntityTypes, ", "), Error: "INVALID_ENTITY_TYPE"})
		return
	}
	page, ok := positiveInt(w, q.Get("page"), "page", 1, 0)
	if !ok {
		return
	}
	limit, ok := positiveInt(w, q.Get("limit"), "limit", scanChangesDefaultLimit, scanChangesMaxLimit)
	if !ok {
		return
	}
	if page > 10000 {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "page is too large", Error: "INVALID_PAGE"})
		return
	}

	result, err := h.service.Changes(r.Context(), claims.UserID, claims.TenantID, claims.DBName, modelsv1.ScanChangesFilter{
		ScanID: scanID, ChangeType: changeType, EntityType: entityType, Page: page, Limit: limit,
	})
	switch {
	case errors.Is(err, svcv1.ErrScanNotFound):
		sendJSONResponse(w, http.StatusNotFound, models.APIResponse{Success: false, Message: "Scan not found", Error: "SCAN_NOT_FOUND"})
		return
	case err != nil:
		sendServerError(w, r, err, "Failed to retrieve scan changes", "SCAN_CHANGES_LOAD_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Scan changes retrieved successfully", Data: result})
}

// positiveInt parses an optional positive integer parameter. max 0 means no
// cap; values above max are clamped.
func positiveInt(w http.ResponseWriter, raw, name string, def, max int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: name + " must be a positive integer", Error: "INVALID_" + strings.ToUpper(name)})
		return 0, false
	}
	if max > 0 && n > max {
		n = max
	}
	return n, true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Status handles GET /scans/{scan_id}/status: {scan_id, status, scan_time}. A
// scan outside the caller's scope is 404, never 403.
func (h *ScanChangesHandler) Status(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}
	scanID := strings.TrimSpace(mux.Vars(r)["scan_id"])
	if strings.EqualFold(scanID, repov1.LatestScanID) {
		scanID = repov1.LatestScanID
	}
	if !scanIDRegex.MatchString(scanID) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid scan ID", Error: "INVALID_SCAN_ID"})
		return
	}
	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}
	result, err := h.service.Status(r.Context(), claims.UserID, claims.TenantID, claims.DBName, scanID)
	if err != nil {
		if errors.Is(err, repov1.ErrScanNotFound) {
			sendJSONResponse(w, http.StatusNotFound, models.APIResponse{Success: false, Message: "scan '" + scanID + "' not found", Error: "SCAN_NOT_FOUND"})
			return
		}
		sendServerError(w, r, err, "Failed to retrieve scan status", "SCAN_STATUS_LOAD_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Scan status retrieved successfully", Data: result})
}
