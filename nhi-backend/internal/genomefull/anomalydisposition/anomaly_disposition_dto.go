// Package anomalydisposition serves
// POST /api/v1/genome/anomalies/{alert_id}/disposition. The current
// pipeline keeps no disposition history table, so the endpoint only
// reports that the feature is unavailable; it never changes alerts.status
// or alert_feedback.
package anomalydisposition

// Response is the data block.
type Response struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}
