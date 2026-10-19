package postures

import "time"

// GET /identities/{id}/posture?window=30d — the Posture tab.

type IdentityPosture struct {
	Score            *float64          `json:"score"`
	Band             string            `json:"band"`
	Grade            string            `json:"grade"`
	PointsLost       float64           `json:"points_lost"`
	ChecksFailed     int               `json:"checks_failed"`
	ChecksEvaluated  int               `json:"checks_evaluated"`
	Change           Change            `json:"change"`
	AfterEveryFix    AfterEveryFix     `json:"after_every_fix"`
	OpenAlerts       int               `json:"open_alerts"`
	WhyThisScore     string            `json:"why_this_score"`
	Pillars          []PillarScore     `json:"pillars"`
	ScoreImpact      []ScoreImpactItem `json:"score_impact"`
	PeerGroup        *PeerGroup        `json:"peer_group"`
	BoundCredentials []BoundCredential `json:"bound_credentials"`
}

type AfterEveryFix struct {
	Score float64 `json:"score"`
	Delta float64 `json:"delta"`
}

type ScoreImpactItem struct {
	RuleID string  `json:"rule_id"`
	Fix    string  `json:"fix"`
	Points float64 `json:"points"`
}

type PeerGroup struct {
	FunctionalClass string  `json:"functional_class"`
	Size            int     `json:"size"`
	Average         float64 `json:"average"`
	Best            float64 `json:"best"`
	Rank            int     `json:"rank"`
	Standing        string  `json:"standing"`
}

type BoundCredential struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	AgeDays      *int   `json:"age_days"`
	LastUsedDays *int   `json:"last_used_days"`
	Status       string `json:"status"`
}

// FailingCheckRow is a failing check as read for the score-impact list.
type FailingCheckRow struct {
	RuleID       string
	Fix          string
	Points       float64
	FailingSince *time.Time
}
