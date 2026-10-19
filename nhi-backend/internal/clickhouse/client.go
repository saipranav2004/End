// Package clickhouse wraps the read-only connection to the CloudTrail event
// store. ClickHouse is optional: when it is not configured, or unreachable at
// startup, callers get a nil *Client and must fall back to Postgres-only data.
package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"nhi-backend-api/internal/config"
)

// Client owns the ClickHouse connection pool.
type Client struct {
	conn    driver.Conn
	table   string
	timeout time.Duration
}

// New opens the pool and verifies it with a ping. It returns (nil, nil) when
// ClickHouse is not configured, so callers can treat "disabled" and "absent"
// the same way. A configured-but-unreachable server returns an error.
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
	c := &Client{conn: conn, table: cfg.Table, timeout: timeout}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := c.Ping(pingCtx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return c, nil
}

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
	return c.conn.Close()
}

// Conn exposes the pool for query code.
func (c *Client) Conn() driver.Conn { return c.conn }

// Table is the validated events table name; safe to interpolate into SQL.
func (c *Client) Table() string { return c.table }

// Timeout is the per-query timeout callers should apply with context.WithTimeout.
func (c *Client) Timeout() time.Duration { return c.timeout }
