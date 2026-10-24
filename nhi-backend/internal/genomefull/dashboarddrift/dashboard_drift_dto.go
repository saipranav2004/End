// Package dashboarddrift serves the v3 fleet drift endpoint.
package dashboarddrift

import "time"

// Response is the data block for the fleet drift endpoint. Snapshots contains
// a bounded history of source-family rows, ordered by source and window.
type Response struct {
	DataAvailable bool       `json:"data_available"`
	Snapshots     []Snapshot `json:"snapshots"`
}

// Snapshot is one source-family row from drift_fleet_snapshots.
type Snapshot struct {
	SourceType             string    `json:"source_type"`
	WindowEnd              time.Time `json:"window_end"`
	StaleCount             *int      `json:"stale_count"`
	MonitorCount           *int      `json:"monitor_count"`
	HealthyCount           *int      `json:"healthy_count"`
	InsufficientCount      *int      `json:"insufficient_count"`
	DataQualityCount       *int      `json:"data_quality_count"`
	MeanConfidence         *float64  `json:"mean_confidence"`
	MeanDrift              *float64  `json:"mean_drift"`
	MeanIdentityRisk       *float64  `json:"mean_identity_risk"`
	FleetRisk              *float64  `json:"fleet_risk"`
	TestsRun               *int      `json:"tests_run"`
	ExpectedFalsePositives *float64  `json:"expected_false_positives"`
}
