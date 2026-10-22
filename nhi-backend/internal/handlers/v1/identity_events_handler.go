package v1

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	modelsv1 "nhi-backend-api/internal/models/v1"
	svcv1 "nhi-backend-api/internal/services/v1"
)

// Query parameters accepted by GET /api/v1/identities/{id}/events. This is a
// separate allow-list from identityDetailAllowed so the identity detail
// endpoint's validation is untouched.
var identityEventsAllowed = map[string]struct{}{
	"section": {}, "time_range": {}, "start_date": {}, "end_date": {},
	"service": {}, "action": {}, "result": {}, "resource": {}, "source_ip": {},
	"credential": {}, "read_write": {}, "page": {}, "page_size": {}, "event_id": {},
}

var (
	validEventsSections = map[string]struct{}{
		modelsv1.EventsSectionTimeline: {}, modelsv1.EventsSectionDetail: {},
		modelsv1.EventsSectionCredentials: {}, modelsv1.EventsSectionResources: {},
		modelsv1.EventsSectionHeatmap: {}, modelsv1.EventsSectionSessions: {},
		modelsv1.EventsSectionErrors: {}, modelsv1.EventsSectionCrossAccount: {},
		modelsv1.EventsSectionIPs: {},
	}
	validEventsResults    = map[string]struct{}{"success": {}, "error": {}, "all": {}}
	validEventsReadWrites = map[string]struct{}{"read": {}, "write": {}, "all": {}}

	eventsServiceRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	eventsActionRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	eventsIDRe      = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
)

const (
	eventsDefaultPageSize = 100
	eventsMaxPageSize     = 500
	// eventsMaxOffsetEnd bounds page*page_size so deep paging stays cheap.
	eventsMaxOffsetEnd = 100000
	eventsMaxTextLen   = 256
	eventsMaxARNLen    = 2048
)

type IdentityEventsHandler struct {
	service *svcv1.IdentityEventsService
}

func NewIdentityEventsHandler(service *svcv1.IdentityEventsService) *IdentityEventsHandler {
	return &IdentityEventsHandler{service: service}
}

func eventsBadRequest(w http.ResponseWriter, code, msg string) {
	sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: msg, Error: code})
}

// Get handles GET /api/v1/identities/{id}/events?section=...
// ("Identity Event Graph": 9 sections over the identity's CloudTrail events).
func (h *IdentityEventsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if !identityUUIDRegex.MatchString(id) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: "Invalid identity ID", Error: "INVALID_REQUEST"})
		return
	}
	if rejectUnknownQuery(w, r, identityEventsAllowed) {
		return
	}
	query, ok := parseIdentityEventsQuery(w, id, r.URL.Query())
	if !ok {
		return
	}

	result, err := h.service.Get(r.Context(), claims.UserID, claims.TenantID, claims.DBName, query)
	if err != nil {
		var timeRangeErr *svcv1.TimeRangeError
		switch {
		case errors.As(err, &timeRangeErr):
			eventsBadRequest(w, "INVALID_TIME_RANGE", timeRangeErr.Msg)
		case errors.Is(err, svcv1.ErrIdentityNotFound):
			sendJSONResponse(w, http.StatusNotFound, models.APIResponse{
				Success: false, Message: fmt.Sprintf("identity with id '%s' not found", id), Error: "IDENTITY_NOT_FOUND"})
		case errors.Is(err, svcv1.ErrEventNotFound):
			sendJSONResponse(w, http.StatusNotFound, models.APIResponse{
				Success: false, Message: fmt.Sprintf("event with id '%s' not found for this identity", query.EventID),
				Error: "EVENT_NOT_FOUND"})
		default:
			sendServerError(w, r, err, "Failed to retrieve identity events", "IDENTITY_EVENTS_FAILED")
		}
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{
		Success: true, Message: "Identity events retrieved successfully", Data: result})
}

