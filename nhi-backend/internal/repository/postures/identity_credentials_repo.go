package postures

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityCredentialsRepository struct{}

func NewIdentityCredentialsRepository() *IdentityCredentialsRepository {
	return &IdentityCredentialsRepository{}
}

// Get implements GET /identities/{id}/credentials: the BOUND_TO edges of
// ispm_access_edges, joined on arn with the credential tables (see
// credentialSource: the scanner's credentials, else the posture sample data in
// dummy_credentials). The number
// of open findings per credential comes from ispm_nhi_findings.
func (r *IdentityCredentialsRepository) Get(ctx context.Context, s *scope.DataScope, id string) ([]models.IdentityCredential, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	ref, err := loadIdentityRef(ctx, s, id)
	if err != nil {
		return nil, err
	}
	out := make([]models.IdentityCredential, 0)
	if ref.ARN == "" {
		return out, nil
	}
	src, err := credentialSource(ctx, s)
	if err != nil {
		return nil, err
	}
	// The worker has written the binding and privilege under more than one key.
	q := fmt.Sprintf(`SELECT e.target_arn, COALESCE(e.target_type, ''),
			COALESCE(e.attributes->>'binding_type', e.attributes->>'binding', e.attributes->>'bound_via', ''),
			COALESCE(e.attributes->>'target_privileged', e.attributes->>'privileged', e.attributes->>'is_privileged'),
			COALESCE(c.credential_type, ''), COALESCE(c.status, ''), c.created_at_aws, c.last_used_at,
			c.permission_boundary_arn, c.max_session_duration,
			(SELECT count(*)::int FROM %s f
			  WHERE f.tenant_id = e.tenant_id AND f.credential_arn = e.target_arn AND f.status = 'OPEN')
		FROM %s e
		%s
		WHERE e.tenant_id = $1 AND e.source_arn = $2 AND e.edge_type = 'BOUND_TO'
		ORDER BY e.target_arn`, s.Table(tFindings), s.Table(tAccessEdges), latestCredential(src))
	rows, err := s.DB.QueryContext(ctx, q, s.TenantID, ref.ARN)
	if err != nil {
		return nil, fmt.Errorf("query identity credentials: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var arn, targetType, binding, credType, status string
		var privileged, boundary sql.NullString
		var created, used sql.NullTime
		var maxSession sql.NullInt64
		var open int
		if err := rows.Scan(&arn, &targetType, &binding, &privileged, &credType, &status, &created, &used,
			&boundary, &maxSession, &open); err != nil {
			return nil, fmt.Errorf("scan identity credential: %w", err)
		}
		typ := strings.ToUpper(firstNonEmpty(credType, targetType))
		c := models.IdentityCredential{CredentialARN: arn, Type: typ, Binding: binding, OpenFindings: open}
		if typ == "IAM_ROLE" || strings.Contains(arn, ":role/") {
			c.IsRole = true
			if privileged.Valid {
				v := strings.EqualFold(privileged.String, "true")
				c.Privileged = &v
			}
			hasBoundary := boundary.Valid && strings.TrimSpace(boundary.String) != ""
			c.HasBoundary = &hasBoundary
			c.MaxSessionDuration = nullInt(maxSession)
		} else {
			c.Status = titleCase(status)
			c.AgeDays, c.LastUsedDays = daysSince(created), daysSince(used)
			c.UsageState = usageState(used, c.LastUsedDays)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
