package postures

import (
	"context"
	"database/sql"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityRoleUseRepository struct{}

func NewIdentityRoleUseRepository() *IdentityRoleUseRepository {
	return &IdentityRoleUseRepository{}
}

// roleUseLimit bounds each direction of the Role use tab.
const roleUseLimit = 500

// Get implements GET /identities/{id}/role-use from ispm_role_use. The table
// stays empty until the scanner provides CloudTrail AssumeRole events, so the
// second return value ("data available") is false while the tenant has no
// rows in it (or the table does not exist yet).
func (r *IdentityRoleUseRepository) Get(ctx context.Context, s *scope.DataScope, id string) (*models.IdentityRoleUse, bool, error) {
	if err := need(s); err != nil {
		return nil, false, err
	}
	ref, err := loadIdentityRef(ctx, s, id)
	if err != nil {
		return nil, false, err
	}
	out := &models.IdentityRoleUse{AssumedBy: []models.RoleUseItem{}, Assumes: []models.RoleUseItem{}}

	var hasRows bool
	probe := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE tenant_id = $1)`, s.Table(tRoleUse))
	if err := s.DB.QueryRowContext(ctx, probe, s.TenantID).Scan(&hasRows); err != nil {
		if isUndefinedTable(err) {
			return out, false, nil
		}
		return nil, false, fmt.Errorf("probe role use: %w", err)
	}
	if !hasRows {
		return out, false, nil
	}

	q := fmt.Sprintf(`SELECT direction, COALESCE(counterpart_arn, ''), COALESCE(counterpart_name, ''), counterpart_class,
			COALESCE(sessions, 0), first_assumed_at, last_assumed_at
		FROM %s
		WHERE tenant_id = $1 AND identity_id = $2
		ORDER BY sessions DESC NULLS LAST, counterpart_name
		LIMIT %d`, s.Table(tRoleUse), roleUseLimit*2)
	rows, err := s.DB.QueryContext(ctx, q, s.TenantID, id)
	if err != nil {
		return nil, false, fmt.Errorf("query role use: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var direction string
		var it models.RoleUseItem
		var class sql.NullString
		var sessions sql.NullInt64
		var first, last sql.NullTime
		if err := rows.Scan(&direction, &it.RoleARN, &it.RoleName, &class, &sessions, &first, &last); err != nil {
			return nil, false, fmt.Errorf("scan role use: %w", err)
		}
		it.FunctionalClass, it.Sessions = nullStr(class), sessions.Int64
		it.FirstAssumedAt, it.LastAssumedAt = nullTime(first), nullTime(last)
		if it.RoleName == "" {
			it.RoleName = it.RoleARN
		}
		switch direction {
		case "ASSUMES":
			out.Assumes = append(out.Assumes, it)
		case "ASSUMED_BY":
			out.AssumedBy = append(out.AssumedBy, it)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	// An identity is assumable when others were seen assuming it, or when the
	// access graph has a CAN_ASSUME edge that ends at it.
	out.Assumable = len(out.AssumedBy) > 0
	if !out.Assumable && ref.ARN != "" {
		eq := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s
			WHERE tenant_id = $1 AND target_arn = $2 AND edge_type = 'CAN_ASSUME')`, s.Table(tAccessEdges))
		if err := s.DB.QueryRowContext(ctx, eq, s.TenantID, ref.ARN).Scan(&out.Assumable); err != nil && !isUndefinedTable(err) {
			return nil, false, fmt.Errorf("check assumable: %w", err)
		}
	}
	return out, true, nil
}
