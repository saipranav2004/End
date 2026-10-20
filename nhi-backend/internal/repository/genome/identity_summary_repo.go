package genome

import (
	"context"
	"database/sql"
	"fmt"
	"nhi-backend-api/internal/scope"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// IdentitySummaryRepository reads what
// GET /api/v1/genome/identities/{source_type}/{identity_name} needs from the
// caller's tenant schema (backend_api_contract.md §5.2 / §10.2):
//
//   - baselines  baseline_summary (status, built_days, required_days)
//   - alerts     headline_metrics.open_anomalies
//
// The identity itself comes from DashboardSummaryRepository.LatestIdentities
// (the same latest-snapshot rule as the dashboard). Like that repository,
// every method takes the resolved *scope.DataScope: the pool and the
// schema-qualified table names come from it (resolved.Table), never from
// configuration or constants. Monitoring tables are looked up by the
// normalized keys the service passes — identity key first, then the shared
// workload key (the contract's fallback). Nothing is written.
type IdentitySummaryRepository struct{}

// NewIdentitySummaryRepository creates the repository.
func NewIdentitySummaryRepository() *IdentitySummaryRepository {
	return &IdentitySummaryRepository{}
}

// BaselineSummaryRow is §5.2 baseline_summary (schema guide §7).
type BaselineSummaryRow struct {
	Key          MonitoringKey // key the row is stored under (identity key or shared workload key)
	Status       string
	BuiltDays    int
	RequiredDays int
}

// BaselineSummary returns the baselines row of the first key, in the order
// given, that has one; nil, nil when none has.
func (r *IdentitySummaryRepository) BaselineSummary(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (*BaselineSummaryRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT b.source_type, b.identity_name, b.status, b.built_days, b.required_days
	      FROM ` + resolved.Table("baselines") + ` b` + fmt.Sprintf(keyPreferenceJoinSQL, "b") + `
	      ORDER BY k.preference
	      LIMIT 1`
	var row BaselineSummaryRow
	err := resolved.DB.QueryRowContext(ctx, q, types, names).Scan(&row.Key.SourceType, &row.Key.IdentityName, &row.Status, &row.BuiltDays, &row.RequiredDays)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, wrapMonitoringErr("baseline summary", err)
	}
	return &row, nil
}

// OpenAlertCount is §10.2 open_anomaly_count / §5.2 headline_metrics
// .open_anomalies: COUNT(*) from alerts where the identity matches (any of
// the keys) and status = 'open' — no time window.
func (r *IdentitySummaryRepository) OpenAlertCount(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT COUNT(*) FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + `
	      WHERE a.status = 'open'`
	var n int
	if err := resolved.DB.QueryRowContext(ctx, q, types, names).Scan(&n); err != nil {
		return 0, wrapMonitoringErr("open alert count", err)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// ClickHouse log_events (backend_api_contract.md §5.2 / §10.2 headline_metrics)
// ---------------------------------------------------------------------------

// LastSeen is §10.2 last_seen: MAX(event_time) over the latest row per
// event_id of the identity, resolved events only (is_resolved = 1), across
// all stored history. nil when the identity has no resolved event.
func (e *MonitoringEvents) LastSeen(ctx context.Context, sourceType, identityName string) (*time.Time, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT maxOrNull(event_time)
	      FROM (
	          SELECT event_id,
	                 argMax(event_time, ingested_at)  AS event_time,
	                 argMax(is_resolved, ingested_at) AS is_resolved
	          FROM ` + e.table + `
	          WHERE source_type = {source_type:String} AND identity_name = {identity_name:String}
	          GROUP BY event_id
	      )
	      WHERE is_resolved = 1`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	var last *time.Time
	if err := e.conn.QueryRow(ctx, q, ch.Named("source_type", sourceType), ch.Named("identity_name", identityName)).Scan(&last); err != nil {
		return nil, fmt.Errorf("log_events last seen: %w", err)
	}
	if last == nil {
		return nil, nil
	}
	t := last.UTC()
	return &t, nil
}
