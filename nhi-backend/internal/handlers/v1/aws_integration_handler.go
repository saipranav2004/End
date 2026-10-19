package v1

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/awsconnect"
	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	modelsv1 "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/repository"
	svcv1 "nhi-backend-api/internal/services/v1"
)

var awsAccountIDRE = regexp.MustCompile(`^\d{12}$`)

// AWSIntegrationHandler serves the "Connect AWS Account" setup.
//
//	GET    /api/v1/integrations/aws                                any signed-in user (setup values: admin only)
//	POST   /api/v1/integrations/aws/verify                         admin
//	PUT    /api/v1/integrations/aws/accounts/{account_id}/regions  admin
//	PUT    /api/v1/integrations/aws/accounts/{account_id}/exposure admin
//	POST   /api/v1/integrations/aws/accounts/{account_id}/sync     admin
//	DELETE /api/v1/integrations/aws/accounts/{account_id}          admin
type AWSIntegrationHandler struct{ service *svcv1.AWSIntegrationService }

func NewAWSIntegrationHandler(service *svcv1.AWSIntegrationService) *AWSIntegrationHandler {
	return &AWSIntegrationHandler{service: service}
}

// Connecting, changing and removing accounts changes what the whole tenant
// sees, so they are admin actions, like the other tenant-wide writes here.
func canManageAWS(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}

func (h *AWSIntegrationHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}
	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	result, err := h.service.Get(r.Context(), svcv1.AWSCaller{TenantID: claims.TenantID, Schema: claims.DBName, Actor: claims.UserID}, canManageAWS(claims.Role))
	if err != nil {
		writeAWSError(w, err, "Failed to load the AWS connection", "AWS_INTEGRATION_LOAD_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "AWS connection retrieved", Data: result})
}

func (h *AWSIntegrationHandler) Verify(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.adminCaller(w, r)
	if !ok {
		return
	}
	var req modelsv1.AWSVerifyRequest
	if !decodeAWSBody(w, r, &req) {
		return
	}
	result, err := h.service.Verify(r.Context(), caller, req.RoleARN, req.Regions)
	if err != nil {
		writeAWSError(w, err, "Failed to verify the AWS connection", "AWS_VERIFY_FAILED")
		return
	}
	message := "Some checks need attention"
	if result.Status == modelsv1.AWSStatusConnected {
		message = "AWS account connected"
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: message, Data: result})
}

func (h *AWSIntegrationHandler) UpdateRegions(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.adminCaller(w, r)
	if !ok {
		return
	}
	accountID, ok := pathAccountID(w, r)
	if !ok {
		return
	}
	var req modelsv1.AWSRegionsRequest
	if !decodeAWSBody(w, r, &req) {
		return
	}
	account, err := h.service.UpdateRegions(r.Context(), caller, accountID, req.Regions)
	if err != nil {
		writeAWSError(w, err, "Failed to update the regions", "AWS_REGIONS_UPDATE_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Regions updated", Data: account})
}

func (h *AWSIntegrationHandler) Exposure(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.adminCaller(w, r)
	if !ok {
		return
	}
	accountID, ok := pathAccountID(w, r)
	if !ok {
		return
	}
	var req modelsv1.AWSExposureRequest
	if !decodeAWSBody(w, r, &req) {
		return
	}
	result, err := h.service.SetExposure(r.Context(), caller, accountID, req)
	if err != nil {
		writeAWSError(w, err, "Failed to update exposed-credential scanning", "AWS_EXPOSURE_UPDATE_FAILED")
		return
	}
	message := "Exposed-credential scanning updated"
	if (req.GitHub && !result.Account.Exposure.GitHub.Connected) || (req.CodeCommit && !result.Account.Exposure.CodeCommit.Connected) {
		message = "Some checks need attention"
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: message, Data: result})
}

func (h *AWSIntegrationHandler) Sync(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.adminCaller(w, r)
	if !ok {
		return
	}
	accountID, ok := pathAccountID(w, r)
	if !ok {
		return
	}
	result, err := h.service.RequestSync(r.Context(), caller, accountID)
	if err != nil {
		writeAWSError(w, err, "Failed to start the sync", "AWS_SYNC_FAILED")
		return
	}
	message := "Sync queued"
	if result.AlreadyQueued {
		message = "A sync is already queued or running"
	}
	// 202: the sync itself runs in the discovery worker.
	sendJSONResponse(w, http.StatusAccepted, models.APIResponse{Success: true, Message: message, Data: result})
}

