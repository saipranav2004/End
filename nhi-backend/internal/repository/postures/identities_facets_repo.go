package postures

import (
	"context"
	"fmt"
	"strings"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentitiesFacetsRepository struct{}

func NewIdentitiesFacetsRepository() *IdentitiesFacetsRepository {
	return &IdentitiesFacetsRepository{}
}

// Get implements GET /identities/facets in one round trip: a UNION ALL of
// count(*) FILTER (…) and GROUP BY branches over ispm_identity_posture. Each
// branch ignores the filter of its own facet so the alternatives stay visible.
func (r *IdentitiesFacetsRepository) Get(ctx context.Context, s *scope.DataScope, f models.IdentityFilter) (*models.IdentityFacets, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	b := newQueryBuilder(s)
	frags := b.buildFragments(s, f)
	table := s.Table(tIdentityPosture)

	var branches []string
	// total: every filter applies
	branches = append(branches, fmt.Sprintf(
		`SELECT 'total'::text AS facet, ''::text AS k, ''::text AS name, count(*)::bigint AS n FROM %s p WHERE %s`,
		table, whereExcept(frags, "")))
	// band
	branches = append(branches, fmt.Sprintf(
		`SELECT 'band', p.band::text, ''::text, count(*)::bigint FROM %s p WHERE %s AND p.band IS NOT NULL GROUP BY p.band`,
		table, whereExcept(frags, facetBand)))
	// severity: identities failing at least one check of that severity
	for _, sev := range models.Severities {
		branches = append(branches, fmt.Sprintf(
			`SELECT 'severity', '%s'::text, ''::text, count(*) FILTER (WHERE COALESCE(%s, 0) > 0)::bigint FROM %s p WHERE %s`,
			sev, severityColumn[strings.ToLower(sev)], table, whereExcept(frags, facetSeverity)))
	}
	// dynamic facets
	branches = append(branches,
		fmt.Sprintf(`SELECT 'category', p.functional_class::text, ''::text, count(*)::bigint FROM %s p WHERE %s AND p.functional_class IS NOT NULL AND p.functional_class <> '' GROUP BY p.functional_class`,
			table, whereExcept(frags, facetCategory)),
		fmt.Sprintf(`SELECT 'account', p.account_id::text, COALESCE(max(p.account_name), '')::text, count(*)::bigint FROM %s p WHERE %s AND p.account_id IS NOT NULL GROUP BY p.account_id`,
			table, whereExcept(frags, facetAccount)),
		fmt.Sprintf(`SELECT 'type', p.identity_type::text, ''::text, count(*)::bigint FROM %s p WHERE %s AND p.identity_type IS NOT NULL GROUP BY p.identity_type`,
			table, whereExcept(frags, facetType)),
		fmt.Sprintf(`SELECT 'pillar', p.weakest_pillar::text, ''::text, count(*)::bigint FROM %s p WHERE %s AND p.weakest_pillar IS NOT NULL AND p.weakest_pillar <> '' GROUP BY p.weakest_pillar`,
			table, whereExcept(frags, facetPillar)),
	)
	// changed: down / up / same, by the window's score_change column
	col := "p." + f.Window.ChangeColumn
	branches = append(branches, fmt.Sprintf(
		`SELECT 'changed', 'down'::text, ''::text, count(*) FILTER (WHERE %[1]s < 0)::bigint FROM %[2]s p WHERE %[3]s
		 UNION ALL SELECT 'changed', 'up'::text, ''::text, count(*) FILTER (WHERE %[1]s > 0)::bigint FROM %[2]s p WHERE %[3]s
		 UNION ALL SELECT 'changed', 'same'::text, ''::text, count(*) FILTER (WHERE %[1]s = 0)::bigint FROM %[2]s p WHERE %[3]s`,
		col, table, whereExcept(frags, facetChanged)))

	out := &models.IdentityFacets{
		Band:     map[string]int{},
		Severity: map[string]int{},
		Category: map[string]int{},
		Account:  map[string]models.AccountFacet{},
		Type:     map[string]int{},
		Pillar:   map[string]int{},
		Changed:  map[string]int{models.ChangedDown: 0, models.ChangedUp: 0, models.ChangedSame: 0},
	}
	for _, band := range models.Bands {
		out.Band[band] = 0
	}
	for _, sev := range models.Severities {
		out.Severity[sev] = 0
	}
	rows, err := s.DB.QueryContext(ctx, strings.Join(branches, "\nUNION ALL\n"), b.args...)
	if isUndefinedTable(err) { // no posture evaluation has run for this tenant yet
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query facets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var facet, key, name string
		var n int64
		if err := rows.Scan(&facet, &key, &name, &n); err != nil {
			return nil, fmt.Errorf("scan facet: %w", err)
		}
		switch facet {
		case "total":
			out.Total = int(n)
		case facetBand:
			out.Band[key] = int(n)
		case facetSeverity:
			out.Severity[key] = int(n)
		case facetCategory:
			out.Category[key] = int(n)
		case facetAccount:
			out.Account[key] = models.AccountFacet{Name: name, Count: int(n)}
		case facetType:
			out.Type[key] = int(n)
		case facetPillar:
			out.Pillar[key] = int(n)
		case facetChanged:
			out.Changed[key] = int(n)
		}
	}
	return out, rows.Err()
}
