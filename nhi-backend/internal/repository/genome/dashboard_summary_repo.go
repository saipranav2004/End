package genome

import (
	"context"
	"database/sql"
	"nhi-backend-api/internal/scope"
	"time"

	"github.com/lib/pq"
)

// DashboardSummaryRepository reads what GET /api/v1/dashboard/summary needs
// from the caller's tenant schema (backend_api_contract.md §4.1):
//
//   - identities           discovery rows (read-only)
//   - baselines            + baseline_versions
//   - identity_risk_scores
//   - alerts
//
// Every entry point takes the resolved *scope.DataScope: the pool and the
// schema-qualified table names come from it (resolved.Table), never from
// configuration or constants. Monitoring tables are keyed by the normalized
// (source_type, identity_name) pair, which the service computes from the
// discovery row — so the readers take a list of MonitoringKey values
// instead of joining the raw discovery columns in SQL. Nothing is written.
type DashboardSummaryRepository struct{}

// NewDashboardSummaryRepository creates the repository.
func NewDashboardSummaryRepository() *DashboardSummaryRepository {
	return &DashboardSummaryRepository{}
}

// ---------------------------------------------------------------------------
// identities
// ---------------------------------------------------------------------------

// DiscoveryIdentityRow is one latest discovery snapshot (raw, not normalized).
type DiscoveryIdentityRow struct {
	ID             string
	ARN            string
	Name           string
	IdentityType   string
	AccountID      string
	Region         string
	Status         string
	TypeAttributes []byte
	LastSeenAt     *time.Time
}

// LatestIdentities returns the latest non-deleted discovery snapshot per
// (identity_type, arn, account_id) inside the scope's accounts/regions —
// the contract's step 1. Unlike the inventory endpoints it is not pinned to
// one scan_id: the monitoring pipeline keys its tables by stable resource,
// not by scan.
func (r *DashboardSummaryRepository) LatestIdentities(ctx context.Context, resolved *scope.DataScope) ([]DiscoveryIdentityRow, error) {
	q := `WITH ranked AS (
	SELECT id::text AS id, arn, name, identity_type, account_id, COALESCE(region, '') AS region, status, type_attributes, last_seen_at,
	       ROW_NUMBER() OVER (PARTITION BY identity_type, arn, account_id
	                          ORDER BY last_seen_at DESC NULLS LAST, updated_at DESC NULLS LAST, created_at DESC NULLS LAST, id DESC) AS row_rank
	FROM ` + resolved.Table("identities") + `
	WHERE deleted_at IS NULL
	  AND account_id = ANY($1::text[])
	  AND COALESCE(region, '') = ANY($2::text[])
)
SELECT id, arn, name, identity_type, account_id, region, status, type_attributes, last_seen_at
FROM ranked WHERE row_rank = 1`
	rows, err := resolved.DB.QueryContext(ctx, q, pq.Array(resolved.AccountIDs), pq.Array(resolved.Regions))
	if err != nil {
		return nil, wrapMonitoringErr("latest identities", err)
	}
	defer rows.Close()
	var out []DiscoveryIdentityRow
	for rows.Next() {
		var row DiscoveryIdentityRow
		var arn, name, typ, account, status sql.NullString
		var attrs []byte
		var lastSeen sql.NullTime
		if err := rows.Scan(&row.ID, &arn, &name, &typ, &account, &row.Region, &status, &attrs, &lastSeen); err != nil {
			return nil, wrapMonitoringErr("latest identities", err)
		}
		row.ARN, row.Name, row.IdentityType, row.AccountID, row.Status = arn.String, name.String, typ.String, account.String, status.String
		row.TypeAttributes = attrs
		if lastSeen.Valid {
			t := lastSeen.Time.UTC()
			row.LastSeenAt = &t
		}
		out = append(out, row)
	}
	return out, wrapMonitoringErr("latest identities", rows.Err())
}

// ---------------------------------------------------------------------------
// baselines / baseline_versions
// ---------------------------------------------------------------------------

// BaselineStateRow is the baseline state of one key.
type BaselineStateRow struct {
	Key           MonitoringKey
	Status        string // active | inactive
	DaysRemaining int
	ActiveSince   *time.Time
}

// BaselineStates loads the baseline state for every key that has one.
func (r *DashboardSummaryRepository) BaselineStates(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (map[MonitoringKey]BaselineStateRow, error) {
	out := map[MonitoringKey]BaselineStateRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT b.source_type, b.identity_name, b.status, b.days_remaining, b.active_since
	      FROM ` + resolved.Table("baselines") + ` b` + keyJoin("b")
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, wrapMonitoringErr("baselines", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row BaselineStateRow
		var active sql.NullTime
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &row.Status, &row.DaysRemaining, &active); err != nil {
			return nil, wrapMonitoringErr("baselines", err)
		}
		if active.Valid {
			t := active.Time.UTC()
			row.ActiveSince = &t
		}
		out[row.Key] = row
	}
	return out, wrapMonitoringErr("baselines", rows.Err())
}

// BaselineEventRow is one row of the recently-baselined feed.
type BaselineEventRow struct {
	Key        MonitoringKey
	Event      string // baseline_established | baseline_version_published
	OccurredAt time.Time
	Version    *int64
}

// RecentlyBaselined merges baselines.active_since and
// baseline_versions.published_at, newest first.
func (r *DashboardSummaryRepository) RecentlyBaselined(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey, limit int) ([]BaselineEventRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT * FROM (
	        SELECT b.source_type, b.identity_name, 'baseline_established' AS event, b.active_since AS occurred_at, NULL::bigint AS version
	        FROM ` + resolved.Table("baselines") + ` b` + keyJoin("b") + ` WHERE b.active_since IS NOT NULL
	        UNION ALL
	        SELECT v.source_type, v.identity_name, 'baseline_version_published', v.published_at, v.version
	        FROM ` + resolved.Table("baseline_versions") + ` v` + keyJoin("v") + `
	      ) e ORDER BY occurred_at DESC LIMIT $3`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names, limit)
	if err != nil {
		return nil, wrapMonitoringErr("recently baselined", err)
	}
	defer rows.Close()
	var out []BaselineEventRow
	for rows.Next() {
		var row BaselineEventRow
		var v sql.NullInt64
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &row.Event, &row.OccurredAt, &v); err != nil {
			return nil, wrapMonitoringErr("recently baselined", err)
		}
		row.OccurredAt = row.OccurredAt.UTC()
		if v.Valid {
			n := v.Int64
			row.Version = &n
		}
		out = append(out, row)
	}
	return out, wrapMonitoringErr("recently baselined", rows.Err())
}

