package identitypeers

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository reads the per-peer monitoring facts of the identity peers
// endpoint from the caller's tenant schema. Everything is one grouped
// query over the whole peer set — never one query per peer. The peers
// themselves are the latest normalized discovery identities that share the
// requested identity's peer key (resolved in the service); the ClickHouse
// call counts are common.MonitoringEvents.CallCountsByIdentity.
type Repository struct{}

// NewRepository creates the repository.
func NewRepository() *Repository { return &Repository{} }

// PeerDimensionCountRow is one (identity, dimension) group of open alerts.
type PeerDimensionCountRow struct {
	Key       common.MonitoringKey
	Dimension string // exact alerts.dimension value
	Count     int
}

// OpenAlertsByIdentityDimension is the single grouped alerts query behind
// peers[].open_anomaly_count (sum of a key's rows) and
// peers[].outlier_dimension (its first row): COUNT(*) of alerts where
// status = 'open', grouped by identity and dimension, ordered per identity
// by count descending and then dimension name.
func (r *Repository) OpenAlertsByIdentityDimension(ctx context.Context, resolved *scope.DataScope, keys []common.MonitoringKey) ([]PeerDimensionCountRow, error) {
	out := []PeerDimensionCountRow{}
	if len(keys) == 0 {
		return out, nil
	}
	types, names := common.KeyArrays(keys)
	q := `SELECT a.source_type, a.identity_name, a.dimension, COUNT(*)
	      FROM ` + resolved.Table("alerts") + ` a` + common.KeyJoin("a") + `
	      WHERE a.status = 'open'
	      GROUP BY a.source_type, a.identity_name, a.dimension
	      ORDER BY a.source_type, a.identity_name, COUNT(*) DESC, a.dimension`
	rows, err := resolved.DB.QueryContext(ctx, q, types, names)
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "open alerts by identity and dimension", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row PeerDimensionCountRow
		if err := rows.Scan(&row.Key.SourceType, &row.Key.IdentityName, &row.Dimension, &row.Count); err != nil {
			return nil, common.WrapMonitoringErr(ctx, "open alerts by identity and dimension", err)
		}
		out = append(out, row)
	}
	return out, common.WrapMonitoringErr(ctx, "open alerts by identity and dimension", rows.Err())
}
