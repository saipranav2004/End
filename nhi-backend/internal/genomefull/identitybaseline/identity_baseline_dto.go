// Package identitybaseline serves
// GET /api/v1/genome/identities/{source_type}/{identity_name}/baseline: the
// baseline provenance panel.
package identitybaseline

import "time"

// Response is the data block. Every baselines-backed field is null while
// the identity (and its workload key) has no baselines row — the
// legitimate "still learning / not yet trained" state.
type Response struct {
	Status        *string    `json:"status"`          // baselines.status
	RequiredDays  *int       `json:"required_days"`   // baselines.required_days
	BuiltDays     *int       `json:"built_days"`      // baselines.built_days
	DaysRemaining *int       `json:"days_remaining"`  // baselines.days_remaining
	FirstSeenDate *string    `json:"first_seen_date"` // baselines.first_seen_date as YYYY-MM-DD
	ActiveSince   *time.Time `json:"active_since"`    // baselines.active_since
	UpdatedAt     *time.Time `json:"updated_at"`      // baselines.updated_at
	DataPoints    *int64     `json:"data_points"`     // SUM(daily_stats.event_count) over the baseline window

	// Confidence is read from the latest identity_drift_scores row; it stays
	// null with the flag false when no model-confidence value is available.
	ModelConfidencePercent   *float64 `json:"model_confidence_percent"`
	ModelConfidenceAvailable bool     `json:"model_confidence_available"`

	Dimensions []DimensionSummary `json:"dimensions"` // safe summaries of baselines.profile_json
	Versions   []Version          `json:"versions"`   // baseline_versions, newest first
}

// DimensionSummary is the safe summary of one dimension model of a
// baseline profile (never the raw profile or its internal counters): the
// dimension name, the model's `ready` flag when the profile stores one,
// and the number of distinct values the model holds when it is stored as a
// value → count map.
type DimensionSummary struct {
	Dimension  string `json:"dimension"`
	Ready      *bool  `json:"ready"`
	ValueCount *int   `json:"value_count"`
}

// Version is one baseline_versions row.
type Version struct {
	Version     int64          `json:"version"`
	PublishedAt time.Time      `json:"published_at"`
	Profile     VersionProfile `json:"profile"` // safe selected fields of profile_json
}

// VersionProfile is the safe selection of a versioned profile: the scalar
// `_meta` fields (version/training metadata) and the same dimension
// summaries as the current baseline. Meta is null when the profile has no
// `_meta` object.
type VersionProfile struct {
	Meta       map[string]any     `json:"meta"`
	Dimensions []DimensionSummary `json:"dimensions"`
}
