package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var databaseNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,62}$`)

// schemaNameRE is stricter than databaseNameRE: tenant schema names come from
// the tenant registry via the JWT and are interpolated (quoted) into SQL.
var schemaNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// DefaultGlobalSchema holds the users and tenant registry tables.
const DefaultGlobalSchema = "nhi_global"

type Config struct {
	Server ServerConfig `yaml:"server"`
	// Database is the single PostgreSQL database (nhi_db). The global schema
	// holds authentication/tenant registry tables; every tenant's data lives in
	// its own schema of this same database.
	Database   DatabaseConfig   `yaml:"database"`
	JWT        JWTConfig        `yaml:"jwt"`
	CORS       CORSConfig       `yaml:"cors"`
	ClickHouse ClickHouseConfig `yaml:"clickhouse"`
	// AWSIntegration: "Connect AWS Account" (see aws_integration.go).
	AWSIntegration AWSIntegrationConfig `yaml:"aws_integration"`
}

// ClickHouseConfig holds the read-only connection to the CloudTrail event
// store. It is optional: when Host is empty ClickHouse is disabled and the API
// serves Postgres data only.
type ClickHouseConfig struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Database       string `yaml:"database"`
	User           string `yaml:"user"`
	Password       string `yaml:"password"`
	Table          string `yaml:"table"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	Secure         bool   `yaml:"secure"`
	// Protocol is "native" (TCP, default port 9000/9440) or "http" (port
	// 8123/8443). Use "http" when only the HTTP port is reachable, e.g. via a tunnel.
	Protocol string `yaml:"protocol"`
}

