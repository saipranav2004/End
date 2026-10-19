package postures

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityAlertsRepository struct{}

func NewIdentityAlertsRepository() *IdentityAlertsRepository {
	return &IdentityAlertsRepository{}
}

// alertCursor holds the sort key of the last alert of a page. The numeric
// parts are carried as decimal strings so no precision is lost.
type alertCursor struct {
	Rank   int    `json:"r"`
	Raised string `json:"t"`
	ID     string `json:"id"`
}

// Get implements GET /identities/{id}/alerts: findings on the identity and on
// the credentials it holds, open first and newest first. The window keeps the
// alerts raised (first_seen) or closed (resolved_at) inside it - except that
// an OPEN alert is current state and is always listed, however long ago it
// was raised, so the list agrees with the identity's open_alerts count.
//
// The two sources are combined with UNION ALL so each branch uses its own
// index: findings by identity_id, and findings by credential_arn for the
// credentials reached through the BOUND_TO edges.
func (r *IdentityAlertsRepository) Get(ctx context.Context, s *scope.DataScope, id string, f models.IdentityAlertsFilter) ([]models.IdentityAlert, *string, error) {
	if err := need(s); err != nil {
		return nil, nil, err
	}
	ref, err := loadIdentityRef(ctx, s, id)
	if err != nil {
		return nil, nil, err
	}
	limit := clampLimit(f.Limit)

	args := []any{s.TenantID, id, time.Now().Add(-f.Window.Duration), ref.ARN}
	statusCond := ""
	switch f.Status {
	case models.AlertsOpen:
		statusCond = " AND f.status = 'OPEN'"
	case models.AlertsClosed:
		statusCond = " AND f.status <> 'OPEN'"
	}
	common := `f.tenant_id = $1 AND (f.status = 'OPEN' OR f.first_seen >= $3 OR f.resolved_at >= $3)` + statusCond
	cols := `f.finding_id::text AS finding_id, COALESCE(f.rule_id, '') AS rule_id, COALESCE(f.title, '') AS title,
		COALESCE(f.severity, '') AS severity, f.risk_score AS risk_score, COALESCE(f.status, '') AS status,
		f.first_seen AS raised_at, f.resolved_at AS resolved_at, f.updated_at AS updated_at,
		CASE WHEN f.status = 'OPEN' THEN 0 ELSE 1 END AS k_rank,
		(-EXTRACT(EPOCH FROM COALESCE(f.first_seen, 'epoch'::timestamptz)))::numeric AS k_epoch`
	findings := s.Table(tFindings)
	edges := s.Table(tAccessEdges)

	q := fmt.Sprintf(`WITH a AS (
			SELECT %[1]s, 'identity'::text AS source
			FROM %[2]s f WHERE %[3]s AND f.identity_id = $2
			UNION ALL
			SELECT %[1]s, 'credential'::text AS source
			FROM %[2]s f WHERE %[3]s AND f.identity_id IS DISTINCT FROM $2
			  AND f.credential_arn IN (SELECT e.target_arn FROM %[4]s e
			        WHERE e.tenant_id = $1 AND e.source_arn = $4 AND e.edge_type = 'BOUND_TO')
		)
		SELECT finding_id, rule_id, title, severity, risk_score, status, raised_at, resolved_at, updated_at, source, k_rank, k_epoch::text AS epoch_text
		FROM a`, cols, findings, common, edges)

	if strings.TrimSpace(f.Cursor) != "" {
		var c alertCursor
		if err := decodeCursor(f.Cursor, &c); err != nil || c.ID == "" || c.Raised == "" {
			return nil, nil, ErrInvalidCursor
		}
		args = append(args, c.Rank, c.Raised, c.ID)
		n := len(args)
		q += fmt.Sprintf(` WHERE (k_rank, k_epoch, finding_id) > ($%d::int, $%d::numeric, $%d::text)`, n-2, n-1, n)
	}
	args = append(args, limit+1)
	q += fmt.Sprintf(` ORDER BY k_rank, k_epoch, finding_id LIMIT $%d`, len(args))

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query identity alerts: %w", err)
	}
	defer rows.Close()
	type keyed struct {
		models.IdentityAlert
		rank  int
		epoch string
	}
	var all []keyed
	for rows.Next() {
		var k keyed
		var risk sql.NullFloat64
		var raised, resolved, updated sql.NullTime
		if err := rows.Scan(&k.FindingID, &k.RuleID, &k.Title, &k.Severity, &risk, &k.Status, &raised, &resolved,
			&updated, &k.Source, &k.rank, &k.epoch); err != nil {
			return nil, nil, fmt.Errorf("scan identity alert: %w", err)
		}
		k.RiskScore, k.RaisedAt, k.ResolvedAt, k.UpdatedAt = nullFloat(risk), nullTime(raised), nullTime(resolved), nullTime(updated)
		all = append(all, k)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(all) > limit {
		all = all[:limit]
		last := all[limit-1]
		c := encodeCursor(alertCursor{Rank: last.rank, Raised: last.epoch, ID: last.FindingID})
		next = &c
	}
	out := make([]models.IdentityAlert, 0, len(all))
	for _, k := range all {
		out = append(out, k.IdentityAlert)
	}
	return out, next, nil
}
