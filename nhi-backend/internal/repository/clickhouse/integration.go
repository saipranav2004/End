package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// AccountHasEventsSince reports whether any CloudTrail event from accountID
// has been stored since `since`. It backs the "events received" check of the
// AWS connection: an event from the customer's account in this table means
// their EventBridge rule forwards to our bus and the pipeline ingests it.
// accountID must already be validated (12 digits); it is bound, not
// interpolated.
func (c *Repository) AccountHasEventsSince(ctx context.Context, accountID string, since time.Time) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("clickhouse is not configured")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	q := fmt.Sprintf(`SELECT 1 FROM %s
		WHERE account_id = {account:String} AND event_time >= {since:DateTime64(3)}
		LIMIT 1`, c.table)
	rows, err := c.conn.Query(ctx, q, ch.Named("account", accountID), ch.Named("since", since.UTC()))
	if err != nil {
		return false, fmt.Errorf("query account events: %w", err)
	}
	defer rows.Close()
	found := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read account events: %w", err)
	}
	return found, nil
}
