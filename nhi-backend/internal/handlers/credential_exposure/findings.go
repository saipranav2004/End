package credential_exposure

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	svc "nhi-backend-api/internal/services/credential_exposure"
	"nhi-backend-api/internal/middleware"
)

var (
	findingsListAllowed = map[string]struct{}{
		"client_id": {}, "platform": {}, "ml_flagged": {},
		"limit": {}, "offset": {}, "finding_id": {},
	}
	findingsSummaryAllowed = map[string]struct{}{
		"client_id": {}, "platform": {}, "ml_flagged": {},
	}
	validPlatforms = map[string]struct{}{
		"codecommit": {}, "github": {},
	}
)

type FindingsHandler struct{ service *svc.FindingsService }

func NewFindingsHandler(service *svc.FindingsService) *FindingsHandler {
	return &FindingsHandler{service: service}
}

func (h *FindingsHandler) List(w http.ResponseWriter, r *http.Request)        { h.list(w, r, false) }
func (h *FindingsHandler) ListFlagged(w http.ResponseWriter, r *http.Request) { h.list(w, r, true) }

func (h *FindingsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, findingsSummaryAllowed) {
		return
	}
	q := r.URL.Query()
	filter := models.FindingsSummaryFilter{
		ClientID: strings.TrimSpace(q.Get("client_id")),
		Platform: strings.TrimSpace(q.Get("platform")),
	}
	if raw := q.Get("ml_flagged"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "ml_flagged must be true or false", "INVALID_QUERY")
			return
		}
		filter.MLFlagged = value
	}
	if filter.Platform != "" {
		if _, ok := validPlatforms[strings.ToLower(filter.Platform)]; !ok {
			writeAPIError(w, http.StatusBadRequest, "Invalid platform: "+filter.Platform+". Allowed: codecommit, github", "INVALID_PLATFORM")
			return
		}
	}
	if len(filter.ClientID) > 200 {
		writeAPIError(w, http.StatusBadRequest, "client_id exceeds 200 characters", "INVALID_QUERY")
		return
	}
	result, err := h.service.Summary(r.Context(), claims.UserID, claims.TenantID, claims.DBName, filter)
	if err != nil {
		if strings.Contains(err.Error(), "platform must") || strings.Contains(err.Error(), "client_id exceeds") {
			writeAPIError(w, http.StatusBadRequest, err.Error(), "INVALID_QUERY")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to summarize findings")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *FindingsHandler) list(w http.ResponseWriter, r *http.Request, forceFlagged bool) {
	if !requireScannerAdmin(w, r) {
		return
	}
	claims := middleware.GetClaims(r)
	if rejectUnknownQuery(w, r, findingsListAllowed) {
		return
	}
	q := r.URL.Query()
	if values, hasID := q["finding_id"]; hasID {
		id := strings.TrimSpace(values[0])
		if len(q) != 1 || id == "" || len(id) > 1000 {
			writeAPIError(w, http.StatusBadRequest, "finding_id must be the only query parameter and must not exceed 1000 characters", "INVALID_QUERY")
			return
		}
		item, err := h.service.Get(r.Context(), claims.UserID, claims.TenantID, claims.DBName, id)
		if err != nil {
			switch {
			case errors.Is(err, repo.ErrFindingNotFound):
				writeError(w, http.StatusNotFound, "NOT_FOUND", "Finding not found")
			case errors.Is(err, repo.ErrFindingAmbiguous):
				writeError(w, http.StatusConflict, "AMBIGUOUS_FINDING", "Finding ID matched more than one platform table")
			default:
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to load finding")
			}
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	filter := models.FindingsFilter{
		ClientID:  strings.TrimSpace(q.Get("client_id")),
		Platform:  strings.TrimSpace(q.Get("platform")),
		Limit:     200,
		MLFlagged: forceFlagged,
	}
	if len(filter.ClientID) > 200 {
		writeAPIError(w, http.StatusBadRequest, "client_id exceeds 200 characters", "INVALID_QUERY")
		return
	}
	if filter.Platform != "" {
		if _, ok := validPlatforms[strings.ToLower(filter.Platform)]; !ok {
			writeAPIError(w, http.StatusBadRequest, "Invalid platform: "+filter.Platform+". Allowed: codecommit, github", "INVALID_PLATFORM")
			return
		}
	}
	if raw := q.Get("ml_flagged"); raw != "" && !forceFlagged {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "ml_flagged must be true or false", "INVALID_QUERY")
			return
		}
		filter.MLFlagged = v
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 500 {
			writeAPIError(w, http.StatusBadRequest, "limit must be between 1 and 500", "INVALID_QUERY")
			return
		}
		filter.Limit = v
	}
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeAPIError(w, http.StatusBadRequest, "offset must be a non-negative integer", "INVALID_QUERY")
			return
		}
		filter.Offset = v
	}
	items, count, err := h.service.List(r.Context(), claims.UserID, claims.TenantID, claims.DBName, filter)
	if err != nil {
		if strings.Contains(err.Error(), "platform must") {
			writeAPIError(w, http.StatusBadRequest, err.Error(), "INVALID_QUERY")
		} else {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to load findings")
		}
		return
	}
	writeJSON(w, http.StatusOK, models.FindingsResponse{Count: count, Findings: items, Limit: filter.Limit, Offset: filter.Offset})
}
