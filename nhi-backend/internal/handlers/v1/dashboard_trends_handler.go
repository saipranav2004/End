package v1

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
	svcv1 "nhi-backend-api/internal/services/v1"
)

var (
	trendsQueryAllowed = map[string]struct{}{"account_ids": {}, "regions": {}, "limit": {}, "aggregate": {}}
	trendAccountRE     = regexp.MustCompile(`^\d{12}$`)
	trendRegionRE      = regexp.MustCompile(`^(global|[a-z]{2}(-[a-z]+)+-\d{1,2})$`)
)

const (
	trendsDefaultLimit = 20
	trendsMaxLimit     = 200
)

type DashboardTrendsHandler struct{ service *svcv1.DashboardTrendsService }

func NewDashboardTrendsHandler(service *svcv1.DashboardTrendsService) *DashboardTrendsHandler {
	return &DashboardTrendsHandler{service: service}
}

// Get handles GET /api/v1/dashboard/trends: identity and credential totals
// across the completed scans, for the trend chart.
//
//	account_ids  comma-separated 12-digit account ids (default: the saved selection)
//	regions      comma-separated regions; keeps accounts that read one of them
//	limit        newest points to return, 1-200 (default 20)
//	aggregate    true (default): one point per scan hour, accounts summed;
//	             false: one point per scan with scan_id and account_id
func (h *DashboardTrendsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		sendJSONResponse(w, http.StatusUnauthorized, models.APIResponse{Success: false, Message: "Unauthorized", Error: "UNAUTHORIZED"})
		return
	}
	if rejectUnknownQuery(w, r, trendsQueryAllowed) {
		return
	}
	bad := func(msg string) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: msg, Error: "INVALID_QUERY_PARAM"})
	}
	q := r.URL.Query()
	query := svcv1.TrendsQuery{Limit: trendsDefaultLimit, Aggregate: true}
	var ok bool
	if query.AccountIDs, ok = csvParam(q.Get("account_ids"), trendAccountRE); !ok {
		bad("account_ids must be comma-separated 12-digit AWS account ids")
		return
	}
	if query.Regions, ok = csvParam(q.Get("regions"), trendRegionRE); !ok {
		bad("regions must be comma-separated AWS region codes such as ap-south-1")
		return
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > trendsMaxLimit {
			bad("limit must be between 1 and 200")
			return
		}
		query.Limit = n
	}
	if raw := q.Get("aggregate"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			bad("aggregate must be true or false")
			return
		}
		query.Aggregate = v
	}
	result, err := h.service.Get(r.Context(), claims.UserID, claims.TenantID, claims.DBName, query)
	if err != nil {
		sendServerError(w, r, err, "Failed to retrieve dashboard trends", "DASHBOARD_TRENDS_FAILED")
		return
	}
	sendJSONResponse(w, http.StatusOK, models.APIResponse{Success: true, Message: "Dashboard trends retrieved successfully", Data: result})
}

// csvParam splits a comma-separated list, validating each entry; at most 50.
func csvParam(raw string, re *regexp.Regexp) ([]string, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, true
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 50 {
		return nil, false
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !re.MatchString(p) {
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}
