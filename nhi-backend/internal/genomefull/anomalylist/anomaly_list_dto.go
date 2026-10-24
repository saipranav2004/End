// Package anomalylist serves GET /api/v1/genome/anomalies: one item per
// PostgreSQL alerts row, newest first, with the event-level columns of
// ClickHouse log_events joined in by event_id.
package anomalylist

import (
	"time"

	"nhi-backend-api/internal/genomefull/common"
)

// Response is the data block.
type Response struct {
	Items []Item `json:"items"`
}

// Item is one list row.
//
//   - id, dimension, state (= alerts.status) and detected_at (= created_at)
//     come from the alerts row;
//   - what_changed is the alerts observed_value/dimension/reason rendered
//     as text;
//   - provenance_confidence, severity and region come from the log_events
//     row of alerts.event_id and are null when that row is not readable.
type Item struct {
	ID                   int64                     `json:"id"`
	Identity             common.AnomalyIdentityRef `json:"identity"`
	WhatChanged          string                    `json:"what_changed"`
	Dimension            string                    `json:"dimension"`
	ProvenanceConfidence *int                      `json:"provenance_confidence"`
	Severity             *string                   `json:"severity"`
	State                string                    `json:"state"`
	DetectedAt           time.Time                 `json:"detected_at"`
	Region               *string                   `json:"region"`
}
