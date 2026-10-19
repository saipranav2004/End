package genome

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nhi-backend-api/internal/middleware"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// DashboardSummaryHandler serves GET /api/v1/dashboard/summary
// (backend_api_contract.md §4.1). Like the other v1 handlers it only reads
// the JWT claims, validates the query and shapes the response; the data
// work is in svc.DashboardSummaryService.
//
// This endpoint answers in the contract's §1.5 envelope — {data, meta} /
// {error} with request_id — not in models.APIResponse.
type DashboardSummaryHandler struct {
	service *svc.DashboardSummaryService
	now     func() time.Time
}

func NewDashboardSummaryHandler(service *svc.DashboardSummaryService) *DashboardSummaryHandler {
	return &DashboardSummaryHandler{service: service, now: func() time.Time { return time.Now().UTC().Truncate(time.Second) }}
}

// Query parameters of §4.1 (nothing else is accepted).
var dashboardSummaryAllowed = map[string]struct{}{
	"from": {}, "to": {}, "source_type": {}, "account_id": {}, "region": {},
}

// §4.1 defaults when from/to are omitted: 14 days for charts/panels, 24 hours
// for the open-anomaly cards.
const (
	summaryDefaultCharts = 14 * 24 * time.Hour
	summaryDefaultCards  = 24 * time.Hour
)

// Get handles GET /api/v1/dashboard/summary.
func (h *DashboardSummaryHandler) Get(w http.ResponseWriter, r *http.Request) {
	rid := summaryRequestID(r)
	w.Header().Set("X-Request-ID", rid)

	claims := middleware.GetClaims(r)
	if claims == nil {
		writeSummaryError(w, http.StatusUnauthorized, models.MonitoringCodeUnauthorized, "authentication required", rid)
		return
	}
	q := r.URL.Query()
	if unknown, bad := checkUnknown(q, dashboardSummaryAllowed); bad {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation,
			fmt.Sprintf("unknown query parameter %q; allowed: %s", unknown, allowedList(dashboardSummaryAllowed)), rid)
		return
	}
	charts, cards, err := parseSummaryWindows(q, h.now())
	if err != nil {
		writeSummaryError(w, http.StatusBadRequest, models.MonitoringCodeValidation, err.Error(), rid)
		return
	}

	data, err := h.service.GetSummary(r.Context(), claims.UserID, claims.TenantID, claims.DBName, svc.DashboardSummaryParams{
		Charts:     charts,
		Cards:      cards,
		SourceType: strings.ToUpper(strings.TrimSpace(q.Get("source_type"))), // §2: compact upper-case family
		AccountID:  strings.TrimSpace(q.Get("account_id")),
		Region:     strings.TrimSpace(q.Get("region")),
	})
	if err != nil {
		var unavailable *svc.StoreUnavailableError
		if errors.As(err, &unavailable) {
			log.Printf("dashboard summary rid=%s: %v", rid, err)
			writeSummaryError(w, http.StatusServiceUnavailable, models.MonitoringCodeStoreUnavailable, unavailable.Op+" failed", rid)
			return
		}
		log.Printf("dashboard summary rid=%s: unexpected error: %v", rid, err)
		writeSummaryError(w, http.StatusInternalServerError, models.MonitoringCodeInternal, "unexpected error", rid)
		return
	}
	writeSummaryJSON(w, http.StatusOK, models.MonitoringEnvelope{
		Data: data,
		Meta: models.MonitoringMeta{RequestID: rid, GeneratedAt: h.now()},
	})
}

// parseSummaryWindows applies §4.1: with from/to (inclusive/exclusive ISO-8601,
// §2) both the charts and the cards use that window; when both are omitted
// the charts cover the last 14 days and the cards the last 24 hours.
func parseSummaryWindows(q url.Values, now time.Time) (charts, cards svc.TimeWindow, err error) {
	fromS, toS := strings.TrimSpace(q.Get("from")), strings.TrimSpace(q.Get("to"))
	if fromS == "" && toS == "" {
		charts = svc.TimeWindow{From: now.Add(-summaryDefaultCharts), To: now}
		cards = svc.TimeWindow{From: now.Add(-summaryDefaultCards), To: now}
		return charts, cards, nil
	}
	win := svc.TimeWindow{To: now}
	if toS != "" {
		if win.To, err = parseSummaryTime(toS); err != nil {
			return charts, cards, fmt.Errorf("to: %w", err)
		}
	}
	win.From = win.To.Add(-summaryDefaultCharts)
	if fromS != "" {
		if win.From, err = parseSummaryTime(fromS); err != nil {
			return charts, cards, fmt.Errorf("from: %w", err)
		}
	}
	if !win.From.Before(win.To) {
		return charts, cards, fmt.Errorf("from must be before to")
	}
	return win, win, nil
}
