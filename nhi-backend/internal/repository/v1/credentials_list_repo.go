package v1

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/scope"
)

type CredentialsRepository struct{}

func NewCredentialsRepository() *CredentialsRepository {
	return &CredentialsRepository{}
}

// credentialSortColumns maps the public `sort_by` query value to the column
// name exposed by the final SELECT (see GetCredentialsList). Only columns
// that are actually present in the final result set may be used — the
// ORDER BY runs against that result set, not against the raw `credentials`
// table, because several sortable fields (age_days, idle_days,
// bound_identities_count, ...) are computed.
var credentialSortColumns = map[string]string{
	"name":                   "name",
	"arn":                    "arn",
	"credential_type":        "credential_type",
	"account_id":             "account_id",
	"region":                 "region",
	"status":                 "status",
	"last_used_at":           "last_used_at",
	"last_rotated_at":        "last_rotated_at",
	"expires_at":             "expires_at",
	"age_days":               "age_days",
	"idle_days":              "idle_days",
	"days_until_expiry":      "days_until_expiry",
	"bound_identities_count": "bound_identities_count",
}

// derivedCredentialSorts are the sort keys whose values come from CloudTrail
// (ClickHouse), so they can only be ordered in Go.
var derivedCredentialSorts = map[string]bool{
	"last_used_at": true,
	"idle_days":    true,
}

// credentialListRow is a list item plus the keys used to look its use up.
type credentialListRow struct {
	item models.CredentialListItem
	keys []string // credential ARN, plus the key ID for access keys
}

// credentialActivityKeys returns what identifies a credential in CloudTrail:
// its ARN (the session issuer for roles) and, for access keys, the key ID
// (which is the credential's name).
func credentialActivityKeys(arn, name, credentialType string) []string {
	var keys []string
	if arn != "" {
		keys = append(keys, arn)
	}
	if strings.EqualFold(credentialType, "access_key") && name != "" {
		keys = append(keys, name)
	}
	return keys
}

