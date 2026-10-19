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

type IdentitiesListRepository struct{}

func NewIdentitiesListRepository() *IdentitiesListRepository {
	return &IdentitiesListRepository{}
}

// identitySortColumns maps the public `sort_by` query value to the column
// name exposed by the final SELECT (see GetIdentitiesList). Only columns
// present in the final result set may be used.
var identitySortColumns = map[string]string{
	"name":                       "name",
	"arn":                        "arn",
	"identity_type":              "identity_type",
	"functional_class":           "functional_class",
	"account_id":                 "account_id",
	"region":                     "region",
	"environment":                "environment",
	"owner":                      "owner",
	"status":                     "status",
	"last_activity_at":           "last_activity_at",
	"created_at_aws":             "created_at_aws",
	"age_days":                   "age_days",
	"idle_days":                  "idle_days",
	"bound_credentials_count":    "bound_credentials_count",
	"permission_utilization_pct": "permission_utilization_pct",
}

// derivedIdentitySorts are the sort keys whose values come from CloudTrail
// (ClickHouse) rather than Postgres, so they can only be ordered in Go.
var derivedIdentitySorts = map[string]bool{
	"last_activity_at":           true,
	"idle_days":                  true,
	"permission_utilization_pct": true,
}

// identityListRow is a list item plus the keys used to look its activity up.
type identityListRow struct {
	item    models.IdentityListItem
	keys    []string // identity ARN, bound credential ARNs, bound access key IDs
	credKey []string // bound credential ARNs and access key IDs only
	granted int64
}

