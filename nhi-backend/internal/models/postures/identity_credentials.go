package postures

import "encoding/json"

// GET /identities/{id}/credentials — the Credentials tab.
//
// Access keys carry usage fields; roles carry privilege fields. Every field
// that applies to the credential's kind is always written (null when unknown),
// so the tab never loses a column; fields of the other kind are left out.

type IdentityCredential struct {
	CredentialARN      string
	Type               string
	Binding            string
	Status             string
	AgeDays            *int
	LastUsedDays       *int
	UsageState         string
	Privileged         *bool
	HasBoundary        *bool
	MaxSessionDuration *int
	OpenFindings       int

	// IsRole: an IAM role, so the privilege fields apply; otherwise usage fields.
	IsRole bool
}

func (c IdentityCredential) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"credential_arn": c.CredentialARN,
		"type":           c.Type,
		"binding":        c.Binding,
		"open_findings":  c.OpenFindings,
	}
	if c.IsRole {
		out["privileged"] = c.Privileged
		out["has_boundary"] = c.HasBoundary
		out["max_session_duration"] = c.MaxSessionDuration
	} else {
		out["status"] = nilIfEmpty(c.Status)
		out["age_days"] = c.AgeDays
		out["last_used_days"] = c.LastUsedDays
		out["usage_state"] = nilIfEmpty(c.UsageState)
	}
	return json.Marshal(out)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
