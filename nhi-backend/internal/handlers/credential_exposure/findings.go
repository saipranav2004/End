package credential_exposure

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	svc "nhi-backend-api/internal/services/credential_exposure"
)

var (
	findingsListAllowed = map[string]struct{}{
		"client_id": {}, "platform": {}, "ml_flagged": {},
		"limit": {}, "offset": {}, "finding_id": {}, "list": {},
	}
	findingsSummaryAllowed = map[string]struct{}{
		"client_id": {}, "platform": {}, "ml_flagged": {}, "list": {},
	}
	validPlatforms = map[string]struct{}{
		"codecommit": {}, "github": {},
	}
)

type FindingsHandler struct{ service *svc.FindingsService }

func NewFindingsHandler(service *svc.FindingsService) *FindingsHandler {
	return &FindingsHandler{service: service}
}

// The five lists: every finding that is not allowlisted is in exactly one.
func (h *FindingsHandler) List(w http.ResponseWriter, r *http.Request) { h.list(w, r, models.ListMain) }
func (h *FindingsHandler) ListReview(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, models.ListReview)
}
func (h *FindingsHandler) ListRepeated(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, models.ListRepeated)
}
func (h *FindingsHandler) ListNoise(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, models.ListNoise)
}
func (h *FindingsHandler) ListFlagged(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, models.ListFlagged)
}

// listFromParam reads ?list=main|review|repeated|noise|ml_flagged (the scanner
// backend's selector); "" when absent.
func listFromParam(w http.ResponseWriter, raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", true
	case "main":
		return models.ListMain, true
	case "review":
		return models.ListReview, true
	case "repeated":
		return models.ListRepeated, true
	case "noise":
		return models.ListNoise, true
	case "ml_flagged", "flagged", "ml-flagged":
		return models.ListFlagged, true
	}
	writeAPIError(w, http.StatusBadRequest, "list must be main, review, repeated, noise or ml_flagged", "INVALID_QUERY")
	return "", false
}

// listFromFlag keeps the older ml_flagged=true|false switch working: true
// reads the ML-flagged list, false the main one.
func listFromFlag(w http.ResponseWriter, raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "ml_flagged must be true or false", "INVALID_QUERY")
		return "", false
	}
	if value {
		return models.ListFlagged, true
	}
	return models.ListMain, true
}

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
	list, ok := listFromFlag(w, q.Get("ml_flagged"))
	if !ok {
		return
	}
	if byName, ok := listFromParam(w, q.Get("list")); !ok {
		return
	} else if byName != "" {
		list = byName
	}
	filter.List = list
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
		writeInvalidOr(w, r, err, "INVALID_QUERY", "Unable to summarize findings")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *FindingsHandler) list(w http.ResponseWriter, r *http.Request, list string) {
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
				writeServerError(w, r, err, "Unable to load finding")
			}
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	filter := models.FindingsFilter{
		ClientID: strings.TrimSpace(q.Get("client_id")),
		Platform: strings.TrimSpace(q.Get("platform")),
		Limit:    200,
		List:     list,
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
	// ml_flagged only switches the main list; the other routes already name theirs.
	if list == models.ListMain {
		flagged, ok := listFromFlag(w, q.Get("ml_flagged"))
		if !ok {
			return
		}
		if flagged != "" {
			filter.List = flagged
		}
		if byName, ok := listFromParam(w, q.Get("list")); !ok {
			return
		} else if byName != "" {
			filter.List = byName
		}
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
		writeInvalidOr(w, r, err, "INVALID_QUERY", "Unable to load findings")
		return
	}
	writeJSON(w, http.StatusOK, models.FindingsResponse{Count: count, Findings: items, Limit: filter.Limit, Offset: filter.Offset})
}