// GetCredentialsList implements "ENDPOINT 6: Credentials List"
// (GET /api/v1/credentials) from the AWS-NHI Updated API spec.
//
// It is scoped by the shared resolver to the user's saved account/region
// selection, or the tenant's configured defaults when no settings row exists.
//
// last_used_at and idle_days come from the ActivitySource (ClickHouse) in one
// bulk call. Filtering by min_idle_days or sorting by last_used_at / idle_days
// (the default sort is last_used_at) is done in Go over all matching rows;
// otherwise Postgres pages and only the returned page is enriched.
func (r *CredentialsRepository) GetCredentialsList(ctx context.Context, resolved *scope.DataScope, f models.CredentialListFilter, src ActivitySource) (*models.CredentialListResponse, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	src = orNoActivity(src)
	tenantDB := resolved.DB
	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 200 {
		pageSize = 200
	}

	resp := &models.CredentialListResponse{
		Pagination: models.CredentialListPagination{Page: page, PageSize: pageSize},
		Data:       make([]models.CredentialListItem, 0),
	}

	// The user/tenant account and region filters are resolved centrally.
	args := resolved.QueryArgs()
	addArg := func(v interface{}) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	// ── inner (raw-column) filters — pushed down onto indexed columns ───
	innerWhere := []string{"c.account_id = ANY($1::text[])", "c.region = ANY($2::text[])", "c.deleted_at IS NULL"}

	if s := strings.TrimSpace(f.Search); s != "" {
		ph := addArg("%" + escapeLikePattern(s) + "%")
		innerWhere = append(innerWhere, fmt.Sprintf(`(c.name ILIKE %[1]s ESCAPE '\' OR c.arn ILIKE %[1]s ESCAPE '\')`, ph))
	}
	if len(f.CredentialTypes) > 0 {
		innerWhere = append(innerWhere, fmt.Sprintf("LOWER(c.credential_type) = ANY(%s)", addArg(pq.Array(lowerTrimAll(f.CredentialTypes)))))
	}
	if len(f.Statuses) > 0 {
		innerWhere = append(innerWhere, fmt.Sprintf("LOWER(c.status) = ANY(%s)", addArg(pq.Array(lowerTrimAll(f.Statuses)))))
	}
	if s := strings.TrimSpace(f.OwnerPrincipalARN); s != "" {
		innerWhere = append(innerWhere, fmt.Sprintf("c.owner_principal_arn = %s", addArg(s)))
	}
	if f.RotationEnabled != nil {
		innerWhere = append(innerWhere, fmt.Sprintf("COALESCE(c.rotation_enabled, false) = %s", addArg(*f.RotationEnabled)))
	}
	if f.MaxDaysUntilExpiry != nil {
		innerWhere = append(innerWhere, fmt.Sprintf(
			"c.expires_at IS NOT NULL AND c.expires_at <= NOW() + (%s::int * INTERVAL '1 day')", addArg(*f.MaxDaysUntilExpiry)))
	}

	// ── outer (computed-column) filters ──────────────────────────────────
	var outerWhere []string
	if f.IsOrphaned != nil {
		outerWhere = append(outerWhere, fmt.Sprintf("is_orphaned = %s", addArg(*f.IsOrphaned)))
	}
	if f.RotationOverdue != nil {
		outerWhere = append(outerWhere, fmt.Sprintf("rotation_overdue = %s", addArg(*f.RotationOverdue)))
	}
	if f.HasExternalTrust != nil {
		outerWhere = append(outerWhere, fmt.Sprintf("has_external_trust = %s", addArg(*f.HasExternalTrust)))
	}
	outerWhereSQL := ""
	if len(outerWhere) > 0 {
		outerWhereSQL = "WHERE " + strings.Join(outerWhere, " AND ")
	}

	// ── sort ──────────────────────────────────────────────────────────
	sortKey := strings.ToLower(strings.TrimSpace(f.SortBy))
	sortCol, ok := credentialSortColumns[sortKey]
	if !ok {
		sortKey, sortCol = "last_used_at", "last_used_at"
	}
	desc := !strings.EqualFold(strings.TrimSpace(f.SortOrder), "asc")
	sortDir := "DESC"
	if !desc {
		sortDir = "ASC"
	}

	derivedSort := derivedCredentialSorts[sortKey]
	inGo := derivedSort || f.MinIdleDays != nil
	sqlSortCol, sqlSortDir := sortCol, sortDir
	if derivedSort {
		sqlSortCol, sqlSortDir = "name", "ASC" // stable base order; Go re-sorts
	}
	limitSQL := ""
	if !inGo {
		limitSQL = fmt.Sprintf("LIMIT %s OFFSET %s", addArg(pageSize), addArg((page-1)*pageSize))
	}

	// "bound" counts DISTINCT identities bound to each credential. Note:
	// identity_credentials has NO "is_active"/status column in the real
	// schema — every row is simply a discovered binding, so presence
	// of a row is sufficient to count a credential as bound.
	//
	// has_external_trust is a SQL regex approximation of the precise,
	// per-statement Effect=Allow trust-policy evaluation implemented in
	// internal/repository/inventory/trust_policy.go#computeTrustPolicyRisk.
	// It flags any ARN embedded in trust_policy whose account id differs from
	// the credential's own account_id, ignoring Allow/Deny — an accepted
	// simplification for a list view (the precise computation remains
	// available via the single-credential detail endpoint).
	query := fmt.Sprintf(`
		WITH bound AS (
			SELECT ic.credential_id::text AS credential_id,
			       COUNT(DISTINCT ic.identity_id)::int AS cnt
			FROM %[1]s.identity_credentials ic
			JOIN %[1]s.identities i ON i.id::text = ic.identity_id::text AND i.deleted_at IS NULL
			WHERE i.account_id = ANY($1::text[]) AND i.region = ANY($2::text[])
			GROUP BY ic.credential_id
		),
		scoped AS (
			SELECT
				c.id::text                       AS id,
				COALESCE(c.name, '')              AS name,
				COALESCE(c.arn, '')                AS arn,
				COALESCE(c.credential_type, '')    AS credential_type,
				COALESCE(c.account_id, '')         AS account_id,
				COALESCE(c.region, '')             AS region,
				COALESCE(c.status, '')             AS status,
				c.owner_principal_arn               AS owner_principal_arn,
				COALESCE(b.cnt, 0)                  AS bound_identities_count,
				(COALESCE(b.cnt, 0) = 0)            AS is_orphaned,
				c.last_rotated_at                   AS last_rotated_at,
				COALESCE(c.rotation_enabled, false) AS rotation_enabled,
				(
					COALESCE(c.rotation_enabled, false)
					AND c.rotation_interval_days IS NOT NULL
					AND COALESCE(c.last_rotated_at, c.created_at_aws) IS NOT NULL
					AND NOW() > COALESCE(c.last_rotated_at, c.created_at_aws)
						+ (c.rotation_interval_days || ' days')::interval
				)                                   AS rotation_overdue,
				CASE WHEN c.created_at_aws IS NULL THEN NULL
				     ELSE FLOOR(EXTRACT(EPOCH FROM (NOW() - c.created_at_aws)) / 86400)::int
				END                                  AS age_days,
				c.expires_at                         AS expires_at,
				CASE WHEN c.expires_at IS NULL THEN NULL
				     ELSE FLOOR(EXTRACT(EPOCH FROM (c.expires_at - NOW())) / 86400)::int
				END                                  AS days_until_expiry,
				(
					c.trust_policy IS NOT NULL
					AND c.trust_policy::text ~ ('arn:aws:iam::(?!' || COALESCE(c.account_id, '') || ')[0-9]{12}:')
				)                                    AS has_external_trust,
				c.permission_boundary_arn            AS permission_boundary_arn
			FROM %[1]s.credentials c
			LEFT JOIN bound b ON b.credential_id = c.id::text
			WHERE %[2]s
		)
		SELECT
			id, name, arn, credential_type, account_id, region, status, owner_principal_arn,
			bound_identities_count, is_orphaned, last_rotated_at,
			rotation_enabled, rotation_overdue, age_days, expires_at,
			days_until_expiry, has_external_trust, permission_boundary_arn,
			COUNT(*) OVER()::int AS total_count
		FROM scoped
		%[3]s
		ORDER BY %[4]s %[5]s NULLS LAST, id
		%[6]s`,
		resolved.QSchema(), strings.Join(innerWhere, " AND "), outerWhereSQL, sqlSortCol, sqlSortDir, limitSQL)

	rows, err := tenantDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query credentials list: %w", err)
	}
	defer rows.Close()

	var list []credentialListRow
	sqlTotal := 0
	for rows.Next() {
		var row credentialListRow
		item := &row.item
		var ownerArn, permBoundary sql.NullString
		var ageDays, daysUntilExpiry sql.NullInt64

		if err := rows.Scan(
			&item.ID, &item.Name, &item.ARN, &item.CredentialType, &item.AccountID,
			&item.Region, &item.Status, &ownerArn, &item.BoundIdentitiesCount, &item.IsOrphaned,
			&item.LastRotatedAt, &item.RotationEnabled, &item.RotationOverdue,
			&ageDays, &item.ExpiresAt, &daysUntilExpiry,
			&item.HasExternalTrust, &permBoundary, &sqlTotal,
		); err != nil {
			return nil, fmt.Errorf("scan credential row: %w", err)
		}

		if ownerArn.Valid && ownerArn.String != "" {
			v := ownerArn.String
			item.OwnerPrincipalARN = &v
		}
		if permBoundary.Valid && permBoundary.String != "" {
			v := permBoundary.String
			item.PermissionBoundaryARN = &v
		}
		if ageDays.Valid {
			v := int(ageDays.Int64)
			item.AgeDays = &v
		}
		if daysUntilExpiry.Valid {
			v := int(daysUntilExpiry.Int64)
			item.DaysUntilExpiry = &v
		}
		row.keys = credentialActivityKeys(item.ARN, item.Name, item.CredentialType)
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate credentials list: %w", err)
	}

	attachCredentialActivity(ctx, list, src, time.Now())

	total := sqlTotal
	if inGo {
		if f.MinIdleDays != nil {
			kept := list[:0]
			for _, row := range list {
				// Same rule as the dashboard's stale count: idle since the last
				// recorded use, or since creation when it was never used.
				if idle := effectiveIdleDays(row.item.IdleDays, row.item.AgeDays); idle != nil && *idle >= *f.MinIdleDays {
					kept = append(kept, row)
				}
			}
			list = kept
		}
		if derivedSort {
			sortCredentialRows(list, sortKey, desc)
		}
		total = len(list)
		from := (page - 1) * pageSize
		if from > len(list) {
			from = len(list)
		}
		to := from + pageSize
		if to > len(list) {
			to = len(list)
		}
		list = list[from:to]
	}

	for _, row := range list {
		resp.Data = append(resp.Data, row.item)
	}
	resp.Pagination.TotalCount = total
	if pageSize > 0 {
		resp.Pagination.TotalPages = (total + pageSize - 1) / pageSize
	}
	resp.ActivityDataAvailable = src.Available() && !src.Degraded()
	return resp, nil
}

