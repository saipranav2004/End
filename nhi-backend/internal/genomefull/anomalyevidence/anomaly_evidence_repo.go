package anomalyevidence

import (
	"context"
	"database/sql"
	"errors"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository reads the PostgreSQL side of the anomaly evidence endpoint:
// the alerts row and its alert_feedback row (schema guide §10). The event
// itself comes from ClickHouse via common.MonitoringEvents.EventsByID.
// Read-only.
type Repository struct{}

// NewRepository creates the repository.
func NewRepository() *Repository { return &Repository{} }

// Alert returns the alerts row with the given primary key, or nil.
func (r *Repository) Alert(ctx context.Context, resolved *scope.DataScope, alertID int64) (*common.AlertRow, error) {
	return common.AlertByID(ctx, resolved, alertID)
}

// Feedback returns the alert_feedback row of (accountID, sourceType,
// eventID), or nil when the event has not been reviewed.
func (r *Repository) Feedback(ctx context.Context, resolved *scope.DataScope, accountID, sourceType, eventID string) (*common.FeedbackRow, error) {
	q := `SELECT ` + common.FeedbackColumns + ` FROM ` + resolved.Table("alert_feedback") + ` f WHERE f.account_id = $1 AND f.source_type = $2 AND f.event_id = $3`
	row, err := common.ScanFeedback(resolved.DB.QueryRowContext(ctx, q, accountID, sourceType, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "alert feedback", err)
	}
	return &row, nil
}
