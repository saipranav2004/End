package repository

import (
	"database/sql"
	"fmt"
	"strings"

	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/database"
)

type PostgresDB struct {
	// DB is reserved for global authentication/tenant-registry queries only.
	DB      *sql.DB
	manager *database.Manager
}

func NewPostgresDB(cfg *config.Config) (*PostgresDB, error) {
	manager, err := database.NewManager(cfg)
	if err != nil {
		return nil, err
	}
	return &PostgresDB{DB: manager.Global(), manager: manager}, nil
}

// GetTenantDB validates the tenant schema name and returns the shared pool;
// blank names are rejected rather than defaulted.
func (p *PostgresDB) GetTenantDB(dbName string) (*sql.DB, error) {
	if p == nil || p.manager == nil {
		return nil, fmt.Errorf("postgres database manager is not initialized")
	}
	return p.manager.Tenant(dbName)
}

func (p *PostgresDB) GlobalSchema() string {
	if p == nil || p.manager == nil {
		return ""
	}
	return p.manager.GlobalSchema()
}

func (p *PostgresDB) Close() error {
	if p == nil || p.manager == nil {
		return nil
	}
	return p.manager.Close()
}

func ownedCredsArray(alias string) string {
	col := "NULLIF(" + alias + ".owned_credentials::text, '')::jsonb"
	return "(CASE WHEN jsonb_typeof(" + col + ") = 'array' THEN " + col + " ELSE '[]'::jsonb END)"
}

// Identifier quotes a static schema/table name safely.
func Identifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
