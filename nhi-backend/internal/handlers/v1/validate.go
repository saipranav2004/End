package v1

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"nhi-backend-api/internal/models"
)

// allowed query params per endpoint — strict validation so typos like
// credential_tye are not silently ignored.

var (
	identitiesListAllowed = map[string]struct{}{
		"page": {}, "page_size": {}, "search": {},
		"identity_type": {}, "functional_class": {}, "environment": {},
		"status": {}, "owner": {}, "vpc_id": {},
		"is_publicly_accessible": {}, "min_idle_days": {},
		"sort_by": {}, "sort_order": {},
	}

	credentialsListAllowed = map[string]struct{}{
		"page": {}, "page_size": {}, "search": {},
		"credential_type": {}, "status": {}, "owner_principal_arn": {},
		"rotation_enabled": {}, "rotation_overdue": {},
		"is_orphaned": {}, "has_external_trust": {},
		"min_idle_days": {}, "max_days_until_expiry": {},
		"sort_by": {}, "sort_order": {},
	}

	identityDetailAllowed = map[string]struct{}{
		"tab": {}, "time_range": {}, "start_date": {}, "end_date": {},
		"service": {}, "used_only": {}, "depth": {}, "edge_type": {},
		"page": {}, "page_size": {},
	}

	credentialDetailAllowed = map[string]struct{}{
		"tab": {}, "time_range": {}, "start_date": {}, "end_date": {},
		"depth": {}, "edge_type": {}, "page": {}, "page_size": {},
	}

	noQueryAllowed = map[string]struct{}{}

	// sort_by enums — must match repository maps
	identitySortColumnsAllowed = map[string]struct{}{
		"name": {}, "arn": {}, "identity_type": {}, "functional_class": {},
		"account_id": {}, "region": {}, "environment": {}, "owner": {},
		"status": {}, "last_activity_at": {}, "created_at_aws": {},
		"age_days": {}, "idle_days": {}, "bound_credentials_count": {},
		"permission_utilization_pct": {},
	}

	credentialSortColumnsAllowed = map[string]struct{}{
		"name": {}, "arn": {}, "credential_type": {}, "account_id": {},
		"region": {}, "status": {}, "last_used_at": {}, "last_rotated_at": {},
		"expires_at": {}, "age_days": {}, "idle_days": {},
		"days_until_expiry": {}, "bound_identities_count": {},
	}

	// value enums — strict so credential_type=w errors
	validEdgeTypes = map[string]struct{}{
		"assume_role": {}, "bound_to": {}, "accesses": {}, "trusts": {},
		"contains": {}, "manages": {}, "attached_to": {}, "uses": {},
		// Stored in trust_edges today, and the CloudTrail-derived created_by.
		"service_trust": {}, "cross_account": {}, "federated_oidc": {}, "federated_saml": {}, "pass_role": {},
		"created_by": {},
	}

	validIdentityTabs = map[string]struct{}{
		"overview": {}, "permissions": {}, "activity": {}, "relationships": {},
		"credentials": {}, "raw": {}, "changes": {},
	}

	validCredentialTabs = map[string]struct{}{
		"overview": {}, "trust": {}, "activity": {}, "rotation": {},
		"relationships": {}, "raw": {}, "changes": {},
	}
)

// checkUnknown returns the first unknown param name, if any.
func checkUnknown(q url.Values, allowed map[string]struct{}) (string, bool) {
	for k := range q {
		if _, ok := allowed[k]; !ok {
			return k, true
		}
	}
	return "", false
}

func allowedList(allowed map[string]struct{}) string {
	if len(allowed) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(allowed))
	for k := range allowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// rejectUnknownQuery checks r.URL.Query() against allowed and writes 400 if unknown found.
// Returns true if request was rejected (caller should return).
func rejectUnknownQuery(w http.ResponseWriter, r *http.Request, allowed map[string]struct{}) bool {
	if unknown, ok := checkUnknown(r.URL.Query(), allowed); ok {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("Unknown query parameter: %s. Allowed parameters: %s", unknown, allowedList(allowed)),
			Error:   "INVALID_QUERY_PARAM",
		})
		return true
	}
	return false
}

// enum validation helpers

var (
	validSortOrders = map[string]struct{}{"asc": {}, "desc": {}}
)

func isValidSortOrder(s string) bool {
	if s == "" {
		return true
	}
	_, ok := validSortOrders[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

func isValidDate(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	layouts := []string{
		"2006-01-02",
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
	}
	for _, l := range layouts {
		if _, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return true
		}
	}
	return false
}

func validateSingleEnum(value string, allowed map[string]struct{}) (bool, string) {
	low := strings.ToLower(strings.TrimSpace(value))
	if low == "" {
		return true, ""
	}
	if _, ok := allowed[low]; !ok {
		return false, value
	}
	return true, ""
}

// validateEnumList checks a comma-separated list (e.g. ?edge_type=a,b), case
// insensitively. It returns the first value that is not allowed.
func validateEnumList(value string, allowed map[string]struct{}) (bool, string) {
	for _, item := range strings.Split(value, ",") {
		if ok, bad := validateSingleEnum(item, allowed); !ok {
			return false, strings.TrimSpace(bad)
		}
	}
	return true, ""
}

// Activity-tab paging bounds: up to 100 events per page and at most 1000
// events (page * page_size) reachable per request.
const (
	maxActivityPageSize  = 100
	maxActivityEventsEnd = 1000
)

// parseActivityPaging reads ?page= and ?page_size= for the activity tabs. It
// writes the 400 itself and returns ok=false when a value is invalid.
func parseActivityPaging(w http.ResponseWriter, q url.Values) (page, size int, ok bool) {
	page, size = 1, 50
	bad := func(msg string) (int, int, bool) {
		sendJSONResponse(w, http.StatusBadRequest, models.APIResponse{Success: false, Message: msg, Error: "INVALID_PAGINATION"})
		return 0, 0, false
	}
	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return bad("page must be a positive integer")
		}
		page = v
	}
	if raw := strings.TrimSpace(q.Get("page_size")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > maxActivityPageSize {
			return bad("page_size must be an integer between 1 and 100")
		}
		size = v
	}
	if page*size > maxActivityEventsEnd {
		return bad("page * page_size must not exceed 1000 events; narrow the time range or filter instead")
	}
	return page, size, true
}

var typeTokenRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9 _.:-]{0,63}$`)

// validateTokens accepts filter values such as "login_profile" or a "prod-eu"
// tag (any case); it returns the first value that is not one. The list
// filters are not enums: discovery keeps adding types, classes and statuses,
// and every value only ever reaches a bound parameter.
func validateTokens(vals []string) (string, bool) {
	for _, v := range vals {
		if !typeTokenRegex.MatchString(strings.ToLower(v)) {
			return v, false
		}
	}
	return "", true
}
