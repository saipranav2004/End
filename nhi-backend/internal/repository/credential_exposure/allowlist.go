package credential_exposure

import (
	"context"
	"fmt"
	"time"

	models "nhi-backend-api/internal/models/credential_exposure"
	"nhi-backend-api/internal/scope"
)

type AllowlistRepository struct{}

func NewAllowlistRepository() *AllowlistRepository { return &AllowlistRepository{} }

func (r *AllowlistRepository) List(ctx context.Context, resolved *scope.DataScope, clientID string) ([]models.AllowlistEntry, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	schema := resolved.QSchema()
	query := `SELECT client_id,file_path,detector,redacted,reason,dismissed_by,dismissed_at FROM ` + schema + `.secscan_allowlist_dashboard`
	args := []any{}
	if clientID != "" {
		query += ` WHERE client_id=$1`
		args = append(args, clientID)
	}
	query += ` ORDER BY ` + scannerTimeOrder("dismissed_at") + `,client_id,file_path,detector`
	rows, err := resolved.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query dashboard allowlist: %w", err)
	}
	defer rows.Close()
	out := make([]models.AllowlistEntry, 0)
	for rows.Next() {
		var item models.AllowlistEntry
		if err := rows.Scan(&item.ClientID, &item.FilePath, &item.Detector, &item.Redacted, &item.Reason, &item.DismissedBy, &item.DismissedAt); err != nil {
			return nil, fmt.Errorf("scan dashboard allowlist: %w", err)
		}
		item.DismissedAt = scannerTime(item.DismissedAt)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard allowlist: %w", err)
	}
	return out, nil
}

func (r *AllowlistRepository) Upsert(ctx context.Context, resolved *scope.DataScope, req models.AllowlistRequest) (*models.AllowlistEntry, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	schema := resolved.QSchema()
	by := req.DismissedBy
	if by == "" {
		by = "dashboard user"
	}
	now := dashboardTimestamp(time.Now())
	query := `INSERT INTO ` + schema + `.secscan_allowlist_dashboard (client_id,file_path,detector,redacted,reason,dismissed_by,dismissed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (client_id,file_path,detector,redacted) DO UPDATE SET reason=EXCLUDED.reason,dismissed_by=EXCLUDED.dismissed_by,dismissed_at=EXCLUDED.dismissed_at
RETURNING client_id,file_path,detector,redacted,reason,dismissed_by,dismissed_at`
	var out models.AllowlistEntry
	err := resolved.DB.QueryRowContext(ctx, query, req.ClientID, req.FilePath, req.Detector, req.Redacted, req.Reason, by, now).Scan(&out.ClientID, &out.FilePath, &out.Detector, &out.Redacted, &out.Reason, &out.DismissedBy, &out.DismissedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert dashboard allowlist: %w", err)
	}
	return &out, nil
}

func (r *AllowlistRepository) Delete(ctx context.Context, resolved *scope.DataScope, req models.AllowlistRequest) (bool, error) {
	if resolved == nil || resolved.DB == nil {
		return false, errScopeRequired
	}
	schema := resolved.QSchema()
	res, err := resolved.DB.ExecContext(ctx, `DELETE FROM `+schema+`.secscan_allowlist_dashboard WHERE client_id=$1 AND file_path=$2 AND detector=$3 AND redacted=$4`, req.ClientID, req.FilePath, req.Detector, req.Redacted)
	if err != nil {
		return false, fmt.Errorf("delete dashboard allowlist entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count deleted allowlist rows: %w", err)
	}
	return n > 0, nil
}
