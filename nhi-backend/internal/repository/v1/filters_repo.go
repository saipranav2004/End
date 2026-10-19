package v1

import (
	"context"
	"database/sql"
	"fmt"

	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/scope"
)

type FilterRepository struct{}

func NewFilterRepository() *FilterRepository { return &FilterRepository{} }

func (r *FilterRepository) GetFilterOptions(ctx context.Context, resolved *scope.DataScope) (*models.FilterResponse, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	result := &models.FilterResponse{
		Accounts:          []models.AccountFilter{},
		Regions:           []models.RegionFilter{},
		IdentityTypes:     []models.TypeCountFilter{},
		FunctionalClasses: []models.ClassCountFilter{},
		CredentialTypes:   []models.TypeCountFilter{},
		Environments:      []models.EnvironmentCountFilter{},
		Statuses:          []models.StatusCountFilter{},
	}
	db := resolved.DB

	accountsQuery := fmt.Sprintf(`
		WITH registered AS (
			SELECT account_id, BOOL_OR(COALESCE(is_default, false)) AS is_default
			FROM %s.accounts
			WHERE tenant_id = $1::uuid AND account_id IS NOT NULL AND account_id <> ''
			GROUP BY account_id
		), identity_counts AS (
			SELECT i.account_id, COUNT(*)::int AS count
			FROM %s.identities i JOIN registered r ON r.account_id = i.account_id
			WHERE i.deleted_at IS NULL
			GROUP BY i.account_id
		), credential_counts AS (
			SELECT c.account_id, COUNT(*)::int AS count
			FROM %s.credentials c JOIN registered r ON r.account_id = c.account_id
			WHERE c.deleted_at IS NULL
			GROUP BY c.account_id
		)
		SELECT r.account_id, r.is_default, COALESCE(i.count, 0), COALESCE(c.count, 0)
		FROM registered r
		LEFT JOIN identity_counts i ON i.account_id = r.account_id
		LEFT JOIN credential_counts c ON c.account_id = r.account_id
		ORDER BY r.is_default DESC, r.account_id`,
		resolved.QSchema(), resolved.QSchema(), resolved.QSchema())
	rows, err := db.QueryContext(ctx, accountsQuery, resolved.TenantID)
	if err != nil {
		return nil, fmt.Errorf("query registered account filters: %w", err)
	}
	for rows.Next() {
		var item models.AccountFilter
		if err := rows.Scan(&item.AccountID, &item.IsDefault, &item.IdentityCount, &item.CredentialCount); err != nil {
			rows.Close()
			return nil, err
		}
		result.Accounts = append(result.Accounts, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	regionsQuery := fmt.Sprintf(`
		SELECT r, COUNT(DISTINCT i.id)::int
		FROM %s.accounts a
		CROSS JOIN LATERAL unnest(a.regions) AS r
		LEFT JOIN %s.identities i
		  ON i.account_id = a.account_id AND i.region = r
		 AND i.deleted_at IS NULL
		WHERE a.tenant_id = $1::uuid AND r IS NOT NULL AND r <> ''
		GROUP BY r ORDER BY r`, resolved.QSchema(), resolved.QSchema())
	rows, err = db.QueryContext(ctx, regionsQuery, resolved.TenantID)
	if err != nil {
		return nil, fmt.Errorf("query tenant regions: %w", err)
	}
	for rows.Next() {
		var item models.RegionFilter
		if err := rows.Scan(&item.Region, &item.Count); err != nil {
			rows.Close()
			return nil, err
		}
		result.Regions = append(result.Regions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	queryGrouped := func(query string, scanRow func(*sql.Rows) error, label string) error {
		rows, err := db.QueryContext(ctx, query, resolved.QueryArgs()...)
		if err != nil {
			return fmt.Errorf("query %s: %w", label, err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scanRow(rows); err != nil {
				return fmt.Errorf("scan %s: %w", label, err)
			}
		}
		return rows.Err()
	}
	identityWhere := `account_id = ANY($1::text[]) AND region = ANY($2::text[]) AND deleted_at IS NULL`
	credentialWhere := `account_id = ANY($1::text[]) AND region = ANY($2::text[]) AND deleted_at IS NULL`

	queries := []struct {
		label string
		query string
		scan  func(*sql.Rows) error
	}{
		{
			label: "identity types",
			query: fmt.Sprintf(`SELECT identity_type, COUNT(*)::int FROM %s.identities WHERE %s AND identity_type IS NOT NULL AND identity_type <> '' GROUP BY identity_type ORDER BY COUNT(*) DESC`, resolved.QSchema(), identityWhere),
			scan: func(rows *sql.Rows) error {
				var item models.TypeCountFilter
				if err := rows.Scan(&item.Type, &item.Count); err != nil {
					return err
				}
				result.IdentityTypes = append(result.IdentityTypes, item)
				return nil
			},
		},
		{
			label: "functional classes",
			query: fmt.Sprintf(`SELECT functional_class, COUNT(*)::int FROM %s.identities WHERE %s AND functional_class IS NOT NULL AND functional_class <> '' GROUP BY functional_class ORDER BY COUNT(*) DESC`, resolved.QSchema(), identityWhere),
			scan: func(rows *sql.Rows) error {
				var item models.ClassCountFilter
				if err := rows.Scan(&item.Class, &item.Count); err != nil {
					return err
				}
				result.FunctionalClasses = append(result.FunctionalClasses, item)
				return nil
			},
		},
		{
			label: "credential types",
			query: fmt.Sprintf(`SELECT credential_type, COUNT(*)::int FROM %s.credentials WHERE %s AND credential_type IS NOT NULL AND credential_type <> '' GROUP BY credential_type ORDER BY COUNT(*) DESC`, resolved.QSchema(), credentialWhere),
			scan: func(rows *sql.Rows) error {
				var item models.TypeCountFilter
				if err := rows.Scan(&item.Type, &item.Count); err != nil {
					return err
				}
				result.CredentialTypes = append(result.CredentialTypes, item)
				return nil
			},
		},
		{
			label: "environments",
			query: fmt.Sprintf(`SELECT environment, COUNT(*)::int FROM %s.identities WHERE %s AND environment IS NOT NULL AND environment <> '' GROUP BY environment ORDER BY COUNT(*) DESC`, resolved.QSchema(), identityWhere),
			scan: func(rows *sql.Rows) error {
				var item models.EnvironmentCountFilter
				if err := rows.Scan(&item.Environment, &item.Count); err != nil {
					return err
				}
				result.Environments = append(result.Environments, item)
				return nil
			},
		},
		{
			label: "statuses",
			query: fmt.Sprintf(`SELECT status, COUNT(*)::int FROM %s.identities WHERE %s AND status IS NOT NULL AND status <> '' GROUP BY status ORDER BY COUNT(*) DESC`, resolved.QSchema(), identityWhere),
			scan: func(rows *sql.Rows) error {
				var item models.StatusCountFilter
				if err := rows.Scan(&item.Status, &item.Count); err != nil {
					return err
				}
				result.Statuses = append(result.Statuses, item)
				return nil
			},
		},
	}
	for _, query := range queries {
		if err := queryGrouped(query.query, query.scan, query.label); err != nil {
			return nil, err
		}
	}

	return result, nil
}
