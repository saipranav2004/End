package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/database"
	"nhi-backend-api/internal/models"
)

const (
	globalUsersTable   = "nhi_users"
	globalTenantsTable = "tenants"
)

// UserRepository is bound only to the global schema (users + tenant registry).
type UserRepository struct {
	db     *PostgresDB
	schema string
}

func NewUserRepository(db *PostgresDB) *UserRepository {
	schema := config.DefaultGlobalSchema
	if db != nil && strings.TrimSpace(db.GlobalSchema()) != "" {
		schema = db.GlobalSchema()
	}
	return &UserRepository{db: db, schema: schema}
}

func (r *UserRepository) baseSelect() string {
	return `SELECT id, email, password, COALESCE(full_name, ''),
		COALESCE(team, ''), COALESCE(role, ''), tenant_id::text,
		COALESCE(status, ''), created_at
		FROM ` + database.Table(r.schema, globalUsersTable)
}

func scanUser(row *sql.Row) (*models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Email, &u.Password, &u.FullName, &u.Team, &u.Role,
		&u.TenantID, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepository) globalDB() (*sql.DB, error) {
	if r == nil || r.db == nil || r.db.DB == nil {
		return nil, errors.New("global user repository is not initialized")
	}
	return r.db.DB, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	db, err := r.globalDB()
	if err != nil {
		return nil, err
	}
	query := r.baseSelect() + ` WHERE LOWER(TRIM(email)) = LOWER(TRIM($1))`
	u, err := scanUser(db.QueryRowContext(ctx, query, strings.TrimSpace(email)))
	if err != nil {
		return nil, fmt.Errorf("query user by email: %w", err)
	}
	return u, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id int) (*models.User, error) {
	db, err := r.globalDB()
	if err != nil {
		return nil, err
	}
	u, err := scanUser(db.QueryRowContext(ctx, r.baseSelect()+` WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	return u, nil
}

// GetTenantDBName resolves the tenant schema name (the registry's db_name
// column) from the global registry. An absent or blank mapping is an error;
// there is deliberately no default schema.
func (r *UserRepository) GetTenantDBName(ctx context.Context, tenantID string) (string, error) {
	db, err := r.globalDB()
	if err != nil {
		return "", err
	}
	var dbName sql.NullString
	query := `SELECT db_name FROM ` + database.Table(r.schema, globalTenantsTable) + ` WHERE tenant_id = $1::uuid`
	if err := db.QueryRowContext(ctx, query, strings.TrimSpace(tenantID)).Scan(&dbName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("tenant %q is not registered", tenantID)
		}
		return "", fmt.Errorf("query tenant database mapping: %w", err)
	}
	name := strings.TrimSpace(dbName.String)
	if !dbName.Valid || name == "" {
		return "", errors.New("tenant database mapping is blank")
	}
	if err := config.ValidateSchemaName(name); err != nil {
		return "", err
	}
	return name, nil
}

// ValidateAuthContext revalidates the signed token's user/tenant/database tuple
// against the global registry and confirms the account is still active and the
// token's session (jti) exists and has not been ended by logout.
func (r *UserRepository) ValidateAuthContext(ctx context.Context, email, tenantID, dbName, sessionID string) (bool, string, error) {
	db, err := r.globalDB()
	if err != nil {
		return false, "", err
	}
	// The role is read here, not trusted from the token, so a demoted admin
	// loses admin endpoints on the next request rather than at token expiry.
	query := `SELECT COALESCE(u.role, '')
		FROM ` + database.Table(r.schema, globalUsersTable) + ` u
		JOIN ` + database.Table(r.schema, globalTenantsTable) + ` t ON t.tenant_id = u.tenant_id
		WHERE LOWER(TRIM(u.email)) = LOWER(TRIM($1))
		  AND u.tenant_id = $2::uuid
		  AND t.db_name = $3
		  AND LOWER(TRIM(COALESCE(u.status, ''))) = 'active'
		  AND EXISTS (SELECT 1 FROM ` + r.refreshTable() + ` s WHERE s.session_id = $4::uuid AND s.user_id = u.id)
		  AND NOT EXISTS (SELECT 1 FROM ` + r.refreshTable() + ` s WHERE s.session_id = $4::uuid AND s.session_ended_at IS NOT NULL)
		LIMIT 1`
	var role string
	err = db.QueryRowContext(ctx, query, strings.TrimSpace(email), strings.TrimSpace(tenantID), strings.TrimSpace(dbName), strings.TrimSpace(sessionID)).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("validate authenticated tenant context: %w", err)
	}
	return true, role, nil
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID int) error {
	db, err := r.globalDB()
	if err != nil {
		return err
	}
	query := `UPDATE ` + database.Table(r.schema, globalUsersTable) + ` SET last_login_at = NOW() WHERE id = $1`
	res, err := db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("update last_login_at: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("check last_login_at update: %w", err)
	} else if n != 1 {
		return fmt.Errorf("last_login_at update affected %d users", n)
	}
	return nil
}
