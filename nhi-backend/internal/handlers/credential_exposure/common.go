package credential_exposure

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/models"
)

func requireScannerAdmin(w http.ResponseWriter, r *http.Request) bool {
	claims := middleware.GetClaims(r)
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(claims.Role), "admin") {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Credential Exposure APIs require an admin role")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeAPIError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, models.APIResponse{Success: false, Message: message, Error: code})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON with supported fields: "+err.Error())
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must contain exactly one JSON object")
		return false
	}
	return true
}

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

func rejectUnknownQuery(w http.ResponseWriter, r *http.Request, allowed map[string]struct{}) bool {
	if unknown, ok := checkUnknown(r.URL.Query(), allowed); ok {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("Unknown query parameter: %s. Allowed parameters: %s", unknown, allowedList(allowed)), "INVALID_QUERY_PARAM")
		return true
	}
	return false
}

func isValidDate(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	layouts := []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05"}
	for _, l := range layouts {
		if _, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return true
		}
	}
	return false
}