// attachCredentialActivity fills last_used_at and idle_days from the
// ActivitySource with a single bulk call for all rows.
func attachCredentialActivity(ctx context.Context, rows []credentialListRow, src ActivitySource, now time.Time) {
	if len(rows) == 0 || !src.Available() {
		return
	}
	var keys []string
	for _, row := range rows {
		keys = append(keys, row.keys...)
	}
	seen := src.LastSeen(ctx, keys)
	for i := range rows {
		item := &rows[i].item
		item.LastUsedAt = latestOf(seen, rows[i].keys)
		item.IdleDays = wholeDaysSince(item.LastUsedAt, now)
	}
}

// sortCredentialRows orders rows by a CloudTrail-derived key. Rows without a
// value always come last, whichever the direction; ties break on name.
func sortCredentialRows(rows []credentialListRow, key string, desc bool) {
	val := func(r credentialListRow) (float64, bool) {
		if key == "idle_days" {
			if r.item.IdleDays == nil {
				return 0, false
			}
			return float64(*r.item.IdleDays), true
		}
		if r.item.LastUsedAt == nil {
			return 0, false
		}
		return float64(r.item.LastUsedAt.Unix()), true
	}
	sort.SliceStable(rows, func(i, j int) bool {
		vi, oki := val(rows[i])
		vj, okj := val(rows[j])
		switch {
		case oki != okj:
			return oki
		case oki && vi != vj:
			if desc {
				return vi > vj
			}
			return vi < vj
		}
		return rows[i].item.Name < rows[j].item.Name
	})
}

func lowerTrimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