// GetIdentitiesList implements "ENDPOINT 4: Identities List"
// (GET /api/v1/identities) from the AWS-NHI Updated API spec.
//
// Postgres supplies everything except the CloudTrail-derived fields.
// last_activity_at / idle_days, created_by and permission_utilization_pct
// come from the ActivitySource (ClickHouse): the identity ARN, its bound
// credential ARNs and its access key IDs are looked up in one bulk call per
// page, never one query per row.
//
// When a request filters or sorts on those fields (min_idle_days, or sorting
// by last_activity_at / idle_days / permission_utilization_pct — the default
// sort is last_activity_at), the SQL returns every matching row, the activity
// is attached, and the filter, sort and paging are applied here. Otherwise
// Postgres pages as before and only the returned page is enriched.
//
// "bound_credentials_count" counts DISTINCT credentials bound to each
// identity via identity_credentials. permission_utilization_pct is
// (distinct actions used across the identity's credentials, from CloudTrail)
// / (SUM of effective_permissions.granted_count) * 100, rounded.
func (r *IdentitiesListRepository) GetIdentitiesList(ctx context.Context, resolved *scope.DataScope, f models.IdentityListFilter, src ActivitySource) (*models.IdentityListResponse, error) {
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
	if pageSize > 100 {
		pageSize = 100
	}

	resp := &models.IdentityListResponse{
		Pagination: models.IdentityListPagination{Page: page, PageSize: pageSize},
		Data:       make([]models.IdentityListItem, 0),
	}

	args := resolved.QueryArgs()
	addArg := func(v interface{}) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	where := []string{"i.account_id = ANY($1::text[])", "i.region = ANY($2::text[])", "i.deleted_at IS NULL"}

	if s := strings.TrimSpace(f.Search); s != "" {
		ph := addArg("%" + escapeLikePattern(s) + "%")
		where = append(where, fmt.Sprintf(`(i.name ILIKE %[1]s ESCAPE '\' OR i.arn ILIKE %[1]s ESCAPE '\' OR i.tags::text ILIKE %[1]s ESCAPE '\')`, ph))
	}
	if len(f.IdentityTypes) > 0 {
		where = append(where, fmt.Sprintf("LOWER(i.identity_type) = ANY(%s)", addArg(pq.Array(lowerTrimAll(f.IdentityTypes)))))
	}
	if len(f.FunctionalClasses) > 0 {
		where = append(where, fmt.Sprintf("LOWER(i.functional_class) = ANY(%s)", addArg(pq.Array(lowerTrimAll(f.FunctionalClasses)))))
	}
	if len(f.Environments) > 0 {
		where = append(where, fmt.Sprintf("LOWER(i.environment) = ANY(%s)", addArg(pq.Array(lowerTrimAll(f.Environments)))))
	}
	if len(f.Statuses) > 0 {
		where = append(where, fmt.Sprintf("LOWER(i.status) = ANY(%s)", addArg(pq.Array(lowerTrimAll(f.Statuses)))))
	}
	if s := strings.TrimSpace(f.Owner); s != "" {
		where = append(where, fmt.Sprintf("i.owner = %s", addArg(s)))
	}
	if s := strings.TrimSpace(f.VPCID); s != "" {
		where = append(where, fmt.Sprintf("i.vpc_id = %s", addArg(s)))
	}
	if f.IsPubliclyAccessible != nil {
		where = append(where, fmt.Sprintf("COALESCE(i.is_publicly_accessible, false) = %s", addArg(*f.IsPubliclyAccessible)))
	}

	sortKey := strings.ToLower(strings.TrimSpace(f.SortBy))
	sortCol, ok := identitySortColumns[sortKey]
	if !ok {
		sortKey, sortCol = "last_activity_at", "last_activity_at"
	}
	desc := !strings.EqualFold(strings.TrimSpace(f.SortOrder), "asc")
	sortDir := "DESC"
	if !desc {
		sortDir = "ASC"
	}

	derivedSort := derivedIdentitySorts[sortKey]
	inGo := derivedSort || f.MinIdleDays != nil
	sqlSortCol, sqlSortDir := sortCol, sortDir
	if derivedSort {
		sqlSortCol, sqlSortDir = "name", "ASC" // stable base order; Go re-sorts
	}
	limitSQL := ""
	if !inGo {
		limitSQL = fmt.Sprintf("LIMIT %s OFFSET %s", addArg(pageSize), addArg((page-1)*pageSize))
	}

	query := fmt.Sprintf(`
		WITH bound AS (
			SELECT ic.identity_id::text AS identity_id,
			       COUNT(DISTINCT ic.credential_id)::int AS cnt
			FROM %[1]s.identity_credentials ic
			JOIN %[1]s.credentials c ON c.id::text = ic.credential_id::text AND c.deleted_at IS NULL
			WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
			GROUP BY ic.identity_id
		),
		perm_grant AS (
			SELECT ic.identity_id::text AS identity_id,
			       COALESCE(SUM(ep.granted_count), 0)::bigint AS granted
			FROM %[1]s.identity_credentials ic
			JOIN %[1]s.credentials c ON c.id::text = ic.credential_id::text AND c.deleted_at IS NULL
			JOIN %[1]s.effective_permissions ep
			  ON ep.credential_id::text = ic.credential_id::text
			WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
			GROUP BY ic.identity_id
		),
		bound_keys AS (
			SELECT ic.identity_id::text AS identity_id,
			       ARRAY_AGG(DISTINCT c.arn) FILTER (WHERE c.arn IS NOT NULL AND c.arn <> '') AS arns,
			       ARRAY_AGG(DISTINCT c.name) FILTER (WHERE LOWER(c.credential_type) = 'access_key' AND c.name IS NOT NULL AND c.name <> '') AS access_keys
			FROM %[1]s.identity_credentials ic
			JOIN %[1]s.credentials c ON c.id::text = ic.credential_id::text AND c.deleted_at IS NULL
			WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
			GROUP BY ic.identity_id
		),
		scoped AS (
			SELECT
				i.id::text                         AS id,
				COALESCE(i.name, '')                AS name,
				COALESCE(i.arn, '')                  AS arn,
				COALESCE(i.identity_type, '')         AS identity_type,
				i.functional_class                    AS functional_class,
				COALESCE(i.account_id, '')             AS account_id,
				COALESCE(i.region, '')                  AS region,
				i.environment                            AS environment,
				i.owner                                   AS owner,
				COALESCE(i.status, '')                     AS status,
				COALESCE(i.is_publicly_accessible, false)   AS is_publicly_accessible,
				i.vpc_id                                     AS vpc_id,
				COALESCE(b.cnt, 0)                            AS bound_credentials_count,
				i.created_at_aws                                AS created_at_aws,
				CASE WHEN i.created_at_aws IS NULL THEN NULL
				     ELSE FLOOR(EXTRACT(EPOCH FROM (NOW() - i.created_at_aws)) / 86400)::int
				END                                               AS age_days,
				COALESCE(pg.granted, 0)                            AS granted,
				COALESCE(bk.arns, '{}'::text[])                     AS cred_arns,
				COALESCE(bk.access_keys, '{}'::text[])               AS access_keys
			FROM %[1]s.identities i
			LEFT JOIN bound b ON b.identity_id = i.id::text
			LEFT JOIN perm_grant pg ON pg.identity_id = i.id::text
			LEFT JOIN bound_keys bk ON bk.identity_id = i.id::text
			WHERE %[2]s
		)
		SELECT
			id, name, arn, identity_type, functional_class, account_id, region,
			environment, owner, status, is_publicly_accessible, vpc_id,
			bound_credentials_count, created_at_aws, age_days, granted, cred_arns, access_keys,
			COUNT(*) OVER()::int AS total_count
		FROM scoped
		ORDER BY %[3]s %[4]s NULLS LAST, id
		%[5]s`,
		resolved.QSchema(), strings.Join(where, " AND "), sqlSortCol, sqlSortDir, limitSQL)

	rows, err := tenantDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query identities list: %w", err)
	}
	defer rows.Close()

	var list []identityListRow
	sqlTotal := 0
	for rows.Next() {
		var row identityListRow
		item := &row.item
		var functionalClass, environment, owner, vpcID sql.NullString
		var ageDays sql.NullInt64
		var credARNs, accessKeys []string

		if err := rows.Scan(
			&item.ID, &item.Name, &item.ARN, &item.IdentityType, &functionalClass,
			&item.AccountID, &item.Region, &environment, &owner, &item.Status,
			&item.IsPubliclyAccessible, &vpcID, &item.BoundCredentialsCount,
			&item.CreatedAtAWS, &ageDays, &row.granted, pq.Array(&credARNs), pq.Array(&accessKeys), &sqlTotal,
		); err != nil {
			return nil, fmt.Errorf("scan identity row: %w", err)
		}

		if functionalClass.Valid && functionalClass.String != "" {
			v := functionalClass.String
			item.FunctionalClass = &v
		}
		if environment.Valid && environment.String != "" {
			v := environment.String
			item.Environment = &v
		}
		if owner.Valid && owner.String != "" {
			v := owner.String
			item.Owner = &v
		}
		if vpcID.Valid && vpcID.String != "" {
			v := vpcID.String
			item.VPCID = &v
		}
		if ageDays.Valid {
			v := int(ageDays.Int64)
			item.AgeDays = &v
		}
		row.credKey = append(append([]string(nil), credARNs...), accessKeys...)
		row.keys = append([]string{item.ARN}, row.credKey...)
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identities list: %w", err)
	}

	attachIdentityActivity(ctx, list, src, time.Now())

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
			sortIdentityRows(list, sortKey, desc)
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

