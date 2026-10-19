package genome

import "time"

// Response shapes of GET /api/v1/identities/{source_type}/{identity_name}/baseline
// — exactly backend_api_contract.md §5.5 / §10.3, in the §1.5 envelope
// (MonitoringEnvelope).

// IdentityBaselineResponse is the data block of §5.5. Every baselines-backed
// field is null while the identity (and its workload key) has no baselines
// row — the legitimate "still learning / not yet trained" state.
type IdentityBaselineResponse struct {
	Status        *string    `json:"status"`          // baselines.status
	RequiredDays  *int       `json:"required_days"`   // baselines.required_days
	BuiltDays     *int       `json:"built_days"`      // baselines.built_days
	DaysRemaining *int       `json:"days_remaining"`  // baselines.days_remaining
	FirstSeenDate *string    `json:"first_seen_date"` // baselines.first_seen_date as YYYY-MM-DD
	ActiveSince   *time.Time `json:"active_since"`    // baselines.active_since
	UpdatedAt     *time.Time `json:"updated_at"`      // baselines.updated_at
	DataPoints    *int64     `json:"data_points"`     // SUM(daily_stats.event_count) over the baseline window

	// No model-confidence field exists in the baseline tables (§10.3
	// NEEDS TO BE ADDED): the percentage is always null and the flag false.
	ModelConfidencePercent   *float64 `json:"model_confidence_percent"`
	ModelConfidenceAvailable bool     `json:"model_confidence_available"`

	Dimensions []BaselineDimensionSummary `json:"dimensions"` // safe summaries of baselines.profile_json
	Versions   []BaselineVersion          `json:"versions"`   // baseline_versions, newest first
}

// BaselineDimensionSummary is the safe summary of one dimension model of a
// baseline profile (§5.5: never the raw profile or its internal counters):
// the dimension name, the model's `ready` flag (schema guide §7 activation
// rule) when the profile stores one, and the number of distinct values the
// model holds when it is stored as a value → count map.
type BaselineDimensionSummary struct {
	Dimension  string `json:"dimension"`
	Ready      *bool  `json:"ready"`
	ValueCount *int   `json:"value_count"`
}

// BaselineVersion is one baseline_versions row.
type BaselineVersion struct {
	Version     int64                  `json:"version"`
	PublishedAt time.Time              `json:"published_at"`
	Profile     BaselineVersionProfile `json:"profile"` // safe selected fields of profile_json
}

// BaselineVersionProfile is the safe selection of a versioned profile: the
// scalar `_meta` fields (version/training metadata) and the same dimension
// summaries as the current baseline. meta is null when the profile has no
// `_meta` object.
type BaselineVersionProfile struct {
	Meta       map[string]any             `json:"meta"`
	Dimensions []BaselineDimensionSummary `json:"dimensions"`
}
