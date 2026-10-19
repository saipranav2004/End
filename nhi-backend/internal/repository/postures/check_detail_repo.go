package postures

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type CheckDetailRepository struct{}

func NewCheckDetailRepository() *CheckDetailRepository {
	return &CheckDetailRepository{}
}

// complianceSampleSize is how many recent findings of the rule are read to
// collect its compliance tags and remediation text.
const complianceSampleSize = 50

// Get implements GET /checks/{rule_id}: the details behind the Remediate
// button. The rule comes from ispm_rule_weights (with its category and
// pillar); remediation text and compliance tags are taken from the rule's
// recent findings; identities_failing counts FAIL rows in ispm_identity_checks.
func (r *CheckDetailRepository) Get(ctx context.Context, s *scope.DataScope, ruleID string) (*models.CheckDetail, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT w.rule_id, COALESCE(NULLIF(w.check_title, ''), NULLIF(w.rule_name, ''), w.rule_id),
			COALESCE(w.severity, ''), COALESCE(w.penalty_points, 0), COALESCE(cat.pillar_code, ''),
			COALESCE(w.fix_label, ''), COALESCE(w.description, '')
		FROM %s w LEFT JOIN %s cat ON cat.category_code = w.category_code
		WHERE w.rule_id = $1`, s.Table(tRuleWeights), s.Table(tCategories))
	d := &models.CheckDetail{Compliance: []string{}}
	var description string
	err := s.DB.QueryRowContext(ctx, q, ruleID).Scan(&d.RuleID, &d.Title, &d.Severity, &d.PenaltyPoints, &d.Pillar, &d.Fix, &description)
	if errors.Is(err, sql.ErrNoRows) || isUndefinedTable(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load rule: %w", err)
	}

	fq := fmt.Sprintf(`SELECT COALESCE(remediation, ''), to_jsonb(compliance_tags)
		FROM %s WHERE tenant_id = $1 AND rule_id = $2
		ORDER BY last_seen DESC NULLS LAST LIMIT %d`, s.Table(tFindings), complianceSampleSize)
	rows, err := s.DB.QueryContext(ctx, fq, s.TenantID, ruleID)
	if err != nil {
		return nil, fmt.Errorf("load rule findings: %w", err)
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	for rows.Next() {
		var remediation string
		var tags []byte
		if err := rows.Scan(&remediation, &tags); err != nil {
			return nil, fmt.Errorf("scan rule finding: %w", err)
		}
		if d.Remediation == "" {
			d.Remediation = remediation
		}
		for _, t := range tagList(tags) {
			if _, dup := seen[t]; !dup {
				seen[t] = struct{}{}
				d.Compliance = append(d.Compliance, t)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(d.Compliance)
	if d.Remediation == "" {
		d.Remediation = description
	}

	cq := fmt.Sprintf(`SELECT count(*)::int FROM %s WHERE tenant_id = $1 AND rule_id = $2 AND status = 'FAIL'`, s.Table(tIdentityChecks))
	if err := s.DB.QueryRowContext(ctx, cq, s.TenantID, ruleID).Scan(&d.IdentitiesFailing); err != nil {
		return nil, fmt.Errorf("count failing identities: %w", err)
	}
	return d, nil
}
