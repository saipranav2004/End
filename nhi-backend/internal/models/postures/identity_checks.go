package postures

import "time"

// GET /identities/{id}/checks?status=failed|passed|all&pillar=

// Values of ?status= on the checks tab.
const (
	ChecksFailed = "failed"
	ChecksPassed = "passed"
	ChecksAll    = "all"
)

type IdentityChecksFilter struct {
	Status string
	Pillar string
}

type IdentityCheck struct {
	RuleID        string     `json:"rule_id"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	Severity      string     `json:"severity"`
	Pillar        string     `json:"pillar"`
	PointsIfFixed float64    `json:"points_if_fixed"`
	Detail        *string    `json:"detail"`
	FailingSince  *time.Time `json:"failing_since"`
	Compliance    []string   `json:"compliance"`
	FindingID     *string    `json:"finding_id"`
	// FindingUpdatedAt is when the linked finding last changed.
	FindingUpdatedAt *time.Time `json:"finding_updated_at"`
}
