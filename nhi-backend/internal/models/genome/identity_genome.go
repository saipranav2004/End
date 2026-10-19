package genome

// Response shapes of GET /api/v1/identities/{source_type}/{identity_name}/genome
// — exactly backend_api_contract.md §5.4 / §10.3, in the §1.5 envelope
// (MonitoringEnvelope). Every number is computed by ClickHouse from
// log_events; all four lists are empty until the identity has events in the
// window (or while the monitoring ClickHouse is unavailable).

// IdentityGenomeResponse is the data block of §5.4.
type IdentityGenomeResponse struct {
	Fingerprint      []IdentityFingerprintPoint `json:"fingerprint"`
	TypicalActions   []IdentityTypicalAction    `json:"typical_actions"`
	TypicalResources []IdentityTypicalResource  `json:"typical_resources"`
	ActivitySchedule []IdentityScheduleCell     `json:"activity_schedule"`
}

// IdentityFingerprintPoint is one (dimension, value) of the behavioural
// fingerprint: dimension/value are a key/value of log_events.dimensions_json,
// share_percent = distinct events carrying the value / all calls × 100.
type IdentityFingerprintPoint struct {
	Dimension    string  `json:"dimension"`
	Value        string  `json:"value"`
	SharePercent float64 `json:"share_percent"`
}

// IdentityTypicalAction is one typical_actions row: event_source + ':' +
// event_name, its distinct-event count and share of all calls.
type IdentityTypicalAction struct {
	APIOperation string  `json:"api_operation"`
	Calls        uint64  `json:"calls"`
	SharePercent float64 `json:"share_percent"`
}

// IdentityTypicalResource is one typical_resources row: one ARN of the
// split log_events.resource_arns, the distinct events that touched it and
// the dimensions_json.resource_type seen with it (null when the key is
// missing — NEEDS CONFIRMATION per §10.3).
type IdentityTypicalResource struct {
	Resource     string  `json:"resource"`
	ResourceType *string `json:"resource_type"`
	Calls        uint64  `json:"calls"`
}

// IdentityScheduleCell is one activity_schedule cell: ClickHouse
// toDayOfWeek (1 = Monday … 7 = Sunday) × toHour (0–23, UTC).
type IdentityScheduleCell struct {
	DayOfWeek int    `json:"day_of_week"`
	Hour      int    `json:"hour"`
	Calls     uint64 `json:"calls"`
}