// parseIdentityEventsQuery validates every parameter. It writes the 400 itself
// and returns ok=false on the first problem.
func parseIdentityEventsQuery(w http.ResponseWriter, id string, q url.Values) (modelsv1.IdentityEventsQuery, bool) {
	out := modelsv1.IdentityEventsQuery{IdentityID: id}
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }

	out.Section = strings.ToLower(get("section"))
	if out.Section == "" {
		out.Section = modelsv1.EventsSectionTimeline
	}
	if _, ok := validEventsSections[out.Section]; !ok {
		eventsBadRequest(w, "INVALID_SECTION", "Invalid section: "+out.Section+". Allowed: "+allowedList(validEventsSections))
		return out, false
	}

	out.TimeRange = strings.ToLower(get("time_range"))
	if out.TimeRange == "" {
		out.TimeRange = svcv1.EventsDefaultTimeRange
	}
	out.StartDate, out.EndDate = get("start_date"), get("end_date")
	if out.StartDate != "" && !isValidDate(out.StartDate) {
		eventsBadRequest(w, "INVALID_DATE", "Invalid start_date format. Use YYYY-MM-DD or RFC3339")
		return out, false
	}
	if out.EndDate != "" && !isValidDate(out.EndDate) {
		eventsBadRequest(w, "INVALID_DATE", "Invalid end_date format. Use YYYY-MM-DD or RFC3339")
		return out, false
	}
	if out.TimeRange == "custom" {
		if out.StartDate == "" || out.EndDate == "" {
			eventsBadRequest(w, "INVALID_TIME_RANGE", "time_range=custom requires both start_date and end_date")
			return out, false
		}
	} else if out.StartDate != "" || out.EndDate != "" {
		eventsBadRequest(w, "INVALID_TIME_RANGE", "start_date and end_date are only allowed when time_range=custom")
		return out, false
	}

	out.Result = strings.ToLower(get("result"))
	if out.Result == "" {
		out.Result = "all"
	}
	if _, ok := validEventsResults[out.Result]; !ok {
		eventsBadRequest(w, "INVALID_RESULT", "Invalid result: "+out.Result+". Allowed: "+allowedList(validEventsResults))
		return out, false
	}
	out.ReadWrite = strings.ToLower(get("read_write"))
	if out.ReadWrite == "" {
		out.ReadWrite = "all"
	}
	if _, ok := validEventsReadWrites[out.ReadWrite]; !ok {
		eventsBadRequest(w, "INVALID_READ_WRITE", "Invalid read_write: "+out.ReadWrite+". Allowed: "+allowedList(validEventsReadWrites))
		return out, false
	}

	out.Service = strings.ToLower(get("service"))
	if out.Service != "" && !eventsServiceRe.MatchString(out.Service) {
		eventsBadRequest(w, "INVALID_SERVICE", "service must be an AWS service short name such as s3 or sts")
		return out, false
	}
	out.Action = get("action")
	if out.Action != "" && !eventsActionRe.MatchString(out.Action) {
		eventsBadRequest(w, "INVALID_ACTION", "action must be an API action name such as GetObject")
		return out, false
	}
	for _, f := range []struct {
		name string
		dst  *string
		max  int
	}{{"resource", &out.Resource, eventsMaxTextLen}, {"credential", &out.Credential, eventsMaxARNLen}} {
		v := get(f.name)
		if len(v) > f.max || !utf8.ValidString(v) || strings.ContainsAny(v, "\x00\r\n") {
			eventsBadRequest(w, "INVALID_FILTER", f.name+" is too long or contains invalid characters")
			return out, false
		}
		*f.dst = v
	}
	out.SourceIP = get("source_ip")
	if out.SourceIP != "" && net.ParseIP(out.SourceIP) == nil {
		eventsBadRequest(w, "INVALID_SOURCE_IP", "source_ip must be a valid IPv4 or IPv6 address")
		return out, false
	}

	out.EventID = get("event_id")
	if out.Section == modelsv1.EventsSectionDetail {
		if out.EventID == "" {
			eventsBadRequest(w, "MISSING_EVENT_ID", "event_id is required when section=detail")
			return out, false
		}
		if !eventsIDRe.MatchString(out.EventID) {
			eventsBadRequest(w, "INVALID_EVENT_ID", "event_id has an invalid format")
			return out, false
		}
	}

	out.Page, out.PageSize = 1, eventsDefaultPageSize
	if raw := get("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			eventsBadRequest(w, "INVALID_PAGINATION", "page must be a positive integer")
			return out, false
		}
		out.Page = v
	}
	if raw := get("page_size"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > eventsMaxPageSize {
			eventsBadRequest(w, "INVALID_PAGINATION", fmt.Sprintf("page_size must be an integer between 1 and %d", eventsMaxPageSize))
			return out, false
		}
		out.PageSize = v
	}
	if out.Page*out.PageSize > eventsMaxOffsetEnd {
		eventsBadRequest(w, "INVALID_PAGINATION",
			fmt.Sprintf("page * page_size must not exceed %d events; narrow the time range or filter instead", eventsMaxOffsetEnd))
		return out, false
	}
	return out, true
}