// Enabled reports whether a ClickHouse host has been configured.
func (c ClickHouseConfig) Enabled() bool {
	return strings.TrimSpace(c.Host) != ""
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

type DatabaseConfig struct {
	Host         string `yaml:"host"`
	DatabaseName string `yaml:"database_name"`
	LegacyName   string `yaml:"name"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	Schema       string `yaml:"schema"`
}

func (d DatabaseConfig) DBName() string {
	if name := strings.TrimSpace(d.DatabaseName); name != "" {
		return name
	}
	return strings.TrimSpace(d.LegacyName)
}

func (d DatabaseConfig) GetConnectionString() string {
	// Port and SSL are fixed in code — RDS and local both use 5432 + require.
	// No auto-upgrade, no fallback, no hints.
	const port = 5432
	const sslmode = "require"
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     net.JoinHostPort(d.Host, strconv.Itoa(port)),
		Path:     "/" + d.DBName(),
		RawQuery: url.Values{"sslmode": []string{sslmode}}.Encode(),
	}
	return u.String()
}

type JWTConfig struct {
	Secret           string `yaml:"secret"`
	AccessTTLMinutes int    `yaml:"access_ttl_minutes"`
	RefreshTTLHours  int    `yaml:"refresh_ttl_hours"`
}

type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

func Load() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("NHI_CONFIG_FILE"))
	if path == "" {
		path = "configs/config.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.applyEnvironment(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := cfg.AWSIntegration.load(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyEnvironment() error {
	setString := func(dst *string, key string) {
		if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
			*dst = strings.TrimSpace(value)
		}
	}
	setInt := func(dst *int, key string) error {
		if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return fmt.Errorf("config: %s must be an integer", key)
			}
			*dst = parsed
		}
		return nil
	}

	setString(&c.Server.Host, "NHI_SERVER_HOST")
	if err := setInt(&c.Server.Port, "NHI_SERVER_PORT"); err != nil {
		return err
	}

	setString(&c.Database.Host, "NHI_DB_HOST")
	setString(&c.Database.DatabaseName, "NHI_DB_NAME")
	setString(&c.Database.User, "NHI_DB_USER")
	setString(&c.Database.Password, "NHI_DB_PASSWORD")
	setString(&c.Database.Schema, "NHI_GLOBAL_SCHEMA")

	setString(&c.JWT.Secret, "NHI_JWT_SECRET")
	if err := setInt(&c.JWT.AccessTTLMinutes, "NHI_JWT_ACCESS_TTL_MINUTES"); err != nil {
		return err
	}
	if err := setInt(&c.JWT.RefreshTTLHours, "NHI_JWT_REFRESH_TTL_HOURS"); err != nil {
		return err
	}
	setString(&c.ClickHouse.Host, "NHI_CLICKHOUSE_HOST")
	if err := setInt(&c.ClickHouse.Port, "NHI_CLICKHOUSE_PORT"); err != nil {
		return err
	}
	setString(&c.ClickHouse.Database, "NHI_CLICKHOUSE_DATABASE")
	setString(&c.ClickHouse.User, "NHI_CLICKHOUSE_USER")
	setString(&c.ClickHouse.Password, "NHI_CLICKHOUSE_PASSWORD")
	setString(&c.ClickHouse.Table, "NHI_CLICKHOUSE_TABLE")
	setString(&c.ClickHouse.Protocol, "NHI_CLICKHOUSE_PROTOCOL")
	if err := setInt(&c.ClickHouse.TimeoutSeconds, "NHI_CLICKHOUSE_TIMEOUT_SECONDS"); err != nil {
		return err
	}
	if value, ok := os.LookupEnv("NHI_CLICKHOUSE_SECURE"); ok && strings.TrimSpace(value) != "" {
		secure, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("config: NHI_CLICKHOUSE_SECURE must be true or false")
		}
		c.ClickHouse.Secure = secure
	}
	if origins := strings.TrimSpace(os.Getenv("NHI_CORS_ALLOWED_ORIGINS")); origins != "" {
		c.CORS.AllowedOrigins = make([]string, 0)
		for _, origin := range strings.Split(origins, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				c.CORS.AllowedOrigins = append(c.CORS.AllowedOrigins, origin)
			}
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Database.Schema == "" {
		c.Database.Schema = DefaultGlobalSchema
	}
	if c.ClickHouse.Port == 0 {
		c.ClickHouse.Port = 9000
	}
	c.ClickHouse.Protocol = strings.ToLower(strings.TrimSpace(c.ClickHouse.Protocol))
	if c.ClickHouse.Protocol == "" {
		c.ClickHouse.Protocol = "native"
	}
	if c.ClickHouse.Table == "" {
		c.ClickHouse.Table = "cloudtrail_events"
	}
	if c.ClickHouse.TimeoutSeconds == 0 {
		c.ClickHouse.TimeoutSeconds = 5
	}
}

func (c *Config) validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("config: server.port must be between 1 and 65535")
	}
	if strings.TrimSpace(c.Server.Host) == "" {
		return fmt.Errorf("config: server.host is required")
	}
	if c.Database.DBName() == "" || !databaseNameRE.MatchString(c.Database.DBName()) {
		return fmt.Errorf("config: database.database_name must be a valid PostgreSQL database name")
	}
	if strings.TrimSpace(c.Database.Host) == "" || strings.TrimSpace(c.Database.User) == "" {
		return fmt.Errorf("config: database.host and database.user are required")
	}
	if c.Database.Password == "" {
		return fmt.Errorf("config: database.password is required (set NHI_DB_PASSWORD)")
	}
	if strings.TrimSpace(c.JWT.Secret) == "" || len(c.JWT.Secret) < 32 {
		return fmt.Errorf("config: jwt.secret must be at least 32 characters")
	}
	if c.JWT.AccessTTLMinutes < 1 || c.JWT.AccessTTLMinutes > 24*60 {
		return fmt.Errorf("config: jwt.access_ttl_minutes must be between 1 and 1440")
	}
	if c.JWT.RefreshTTLHours < 1 || c.JWT.RefreshTTLHours > 24*90 {
		return fmt.Errorf("config: jwt.refresh_ttl_hours must be between 1 and 2160")
	}
	if time.Duration(c.JWT.RefreshTTLHours)*time.Hour <= time.Duration(c.JWT.AccessTTLMinutes)*time.Minute {
		return fmt.Errorf("config: jwt.refresh_ttl_hours must be longer than jwt.access_ttl_minutes")
	}
	if !schemaNameRE.MatchString(c.Database.Schema) {
		return fmt.Errorf("config: database.schema (global schema) must be a valid identifier")
	}
	if c.ClickHouse.Enabled() {
		ch := c.ClickHouse
		if ch.Port < 1 || ch.Port > 65535 {
			return fmt.Errorf("config: clickhouse.port must be between 1 and 65535")
		}
		if strings.TrimSpace(ch.Database) == "" || !databaseNameRE.MatchString(ch.Database) {
			return fmt.Errorf("config: clickhouse.database must be a valid identifier")
		}
		// The table name is interpolated into SQL, so it must be a plain identifier.
		if !databaseNameRE.MatchString(ch.Table) {
			return fmt.Errorf("config: clickhouse.table must be a valid identifier")
		}
		if ch.Protocol != "native" && ch.Protocol != "http" {
			return fmt.Errorf("config: clickhouse.protocol must be native or http")
		}
		if ch.TimeoutSeconds < 1 || ch.TimeoutSeconds > 60 {
			return fmt.Errorf("config: clickhouse.timeout_seconds must be between 1 and 60")
		}
	}
	return nil
}

// ValidateSchemaName rejects unsafe or malformed tenant schema names before
// they are used to qualify queries.
func ValidateSchemaName(name string) error {
	if !schemaNameRE.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("invalid tenant schema name")
	}
	return nil
}
