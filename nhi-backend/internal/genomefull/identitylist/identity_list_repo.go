package identitylist

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository reads the per-identity monitoring facts of the identity list
// from the caller's tenant schema: baselines and alerts. Every method is
// one grouped query over the whole key set — never one query per identity.
// The discovery rows themselves come from common.LatestIdentities and are
// normalized in the service; LastSeenByIdentity (ClickHouse) is called
// directly off common.MonitoringEvents.
type Repository struct{}

// NewRepository creates the repository.
func NewRepository() *Repository { return &Repository{} }

// BaselineSummaryRow is the baselines row (status, built_days,
// required_days) of one key.
type BaselineSummaryRow struct {
	Key          common.MonitoringKey
	Status       string
	BuiltDays    int
	RequiredDays int
}

// BaselineSummaries returns the baselines row of every key that has one.
func (r *Repository) BaselineSummaries(ctx context.Context, resolved *scope.DataScope, keys []common.MonitoringKey) (map[common.MonitoringKey]BaselineSummaryRow, error) {
	out := map[common.MonitoringKey]BaselineSummaryRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := common.KeyArrays(keys)
	q := `SELECT b.source_type, b.identity_name, b.status, b.built_days, b.required_days
	      FROM ` + resolved.Table("baselines") + ` b` + common.KeyJoin("b")
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "baseline summaries", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row BaselineSummaryRow
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &row.Status, &row.BuiltDays, &row.RequiredDays); err != nil {
			return nil, common.WrapMonitoringErr(ctx, "baseline summaries", err)
		}
		out[row.Key] = row
	}
	return out, common.WrapMonitoringErr(ctx, "baseline summaries", rows.Err())
}

// OpenAlertCounts is open_anomaly_count for every key at once: COUNT(*)
// from alerts where status = 'open', grouped by identity. Keys without
// open alerts are absent from the map.
func (r *Repository) OpenAlertCounts(ctx context.Context, resolved *scope.DataScope, keys []common.MonitoringKey) (map[common.MonitoringKey]int, error) {
	out := map[common.MonitoringKey]int{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := common.KeyArrays(keys)
	q := `SELECT a.source_type, a.identity_name, COUNT(*) FROM ` + resolved.Table("alerts") + ` a` + common.KeyJoin("a") + `
	      WHERE a.status = 'open'
	      GROUP BY a.source_type, a.identity_name`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "open alert counts", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k common.MonitoringKey
		var n int
		if err := rows.Scan(&k.SourceType, &k.IdentityName, &n); err != nil {
			return nil, common.WrapMonitoringErr(ctx, "open alert counts", err)
		}
		out[k] = n
	}
	return out, common.WrapMonitoringErr(ctx, "open alert counts", rows.Err())
}
