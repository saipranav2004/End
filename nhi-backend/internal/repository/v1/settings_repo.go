package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
)

var ErrInvalidTenantSettings = errors.New("settings selection is not valid for this tenant")

type SettingsRepository struct{}

func NewSettingsRepository() *SettingsRepository { return &SettingsRepository{} }

// UpdateSettings validates every selection against the tenant's own database
// and performs one atomic upsert. The caller must pass the authenticated email
// from the JWT as userID.
func (r *SettingsRepository) UpdateSettings(ctx context.Context, tenantDB *sql.DB, schema, tenantID, userID string, req models.UpdateSettingsRequest) (*models.UpdateSettingsResponse, error) {
	if tenantDB == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(userID) == "" {
		return nil, errors.New("tenant database, tenant_id and user_id are required")
	}
	if req.SelectedAccountIDs == nil {
		req.SelectedAccountIDs = []string{}
	}
	if req.SelectedRegions == nil {
		req.SelectedRegions = []string{}
	}
	if err := validateSelection(ctx, tenantDB, schema, tenantID, req.SelectedAccountIDs, "account_id"); err != nil {
		return nil, err
	}
	if err := validateSelection(ctx, tenantDB, schema, tenantID, req.SelectedRegions, "region"); err != nil {
		return nil, err
	}
	accountJSON, err := json.Marshal(req.SelectedAccountIDs)
	if err != nil {
		return nil, fmt.Errorf("encode selected_account_ids: %w", err)
	}
	regionJSON, err := json.Marshal(req.SelectedRegions)
	if err != nil {
		return nil, fmt.Errorf("encode selected_regions: %w", err)
	}
	query := fmt.Sprintf(`
		INSERT INTO %s.user_settings
			(user_id, selected_account_ids, selected_regions, updated_at)
		VALUES ($1, $2::jsonb, $3::jsonb, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			selected_account_ids = EXCLUDED.selected_account_ids,
			selected_regions = EXCLUDED.selected_regions,
			updated_at = NOW()`, schema)
	if _, err := tenantDB.ExecContext(ctx, query, strings.TrimSpace(userID), accountJSON, regionJSON); err != nil {
		return nil, fmt.Errorf("upsert user_settings: %w", err)
	}
	return &models.UpdateSettingsResponse{
		Success: true,
		Settings: models.UpdateSettingsEcho{
			SelectedAccountIDs: req.SelectedAccountIDs,
			SelectedRegions:    req.SelectedRegions,
		},
	}, nil
}

func validateSelection(ctx context.Context, db *sql.DB, schema, tenantID string, values []string, column string) error {
	if len(values) == 0 {
		return nil
	}
	// column is selected from this closed set, never from client input.
	if column != "account_id" && column != "region" {
		return errors.New("unsupported settings selection")
	}
	// accounts.account_id is a scalar; accounts.regions is a text[] per account.
	q := fmt.Sprintf(`SELECT COUNT(DISTINCT account_id) FROM %s.accounts
		WHERE tenant_id=$1::uuid AND account_id = ANY($2::text[])`, schema)
	if column == "region" {
		q = fmt.Sprintf(`SELECT COUNT(DISTINCT r) FROM %s.accounts a, unnest(a.regions) AS r
			WHERE a.tenant_id=$1::uuid AND r = ANY($2::text[])`, schema)
	}
	var count int
	if err := db.QueryRowContext(ctx, q, tenantID, pq.Array(values)).Scan(&count); err != nil {
		return fmt.Errorf("validate selected %s: %w", column, err)
	}
	if count != len(values) {
		return ErrInvalidTenantSettings
	}
	return nil
}
