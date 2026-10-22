package clickhouse

import (
	"context"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"nhi-backend-api/internal/logging"
)

// Option customises a Repository.
type Option func(*Repository)

// WithSlowQueryThreshold logs every query slower than d as a warning. Zero or
// negative keeps DefaultSlowQuery.
func WithSlowQueryThreshold(d time.Duration) Option {
	return func(r *Repository) {
		if d > 0 {
			r.conn = timedConn{Conn: r.conn, slow: d}
		}
	}
}

// timedConn wraps the connection so a slow ClickHouse query leaves a log line
// carrying the request id; a silently slow query otherwise looks like "no data".
type timedConn struct {
	driver.Conn
	slow time.Duration
}

func (t timedConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	start := time.Now()
	rows, err := t.Conn.Query(ctx, query, args...)
	t.observe(ctx, query, start, err)
	return rows, err
}

func (t timedConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	start := time.Now()
	row := t.Conn.QueryRow(ctx, query, args...)
	t.observe(ctx, query, start, nil)
	return row
}

func (t timedConn) observe(ctx context.Context, query string, start time.Time, err error) {
	d := time.Since(start)
	if d < t.slow {
		return
	}
	attrs := []any{"duration_ms", d.Milliseconds(), "threshold_ms", t.slow.Milliseconds(), "query", shortSQL(query)}
	if err != nil {
		attrs = append(attrs, "failed", true)
	}
	logging.FromContext(ctx).Warn("slow clickhouse query", attrs...)
}

// shortSQL collapses whitespace and cuts the statement; bound values (ARNs,
// account ids) are not part of the text, so none are logged.
func shortSQL(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 160 {
		q = q[:160] + "..."
	}
	return q
}
