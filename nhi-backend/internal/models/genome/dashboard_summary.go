package genome

import "time"

// Response shapes of GET /api/v1/dashboard/summary — exactly the fields of
// backend_api_contract.md §4.1 / §10.1 in the contract envelope of §1.5
// ({data, meta} on success, {error} on failure).

// MonitoringEnvelope is the success body (§1.5).
type MonitoringEnvelope struct {
	Data any            `json:"data"`
	Meta MonitoringMeta `json:"meta"`
}

// MonitoringMeta is the §1.5 meta block.
type MonitoringMeta struct {
	RequestID   string    `json:"request_id"`
	GeneratedAt time.Time `json:"generated_at"`
	NextCursor  *string   `json:"next_cursor"`
}

// MonitoringError is the §1.5 error body.
type MonitoringError struct {
	Error MonitoringErrorDetail `json:"error"`
}

// MonitoringErrorDetail describes one failure.
type MonitoringErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// Error codes, in the contract's UPPER_SNAKE style (e.g. IDENTITY_NOT_FOUND).
const (
	MonitoringCodeValidation       = "VALIDATION_ERROR"
	MonitoringCodeUnauthorized     = "UNAUTHORIZED"
	MonitoringCodeInternal         = "INTERNAL_ERROR"
	MonitoringCodeStoreUnavailable = "STORE_UNAVAILABLE"
)

// DashboardSummaryResponse is the data block of §4.1.
type DashboardSummaryResponse struct {
	Cards                   DashboardSummaryCards        `json:"cards"`
	DeparturesByDimension   []DashboardDimensionCount    `json:"departures_by_dimension"`
	AnomaliesByDay          []DashboardDayCount          `json:"anomalies_by_day"`
	MostAnomalousIdentities []DashboardAnomalousIdentity `json:"most_anomalous_identities"`
	RecentlyBaselined       []DashboardRecentlyBaselined `json:"recently_baselined"`
}

// DashboardSummaryCards are the §4.1 cards. A pointer is null when the
// value could not be read from its store (§10.1 sources) or, for
// critical_anomalies / mean_provenance_confidence, until ClickHouse
// log_events is connected. drifting_baselines is null with
// drifting_baselines_available=false as §4.1 prescribes.
type DashboardSummaryCards struct {
	BaselinesEstablished       *int     `json:"baselines_established"`
	IdentitiesWatched          *int     `json:"identities_watched"`
	StillLearning              *int     `json:"still_learning"`
	OpenAnomalies              *int     `json:"open_anomalies"`
	CriticalAnomalies          *int     `json:"critical_anomalies"`
	BaselineCoveragePercent    *float64 `json:"baseline_coverage_percent"`
	MeanProvenanceConfidence   *float64 `json:"mean_provenance_confidence"`
	MeanConfidencePercent      *float64 `json:"mean_confidence_percent"`
	DriftingBaselines          *int     `json:"drifting_baselines"`
	DriftingBaselinesAvailable bool     `json:"drifting_baselines_available"`
	WindowHours                int      `json:"window_hours"`
}

// DashboardIdentityRef is the identity block of §10.2 embedded in rows:
// id = source_type + ":" + identity_name (API formula), display_name from
// the normalizer.
type DashboardIdentityRef struct {
	ID           string `json:"id"`
	SourceType   string `json:"source_type"`
	IdentityName string `json:"identity_name"`
	DisplayName  string `json:"display_name"`
}

// DashboardDimensionCount is one departures_by_dimension row: the exact
// alerts.dimension value and COUNT(*) (§10.1).
type DashboardDimensionCount struct {
	Dimension string `json:"dimension"`
	Count     int    `json:"count"`
}

// DashboardDayCount is one anomalies_by_day row: DATE_TRUNC('day',
// alerts.created_at) as YYYY-MM-DD (UTC) and COUNT(*) (§10.1).
type DashboardDayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// DashboardAnomalousIdentity is one most_anomalous_identities row: the
// latest identity_risk_scores columns of the §4.1 query joined to
// identities for display_name, account_id, region and workload metadata.
type DashboardAnomalousIdentity struct {
	DashboardIdentityRef
	AccountID           string    `json:"account_id"`
	Region              string    `json:"region"`
	ServiceName         string    `json:"service_name"`
	WorkloadKey         string    `json:"workload_key"`
	RiskScore           *int      `json:"risk_score"`
	Severity            string    `json:"severity"`
	ScoreStatus         string    `json:"score_status"`
	EventCount          int       `json:"event_count"`
	AnomalousEventCount int       `json:"anomalous_event_count"`
	WindowStart         time.Time `json:"window_start"`
	WindowEnd           time.Time `json:"window_end"`
	ComputedAt          time.Time `json:"computed_at"`
}

// DashboardRecentlyBaselined is one recently_baselined row:
// baselines.active_since or baseline_versions.published_at joined to
// identities (§10.1).
type DashboardRecentlyBaselined struct {
	DashboardIdentityRef
	AccountID  string    `json:"account_id"`
	Region     string    `json:"region"`
	Event      string    `json:"event"` // baseline_established | baseline_version_published
	OccurredAt time.Time `json:"occurred_at"`
	Version    *int64    `json:"version"`
}
