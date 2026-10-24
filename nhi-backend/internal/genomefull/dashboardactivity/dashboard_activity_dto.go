// Package dashboardactivity serves GET /api/v1/genome/dashboard/activity:
// fleet-wide call volume per UTC day (ClickHouse log_events) and anomalies
// per UTC day (PostgreSQL alerts), both over the half-open window
// [from, to); days without data are simply absent.
package dashboardactivity

import "time"

// Response is the data block of the dashboard activity endpoint.
type Response struct {
	CallVolume []CallVolumePoint `json:"call_volume"`
	Anomalies  []AnomalyPoint    `json:"anomalies"`
}

// CallVolumePoint is one toStartOfDay(event_time) bucket with its
// uniqExact(event_id) count.
type CallVolumePoint struct {
	Bucket time.Time `json:"bucket"`
	Calls  uint64    `json:"calls"`
}

// AnomalyPoint is one UTC day (YYYY-MM-DD) with its alerts count.
type AnomalyPoint struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}
