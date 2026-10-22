package v1

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	svcv1 "nhi-backend-api/internal/services/v1"
)

var permissionSourceAllowed = map[string]struct{}{
	"credential_id": {}, "event_source": {}, "event_name": {},
}

// CloudTrail event sources / names are plain tokens such as
// "s3.amazonaws.com" and "GetObject".
var eventTokenRegex = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type PermissionSourceHandler struct {
	service *svcv1.PermissionSourceService
}

func NewPermissionSourceHandler(service *svcv1.PermissionSourceService) *PermissionSourceHandler {
	return &PermissionSourceHandler{service: service}
}

// Get handles GET /api/v1/permission-source?credential_id=&event_source=&event_name=
// — the policy behind one event, looked up only when a user opens that event.
func (h *PermissionSourceHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}
	if rejectUnknownQuery(w, r, permissionSourceAllowed) {
		return
	}
	q := r.URL.Query()
	credentialID := strings.TrimSpace(q.Get("credential_id"))
	if !credentialUUIDRegex.MatchString(credentialID) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "credential_id must be a credential UUID", Error: "INVALID_CREDENTIAL_ID"})
		return
	}
	eventSource, eventName := strings.TrimSpace(q.Get("event_source")), strings.TrimSpace(q.Get("event_name"))
	if !eventTokenRegex.MatchString(eventSource) || !eventTokenRegex.MatchString(eventName) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "event_source and event_name are required (letters, digits, . _ : -)", Error: "INVALID_EVENT"})
		return
	}

	result, err := h.service.Find(r.Context(), claims.UserID, claims.TenantID, claims.DBName, credentialID, eventSource, eventName)
	if err != nil {
		if errors.Is(err, svcv1.ErrCredentialNotFound) {
			sendJSONResponse(w, http.StatusNotFound, models.APIResponse{Success: false, Message: "credential not found", Error: "CREDENTIAL_NOT_FOUND"})
			return
		}
		sendServerError(w, r, err, "Failed to look up the permission source", "PERMISSION_SOURCE_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Permission source retrieved successfully", Data: result})
}
