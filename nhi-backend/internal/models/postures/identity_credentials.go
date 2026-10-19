package postures

// GET /identities/{id}/credentials — the Credentials tab.
//
// Access keys carry usage fields; roles carry privilege fields. Fields that do
// not apply to a credential are left out.

type IdentityCredential struct {
	CredentialARN      string `json:"credential_arn"`
	Type               string `json:"type"`
	Binding            string `json:"binding,omitempty"`
	Status             string `json:"status,omitempty"`
	AgeDays            *int   `json:"age_days,omitempty"`
	LastUsedDays       *int   `json:"last_used_days,omitempty"`
	UsageState         string `json:"usage_state,omitempty"`
	Privileged         *bool  `json:"privileged,omitempty"`
	HasBoundary        *bool  `json:"has_boundary,omitempty"`
	MaxSessionDuration *int   `json:"max_session_duration,omitempty"`
	OpenFindings       int    `json:"open_findings"`
}
