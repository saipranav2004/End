// Package postures holds the response models and request filters of the ISPM
// posture dashboard API (see ISPM_Dashboard_API_v2.md). Every endpoint of that
// API answers with the Envelope below instead of the generic models.APIResponse.
package postures

import (
	"strings"
	"time"
)

// Caller is the authenticated user, copied from the JWT claims. The tenant
// always comes from here, never from a request parameter.
type Caller struct {
	UserID   string
	TenantID string
	DBName   string
	Role     string
}

// CanWrite reports whether the caller may change findings (analyst / admin).
func (c Caller) CanWrite() bool {
	switch strings.ToLower(strings.TrimSpace(c.Role)) {
	case "analyst", "admin":
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────
// Envelope: { "data": …, "page": {…}, "meta": {…} }
// ─────────────────────────────────────────────────────────────────────────

type Envelope struct {
	Data any   `json:"data"`
	Page *Page `json:"page,omitempty"`
	Meta Meta  `json:"meta"`
}

// Page is the keyset-pagination block. NextCursor is null on the last page.
type Page struct {
	NextCursor *string `json:"next_cursor"`
	Limit      int     `json:"limit"`
}

// Meta describes the scan the answer was computed from. DataAvailable is
// false when the source data does not exist yet (the data is then empty).
type Meta struct {
	ScanID        string     `json:"scan_id"`
	ComputedAt    *time.Time `json:"computed_at"`
	Window        string     `json:"window,omitempty"`
	DataAvailable bool       `json:"data_available"`
}

// ErrorEnvelope is the error body: { "error": { "code", "message", "field" } }.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────
// Time window
// ─────────────────────────────────────────────────────────────────────────

// Window is one of 24h | 3d | 7d | 30d | 60d | 90d.
//
// Short windows (24h, 3d, 7d) are drawn from the per-scan tables and compare
// with the daily row of 1 / 3 / 7 days ago; long windows (30d, 60d, 90d) are
// drawn from the daily tables.
type Window struct {
	Label        string        // "30d"
	Duration     time.Duration // how far back the series reaches
	Days         int           // whole days back, used for the daily comparison
	PerScan      bool          // true: per-scan source tables; false: daily tables
	ChangeColumn string        // ispm_identity_posture.score_change_<N>d
	ChangeSuffix string        // "1d", "3d", …
}

// DefaultWindow is used when ?window= is absent.
const DefaultWindow = "30d"

var windows = map[string]Window{
	"24h": {Label: "24h", Duration: 24 * time.Hour, Days: 1, PerScan: true, ChangeColumn: "score_change_1d", ChangeSuffix: "1d"},
	"3d":  {Label: "3d", Duration: 3 * 24 * time.Hour, Days: 3, PerScan: true, ChangeColumn: "score_change_3d", ChangeSuffix: "3d"},
	"7d":  {Label: "7d", Duration: 7 * 24 * time.Hour, Days: 7, PerScan: true, ChangeColumn: "score_change_7d", ChangeSuffix: "7d"},
	"30d": {Label: "30d", Duration: 30 * 24 * time.Hour, Days: 30, PerScan: false, ChangeColumn: "score_change_30d", ChangeSuffix: "30d"},
	"60d": {Label: "60d", Duration: 60 * 24 * time.Hour, Days: 60, PerScan: false, ChangeColumn: "score_change_60d", ChangeSuffix: "60d"},
	"90d": {Label: "90d", Duration: 90 * 24 * time.Hour, Days: 90, PerScan: false, ChangeColumn: "score_change_90d", ChangeSuffix: "90d"},
}

// WindowLabels lists the accepted values, for error messages.
var WindowLabels = []string{"24h", "3d", "7d", "30d", "60d", "90d"}

// ParseWindow resolves ?window=. An empty value means the 30d default.
func ParseWindow(raw string) (Window, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		raw = DefaultWindow
	}
	w, ok := windows[raw]
	return w, ok
}

// ─────────────────────────────────────────────────────────────────────────
// Shared value types
// ─────────────────────────────────────────────────────────────────────────

type AccountRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Change is "how much did the score move over the window". Value is null when
// there is not enough history yet.
type Change struct {
	Window string   `json:"window"`
	Value  *float64 `json:"value"`
}

// FailedCounts is the failed-check breakdown by severity.
type FailedCounts struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

// PillarScore is one pillar bar (fleet or identity level).
type PillarScore struct {
	Pillar    string   `json:"pillar"`
	Name      string   `json:"name"`
	Score     *float64 `json:"score"`
	Evaluated int      `json:"evaluated"`
	Failing   int      `json:"failing"`
	Status    string   `json:"status"`
}

// Bands and severities accepted by the filters.
var (
	Bands      = []string{"HEALTHY", "FAIR", "POOR", "CRITICAL"}
	Severities = []string{"Critical", "High", "Medium", "Low"}
)
