package postures

import "time"

// GET /posture/quick-wins/{rule_id}/identities?limit=&cursor=

type QuickWinIdentities struct {
	Rule       QuickWinRule       `json:"rule"`
	Identities []QuickWinIdentity `json:"identities"`
}

type QuickWinRule struct {
	RuleID        string `json:"rule_id"`
	Title         string `json:"title"`
	Severity      string `json:"severity"`
	PenaltyPoints int    `json:"penalty_points"`
}

type QuickWinIdentity struct {
	IdentityID    string     `json:"identity_id"`
	Name          string     `json:"name"`
	Account       string     `json:"account"`
	Score         *float64   `json:"score"`
	Band          string     `json:"band"`
	PointsIfFixed float64    `json:"points_if_fixed"`
	FailingSince  *time.Time `json:"failing_since"`
}
