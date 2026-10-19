package v1

// ─────────────────────────────────────────────────────────────────────────
// GET /api/v1/permission-source?credential_id=&event_source=&event_name=
// Which granted permission (and so which policy) allowed a CloudTrail event.
// Looked up on demand for one event, never for the whole events list.
// ─────────────────────────────────────────────────────────────────────────

// PermissionSourceResponse names the policy behind one event. When no granted
// permission matches (for example the policy was removed since it was last discovered)
// Matched is false, the policy fields are null and Note says why.
type PermissionSourceResponse struct {
	Matched       bool    `json:"matched"`
	PolicySource  *string `json:"policy_source"`
	PolicyType    *string `json:"policy_type"`
	Effect        *string `json:"effect"`
	ResourceARN   *string `json:"resource_arn"`
	MatchedAction *string `json:"matched_action"`
	Note          *string `json:"note"`
}
