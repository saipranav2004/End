package genome

import (
	"context"
	"database/sql"
	"fmt"
	"nhi-backend-api/internal/scope"
	"time"
)

// IdentityListRepository reads the per-identity monitoring facts of
// GET /api/v1/identities (backend_api_contract.md §5.1 / §10.2) from the
// caller's tenant schema: baselines, identity_risk_scores and alerts. Every
// method is one grouped query over the whole key set — never one query per
// identity (§5.1 query plan). The discovery rows themselves come from
// DashboardSummaryRepository.LatestIdentities and are normalized in the
// service; the ClickHouse last_seen reader (LastSeenByIdentity) is at the
// end of this file.
type IdentityListRepository struct{}

// NewIdentityListRepository creates the repository.
func NewIdentityListRepository() *IdentityListRepository {
	return &IdentityListRepository{}
}

// BaselineSummaries returns the baselines row (status, built_days,
// required_days — §10.2 baseline.*) of every key that has one.
func (r *IdentityListRepository) BaselineSummaries(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (map[MonitoringKey]BaselineSummaryRow, error) {
	out := map[MonitoringKey]BaselineSummaryRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT b.source_type, b.identity_name, b.status, b.built_days, b.required_days
	      FROM ` + resolved.Table("baselines") + ` b` + keyJoin("b")
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, wrapMonitoringErr("baseline summaries", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row BaselineSummaryRow
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &row.Status, &row.BuiltDays, &row.RequiredDays); err != nil {
			return nil, wrapMonitoringErr("baseline summaries", err)
		}
		out[row.Key] = row
	}
	return out, wrapMonitoringErr("baseline summaries", rows.Err())
}

// LatestRiskSnapshots returns the newest completed identity_risk_scores
// snapshot per key (§10.2 risk.*).
func (r *IdentityListRepository) LatestRiskSnapshots(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (map[MonitoringKey]IdentityRiskSnapshotRow, error) {
	out := map[MonitoringKey]IdentityRiskSnapshotRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT DISTINCT ON (s.source_type, s.identity_name)
	             s.source_type, s.identity_name, s.risk_score, s.severity, s.score_status, s.window_end
	      FROM ` + resolved.Table("identity_risk_scores") + ` s` + keyJoin("s") + `
	      WHERE s.window_end <= now()
	      ORDER BY s.source_type, s.identity_name, s.window_end DESC, s.computed_at DESC`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, wrapMonitoringErr("latest risk snapshots", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row IdentityRiskSnapshotRow
		var score sql.NullInt64
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &score, &row.Severity, &row.ScoreStatus, &row.WindowEnd); err != nil {
			return nil, wrapMonitoringErr("latest risk snapshots", err)
		}
		if score.Valid {
			n := int(score.Int64)
			row.RiskScore = &n
		}
		row.WindowEnd = row.WindowEnd.UTC()
		out[row.Key] = row
	}
	return out, wrapMonitoringErr("latest risk snapshots", rows.Err())
}

// OpenAlertCounts is §10.2 open_anomaly_count for every key at once:
// COUNT(*) from alerts where status = 'open', grouped by identity. Keys
// without open alerts are absent from the map.
func (r *IdentityListRepository) OpenAlertCounts(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (map[MonitoringKey]int, error) {
	out := map[MonitoringKey]int{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT a.source_type, a.identity_name, COUNT(*) FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + `
	      WHERE a.status = 'open'
	      GROUP BY a.source_type, a.identity_name`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, wrapMonitoringErr("open alert counts", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k MonitoringKey
		var n int
		if err := rows.Scan(&k.SourceType, &k.IdentityName, &n); err != nil {
			return nil, wrapMonitoringErr("open alert counts", err)
		}
		out[k] = n
	}
	return out, wrapMonitoringErr("open alert counts", rows.Err())
}

// ---------------------------------------------------------------------------
// ClickHouse log_events (backend_api_contract.md §5.1 / §10.2 last_seen):
// one grouped query for the identities about to be returned — never the
// event history (§5.1 query plan).
// ---------------------------------------------------------------------------

// LastSeenByIdentity is §10.2 last_seen for several identities at once:
// per key, MAX(event_time) over the latest row per event_id, resolved
// events only (is_resolved = 1), across all stored history. Keys without a
// resolved event are absent from the map.
func (e *MonitoringEvents) LastSeenByIdentity(ctx context.Context, keys []MonitoringKey) (map[MonitoringKey]time.Time, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	out := map[MonitoringKey]time.Time{}
	if len(keys) == 0 {
		return out, nil
	}
	q := `SELECT source_type, identity_name, max(event_time)
	      FROM (
	          SELECT source_type, identity_name, event_id,
	                 argMax(event_time, ingested_at)  AS event_time,
	                 argMax(is_resolved, ingested_at) AS is_resolved
	          FROM ` + e.table + `
	          WHERE` + keyFilterSQL + `
	          GROUP BY source_type, identity_name, event_id
	      )
	      WHERE is_resolved = 1
	      GROUP BY source_type, identity_name`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, keyFilterArgs(keys)...)
	if err != nil {
		return nil, fmt.Errorf("log_events last seen by identity: %w", err)
	}
	defer rows.Close()
	want := keySet(keys)
	for rows.Next() {
		var (
			k    MonitoringKey
			last time.Time
		)
		if err := rows.Scan(&k.SourceType, &k.IdentityName, &last); err != nil {
			return nil, fmt.Errorf("log_events last seen by identity: %w", err)
		}
		if _, ok := want[k]; ok {
			out[k] = last.UTC()
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_events last seen by identity: %w", err)
	}
	return out, nil
}
