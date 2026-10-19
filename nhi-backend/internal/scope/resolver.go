// Package scope resolves the tenant database and the per-user/default filters
// required by every data request.
package scope

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"nhi-backend-api/internal/database"
)

type TenantDatabaseProvider interface {
	GetTenantDB(dbName string) (*sql.DB, error)
}

type DataScope struct {
	DB       *sql.DB
	UserID   string
	TenantID string
	// DBName is the tenant's schema name from the registry (JWT db_name).
	DBName string
	// Schema is the validated tenant schema every data query must qualify
	// its tables with (use database.Table(s.Schema, "table")).
	Schema string

	AccountIDs []string
	Regions    []string

	HasUserSettings bool
}

// QueryArgs are the first two PostgreSQL bind values used by scoped data
// queries: selected account IDs ($1) and selected regions ($2).
func (s *DataScope) QueryArgs() []any {
	if s == nil {
		return []any{pq.Array([]string{}), pq.Array([]string{})}
	}
	return []any{pq.Array(s.AccountIDs), pq.Array(s.Regions)}
}

// Predicates returns the standard per-query scope predicates. Callers should
// also add their own deleted/status/business filters as appropriate.
func (s *DataScope) Predicates(alias string) []string {
	if alias == "" {
		alias = "t"
	}
	return []string{
		alias + ".account_id = ANY($1::text[])",
		alias + ".region = ANY($2::text[])",
	}
}

type Resolver struct {
	dbProvider TenantDatabaseProvider
}

func NewResolver(provider TenantDatabaseProvider) *Resolver {
	return &Resolver{dbProvider: provider}
}

// QSchema returns the tenant schema as a quoted identifier, ready to be used
// as the `%s` in `FROM %s.table`. The name is validated when the scope is
// resolved, and quoting preserves the registry's exact spelling.
func (s *DataScope) QSchema() string {
	return database.Table("", s.Schema)
}

// Table qualifies a tenant table with the scope's schema.
func (s *DataScope) Table(table string) string {
	return database.Table(s.Schema, table)
}

// Resolve loads the tenant's database, saved settings (by canonical email),
// tenant defaults. A present settings row is
// authoritative even when one of its selected lists is empty.
func (r *Resolver) Resolve(ctx context.Context, userID, tenantID, dbName string) (*DataScope, error) {
	if r == nil || r.dbProvider == nil {
		return nil, errors.New("tenant scope resolver is not initialized")
	}
	userID, tenantID, dbName = strings.TrimSpace(userID), strings.TrimSpace(tenantID), strings.TrimSpace(dbName)
	if userID == "" || tenantID == "" || dbName == "" {
		return nil, errors.New("user_id, tenant_id and db_name are required to resolve data scope")
	}
	tenantDB, err := r.dbProvider.GetTenantDB(dbName)
	if err != nil {
		return nil, fmt.Errorf("resolve tenant data database: %w", err)
	}
	s := &DataScope{
		DB: tenantDB, UserID: userID, TenantID: tenantID, DBName: dbName, Schema: dbName,
		AccountIDs: make([]string, 0), Regions: make([]string, 0),
	}

	settings, err := r.loadUserSettings(ctx, tenantDB, dbName, userID)
	if err != nil {
		return nil, err
	}
	if settings.exists {
		s.HasUserSettings = true
		validAccounts, err := r.existingAccounts(ctx, tenantDB, dbName, tenantID, settings.accountIDs)
		if err != nil {
			return nil, err
		}
		validRegions, err := r.existingRegions(ctx, tenantDB, dbName, tenantID, settings.regions)
		if err != nil {
			return nil, err
		}
		s.AccountIDs, s.Regions = validAccounts, validRegions
	} else {
		s.AccountIDs, err = r.defaultAccounts(ctx, tenantDB, dbName, tenantID)
		if err != nil {
			return nil, err
		}
		s.Regions, err = r.defaultRegions(ctx, tenantDB, dbName, tenantID)
		if err != nil {
			return nil, err
		}
	}

	return s, nil
}

