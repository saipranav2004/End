package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"nhi-backend-api/internal/models"
)

var noQueryAllowed = map[string]struct{}{}

func checkUnknown(q url.Values, allowed map[string]struct{}) (string, bool) {
	for k := range q {
		if _, ok := allowed[k]; !ok {
			return k, true
		}
	}
	return "", false
}

func rejectUnknownQuery(w http.ResponseWriter, r *http.Request, allowed map[string]struct{}) bool {
	if unknown, ok := checkUnknown(r.URL.Query(), allowed); ok {
		msg := "(none)"
		if len(allowed) > 0 {
			// not used for auth endpoints, but keep generic
			msg = "no query parameters allowed"
		}
		sendJSON(w, http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("Unknown query parameter: %s. %s", unknown, msg),
			Error:   "INVALID_QUERY_PARAM",
		})
		return true
	}
	return false
}
