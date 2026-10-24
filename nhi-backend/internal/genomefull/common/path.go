package common

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Shared pieces of the /identities/{source_type}/{identity_name}[/…]
// handlers: path parsing and the §2 from/to window. Error mapping is
// WriteServiceError in handler_support.go.
//
// Routes (internal/modules/genome/module.go): /identities/{source_type}/{identity_name:.+}
// and its /activity, /genome, /baseline, /timeline, /peers sub-resources. The
// identity_name segment may contain ':' and (URL-encoded) '/' because it is
// an ARN, hence the `.+` pattern; the sub-resource routes are registered
// before the plain identity route.

// IdentityDefaultWindow is the §4.1 chart default (last 14 days), the only
// default window the contract defines; it is used when from/to are omitted.
const IdentityDefaultWindow = 14 * 24 * time.Hour

// IdentityPath returns the normalized {source_type} (compact upper-case
// family) and the decoded {identity_name} of the request.
func IdentityPath(r *http.Request) (sourceType, identityName string, err error) {
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
	if err := CheckSourceType(sourceType); err != nil {
		return "", "", err
	}
	if err := CheckLength("identity_name", identityName, maxIdentityNameLength); err != nil {
		return "", "", err
	}
	return sourceType, identityName, nil
}

// ParseIdentityWindow applies §2 from (inclusive) / to (exclusive); when
// omitted, to = now and from = to − 14 days.
func ParseIdentityWindow(q url.Values, now time.Time) (TimeWindow, error) {
	win := TimeWindow{To: now}
	var err error
	if s := strings.TrimSpace(q.Get("to")); s != "" {
		if win.To, err = ParseTime(s); err != nil {
			return win, fmt.Errorf("to: %w", err)
		}
	}
	win.From = win.To.Add(-IdentityDefaultWindow)
	if s := strings.TrimSpace(q.Get("from")); s != "" {
		if win.From, err = ParseTime(s); err != nil {
			return win, fmt.Errorf("from: %w", err)
		}
	}
	if !win.From.Before(win.To) {
		return win, fmt.Errorf("from must be before to")
	}
	return win, nil
}

// AlertIDPath reads the {alert_id} path variable: the alerts primary key.
func AlertIDPath(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(mux.Vars(r)["alert_id"])
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("alert_id must be a positive integer")
	}
	return id, nil
}
