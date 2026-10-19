package postures

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityHeaderRepository struct{}

func NewIdentityHeaderRepository() *IdentityHeaderRepository {
	return &IdentityHeaderRepository{}
}

// Get implements GET /identities/{id}: the page header, read from
// ispm_identity_posture by primary key. Badges stay null when the scanner did
// not collect the attribute.
func (r *IdentityHeaderRepository) Get(ctx context.Context, s *scope.DataScope, id string) (*models.IdentityHeader, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT identity_id, COALESCE(identity_name, ''), COALESCE(identity_arn, ''), COALESCE(identity_type, ''),
			functional_class, COALESCE(account_id, ''), COALESCE(account_name, ''), COALESCE(region, ''),
			score, COALESCE(band, ''), COALESCE(grade, ''), mfa_enabled, console_access, COALESCE(open_alerts, 0)
		FROM %s WHERE tenant_id = $1 AND identity_id = $2`, s.Table(tIdentityPosture))
	h := &models.IdentityHeader{}
	var class sql.NullString
	var score sql.NullFloat64
	var mfa, console sql.NullBool
	var alerts sql.NullInt64
	err := s.DB.QueryRowContext(ctx, q, s.TenantID, id).Scan(&h.IdentityID, &h.Name, &h.ARN, &h.Type, &class,
		&h.Account.ID, &h.Account.Name, &h.Region, &score, &h.Band, &h.Grade, &mfa, &console, &alerts)
	if errors.Is(err, sql.ErrNoRows) || isUndefinedTable(err) { // not scored yet
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load identity header: %w", err)
	}
	h.FunctionalClass = nullStr(class)
	h.Score = nullFloat(score)
	h.Badges = models.IdentityBadges{MFAEnabled: nullBool(mfa), ConsoleAccess: nullBool(console)}
	h.Counts.OpenAlerts = intVal(alerts)

	// credentials: the BOUND_TO edges that start at this identity
	cq := fmt.Sprintf(`SELECT count(DISTINCT target_arn)::int FROM %s
		WHERE tenant_id = $1 AND source_arn = $2 AND edge_type = 'BOUND_TO'`, s.Table(tAccessEdges))
	if h.ARN != "" {
		if err := s.DB.QueryRowContext(ctx, cq, s.TenantID, h.ARN).Scan(&h.Counts.Credentials); err != nil {
			if !isUndefinedTable(err) {
				return nil, fmt.Errorf("count bound credentials: %w", err)
			}
		}
	}
	return h, nil
}
