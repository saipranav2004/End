package postures

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentitiesListRepository struct{}

func NewIdentitiesListRepository() *IdentitiesListRepository {
	return &IdentitiesListRepository{}
}

// ─────────────────────────────────────────────────────────────────────────
// Filter builder — shared by the list, the facets and the CSV export
// ─────────────────────────────────────────────────────────────────────────

// Facet names, one per filter that has a facet counter.
const (
	facetBand     = "band"
	facetSeverity = "severity"
	facetCategory = "category"
	facetAccount  = "account"
	facetType     = "type"
	facetPillar   = "pillar"
	facetChanged  = "changed"
	facetOther    = "" // filters without a facet (failing_check, q) apply everywhere
)

// fragment is one WHERE condition on ispm_identity_posture (alias p).
type fragment struct {
	facet string
	sql   string
}

// queryBuilder collects bind arguments. $1 is always the tenant.
type queryBuilder struct {
	args []any
}

func newQueryBuilder(s *scope.DataScope) *queryBuilder {
	return &queryBuilder{args: []any{s.TenantID}}
}

func (b *queryBuilder) add(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

// severityColumn maps a severity to its failed-count column on the posture row.
var severityColumn = map[string]string{
	"critical": "p.failed_critical",
	"high":     "p.failed_high",
	"medium":   "p.failed_medium",
	"low":      "p.failed_low",
}

// buildFragments turns the filter into WHERE conditions. Values are always
// bound as parameters; the only interpolated text is the window's change
// column, which comes from a fixed table.
func (b *queryBuilder) buildFragments(s *scope.DataScope, f models.IdentityFilter) []fragment {
	var out []fragment
	if len(f.Bands) > 0 {
		out = append(out, fragment{facetBand, "p.band = ANY(" + b.add(pq.Array(f.Bands)) + "::text[])"})
	}
	if len(f.Severities) > 0 {
		var parts []string
		for _, sev := range f.Severities {
			if col, ok := severityColumn[strings.ToLower(sev)]; ok {
				parts = append(parts, "COALESCE("+col+", 0) > 0")
			}
		}
		if len(parts) > 0 {
			out = append(out, fragment{facetSeverity, "(" + strings.Join(parts, " OR ") + ")"})
		}
	}
	if len(f.Categories) > 0 {
		out = append(out, fragment{facetCategory, "p.functional_class = ANY(" + b.add(pq.Array(f.Categories)) + "::text[])"})
	}
	if len(f.Accounts) > 0 {
		out = append(out, fragment{facetAccount, "p.account_id = ANY(" + b.add(pq.Array(f.Accounts)) + "::text[])"})
	}
	if len(f.Types) > 0 {
		// The handler upper-cases the values; compare without regard to case so
		// the filter matches however the worker stored the type (LAMBDA, lambda_function, …).
		out = append(out, fragment{facetType, "upper(p.identity_type) = ANY(" + b.add(pq.Array(f.Types)) + "::text[])"})
	}
	if len(f.Pillars) > 0 {
		out = append(out, fragment{facetPillar, "p.weakest_pillar = ANY(" + b.add(pq.Array(f.Pillars)) + "::text[])"})
	}
	if len(f.FailingChecks) > 0 {
		out = append(out, fragment{facetOther, fmt.Sprintf(`EXISTS (SELECT 1 FROM %s c
			WHERE c.tenant_id = p.tenant_id AND c.identity_id = p.identity_id
			  AND c.rule_id = ANY(%s::text[]) AND c.status = 'FAIL')`, s.Table(tIdentityChecks), b.add(pq.Array(f.FailingChecks)))})
	}
	if c := changedCondition(f.Changed, f.Window); c != "" {
		out = append(out, fragment{facetChanged, c})
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		ph := b.add("%" + escapeLike(q) + "%")
		out = append(out, fragment{facetOther, `(p.identity_name ILIKE ` + ph + ` ESCAPE '\' OR p.identity_arn ILIKE ` + ph + ` ESCAPE '\')`})
	}
	return out
}

// changedCondition is the score-change test for ?changed=down|up|same.
func changedCondition(kind string, w models.Window) string {
	col := "p." + w.ChangeColumn
	switch kind {
	case models.ChangedDown:
		return col + " < 0"
	case models.ChangedUp:
		return col + " > 0"
	case models.ChangedSame:
		return col + " = 0"
	}
	return ""
}

// whereExcept joins the tenant test and every fragment except those of one facet.
func whereExcept(frags []fragment, skipFacet string) string {
	parts := []string{"p.tenant_id = $1"}
	for _, fr := range frags {
		if skipFacet != "" && fr.facet == skipFacet {
			continue
		}
		parts = append(parts, fr.sql)
	}
	return strings.Join(parts, " AND ")
}

// ─────────────────────────────────────────────────────────────────────────
// Sorting and keyset pagination
// ─────────────────────────────────────────────────────────────────────────

type sortSpec struct {
	// expr is the sort expression with %s standing for the table alias.
	expr string
	desc bool
}

func sortFor(key string, w models.Window) sortSpec {
	switch key {
	case models.SortScoreDesc:
		return sortSpec{expr: "%s.score", desc: true}
	case models.SortFailed:
		return sortSpec{expr: "COALESCE(%s.checks_failed, 0)", desc: true}
	case models.SortChange:
		// biggest drop first; identities without history go last
		return sortSpec{expr: "COALESCE(%s." + w.ChangeColumn + ", 1000000)"}
	case models.SortName:
		return sortSpec{expr: "COALESCE(%s.identity_name, '')"}
	}
	return sortSpec{expr: "%s.score"}
}

func (sp sortSpec) on(alias string) string { return fmt.Sprintf(sp.expr, alias) }

func (sp sortSpec) dir() string {
	if sp.desc {
		return "DESC"
	}
	return "ASC"
}

func (sp sortSpec) cmp() string {
	if sp.desc {
		return "<"
	}
	return ">"
}

// identityCursor carries the sort key and the last identity of the page. The
// next page is found with a row comparison against that identity's own row,
// so it works for any column type and needs no value in the cursor.
type identityCursor struct {
	Sort string `json:"s"`
	ID   string `json:"id"`
}

// ─────────────────────────────────────────────────────────────────────────
// Row loading
// ─────────────────────────────────────────────────────────────────────────

// IdentityRow is one posture row as read for the list and the export.
type IdentityRow struct {
	IdentityID    string
	Name          string
	Type          string
	Class         sql.NullString
	AccountID     string
	AccountName   string
	Score         sql.NullFloat64
	Band          string
	Grade         string
	WeakestPillar sql.NullString
	Failed        sql.NullInt64
	Critical      sql.NullInt64
	High          sql.NullInt64
	Medium        sql.NullInt64
	Low           sql.NullInt64
	CredType      sql.NullString
	CredAgeDays   sql.NullInt64
	Change        sql.NullFloat64
	OpenAlerts    sql.NullInt64
	ARN           string
}

const identityRowColumns = `p.identity_id, COALESCE(p.identity_name, ''), COALESCE(p.identity_type, ''), p.functional_class,
	COALESCE(p.account_id, ''), COALESCE(p.account_name, ''), p.score, COALESCE(p.band, ''), COALESCE(p.grade, ''),
	p.weakest_pillar, p.checks_failed, p.failed_critical, p.failed_high, p.failed_medium, p.failed_low,
	p.credential_type, p.credential_age_days, p.%s, p.open_alerts, COALESCE(p.identity_arn, '')`

// queryIdentityPage reads up to `limit` rows after `afterID` (empty for the
// first page) in the filter's sort order.
func (r *IdentitiesListRepository) queryIdentityPage(ctx context.Context, s *scope.DataScope, f models.IdentityFilter, afterID string, limit int) ([]IdentityRow, error) {
	b := newQueryBuilder(s)
	frags := b.buildFragments(s, f)
	where := whereExcept(frags, "")
	sp := sortFor(f.Sort, f.Window)
	table := s.Table(tIdentityPosture)

	if afterID != "" {
		ph := b.add(afterID)
		where += fmt.Sprintf(` AND (%s, p.identity_id) %s (SELECT %s, x.identity_id FROM %s x WHERE x.tenant_id = $1 AND x.identity_id = %s)`,
			sp.on("p"), sp.cmp(), sp.on("x"), table, ph)
	}
	limitPH := b.add(limit)
	q := fmt.Sprintf(`SELECT `+identityRowColumns+`
		FROM %s p
		WHERE %s
		ORDER BY %s %s, p.identity_id %s
		LIMIT %s`, f.Window.ChangeColumn, table, where, sp.on("p"), sp.dir(), sp.dir(), limitPH)

	rows, err := s.DB.QueryContext(ctx, q, b.args...)
	if isUndefinedTable(err) { // no posture evaluation has run for this tenant yet
		return []IdentityRow{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query identities: %w", err)
	}
	defer rows.Close()
	out := make([]IdentityRow, 0, limit)
	for rows.Next() {
		var r IdentityRow
		if err := rows.Scan(&r.IdentityID, &r.Name, &r.Type, &r.Class, &r.AccountID, &r.AccountName, &r.Score, &r.Band, &r.Grade,
			&r.WeakestPillar, &r.Failed, &r.Critical, &r.High, &r.Medium, &r.Low,
			&r.CredType, &r.CredAgeDays, &r.Change, &r.OpenAlerts, &r.ARN); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// List implements GET /identities. It returns the page and the cursor of the
// next page (nil on the last page).
func (r *IdentitiesListRepository) List(ctx context.Context, s *scope.DataScope, f models.IdentityFilter) ([]models.IdentityListItem, *string, error) {
	if err := need(s); err != nil {
		return nil, nil, err
	}
	f.Limit = clampLimit(f.Limit)
	if f.Sort == "" {
		f.Sort = models.SortScore
	}
	afterID := ""
	if strings.TrimSpace(f.Cursor) != "" {
		var c identityCursor
		if err := decodeCursor(f.Cursor, &c); err != nil || c.ID == "" || c.Sort != f.Sort {
			return nil, nil, ErrInvalidCursor
		}
		afterID = c.ID
	}
	rows, err := r.queryIdentityPage(ctx, s, f, afterID, f.Limit+1)
	if err != nil {
		return nil, nil, err
	}
	var next *string
	if len(rows) > f.Limit {
		rows = rows[:f.Limit]
		c := encodeCursor(identityCursor{Sort: f.Sort, ID: rows[len(rows)-1].IdentityID})
		next = &c
	}
	items := make([]models.IdentityListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.toItem(f.Window))
	}
	return items, next, nil
}

func (row IdentityRow) toItem(w models.Window) models.IdentityListItem {
	item := models.IdentityListItem{
		IdentityID:      row.IdentityID,
		Name:            row.Name,
		ARN:             row.ARN,
		Type:            row.Type,
		FunctionalClass: nullStr(row.Class),
		Account:         models.AccountRef{ID: row.AccountID, Name: row.AccountName},
		Score:           nullFloat(row.Score),
		Band:            row.Band,
		Grade:           row.Grade,
		WeakestPillar:   nullStr(row.WeakestPillar),
		Failed: models.FailedCounts{
			Total: intVal(row.Failed), Critical: intVal(row.Critical), High: intVal(row.High),
			Medium: intVal(row.Medium), Low: intVal(row.Low),
		},
		Change: models.Change{Window: w.Label, Value: nullFloat(row.Change)},
	}
	if row.CredType.Valid && row.CredType.String != "" {
		item.Credential = &models.CredentialBrief{Type: row.CredType.String, AgeDays: nullInt(row.CredAgeDays)}
	}
	return item
}
