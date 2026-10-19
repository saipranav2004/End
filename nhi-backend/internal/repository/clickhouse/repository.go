// Package clickhouse holds the read-only CloudTrail queries. It returns raw
// rows; turning them into API responses is the service layer's job.
package clickhouse

import (
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	chclient "nhi-backend-api/internal/clickhouse"
)

// Repository runs queries over the shared ClickHouse connection pool.
type Repository struct {
	conn    driver.Conn
	table   string
	timeout time.Duration
}

// New wraps the client. It returns nil when ClickHouse is not available, so a
// nil Repository means "activity data disabled"; check Enabled before use.
func New(client *chclient.Client) *Repository {
	if client == nil {
		return nil
	}
	return &Repository{conn: client.Conn(), table: client.Table(), timeout: client.Timeout()}
}

// Enabled reports whether ClickHouse queries can run. Safe on a nil Repository.
func (c *Repository) Enabled() bool { return c != nil }
