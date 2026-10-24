package anomalydetail

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository reads the single alerts row behind the anomaly detail
// endpoint from the caller's tenant schema. The event-level columns come
// from ClickHouse via common.MonitoringEvents.EventsByID. Read-only.
type Repository struct{}

// NewRepository creates the repository.
func NewRepository() *Repository { return &Repository{} }

// ByID returns the alerts row with the given primary key, or nil when
// there is none in this tenant.
func (r *Repository) ByID(ctx context.Context, resolved *scope.DataScope, alertID int64) (*common.AlertRow, error) {
	return common.AlertByID(ctx, resolved, alertID)
}
