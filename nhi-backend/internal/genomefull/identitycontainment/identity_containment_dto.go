// Package identitycontainment serves
// GET /api/v1/genome/identities/{source_type}/{identity_name}/containment.
// The identity is resolved first (unknown → 404); the containment state
// itself is unavailable because the current pipeline persists no
// containment actions.
package identitycontainment

// Response is the data block.
type Response struct {
	Available    bool `json:"available"`
	Active       bool `json:"active"`
	LatestAction any  `json:"latest_action"` // always null
}
