package genome

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/genome"
)

// Helpers shared by the NHI Genome handlers: request parsing and the
// backend_api_contract.md §1.5 envelope.

func parseSummaryTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("must be an ISO-8601 timestamp such as 2026-10-01T00:00:00Z")
}

// ---- envelope helpers -------------------------------------------------------

var summaryRequestIDRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// summaryRequestID echoes a well-formed client X-Request-ID or mints one.
func summaryRequestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); summaryRequestIDRE.MatchString(id) {
		return id
	}
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_unavailable"
	}
	return "req_" + hex.EncodeToString(b[:])
}

func writeSummaryJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeSummaryError(w http.ResponseWriter, status int, code, msg, rid string) {
	writeSummaryJSON(w, status, models.MonitoringError{Error: models.MonitoringErrorDetail{Code: code, Message: msg, RequestID: rid}})
}

// ---- query-parameter helpers ------------------------------------------------

// checkUnknown reports the first query parameter that is not in allowed.
func checkUnknown(q url.Values, allowed map[string]struct{}) (string, bool) {
	for k := range q {
		if _, ok := allowed[k]; !ok {
			return k, true
		}
	}
	return "", false
}

// allowedList renders the allowed parameter names for error messages.
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
