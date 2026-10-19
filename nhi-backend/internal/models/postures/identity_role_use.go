package postures

import "time"

// GET /identities/{id}/role-use — the Role use tab.

type IdentityRoleUse struct {
	Assumable bool          `json:"assumable"`
	AssumedBy []RoleUseItem `json:"assumed_by"`
	Assumes   []RoleUseItem `json:"assumes"`
}

type RoleUseItem struct {
	RoleName        string     `json:"role_name"`
	RoleARN         string     `json:"role_arn"`
	FunctionalClass *string    `json:"functional_class"`
	Sessions        int64      `json:"sessions"`
	FirstAssumedAt  *time.Time `json:"first_assumed_at"`
	LastAssumedAt   *time.Time `json:"last_assumed_at"`
}
