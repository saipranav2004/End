package clickhouse

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// ActivityPageFilter narrows and pages a keyset walk over one subject's events.
type ActivityPageFilter struct {
	// Mutating keeps only write calls (true) or only read-only calls (false);
	// nil keeps both.
	Mutating *bool
	// Cursor: when HasCursor is set, only events after (CursorTime, CursorID)
	// in the newest-first order are returned.
	HasCursor  bool
	CursorTime time.Time
	CursorID   string
	Limit      int
}

// ActivityPage returns events newest first, using (event_time, event_id) as a
// keyset so pages stay stable when timestamps collide. It is the CloudTrail
// source of GET /identities/{id}/activity.
func (c *Repository) ActivityPage(ctx context.Context, sub Subject, sc Scope, pf ActivityPageFilter) ([]Event, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return nil, nil
	}
	if pf.Mutating != nil {
		ro := uint8(1)
		if *pf.Mutating {
			ro = 0
		}
		where += " AND is_read_only = {ro:UInt8}"
		args = append(args, ch.Named("ro", ro))
	}
	if pf.HasCursor {
		where += " AND (event_time < {cursor_time:DateTime64(3)} OR (event_time = {cursor_time:DateTime64(3)} AND event_id > {cursor_id:String}))"
		args = append(args, ch.Named("cursor_time", pf.CursorTime.UTC()), ch.Named("cursor_id", pf.CursorID))
	}
	limit := pf.Limit
	if limit < 1 || limit > maxEventLimit {
		limit = maxEventLimit
	}
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s
		ORDER BY event_time DESC, event_id
		LIMIT %d`, eventColumns, c.table, where, limit)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse activity page: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var ro uint8
		if err := rows.Scan(&e.EventTime, &e.EventName, &e.EventSource, &e.Region, &e.SourceIP, &e.UserAgent,
			&e.ErrorCode, &e.ErrorMessage, &e.ResourceARN, &e.ResourceType, &ro, &e.EventID,
			&e.CallerARN, &e.SessionIssuerARN, &e.AccessKeyID, &e.CallerType); err != nil {
			return nil, fmt.Errorf("clickhouse activity page scan: %w", err)
		}
		e.IsReadOnly = ro != 0
		out = append(out, e)
	}
	return out, rows.Err()
}
