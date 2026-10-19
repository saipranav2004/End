package postures

// GET /checks/{rule_id} — the details behind the Remediate button.

type CheckDetail struct {
	RuleID            string   `json:"rule_id"`
	Title             string   `json:"title"`
	Severity          string   `json:"severity"`
	PenaltyPoints     int      `json:"penalty_points"`
	Pillar            string   `json:"pillar"`
	Fix               string   `json:"fix"`
	Remediation       string   `json:"remediation"`
	Compliance        []string `json:"compliance"`
	IdentitiesFailing int      `json:"identities_failing"`
}
