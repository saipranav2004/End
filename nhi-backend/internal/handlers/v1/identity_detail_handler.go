package v1

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	modelsv1 "nhi-backend-api/internal/models/v1"
	svcv1 "nhi-backend-api/internal/services/v1"
)

var identityUUIDRegex = credentialUUIDRegex // identical UUID shape — reuse the compiled regex from credential_detail_handler.go

type IdentityDetailHandler struct {
	service *svcv1.IdentityDetailService
}

func NewIdentityDetailHandler(service *svcv1.IdentityDetailService) *IdentityDetailHandler {
	return &IdentityDetailHandler{service: service}
}

// Get handles GET /api/v1/identities/{id}?tab=&time_range=&start_date=&end_date=&service=&used_only=&depth=&edge_type=
// ("ENDPOINT 5: Identity Detail").
func (h *IdentityDetailHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "identity id is required", Error: "INVALID_REQUEST"})
		return
	}
	// Validate UUID format before it reaches "id = $1::uuid" in Postgres —
	// otherwise a malformed id throws a raw "invalid input syntax for type
	// uuid" (SQLSTATE 22P02) error that must never reach the client.
	if !identityUUIDRegex.MatchString(id) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid identity ID", Error: "INVALID_REQUEST"})
		return
	}

	if rejectUnknownQuery(w, r, identityDetailAllowed) {
		return
	}

	q := r.URL.Query()

	tab := strings.ToLower(strings.TrimSpace(q.Get("tab")))
	if tab == "" {
		tab = "overview"
	}
	if ok, _ := validateSingleEnum(tab, validIdentityTabs); !ok {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Invalid tab: " + tab + ". Allowed: " + allowedList(validIdentityTabs),
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

	usedOnly, err := parseOptionalBool(q, "used_only")
	if err != nil {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "used_only must be true or false", Error: "INVALID_REQUEST"})
		return
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

	// start_date / end_date validation — only meaningful with time_range=custom,
	// but we validate format regardless so typos are caught early.
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

	filter := modelsv1.IdentityDetailFilter{
		IdentityID: id,
		Tab:        tab,
		TimeRange:  timeRange,
		StartDate:  strings.TrimSpace(q.Get("start_date")),
		EndDate:    strings.TrimSpace(q.Get("end_date")),
		Page:       page,
		PageSize:   pageSize,
		Service:    strings.TrimSpace(q.Get("service")),
		UsedOnly:   usedOnly,
		Depth:      depth,
		EdgeType:   strings.TrimSpace(q.Get("edge_type")),
	}

	result, err := h.service.GetDetail(r.Context(), claims.UserID, claims.TenantID, claims.DBName, filter)
	if err != nil {
		var timeRangeErr *svcv1.TimeRangeError
		switch {
		case errors.As(err, &timeRangeErr):
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false, Message: timeRangeErr.Msg, Error: "INVALID_TIME_RANGE"})
		case errors.Is(err, svcv1.ErrInvalidIdentityTab):
			sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Unsupported tab. Available tabs: overview, permissions, activity, relationships, credentials, raw.",
				Error:   "INVALID_REQUEST",
			})
		case errors.Is(err, svcv1.ErrIdentityNotFound):
			sendJSONResponse(w, http.StatusNotFound, models.APIResponse{
				Success: false,
				Message: fmt.Sprintf("identity with id '%s' not found", id),
				Error:   "IDENTITY_NOT_FOUND",
			})
		default:
			sendServerError(w, err, "Failed to retrieve identity detail", "IDENTITY_DETAIL_FAILED")
		}
		return
	}

	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Identity detail retrieved successfully",
		Data:    result,
	})
}
