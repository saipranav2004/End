package common

import (
	"context"
	"database/sql"
	"time"

	"nhi-backend-api/internal/scope"
)

// IdentityRiskSnapshotRow is the latest completed identity_risk_scores
// snapshot for one key. Shared by identitylist and identitypeers, which
// both need the newest score of a grouped key set.
type IdentityRiskSnapshotRow struct {
	Key         MonitoringKey
	RiskScore   *int // NULL until the pipeline scores the identity
	Severity    string
	ScoreStatus string
	WindowEnd   time.Time
}

// LatestRiskSnapshots returns the newest completed identity_risk_scores
// snapshot per key.
func LatestRiskSnapshots(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (map[MonitoringKey]IdentityRiskSnapshotRow, error) {
	out := map[MonitoringKey]IdentityRiskSnapshotRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := KeyArrays(keys)
	q := `SELECT DISTINCT ON (s.source_type, s.identity_name)
	             s.source_type, s.identity_name, s.risk_score, s.severity, s.score_status, s.window_end
	      FROM ` + resolved.Table("identity_risk_scores") + ` s` + KeyJoin("s") + `
	      WHERE s.window_end <= now()
	      ORDER BY s.source_type, s.identity_name, s.window_end DESC, s.computed_at DESC`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, WrapMonitoringErr(ctx, "latest risk snapshots", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row IdentityRiskSnapshotRow
		var score sql.NullInt64
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &score, &row.Severity, &row.ScoreStatus, &row.WindowEnd); err != nil {
			return nil, WrapMonitoringErr(ctx, "latest risk snapshots", err)
		}
		if score.Valid {
			n := int(score.Int64)
			row.RiskScore = &n
		}
		row.WindowEnd = row.WindowEnd.UTC()
		out[row.Key] = row
	}
	return out, WrapMonitoringErr(ctx, "latest risk snapshots", rows.Err())
}

// LatestRiskPreferred returns the newest completed snapshot of the first
// key, in the order given, that has one; nil, nil when none has.
func LatestRiskPreferred(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey) (*IdentityRiskSnapshotRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	types, names := KeyArrays(keys)
	q := `SELECT s.source_type, s.identity_name, s.risk_score, s.severity, s.score_status, s.window_end
	      FROM ` + resolved.Table("identity_risk_scores") + ` s` + KeyPreferenceJoin("s") + `
	      WHERE s.window_end <= now()
	      ORDER BY k.preference, s.window_end DESC, s.computed_at DESC
	      LIMIT 1`
	var (
		row   IdentityRiskSnapshotRow
		score sql.NullInt64
	)
	err := resolved.DB.QueryRowContext(ctx, q, types, names).Scan(&row.Key.SourceType, &row.Key.IdentityName, &score, &row.Severity, &row.ScoreStatus, &row.WindowEnd)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, WrapMonitoringErr(ctx, "latest risk", err)
	}
	if score.Valid {
		n := int(score.Int64)
		row.RiskScore = &n
	}
	row.WindowEnd = row.WindowEnd.UTC()
	return &row, nil
}
