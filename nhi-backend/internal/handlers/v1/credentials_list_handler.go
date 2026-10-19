package v1

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	modelsv1 "nhi-backend-api/internal/models/v1"
	svcv1 "nhi-backend-api/internal/services/v1"
)

type CredentialsHandler struct {
	credentialsService *svcv1.CredentialsService
}

func NewCredentialsHandler(credentialsService *svcv1.CredentialsService) *CredentialsHandler {
	return &CredentialsHandler{credentialsService: credentialsService}
}

// List handles GET /api/v1/credentials — "ENDPOINT 6: Credentials List".
func (h *CredentialsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{
			Success: false,
			Message: "Unauthorized",
		})
		return
	}

	if rejectUnknownQuery(w, r, credentialsListAllowed) {
		return
	}

	q := r.URL.Query()

	filter := modelsv1.CredentialListFilter{
		Page:              1,
		PageSize:          25,
		Search:            strings.TrimSpace(q.Get("search")),
		OwnerPrincipalARN: strings.TrimSpace(q.Get("owner_principal_arn")),
		SortBy:            "last_used_at",
		SortOrder:         "desc",
	}

	if raw := q.Get("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "page must be a positive integer"})
			return
		}
		filter.Page = v
	}
	if raw := q.Get("page_size"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "page_size must be a positive integer"})
			return
		}
		// Documented maximum is 100 rows per page.
		if v > maxPageSize {
			v = maxPageSize
		}
		filter.PageSize = v
	}
	if raw := strings.TrimSpace(q.Get("credential_type")); raw != "" {
		vals := splitAndTrim(raw)
		if bad, ok := validateEnumValues(vals, validCredentialTypes); !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid credential_type: " + bad + ". Allowed: " + allowedList(validCredentialTypes),
				Error:   "INVALID_CREDENTIAL_TYPE",
			})
			return
		}
		filter.CredentialTypes = vals
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		vals := splitAndTrim(raw)
		if bad, ok := validateEnumValues(vals, validCredentialStatuses); !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid status: " + bad + ". Allowed: " + allowedList(validCredentialStatuses),
				Error:   "INVALID_STATUS",
			})
			return
		}
		filter.Statuses = vals
	}
	if raw := q.Get("sort_by"); raw != "" {
		if _, ok := credentialSortColumnsAllowed[strings.ToLower(strings.TrimSpace(raw))]; !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid sort_by: " + raw + ". Allowed: " + allowedList(credentialSortColumnsAllowed),
				Error:   "INVALID_SORT_BY",
			})
			return
		}
		filter.SortBy = raw
	}
	if raw := q.Get("sort_order"); raw != "" {
		if !isValidSortOrder(raw) {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid sort_order: must be asc or desc",
				Error:   "INVALID_SORT_ORDER",
			})
			return
		}
		filter.SortOrder = raw
	}

	var err error
	if filter.RotationEnabled, err = parseOptionalBool(q, "rotation_enabled"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "rotation_enabled must be true or false"})
		return
	}
	if filter.RotationOverdue, err = parseOptionalBool(q, "rotation_overdue"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "rotation_overdue must be true or false"})
		return
	}
	if filter.IsOrphaned, err = parseOptionalBool(q, "is_orphaned"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "is_orphaned must be true or false"})
		return
	}
	if filter.HasExternalTrust, err = parseOptionalBool(q, "has_external_trust"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "has_external_trust must be true or false"})
		return
	}
	if filter.MinIdleDays, err = parseOptionalInt(q, "min_idle_days"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "min_idle_days must be an integer"})
		return
	}
	if filter.MaxDaysUntilExpiry, err = parseOptionalInt(q, "max_days_until_expiry"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "max_days_until_expiry must be an integer"})
		return
	}

	result, err := h.credentialsService.GetCredentials(r.Context(), claims.UserID, claims.TenantID, claims.DBName, filter)
	if err != nil {
		log.Printf("credentials list failed: %v", err)
		sendJSONResponse(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to retrieve credentials",
			Error:   "CREDENTIALS_LOAD_FAILED",
		})
		return
	}

	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Credentials retrieved successfully",
		Data:    result,
	})
}

func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseOptionalBool(q map[string][]string, key string) (*bool, error) {
	raw := strings.TrimSpace(firstOrEmpty(q, key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func parseOptionalInt(q map[string][]string, key string) (*int, error) {
	raw := strings.TrimSpace(firstOrEmpty(q, key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func firstOrEmpty(q map[string][]string, key string) string {
	if vs, ok := q[key]; ok && len(vs) > 0 {
		return vs[0]
	}
	return ""
}
