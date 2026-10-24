package dashboardactivity

import (
	"context"
	"time"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository reads the PostgreSQL side of the dashboard activity endpoint:
// the anomalies-per-day series from the tenant's alerts table. The
// ClickHouse call-volume series is read directly from
// common.MonitoringEvents.FleetVolume. Read-only.
type Repository struct{}

// NewRepository creates the repository.
func NewRepository() *Repository { return &Repository{} }

// DayCountRow is one alerts-per-UTC-day row.
type DayCountRow struct {
	Day   time.Time
	Count int
}

// AnomaliesByDay counts alerts (all statuses) created per UTC day inside
// [from, to) for the given identity keys.
func (r *Repository) AnomaliesByDay(ctx context.Context, resolved *scope.DataScope, keys []common.MonitoringKey, from, to time.Time) ([]DayCountRow, error) {
	out := []DayCountRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := common.KeyArrays(keys)
	q := `SELECT date_trunc('day', a.created_at AT TIME ZONE 'UTC') AS day, COUNT(*)
	      FROM ` + resolved.Table("alerts") + ` a` + common.KeyJoin("a") + `
	      WHERE a.created_at >= $3 AND a.created_at < $4
	      GROUP BY 1 ORDER BY 1`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names, from, to)
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "dashboard activity anomalies", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row DayCountRow
		if err := rows.Scan(&row.Day, &row.Count); err != nil {
			return nil, common.WrapMonitoringErr(ctx, "dashboard activity anomalies", err)
		}
		row.Day = time.Date(row.Day.Year(), row.Day.Month(), row.Day.Day(), 0, 0, 0, 0, time.UTC)
		out = append(out, row)
	}
	return out, common.WrapMonitoringErr(ctx, "dashboard activity anomalies", rows.Err())
}