func (h *AWSIntegrationHandler) Remove(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.adminCaller(w, r)
	if !ok {
		return
	}
	accountID, ok := pathAccountID(w, r)
	if !ok {
		return
	}
	if err := h.service.RemoveAccount(r.Context(), caller, accountID); err != nil {
		writeAWSError(w, err, "Failed to remove the account", "AWS_REMOVE_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "AWS account removed", Data: map[string]string{"account_id": accountID}})
}

// adminCaller checks the token, the role and the query string shared by every
// write, and returns who is calling.
func (h *AWSIntegrationHandler) adminCaller(w http.ResponseWriter, r *http.Request) (svcv1.AWSCaller, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return svcv1.AWSCaller{}, false
	}
	if rejectUnknownQuery(w, r, noQueryAllowed) {
		return svcv1.AWSCaller{}, false
	}
	w.Header().Set("Cache-Control", "no-store")
	if !canManageAWS(claims.Role) {
		sendJSONResponse(w, http.StatusForbidden, models.APIResponse{Success: false, Message: "Only an admin can change AWS accounts", Error: "FORBIDDEN"})
		return svcv1.AWSCaller{}, false
	}
	return svcv1.AWSCaller{TenantID: claims.TenantID, Schema: claims.DBName, Actor: claims.UserID}, true
}

func pathAccountID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(mux.Vars(r)["account_id"])
	if !awsAccountIDRE.MatchString(id) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "An AWS account ID is 12 digits", Error: "INVALID_ACCOUNT_ID"})
		return "", false
	}
	return id, true
}

func decodeAWSBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Request body must be valid JSON with supported fields", Error: "INVALID_REQUEST"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Request body must contain exactly one JSON object", Error: "INVALID_REQUEST"})
		return false
	}
	return true
}

// writeAWSError maps service errors to responses. Messages are written for
// the person on the setup page; internal detail stays in the server log.
func writeAWSError(w http.ResponseWriter, err error, fallback, fallbackCode string) {
	var invalidARN *awsconnect.ErrInvalidRoleARN
	var invalidRegions *awsconnect.ErrInvalidRegions
	var invalidSecret *awsconnect.ErrInvalidSecretARN
	switch {
	case errors.As(err, &invalidSecret):
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: invalidSecret.Message, Error: "INVALID_SECRET_ARN"})
	case errors.As(err, &invalidARN):
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: invalidARN.Message, Error: "INVALID_ROLE_ARN"})
	case errors.As(err, &invalidRegions):
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: invalidRegions.Message, Error: "INVALID_REGIONS"})
	case errors.Is(err, repository.ErrAWSAccountNotFound):
		sendJSONResponse(w, http.StatusNotFound, models.APIResponse{Success: false, Message: "This AWS account is not connected to your workspace", Error: "AWS_ACCOUNT_NOT_FOUND"})
	case errors.Is(err, repository.ErrAWSAccountNotConnected):
		sendJSONResponse(w, http.StatusConflict, models.APIResponse{Success: false, Message: "This account has not passed its checks yet. Verify the connection first", Error: "AWS_ACCOUNT_NOT_CONNECTED"})
	case errors.Is(err, svcv1.ErrAWSAccountClaimed):
		sendJSONResponse(w, http.StatusConflict, models.APIResponse{Success: false, Message: "This AWS account is already connected to another workspace. Contact support if that is unexpected", Error: "AWS_ACCOUNT_IN_USE"})
	case errors.Is(err, svcv1.ErrAWSVerifyInProgress):
		sendJSONResponse(w, http.StatusTooManyRequests, models.APIResponse{Success: false, Message: "A connection check is already running. Wait for it to finish", Error: "AWS_VERIFY_IN_PROGRESS"})
	case errors.Is(err, repository.ErrAWSTablesMissing), errors.Is(err, svcv1.ErrAWSTenantNotConfigured):
		if errors.Is(err, repository.ErrAWSTablesMissing) {
			log.Printf("aws integration: %v", err)
		}
		sendJSONResponse(w, http.StatusServiceUnavailable, models.APIResponse{Success: false, Message: "AWS connections are not set up for your workspace yet. Contact support", Error: "AWS_PLATFORM_NOT_CONFIGURED"})
	case errors.Is(err, svcv1.ErrAWSUnavailable):
		sendJSONResponse(w, http.StatusServiceUnavailable, models.APIResponse{Success: false, Message: "AWS cannot be reached right now. Try again shortly", Error: "AWS_UNAVAILABLE"})
	default:
		log.Printf("aws integration: %v", err)
		sendJSONResponse(w, http.StatusInternalServerError, models.APIResponse{Success: false, Message: fallback, Error: fallbackCode})
	}
}
