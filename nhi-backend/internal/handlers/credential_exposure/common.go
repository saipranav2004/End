package credential_exposure

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"nhi-backend-api/internal/logging"
	"sort"
	"strings"

	"nhi-backend-api/internal/database"
	"nhi-backend-api/internal/middleware"
	svc "nhi-backend-api/internal/services/credential_exposure"
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

// writeAPIError is writeError with the arguments the other way round.
func writeAPIError(w http.ResponseWriter, status int, message, code string) {
	writeError(w, status, code, message)
}

// writeInvalidOr answers 400 with the message of a client mistake, else hides
// the failure behind a logged 500 (503 TENANT_NOT_PROVISIONED when the tenant
// schema lacks the table).
func writeInvalidOr(w http.ResponseWriter, r *http.Request, err error, invalidCode, failMessage string) {
	var invalid *svc.InvalidInputError
	if errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, invalidCode, invalid.Msg)
		return
	}
	writeServerError(w, r, err, failMessage)
}

func writeServerError(w http.ResponseWriter, r *http.Request, err error, message string) {
	logging.FromContext(r.Context()).Error("credential exposure: "+message, "err", err)
	if database.IsMissingTable(err) {
		writeError(w, http.StatusServiceUnavailable, database.NotProvisionedCode, database.NotProvisionedMessage)
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", message)
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
