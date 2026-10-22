package v1

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	modelsv1 "nhi-backend-api/internal/models/v1"
	svcv1 "nhi-backend-api/internal/services/v1"
)

var credentialUUIDRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type CredentialDetailHandler struct {
	service *svcv1.CredentialDetailService
}

func NewCredentialDetailHandler(service *svcv1.CredentialDetailService) *CredentialDetailHandler {
	return &CredentialDetailHandler{service: service}
}

// Get handles GET /api/v1/credentials/{id}?tab=&time_range=&depth=&edge_type=
// ("ENDPOINT 7: Credential Detail").
func (h *CredentialDetailHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "credential id is required", Error: "INVALID_REQUEST"})
		return
	}
	// Validate UUID format before it reaches "id = $1::uuid" in Postgres —
	// otherwise a malformed id throws a raw "invalid input syntax for type
	// uuid" (SQLSTATE 22P02) error that must never reach the client.
	if !credentialUUIDRegex.MatchString(id) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid credential ID", Error: "INVALID_REQUEST"})
		return
	}

	if rejectUnknownQuery(w, r, credentialDetailAllowed) {
		return
	}

	q := r.URL.Query()

	tab := strings.ToLower(strings.TrimSpace(q.Get("tab")))
	if tab == "" {
		tab = "overview"
	}
	if ok, _ := validateSingleEnum(tab, validCredentialTabs); !ok {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Invalid tab: " + tab + ". Allowed: " + allowedList(validCredentialTabs),
			Error:   "INVALID_TAB",
		})
		return
	}

	timeRange := strings.TrimSpace(q.Get("time_range"))
	if timeRange == "" {
		timeRange = "90d"
	}

	depth := 1
	if raw := q.Get("depth"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > maxRelationshipDepth {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: fmt.Sprintf("depth must be an integer between 1 and %d", maxRelationshipDepth),
				Error:   "INVALID_REQUEST",
			})
			return
		}
		depth = v
	}

	if et := strings.TrimSpace(q.Get("edge_type")); et != "" {
		if ok, bad := validateEnumList(et, validEdgeTypes); !ok {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Invalid edge_type: " + bad + ". Allowed: " + allowedList(validEdgeTypes),
				Error:   "INVALID_EDGE_TYPE",
			})
			return
		}
	}

	if sd := strings.TrimSpace(q.Get("start_date")); sd != "" {
		if !isValidDate(sd) {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid start_date format. Use YYYY-MM-DD or RFC3339", Error: "INVALID_DATE"})
			return
		}
	}
	if ed := strings.TrimSpace(q.Get("end_date")); ed != "" {
		if !isValidDate(ed) {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid end_date format. Use YYYY-MM-DD or RFC3339", Error: "INVALID_DATE"})
			return
		}
	}
	if strings.EqualFold(timeRange, "custom") {
		if strings.TrimSpace(q.Get("start_date")) == "" || strings.TrimSpace(q.Get("end_date")) == "" {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "time_range=custom requires both start_date and end_date", Error: "INVALID_TIME_RANGE"})
			return
		}
	} else {
		if strings.TrimSpace(q.Get("start_date")) != "" || strings.TrimSpace(q.Get("end_date")) != "" {
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "start_date and end_date are only allowed when time_range=custom", Error: "INVALID_TIME_RANGE"})
			return
		}
	}

	page, pageSize, ok := parseActivityPaging(w, q)
	if !ok {
		return
	}

	filter := modelsv1.DetailFilter{
		CredentialID: id,
		Tab:          tab,
		TimeRange:    timeRange,
		StartDate:    strings.TrimSpace(q.Get("start_date")),
		EndDate:      strings.TrimSpace(q.Get("end_date")),
		Page:         page,
		PageSize:     pageSize,
		Depth:        depth,
		EdgeType:     strings.TrimSpace(q.Get("edge_type")),
	}

	result, err := h.service.GetDetail(r.Context(), claims.UserID, claims.TenantID, claims.DBName, filter)
	if err != nil {
		var timeRangeErr *svcv1.TimeRangeError
		switch {
		case errors.As(err, &timeRangeErr):
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false, Message: timeRangeErr.Msg, Error: "INVALID_TIME_RANGE"})
		case errors.Is(err, svcv1.ErrInvalidTab):
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Unsupported tab. Available tabs: overview, trust, activity, rotation, relationships, raw.",
				Error:   "INVALID_REQUEST",
			})
		case errors.Is(err, svcv1.ErrCredentialNotFound):
			sendJSONResponse(w, http.StatusNotFound, models.APIResponse{
				Success: false,
				Message: fmt.Sprintf("credential with id '%s' not found", id),
				Error:   "CREDENTIAL_NOT_FOUND",
			})
		default:
			sendServerError(w, r, err, "Failed to retrieve credential detail", "CREDENTIAL_DETAIL_FAILED")
		}
		return
	}

	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Credential detail retrieved successfully",
		Data:    result,
	})
}