// ---------------------------------------------------------------------------
// identity_risk_scores
// ---------------------------------------------------------------------------

// IdentityRiskRow is the latest identity_risk_scores row for one key.
type IdentityRiskRow struct {
	Key                 MonitoringKey
	RiskScore           *int // NULL until the pipeline scores the identity
	Severity            string
	ScoreStatus         string
	EventCount          int
	AnomalousEventCount int
	WindowStart         time.Time
	WindowEnd           time.Time
	ComputedAt          time.Time
}

// LatestRiskScores returns the newest completed risk snapshot per key
// (contract §4.1 "Most anomalous identities").
func (r *DashboardSummaryRepository) LatestRiskScores(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (map[MonitoringKey]IdentityRiskRow, error) {
	out := map[MonitoringKey]IdentityRiskRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT DISTINCT ON (s.source_type, s.identity_name)
	             s.source_type, s.identity_name, s.risk_score, s.severity, s.score_status,
	             s.event_count, s.anomalous_event_count, s.window_start, s.window_end, s.computed_at
	      FROM ` + resolved.Table("identity_risk_scores") + ` s` + keyJoin("s") + `
	      WHERE s.window_end <= now()
	      ORDER BY s.source_type, s.identity_name, s.window_end DESC, s.computed_at DESC`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, wrapMonitoringErr("identity risk scores", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row IdentityRiskRow
		var score sql.NullInt64
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &score, &row.Severity, &row.ScoreStatus,
			&row.EventCount, &row.AnomalousEventCount, &row.WindowStart, &row.WindowEnd, &row.ComputedAt); err != nil {
			return nil, wrapMonitoringErr("identity risk scores", err)
		}
		if score.Valid {
			n := int(score.Int64)
			row.RiskScore = &n
		}
		out[row.Key] = row
	}
	return out, wrapMonitoringErr("identity risk scores", rows.Err())
}

// ---------------------------------------------------------------------------
// alerts
// ---------------------------------------------------------------------------

// CountOpenAlerts is cards.open_anomalies (§10.1): COUNT(*) from alerts
// where status = 'open' and created_at is in [from, to), for the keys.
func (r *DashboardSummaryRepository) CountOpenAlerts(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey, from, to time.Time) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT COUNT(*) FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + `
	      WHERE a.status = 'open' AND a.created_at >= $3 AND a.created_at < $4`
	var n int
	if err := resolved.DB.QueryRowContext(ctx, q, types, names, from, to).Scan(&n); err != nil {
		return 0, wrapMonitoringErr("count open alerts", err)
	}
	return n, nil
}

// DimensionCountRow is one GROUP BY dimension row.
type DimensionCountRow struct {
	Dimension string
	Count     int
}

// OpenAlertsByDimension counts open alerts per stored alerts.dimension in
// [from, to) — the contract's departures query, restricted to the keys.
func (r *DashboardSummaryRepository) OpenAlertsByDimension(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey, from, to time.Time) ([]DimensionCountRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT a.dimension, COUNT(*) FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + `
	      WHERE a.status = 'open' AND a.created_at >= $3 AND a.created_at < $4
	      GROUP BY a.dimension ORDER BY COUNT(*) DESC, a.dimension`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names, from, to)
	if err != nil {
		return nil, wrapMonitoringErr("open alerts by dimension", err)
	}
	defer rows.Close()
	var out []DimensionCountRow
	for rows.Next() {
		var row DimensionCountRow
		if err := rows.Scan(&row.Dimension, &row.Count); err != nil {
			return nil, wrapMonitoringErr("open alerts by dimension", err)
		}
		out = append(out, row)
	}
	return out, wrapMonitoringErr("open alerts by dimension", rows.Err())
}

// DayCountRow is one alerts-per-UTC-day row.
type DayCountRow struct {
	Day   time.Time
	Count int
}

// AlertsByDay counts alerts (all statuses) created per UTC day in [from, to).
func (r *DashboardSummaryRepository) AlertsByDay(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey, from, to time.Time) ([]DayCountRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT date_trunc('day', a.created_at AT TIME ZONE 'UTC') AS day, COUNT(*)
	      FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + `
	      WHERE a.created_at >= $3 AND a.created_at < $4
	      GROUP BY 1 ORDER BY 1`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names, from, to)
	if err != nil {
		return nil, wrapMonitoringErr("alerts by day", err)
	}
	defer rows.Close()
	var out []DayCountRow
	for rows.Next() {
		var row DayCountRow
		if err := rows.Scan(&row.Day, &row.Count); err != nil {
			return nil, wrapMonitoringErr("alerts by day", err)
		}
		row.Day = time.Date(row.Day.Year(), row.Day.Month(), row.Day.Day(), 0, 0, 0, 0, time.UTC)
		out = append(out, row)
	}
	return out, wrapMonitoringErr("alerts by day", rows.Err())
}
