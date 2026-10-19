package v1

// ─────────────────────────────────────────────────────────────────────────
// GET/PUT /api/v1/settings ("ENDPOINT 1A: Get Settings" /
// "ENDPOINT 1B: Update Settings")
//
// Backing tables: user_settings (user_id, selected_account_ids,
// selected_regions, updated_at), accounts (tenant_id, account_id, regions[],
// is_default).
//
// Multi-tenant note: user_settings is provisioned separately in every
// tenant data database by migrations/tenant/001_user_settings.sql. The
// repository does not create schema objects at request time; apply the
// migration before serving traffic.
// ─────────────────────────────────────────────────────────────────────────

type SettingsResponse struct {
	Settings SettingsBlock `json:"settings"`
	Source   string        `json:"source"` // "user_settings" | "accounts"
}

type SettingsBlock struct {
	Accounts SettingsAccounts `json:"accounts"`
	Regions  SettingsRegions  `json:"regions"`
}

type SettingsAccounts struct {
	Selected  []string `json:"selected"`
	IsDefault bool     `json:"is_default"`
}

type SettingsRegions struct {
	Selected  []string `json:"selected"`
	IsDefault bool     `json:"is_default"`
}

// UpdateSettingsRequest is the PUT /api/v1/settings request body.
type UpdateSettingsRequest struct {
	SelectedAccountIDs []string `json:"selected_account_ids"`
	SelectedRegions    []string `json:"selected_regions"`
}

type UpdateSettingsResponse struct {
	Success  bool               `json:"success"`
	Settings UpdateSettingsEcho `json:"settings"`
}

type UpdateSettingsEcho struct {
	SelectedAccountIDs []string `json:"selected_account_ids"`
	SelectedRegions    []string `json:"selected_regions"`
}