// ResolveTenant resolves only the tenant's pool and schema. The ISPM posture
// API reads tables the worker precomputes for the whole tenant, so it needs
// neither the user's saved account / region selection nor the account list;
// both are skipped here. The tenant still comes only from the signed JWT.
func (r *Resolver) ResolveTenant(_ context.Context, userID, tenantID, dbName string) (*DataScope, error) {
	if r == nil || r.dbProvider == nil {
		return nil, errors.New("tenant scope resolver is not initialized")
	}
	userID, tenantID, dbName = strings.TrimSpace(userID), strings.TrimSpace(tenantID), strings.TrimSpace(dbName)
	if userID == "" || tenantID == "" || dbName == "" {
		return nil, errors.New("user_id, tenant_id and db_name are required to resolve data scope")
	}
	tenantDB, err := r.dbProvider.GetTenantDB(dbName)
	if err != nil {
		return nil, fmt.Errorf("resolve tenant data database: %w", err)
	}
	return &DataScope{
		DB: tenantDB, UserID: userID, TenantID: tenantID, DBName: dbName, Schema: dbName,
		AccountIDs: make([]string, 0), Regions: make([]string, 0),
	}, nil
}

type savedSettings struct {
	exists     bool
	accountIDs []string
	regions    []string
}

func (r *Resolver) loadUserSettings(ctx context.Context, db *sql.DB, schema, userID string) (savedSettings, error) {
	q := `SELECT selected_account_ids, selected_regions
		FROM ` + database.Table(schema, "user_settings") + ` WHERE user_id = $1 LIMIT 1`
	var accountJSON, regionJSON []byte
	err := db.QueryRowContext(ctx, q, userID).Scan(&accountJSON, &regionJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return savedSettings{exists: false}, nil
	}
	if err != nil {
		return savedSettings{}, fmt.Errorf("load user settings: %w", err)
	}
	out := savedSettings{exists: true, accountIDs: decodeStringList(accountJSON), regions: decodeStringList(regionJSON)}
	return out, nil
}

func (r *Resolver) defaultAccounts(ctx context.Context, db *sql.DB, schema, tenantID string) ([]string, error) {
	q := `SELECT DISTINCT account_id FROM ` + database.Table(schema, "accounts") + `
		WHERE tenant_id = $1::uuid AND is_default = true AND account_id IS NOT NULL AND account_id <> ''
		ORDER BY account_id`
	return queryStrings(ctx, db, q, tenantID)
}

func (r *Resolver) defaultRegions(ctx context.Context, db *sql.DB, schema, tenantID string) ([]string, error) {
	q := `SELECT DISTINCT r FROM ` + database.Table(schema, "accounts") + ` a, unnest(a.regions) AS r
		WHERE a.tenant_id = $1::uuid AND a.is_default = true AND r IS NOT NULL AND r <> ''
		ORDER BY r`
	return queryStrings(ctx, db, q, tenantID)
}

func (r *Resolver) existingAccounts(ctx context.Context, db *sql.DB, schema, tenantID string, selected []string) ([]string, error) {
	if len(selected) == 0 {
		return make([]string, 0), nil
	}
	q := `SELECT DISTINCT account_id FROM ` + database.Table(schema, "accounts") + `
		WHERE tenant_id = $1::uuid AND account_id = ANY($2::text[]) ORDER BY account_id`
	return queryStrings(ctx, db, q, tenantID, pq.Array(selected))
}

func (r *Resolver) existingRegions(ctx context.Context, db *sql.DB, schema, tenantID string, selected []string) ([]string, error) {
	if len(selected) == 0 {
		return make([]string, 0), nil
	}
	q := `SELECT DISTINCT r FROM ` + database.Table(schema, "accounts") + ` a, unnest(a.regions) AS r
		WHERE a.tenant_id = $1::uuid AND r = ANY($2::text[]) ORDER BY r`
	return queryStrings(ctx, db, q, tenantID, pq.Array(selected))
}

func queryStrings(ctx context.Context, db *sql.DB, q string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func decodeStringList(raw []byte) []string {
	if len(raw) == 0 {
		return make([]string, 0)
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return make([]string, 0)
	}
	if values == nil {
		return make([]string, 0)
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
