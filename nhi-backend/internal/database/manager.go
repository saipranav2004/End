// Package database owns the single shared PostgreSQL pool (nhi_db). The global
// schema holds authentication and the tenant registry; each tenant's data lives
// in its own schema. Tenant schema names are supplied by the trusted tenant
// registry, carried in a signed JWT, and validated before use. Queries are
// always schema-qualified (see Table); search_path is never changed.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nhi-backend-api/internal/config"
)

const driverName = "pgx"

var ErrTenantDBNotConfigured = errors.New("tenant schema is not configured")

type Manager struct {
	pool *sql.DB
	cfg  config.DatabaseConfig
}

func NewManager(cfg *config.Config) (*Manager, error) {
	if cfg == nil {
		return nil, errors.New("database: nil config")
	}
	if err := config.ValidateSchemaName(cfg.Database.Schema); err != nil {
		return nil, fmt.Errorf("global schema name: %w", err)
	}
	pool, err := openPool(cfg.Database, 30, 15)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return &Manager{pool: pool, cfg: cfg.Database}, nil
}

// Global returns the shared pool, used for authentication/registry queries.
func (m *Manager) Global() *sql.DB {
	if m == nil {
		return nil
	}
	return m.pool
}

func (m *Manager) GlobalSchema() string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.cfg.Schema)
}

// Tenant validates a tenant schema name and returns the shared pool. It
// deliberately has no default fallback: a missing or invalid name is an error,
// not a request to use another schema.
func (m *Manager) Tenant(schema string) (*sql.DB, error) {
	if m == nil || m.pool == nil {
		return nil, errors.New("database manager is not initialized")
	}
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return nil, ErrTenantDBNotConfigured
	}
	if err := config.ValidateSchemaName(schema); err != nil {
		return nil, err
	}
	return m.pool, nil
}

func (m *Manager) Close() error {
	if m == nil || m.pool == nil {
		return nil
	}
	err := m.pool.Close()
	m.pool = nil
	if err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

func Table(schema, table string) string {
	if strings.TrimSpace(schema) == "" {
		return quoteIdent(table)
	}
	return quoteIdent(schema) + "." + quoteIdent(table)
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func openPool(cfg config.DatabaseConfig, maxOpen, maxIdle int) (*sql.DB, error) {
	connStr := strings.TrimSpace(cfg.GetConnectionString())
	if cfg.DBName() == "" || connStr == "" {
		return nil, errors.New("empty database name or connection string")
	}
	db, err := sql.Open(driverName, connStr)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}
