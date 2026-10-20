package genome

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/database"
	models "nhi-backend-api/internal/models/genome"
	svc "nhi-backend-api/internal/services/genome"
)

// Shared pieces of the /identities/{source_type}/{identity_name}[/…]
// handlers (backend_api_contract.md §5.2–§5.7): path parsing, the §2
// from/to window and the error mapping into the §1.5 envelope.

// Routes (internal/routes/routes.go): /identities/{source_type}/{identity_name:.+}
// and its /activity, /genome, /baseline, /timeline sub-resources. The
// identity_name segment may contain ':' and (URL-encoded) '/' because it is
// an ARN, hence the `.+` pattern; the sub-resource routes are registered
// before the plain identity route.

// identityDefaultWindow is the §4.1 chart default (last 14 days), the only
// default window the contract defines; it is used when from/to are omitted.
const identityDefaultWindow = 14 * 24 * time.Hour

// identityPath returns the normalized {source_type} (compact upper-case
// family, §2) and the decoded {identity_name} of the request.
func identityPath(r *http.Request) (sourceType, identityName string, err error) {
	vars := mux.Vars(r)
	sourceType = strings.ToUpper(strings.TrimSpace(vars["source_type"]))
	identityName = strings.TrimSpace(vars["identity_name"])
	if strings.Contains(identityName, "%") { // routers that keep the escaped path
		if decoded, uerr := url.PathUnescape(identityName); uerr == nil {
			identityName = decoded
		}
	}
	if sourceType == "" || identityName == "" {
		return "", "", fmt.Errorf("source_type and identity_name are required")
	}
	return sourceType, identityName, nil
}

// parseIdentityWindow applies §2 from (inclusive) / to (exclusive); when
// omitted, to = now and from = to − 14 days.
func parseIdentityWindow(q url.Values, now time.Time) (svc.TimeWindow, error) {
	win := svc.TimeWindow{To: now}
	var err error
	if s := strings.TrimSpace(q.Get("to")); s != "" {
		if win.To, err = parseSummaryTime(s); err != nil {
			return win, fmt.Errorf("to: %w", err)
		}
	}
	win.From = win.To.Add(-identityDefaultWindow)
	if s := strings.TrimSpace(q.Get("from")); s != "" {
		if win.From, err = parseSummaryTime(s); err != nil {
			return win, fmt.Errorf("from: %w", err)
		}
	}
	if !win.From.Before(win.To) {
		return win, fmt.Errorf("from must be before to")
	}
	return win, nil
}

// writeIdentityServiceError maps service failures of the identity endpoints:
// unknown identity → 404 IDENTITY_NOT_FOUND, store failure → 503
// STORE_UNAVAILABLE, anything else → 500 INTERNAL_ERROR.
func writeIdentityServiceError(w http.ResponseWriter, endpoint, rid string, err error) {
	var unavailable *svc.StoreUnavailableError
	switch {
	case errors.Is(err, svc.ErrIdentityNotFound):
		writeSummaryError(w, http.StatusNotFound, models.MonitoringCodeIdentityNotFound, "The requested identity was not found", rid)
	case errors.As(err, &unavailable):
		log.Printf("%s rid=%s: %v", endpoint, rid, err)
		writeUnavailable(w, unavailable, rid)
	default:
		log.Printf("%s rid=%s: unexpected error: %v", endpoint, rid, err)
		writeSummaryError(w, http.StatusInternalServerError, models.MonitoringCodeInternal, "unexpected error", rid)
	}
}

// writeUnavailable answers 503: TENANT_NOT_PROVISIONED when the tenant schema
// lacks a table (the same code as every other API), else STORE_UNAVAILABLE.
func writeUnavailable(w http.ResponseWriter, unavailable *svc.StoreUnavailableError, rid string) {
	if database.IsMissingTable(unavailable) {
		writeSummaryError(w, http.StatusServiceUnavailable, database.NotProvisionedCode, database.NotProvisionedMessage, rid)
		return
	}
	writeSummaryError(w, http.StatusServiceUnavailable, models.MonitoringCodeStoreUnavailable, unavailable.Op+" failed", rid)
}
