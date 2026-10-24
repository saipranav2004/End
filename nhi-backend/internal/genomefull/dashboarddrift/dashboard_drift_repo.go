package dashboarddrift

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

const maxDriftWindowsPerSource = 30

// Repository reads the pipeline-owned fleet drift rollups from the tenant
// schema. It never creates or modifies drift tables.
type Repository struct{}

// NewRepository creates the read-only repository.
func NewRepository() *Repository { return &Repository{} }

// LatestBySource returns the newest snapshot for each enabled source family.
// The dashboard summary uses these current values for its cards.
func (r *Repository) LatestBySource(ctx context.Context, resolved *scope.DataScope) ([]Snapshot, error) {
	sourceTypes := common.SupportedSourceTypes()
	if len(sourceTypes) == 0 {
		return []Snapshot{}, nil
	}
	q := `SELECT DISTINCT ON (source_type)
	             source_type, window_end, stale_count, monitor_count, healthy_count,
	             insufficient_count, data_quality_count, mean_confidence, mean_drift,
	             mean_identity_risk, fleet_risk, tests_run, expected_false_positives
	      FROM ` + resolved.Table("drift_fleet_snapshots") + `
	      WHERE source_type = ANY($1::text[]) AND window_end <= now()
	      ORDER BY source_type, window_end DESC`
	rows, err := resolved.DB.QueryContext(ctx, q, pq.Array(sourceTypes))
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "latest fleet drift snapshots", err)
	}
	defer rows.Close()
	return scanSnapshots(ctx, rows, "latest fleet drift snapshots", len(sourceTypes))
}

// RecentBySource returns a bounded history for the dashboard drift charts:
// the newest 30 windows per currently enabled source family.
func (r *Repository) RecentBySource(ctx context.Context, resolved *scope.DataScope) ([]Snapshot, error) {
	sourceTypes := common.SupportedSourceTypes()
	if len(sourceTypes) == 0 {
		return []Snapshot{}, nil
	}
	q := `WITH ranked AS (
	          SELECT source_type, window_end, stale_count, monitor_count, healthy_count,
	                 insufficient_count, data_quality_count, mean_confidence, mean_drift,
	                 mean_identity_risk, fleet_risk, tests_run, expected_false_positives,
	                 ROW_NUMBER() OVER (PARTITION BY source_type ORDER BY window_end DESC) AS row_rank
	          FROM ` + resolved.Table("drift_fleet_snapshots") + `
	          WHERE source_type = ANY($1::text[]) AND window_end <= now()
	      )
	      SELECT source_type, window_end, stale_count, monitor_count, healthy_count,
	             insufficient_count, data_quality_count, mean_confidence, mean_drift,
	             mean_identity_risk, fleet_risk, tests_run, expected_false_positives
	      FROM ranked WHERE row_rank <= $2
	      ORDER BY source_type, window_end`
	rows, err := resolved.DB.QueryContext(ctx, q, pq.Array(sourceTypes), maxDriftWindowsPerSource)
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "recent fleet drift snapshots", err)
	}
	defer rows.Close()
	return scanSnapshots(ctx, rows, "recent fleet drift snapshots", len(sourceTypes)*maxDriftWindowsPerSource)
}

func scanSnapshots(ctx context.Context, rows *sql.Rows, op string, capacity int) ([]Snapshot, error) {
	out := make([]Snapshot, 0, capacity)
	for rows.Next() {
		var (
			row              Snapshot
			stale            sql.NullInt64
			monitor          sql.NullInt64
			healthy          sql.NullInt64
			insufficient     sql.NullInt64
			dataQuality      sql.NullInt64
			meanConfidence   sql.NullFloat64
			meanDrift        sql.NullFloat64
			meanIdentityRisk sql.NullFloat64
			fleetRisk        sql.NullFloat64
			testsRun         sql.NullInt64
			falsePositives   sql.NullFloat64
		)
		if err := rows.Scan(
			&row.SourceType, &row.WindowEnd, &stale, &monitor, &healthy,
			&insufficient, &dataQuality, &meanConfidence, &meanDrift,
			&meanIdentityRisk, &fleetRisk, &testsRun, &falsePositives,
		); err != nil {
			return nil, common.WrapMonitoringErr(ctx, op, err)
		}
		row.WindowEnd = row.WindowEnd.UTC()
		row.StaleCount = intPointer(stale)
		row.MonitorCount = intPointer(monitor)
		row.HealthyCount = intPointer(healthy)
		row.InsufficientCount = intPointer(insufficient)
		row.DataQualityCount = intPointer(dataQuality)
		row.MeanConfidence = floatPointer(meanConfidence)
		row.MeanDrift = floatPointer(meanDrift)
		row.MeanIdentityRisk = floatPointer(meanIdentityRisk)
		row.FleetRisk = floatPointer(fleetRisk)
		row.TestsRun = intPointer(testsRun)
		row.ExpectedFalsePositives = floatPointer(falsePositives)
		out = append(out, row)
	}
	return out, common.WrapMonitoringErr(ctx, op, rows.Err())
}

func intPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	n := int(value.Int64)
	return &n
}

func floatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	n := value.Float64
	return &n
}
