package genome

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"nhi-backend-api/internal/scope"
)

// IdentityTimelineRepository reads the rows behind
// GET /api/v1/identities/{source_type}/{identity_name}/timeline
// (backend_api_contract.md §5.6 / §10.3) from the caller's tenant schema.
// The timeline is derived from current tables — alerts, baseline_versions
// and baselines — because the pipeline keeps no immutable event history
// (§5.6 SCHEMA GAP). Read-only.
type IdentityTimelineRepository struct{}

// NewIdentityTimelineRepository creates the repository.
func NewIdentityTimelineRepository() *IdentityTimelineRepository {
	return &IdentityTimelineRepository{}
}

// TimelineAlertRow is one alerts row of the identity (the "anomaly" item).
type TimelineAlertRow struct {
	ID        int64
	Key       MonitoringKey
	Dimension string
	Reason    string
	Status    string
	WindowEnd time.Time // end of the detection window that raised it
	CreatedAt time.Time
}

// Alerts returns every alert stored under the keys (identity key and shared
// workload key), newest first (alerts_identity_created_idx order).
func (r *IdentityTimelineRepository) Alerts(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) ([]TimelineAlertRow, error) {
	out := []TimelineAlertRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT a.id, a.source_type, a.identity_name, a.dimension, a.reason, a.status, a.window_end, a.created_at
	      FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + `
	      ORDER BY a.created_at DESC, a.id DESC`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, wrapMonitoringErr("timeline alerts", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row TimelineAlertRow
		if err := rows.Scan(&row.ID, &row.Key.SourceType, &row.Key.IdentityName, &row.Dimension, &row.Reason, &row.Status, &row.WindowEnd, &row.CreatedAt); err != nil {
			return nil, wrapMonitoringErr("timeline alerts", err)
		}
		row.CreatedAt, row.WindowEnd = row.CreatedAt.UTC(), row.WindowEnd.UTC()
		out = append(out, row)
	}
	return out, wrapMonitoringErr("timeline alerts", rows.Err())
}

// BaselineMilestoneRow carries the two lifecycle timestamps of a baselines
// row: first_seen_date (learning started) and active_since (established).
type BaselineMilestoneRow struct {
	Key           MonitoringKey // the key the row is stored under (identity key or shared workload key)
	FirstSeenDate *time.Time    // DATE column, midnight UTC
	ActiveSince   *time.Time
}

// BaselineMilestones returns the baselines row of the identity, preferring
// the identity key over the shared workload key; nil when there is none.
func (r *IdentityTimelineRepository) BaselineMilestones(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (*BaselineMilestoneRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, names := keyArrays(keys)
	q := `SELECT b.source_type, b.identity_name, b.first_seen_date, b.active_since
	      FROM ` + resolved.Table("baselines") + ` b` + fmt.Sprintf(keyPreferenceJoinSQL, "b") + `
	      ORDER BY k.preference
	      LIMIT 1`
	var (
		row       BaselineMilestoneRow
		firstSeen sql.NullTime
		active    sql.NullTime
	)
	err := resolved.DB.QueryRowContext(ctx, q, types, names).Scan(&row.Key.SourceType, &row.Key.IdentityName, &firstSeen, &active)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, wrapMonitoringErr("baseline milestones", err)
	}
	if firstSeen.Valid {
		d := firstSeen.Time
		t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		row.FirstSeenDate = &t
	}
	if active.Valid {
		t := active.Time.UTC()
		row.ActiveSince = &t
	}
	return &row, nil
}

// PublishedVersionRow is one baseline_versions row (version, published_at).
type PublishedVersionRow struct {
	Key         MonitoringKey
	Version     int64
	PublishedAt time.Time
}

// PublishedVersions returns the version history stored under key, newest
// first (baseline_versions_identity_published_idx order).
func (r *IdentityTimelineRepository) PublishedVersions(ctx context.Context, resolved *scope.DataScope, key MonitoringKey) ([]PublishedVersionRow, error) {
	q := `SELECT v.source_type, v.identity_name, v.version, v.published_at
	      FROM ` + resolved.Table("baseline_versions") + ` v
	      WHERE v.source_type = $1 AND v.identity_name = $2
	      ORDER BY v.published_at DESC, v.version DESC`
	rows, err := resolved.DB.QueryContext(ctx, q, key.SourceType, key.IdentityName)
	if err != nil {
		return nil, wrapMonitoringErr("published baseline versions", err)
	}
	defer rows.Close()
	out := []PublishedVersionRow{}
	for rows.Next() {
		var v PublishedVersionRow
		if err := rows.Scan(&v.Key.SourceType, &v.Key.IdentityName, &v.Version, &v.PublishedAt); err != nil {
			return nil, wrapMonitoringErr("published baseline versions", err)
		}
		v.PublishedAt = v.PublishedAt.UTC()
		out = append(out, v)
	}
	return out, wrapMonitoringErr("published baseline versions", rows.Err())
}
