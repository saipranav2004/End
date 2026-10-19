package postures

// GET /identities/{id} — the page header.

type IdentityHeader struct {
	IdentityID      string         `json:"identity_id"`
	Name            string         `json:"name"`
	ARN             string         `json:"arn"`
	Type            string         `json:"type"`
	FunctionalClass *string        `json:"functional_class"`
	Account         AccountRef     `json:"account"`
	Region          string         `json:"region"`
	Score           *float64       `json:"score"`
	Band            string         `json:"band"`
	Grade           string         `json:"grade"`
	Badges          IdentityBadges `json:"badges"`
	Counts          IdentityCounts `json:"counts"`
}

// IdentityBadges are null when the data was not collected; the UI then hides
// the badge.
type IdentityBadges struct {
	MFAEnabled    *bool `json:"mfa_enabled"`
	ConsoleAccess *bool `json:"console_access"`
}

type IdentityCounts struct {
	Credentials int `json:"credentials"`
	OpenAlerts  int `json:"open_alerts"`
}
