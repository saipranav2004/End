// Package clickhouse holds the read-only CloudTrail queries. It returns raw
// rows; turning them into API responses is the service layer's job.
package clickhouse

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	chclient "nhi-backend-api/internal/clickhouse"
)

// Repository runs queries over the shared ClickHouse connection pool.
type Repository struct {
	conn    driver.Conn
	table   string
	timeout time.Duration
	client  *chclient.Client
}

// New wraps the client. It returns nil when ClickHouse is not available, so a
// nil Repository means "activity data disabled"; check Enabled before use.
func New(client *chclient.Client, opts ...Option) *Repository {
	if client == nil {
		return nil
	}
	r := &Repository{conn: client.Conn(), table: client.Table(), timeout: client.Timeout(), client: client}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// tbl is the events table for this request's tenant (see clickhouse.Client.TableFor).
func (c *Repository) tbl(ctx context.Context) string {
	if c.client != nil {
		return c.client.TableFor(ctx)
	}
	return c.table
}

// Enabled reports whether ClickHouse queries can run: configured and
// answering pings. Safe on a nil Repository.
func (c *Repository) Enabled() bool { return c != nil && c.client.Up() }