// attachIdentityActivity fills last_activity_at, idle_days, created_by and
// permission_utilization_pct from the ActivitySource, using three bulk calls
// for the whole set of rows.
func attachIdentityActivity(ctx context.Context, rows []identityListRow, src ActivitySource, now time.Time) {
	if len(rows) == 0 || !src.Available() {
		return
	}
	var allKeys, arns, credKeys []string
	for _, row := range rows {
		allKeys = append(allKeys, row.keys...)
		arns = append(arns, row.item.ARN)
		credKeys = append(credKeys, row.credKey...)
	}
	seen := src.LastSeen(ctx, allKeys)
	creators := src.Creators(ctx, arns)
	used := src.UsedActionSets(ctx, credKeys, usageSince(now))

	for i := range rows {
		item := &rows[i].item
		item.LastActivityAt = latestOf(seen, rows[i].keys)
		item.IdleDays = wholeDaysSince(item.LastActivityAt, now)
		if c := creators[item.ARN]; c != "" {
			v := c
			item.CreatedBy = &v
		}
		item.PermissionUtilizationPct = roundPctInt(distinctUsedCount(used, rows[i].credKey), int(rows[i].granted))
	}
}

// sortIdentityRows orders rows by a CloudTrail-derived key. Rows without a
// value always come last, whichever the direction; ties break on name.
func sortIdentityRows(rows []identityListRow, key string, desc bool) {
	val := func(r identityListRow) (float64, bool) {
		switch key {
		case "last_activity_at":
			if r.item.LastActivityAt == nil {
				return 0, false
			}
			return float64(r.item.LastActivityAt.Unix()), true
		case "idle_days":
			if r.item.IdleDays == nil {
				return 0, false
			}
			return float64(*r.item.IdleDays), true
		default: // permission_utilization_pct
			if r.item.PermissionUtilizationPct == nil {
				return 0, false
			}
			return float64(*r.item.PermissionUtilizationPct), true
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		vi, oki := val(rows[i])
		vj, okj := val(rows[j])
		switch {
		case oki != okj:
			return oki // values before nulls
		case oki && vi != vj:
			if desc {
				return vi > vj
			}
			return vi < vj
		}
		return rows[i].item.Name < rows[j].item.Name
	})
}
