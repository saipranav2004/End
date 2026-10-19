package genome

import "time"

// Response shapes of
// GET /api/v1/identities/{source_type}/{identity_name}/timeline
// (backend_api_contract.md §5.6 / §10.3 "timeline[]") in the §1.5 envelope.
//
// The timeline is derived from the current tables (there is no immutable
// event-history table — contract §5.6 SCHEMA GAP): alerts,
// baseline_versions and baselines.

// Timeline item types (API constants chosen by source table, §10.3).
const (
	TimelineTypeAnomaly                  = "anomaly"                    // alerts.created_at
	TimelineTypeBaselineVersionPublished = "baseline_version_published" // baseline_versions.published_at
	TimelineTypeLearningStarted          = "learning_started"           // baselines.first_seen_date
	TimelineTypeBaselineEstablished      = "baseline_established"       // baselines.active_since
)

// IdentityTimelineResponse is the data block of §5.6.
type IdentityTimelineResponse struct {
	Items []IdentityTimelineItem `json:"items"`
}

// IdentityTimelineItem is one §5.6 entry, newest first. description is
// omitted for the milestone types that carry none; status is only present
// on anomaly items (the stored alerts.status).
type IdentityTimelineItem struct {
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Description *string   `json:"description,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
	Status      *string   `json:"status,omitempty"`
}
