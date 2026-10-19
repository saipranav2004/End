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

type IdentitiesListHandler struct {
	service *svcv1.IdentitiesListService
}

func NewIdentitiesListHandler(service *svcv1.IdentitiesListService) *IdentitiesListHandler {
	return &IdentitiesListHandler{service: service}
}

// List handles GET /api/v1/identities — "ENDPOINT 4: Identities List".
func (h *IdentitiesListHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized"})
		return
	}

	if rejectUnknownQuery(w, r, identitiesListAllowed) {
		return
	}

	q := r.URL.Query()

	filter := modelsv1.IdentityListFilter{
		Page:      1,
		PageSize:  25,
		Search:    strings.TrimSpace(q.Get("search")),
		Owner:     strings.TrimSpace(q.Get("owner")),
		VPCID:     strings.TrimSpace(q.Get("vpc_id")),
		SortBy:    "last_activity_at",
		SortOrder: "desc",
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
	if raw := strings.TrimSpace(q.Get("identity_type")); raw != "" {
		vals := splitAndTrim(raw)
		if bad, ok := validateEnumValues(vals, validIdentityTypes); !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid identity_type: " + bad + ". Allowed: " + allowedList(validIdentityTypes),
				Error:   "INVALID_IDENTITY_TYPE",
			})
			return
		}
		filter.IdentityTypes = vals
	}
	if raw := strings.TrimSpace(q.Get("functional_class")); raw != "" {
		vals := splitAndTrim(raw)
		if bad, ok := validateEnumValues(vals, validFunctionalClasses); !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid functional_class: " + bad + ". Allowed: " + allowedList(validFunctionalClasses),
				Error:   "INVALID_FUNCTIONAL_CLASS",
			})
			return
		}
		filter.FunctionalClasses = vals
	}
	if raw := strings.TrimSpace(q.Get("environment")); raw != "" {
		vals := splitAndTrim(raw)
		if bad, ok := validateEnumValues(vals, validEnvironments); !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid environment: " + bad + ". Allowed: " + allowedList(validEnvironments),
				Error:   "INVALID_ENVIRONMENT",
			})
			return
		}
		filter.Environments = vals
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		vals := splitAndTrim(raw)
		if bad, ok := validateEnumValues(vals, validIdentityStatuses); !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid status: " + bad + ". Allowed: " + allowedList(validIdentityStatuses),
				Error:   "INVALID_STATUS",
			})
			return
		}
		filter.Statuses = vals
	}
	if raw := q.Get("sort_by"); raw != "" {
		if _, ok := identitySortColumnsAllowed[strings.ToLower(strings.TrimSpace(raw))]; !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid sort_by: " + raw + ". Allowed: " + allowedList(identitySortColumnsAllowed),
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
	if filter.IsPubliclyAccessible, err = parseOptionalBool(q, "is_publicly_accessible"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "is_publicly_accessible must be true or false"})
		return
	}
	if filter.MinIdleDays, err = parseOptionalInt(q, "min_idle_days"); err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "min_idle_days must be an integer"})
		return
	}

	result, err := h.service.GetIdentities(r.Context(), claims.UserID, claims.TenantID, claims.DBName, filter)
	if err != nil {
		log.Printf("identities list failed: %v", err)
		sendJSONResponse(w, http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to retrieve identities",
			Error:   "IDENTITIES_LOAD_FAILED",
		})
		return
	}

	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Identities retrieved successfully",
		Data:    result,
	})
}
