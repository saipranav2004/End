package genome

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// AnomaliesHandler serves GET /genome/anomalies (backend-nhi-genome.md
// §6.1) and POST /genome/anomalies/{alert_id}/feedback (§6.4).
type AnomaliesHandler struct {
	service *svc.AnomaliesService
	now     func() time.Time
}

// NewAnomaliesHandler creates the handler.
func NewAnomaliesHandler(service *svc.AnomaliesService) *AnomaliesHandler {
	return &AnomaliesHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Codes beyond the shared §1.5 set.
const (
	codeAnomalyNotFound      = "ANOMALY_NOT_FOUND"
	codeFeedbackUnavailable  = "FEEDBACK_UNAVAILABLE"
	codeAnomalyNotReviewable = "ANOMALY_NOT_REVIEWABLE"
	codeForbidden            = "FORBIDDEN"
)

// §6.1 parameters, plus review (pending | reviewed) and identity_name (one
// identity's anomalies, with source_type).
var anomalyListAllowed = map[string]struct{}{
	"source_type": {}, "identity_name": {}, "search": {}, "dimension": {}, "state": {}, "review": {},
	"from": {}, "to": {}, "limit": {}, "cursor": {},
}

// Stored values are simple tokens; anything else is a typo, not a filter.
var anomalyTokenRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// List handles GET /genome/anomalies.
func (h *AnomaliesHandler) List(w http.ResponseWriter, r *http.Request) {
	rid := summaryRequestID(r)
	w.Header().Set("X-Request-ID", rid)
	claims := middleware.GetClaims(r)
	if claims == nil {
		writeSummaryError(w, http.StatusUnauthorized, models.MonitoringCodeUnauthorized, "authentication required", rid)
		return
	}
	q := r.URL.Query()
	if unknown, bad := checkUnknown(q, anomalyListAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(anomalyListAllowed)), rid)
		return
	}
	p := svc.AnomalyListParams{
		SourceType:   strings.ToUpper(strings.TrimSpace(q.Get("source_type"))),
		IdentityName: strings.TrimSpace(q.Get("identity_name")),
		Search:       strings.TrimSpace(q.Get("search")),
		Dimension:    strings.TrimSpace(q.Get("dimension")),
		State:        strings.ToLower(strings.TrimSpace(q.Get("state"))),
		Review:       strings.ToLower(strings.TrimSpace(q.Get("review"))),
		Cursor:       strings.TrimSpace(q.Get("cursor")),
	}
	bad := func(msg string) {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, msg, rid)
	}
	if p.IdentityName != "" && p.SourceType == "" {
		bad("identity_name needs source_type")
		return
	}
	if p.Dimension != "" && !anomalyTokenRE.MatchString(p.Dimension) {
		bad("dimension must be one stored dimension name, e.g. api_operation")
		return
	}
	if p.State != "" && !anomalyTokenRE.MatchString(p.State) {
		bad("state must be one stored state, e.g. open")
		return
	}
	switch p.Review {
	case "", svc.ReviewPending, svc.ReviewReviewed:
	default:
		bad("review must be pending or reviewed")
		return
	}
	for name, dst := range map[string]**time.Time{"from": &p.From, "to": &p.To} {
		if s := strings.TrimSpace(q.Get(name)); s != "" {
			t, err := parseSummaryTime(s)
			if err != nil {
				bad(name + " " + err.Error())
				return
			}
			*dst = &t
		}
	}
	if p.From != nil && p.To != nil && !p.From.Before(*p.To) {
		bad("from must be before to")
		return
	}
	limit, err := parseIdentityLimit(q, "limit")
	if err != nil {
		bad(err.Error())
		return
	}
	p.Limit = limit

	data, next, err := h.service.List(r.Context(), claims.UserID, claims.TenantID, claims.DBName, p)
	if errors.Is(err, svc.ErrInvalidCursor) {
		bad("cursor is invalid")
		return
	}
	if err != nil {
		writeIdentityServiceError(w, "anomaly list", rid, err)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now(), NextCursor: next},
	})
}

// maxFeedbackBody bounds the request body.
const maxFeedbackBody = 8 << 10

// Feedback handles POST /genome/anomalies/{alert_id}/feedback.
func (h *AnomaliesHandler) Feedback(w http.ResponseWriter, r *http.Request) {
	rid := summaryRequestID(r)
	w.Header().Set("X-Request-ID", rid)
	claims := middleware.GetClaims(r)
	if claims == nil {
		writeSummaryError(w, http.StatusUnauthorized, models.MonitoringCodeUnauthorized, "authentication required", rid)
		return
	}
	bad := func(msg string) {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, msg, rid)
	}
	if len(r.URL.Query()) > 0 {
		bad("this endpoint takes no query parameters")
		return
	}
	alertID, err := strconv.ParseInt(mux.Vars(r)["alert_id"], 10, 64)
	if err != nil || alertID <= 0 {
		bad("alert_id must be a positive integer")
		return
	}
	var req models.AnomalyFeedbackRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxFeedbackBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		bad("body must be JSON: {\"decision\": \"legitimate|malicious|unknown\", \"approved_for_baseline\": false, \"reason\": \"...\"}")
		return
	}
	reviewer := strings.TrimSpace(claims.Email)
	if reviewer == "" {
		reviewer = claims.UserID
	}
	fb, err := h.service.SaveFeedback(r.Context(), svc.FeedbackCaller{
		UserID: claims.UserID, TenantID: claims.TenantID, DBName: claims.DBName, Role: claims.Role, Reviewer: reviewer,
	}, alertID, req)
	var invalid *svc.FeedbackValidationError
	var unavailable *svc.StoreUnavailableError
	switch {
	case err == nil:
		writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
			Data: map[string]any{"alert_id": alertID, "feedback": fb},
			Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now()},
		})
	case errors.As(err, &invalid):
		bad(invalid.Msg)
	case errors.Is(err, svc.ErrFeedbackForbidden):
		writeSummaryError(w, http.StatusForbidden, codeForbidden, "Only analysts and admins can review anomalies", rid)
	case errors.Is(err, svc.ErrAnomalyNotFound):
		writeSummaryError(w, http.StatusNotFound, codeAnomalyNotFound, "The anomaly was not found", rid)
	case errors.Is(err, svc.ErrFeedbackUnavailable):
		writeSummaryError(w, http.StatusServiceUnavailable, codeFeedbackUnavailable,
			"Anomaly reviews cannot be saved yet: the behaviour pipeline's alert_feedback table is missing", rid)
	case errors.Is(err, svc.ErrAnomalyNotReviewable):
		writeSummaryError(w, http.StatusConflict, codeAnomalyNotReviewable,
			"This anomaly has no recorded event, so a review cannot be attached to it", rid)
	case errors.As(err, &unavailable):
		log.Printf("anomaly feedback rid=%s: %v", rid, err)
		writeSummaryError(w, http.StatusServiceUnavailable, models.MonitoringCodeStoreUnavailable, unavailable.Op+" failed", rid)
	default:
		writeIdentityServiceError(w, "anomaly feedback", rid, err)
	}
}
