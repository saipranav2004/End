package postures

import "time"

// GET /identities/{id}/alerts?window=30d&status=open|closed|all&limit=&cursor=

// Values of ?status= on the alerts tab.
const (
	AlertsOpen   = "open"
	AlertsClosed = "closed"
	AlertsAll    = "all"
)

type IdentityAlertsFilter struct {
	Window Window
	Status string
	Limit  int
	Cursor string
}

// Alert sources.
const (
	AlertSourceIdentity   = "identity"
	AlertSourceCredential = "credential"
)

type IdentityAlert struct {
	FindingID  string     `json:"finding_id"`
	RuleID     string     `json:"rule_id"`
	Title      string     `json:"title"`
	Severity   string     `json:"severity"`
	RiskScore  *float64   `json:"risk_score"`
	Status     string     `json:"status"`
	RaisedAt   *time.Time `json:"raised_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Source     string     `json:"source"`
	// UpdatedAt is when the finding last changed.
	UpdatedAt *time.Time `json:"updated_at"`
}
