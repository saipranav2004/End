// Package clickhouse wraps the read-only connection to the CloudTrail event
// store. ClickHouse is optional: when it is not configured callers get a nil
// *Client and fall back to Postgres-only data. A configured server is pinged
// every 15 s; while it does not answer, Up is false and callers skip it
// rather than wait for a timeout on every query, and it comes back on its own.
package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"nhi-backend-api/internal/config"
)

// Client owns the ClickHouse connection pool.
type Client struct {
	conn    driver.Conn
	table   string
	suffix  string // per-tenant mode: the tenant schema + suffix is the database
	timeout time.Duration
	up      atomic.Bool
	bad     atomic.Bool // the events table lacks columns the queries need
	stop    chan struct{}
}

// New opens the pool and pings it. It returns (nil, nil) when ClickHouse is
// not configured. A configured-but-unreachable server returns the client
// (down, retried in the background) together with the ping error.
func New(ctx context.Context, cfg config.ClickHouseConfig) (*Client, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second

	opts := &ch.Options{
		Addr: []string{net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))},
		Auth: ch.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		DialTimeout:     timeout,
		ReadTimeout:     timeout,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		Compression:     &ch.Compression{Method: ch.CompressionLZ4},
	}
	if cfg.Protocol == "http" {
		opts.Protocol = ch.HTTP
	}
	if cfg.Secure {
		opts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	conn, err := ch.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	c := &Client{conn: conn, table: cfg.Table, timeout: timeout, stop: make(chan struct{})}
	if cfg.PerTenant() {
		c.suffix = cfg.DatabaseSuffix
	}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	err = c.Ping(pingCtx)
	cancel()
	c.up.Store(err == nil)
	go c.watch(cfg.Host)
	if err != nil {
		return c, fmt.Errorf("ping clickhouse: %w", err)
	}
	return c, nil
}

// RequireColumns checks that the configured table has the columns the
// CloudTrail queries select. A table with another shape (typically the genome
// pipeline's log_events set as NHI_CLICKHOUSE_TABLE) would otherwise fail on
// every request with "Unknown identifier"; here it is reported once, by name,
// and activity data stays off (Postgres data is still served).
func (c *Client) RequireColumns(ctx context.Context, columns ...string) error {
	if c == nil || !c.Up() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	rows, err := c.conn.Query(ctx, "SELECT name FROM system.columns WHERE database = currentDatabase() AND table = {t:String}", ch.Named("t", c.table))
	if err != nil {
		return nil // cannot inspect (permissions): leave it to the queries
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			have[name] = true
		}
	}
	if len(have) == 0 {
		c.bad.Store(true)
		return fmt.Errorf("table %q was not found in the configured database (check NHI_CLICKHOUSE_DATABASE and NHI_CLICKHOUSE_TABLE)", c.table)
	}
	var missing []string
	for _, name := range columns {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		c.bad.Store(true)
		return fmt.Errorf("table %q has no column(s) %v: NHI_CLICKHOUSE_TABLE must be the CloudTrail events table (default cloudtrail_events), not the genome log_events table", c.table, missing)
	}
	return nil
}

// watch re-pings the server and records whether it answers.
func (c *Client) watch(host string) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		err := c.Ping(ctx)
		cancel()
		if was := c.up.Swap(err == nil); was != (err == nil) {
			if err == nil {
				slog.Info("clickhouse is reachable again", "host", host)
			} else {
				slog.Warn("clickhouse stopped answering", "host", host, "err", err)
			}
		}
	}
}

// Up reports whether the last ping succeeded. False on a nil client.
func (c *Client) Up() bool { return c != nil && c.up.Load() && !c.bad.Load() }

// Ping checks that the server answers.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("clickhouse is not configured")
	}
	return c.conn.Ping(ctx)
}

// Close releases the pool. Safe on a nil client.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	close(c.stop)
	return c.conn.Close()
}

// Conn exposes the pool for query code.
func (c *Client) Conn() driver.Conn { return c.conn }

type tenantKey struct{}

// WithTenant records the tenant's schema name (already validated) on the
// request context, so that a per-tenant client reads that tenant's database.
func WithTenant(ctx context.Context, schema string) context.Context {
	return context.WithValue(ctx, tenantKey{}, schema)
}

// PerTenant reports whether queries go to a database per tenant.
func (c *Client) PerTenant() bool { return c != nil && c.suffix != "" }

// Database is the database a request of ctx's tenant reads in per-tenant
// mode ("" otherwise, and when the context carries no tenant).
func (c *Client) Database(ctx context.Context) string {
	if c == nil || c.suffix == "" {
		return ""
	}
	if t, _ := ctx.Value(tenantKey{}).(string); t != "" {
		return t + c.suffix
	}
	return ""
}

// TableFor is the table to query for this request: `<tenant>_clickhouse`.`table`
// in per-tenant mode, else the configured (connection-default) database's table.
// Both parts are validated identifiers.
func (c *Client) TableFor(ctx context.Context) string {
	if db := c.Database(ctx); db != "" {
		return "`" + db + "`.`" + c.table + "`"
	}
	return c.table
}

// Table is the validated events table name; safe to interpolate into SQL.
func (c *Client) Table() string { return c.table }

// Timeout is the per-query timeout callers should apply with context.WithTimeout.
func (c *Client) Timeout() time.Duration { return c.timeout }
