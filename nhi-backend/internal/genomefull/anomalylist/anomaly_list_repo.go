package anomalylist

import (
	"context"
	"github.com/lib/pq"
	"nhi-backend-api/internal/workloadtypes"
	"strconv"
	"strings"
	"time"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository reads the alerts page behind the anomaly list from the
// caller's tenant schema: one keyset page ordered created_at DESC, id
// DESC. The event-level columns are read afterwards from ClickHouse in ONE
// batch (common.MonitoringEvents.EventsByID), never once per alert.
// Read-only.
type Repository struct{}

// NewRepository creates the repository.
func NewRepository() *Repository { return &Repository{} }

// Filter is the parsed filter set applied in SQL.
type Filter struct {
	Dimension  string                 // exact alerts.dimension
	Status     string                 // exact alerts.status (the API's `state`)
	SourceType string                 // exact alerts.source_type
	Search     string                 // case-insensitive substring of identity_name or identity_metadata.display_name
	SearchKeys []common.MonitoringKey // keys of discovered identities whose display name matches Search
	From, To   *time.Time             // created_at window, [From, To)
	After      *common.AlertCursor    // continue after this position
	Limit      int                    // rows to return (callers ask for one more than the page size)
}

// Page returns the alerts matching f, newest first (created_at DESC, id DESC).
func (r *Repository) Page(ctx context.Context, resolved *scope.DataScope, f Filter) ([]common.AlertRow, error) {
	out := []common.AlertRow{}
	if f.Limit < 1 {
		return out, nil
	}
	var (
		where []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.Dimension != "" {
		where = append(where, "a.dimension = "+arg(f.Dimension))
	}
	if f.Status != "" {
		where = append(where, "a.status = "+arg(f.Status))
	}
	if f.SourceType != "" {
		where = append(where, "a.source_type = "+arg(f.SourceType))
	}
	if families, all := workloadtypes.Enabled(); !all {
		where = append(where, "a.source_type = ANY("+arg(pq.Array(families))+"::text[])")
	}
	if f.From != nil {
		where = append(where, "a.created_at >= "+arg(*f.From))
	}
	if f.To != nil {
		where = append(where, "a.created_at < "+arg(*f.To))
	}
	if f.After != nil {
		where = append(where, "(a.created_at, a.id) < ("+arg(f.After.CreatedAt)+", "+arg(f.After.ID)+")")
	}
	if f.Search != "" {
		pattern := "%" + common.EscapeLike(f.Search) + "%"
		types, names := common.KeyArrays(f.SearchKeys)
		where = append(where, "(a.identity_name ILIKE "+arg(pattern)+
			" OR a.identity_metadata->>'display_name' ILIKE "+arg(pattern)+
			" OR (a.source_type, a.identity_name) IN (SELECT * FROM unnest("+arg(types)+"::text[], "+arg(names)+"::text[])))")
	}
	q := `SELECT ` + common.AlertColumns + ` FROM ` + resolved.Table("alerts") + ` a`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY a.created_at DESC, a.id DESC LIMIT ` + arg(f.Limit)
	rows, err := resolved.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "anomaly list", err)
	}
	defer rows.Close()
	for rows.Next() {
		row, err := common.ScanAlert(rows)
		if err != nil {
			return nil, common.WrapMonitoringErr(ctx, "anomaly list", err)
		}
		out = append(out, row)
	}
	return out, common.WrapMonitoringErr(ctx, "anomaly list", rows.Err())
}
