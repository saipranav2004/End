package clickhouse

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

const maxEventLimit = 1000

// Event is one CloudTrail event row.
type Event struct {
	// CallerARN / SessionIssuerARN / AccessKeyID / CallerType say who made the
	// call; they are what events are resolved to Postgres identities with.
	CallerARN        string
	SessionIssuerARN string
	AccessKeyID      string
	CallerType       string

	EventTime    time.Time
	EventName    string
	EventSource  string
	Region       string
	SourceIP     string
	UserAgent    string
	ErrorCode    string
	ErrorMessage string
	ResourceARN  string
	ResourceType string
	IsReadOnly   bool
	EventID      string
}

// VolumePoint is the number of events on one UTC day.
type VolumePoint struct {
	Date  string // YYYY-MM-DD
	Count uint64
}

// CallerUsage is the most recent action by one caller using a credential.
type CallerUsage struct {
	CallerARN      string
	SourceIP       string
	LastAction     string
	LastActionTime time.Time
}

const eventColumns = `event_time, event_name, event_source, aws_region, source_ip, user_agent,
	error_code, error_message, resource_arn, resource_type, is_read_only, event_id,
	caller_arn, session_issuer_arn, access_key_id, caller_type`

func (c *Repository) queryEvents(ctx context.Context, sub Subject, sc Scope, errorsOnly bool, limit, offset int) ([]Event, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return nil, nil
	}
	if limit < 1 || limit > maxEventLimit {
		limit = maxEventLimit
	}
	if offset < 0 {
		offset = 0
	}
	if errorsOnly {
		where += " AND error_code != ''"
	}
	// event_id is a tie-breaker so paging is stable when timestamps collide.
	// limit and offset are ints bounded above, so they go into the SQL text
	// directly (native-protocol typed parameters reject numeric LIMIT values).
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s
		ORDER BY event_time DESC, event_id
		LIMIT %d OFFSET %d`, eventColumns, c.table, where, limit, offset)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var ro uint8
		if err := rows.Scan(&e.EventTime, &e.EventName, &e.EventSource, &e.Region, &e.SourceIP, &e.UserAgent,
			&e.ErrorCode, &e.ErrorMessage, &e.ResourceARN, &e.ResourceType, &ro, &e.EventID,
			&e.CallerARN, &e.SessionIssuerARN, &e.AccessKeyID, &e.CallerType); err != nil {
			return nil, fmt.Errorf("clickhouse events scan: %w", err)
		}
		e.IsReadOnly = ro != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// ActivityEvents is PDF function A: recent events, newest first.
func (c *Repository) ActivityEvents(ctx context.Context, sub Subject, sc Scope, limit, offset int) ([]Event, error) {
	return c.queryEvents(ctx, sub, sc, false, limit, offset)
}

// ErrorEvents is PDF function D: only failed calls, newest first.
func (c *Repository) ErrorEvents(ctx context.Context, sub Subject, sc Scope, limit int) ([]Event, error) {
	return c.queryEvents(ctx, sub, sc, true, limit, 0)
}

// CountEvents returns the total number of matching events, for pagination.
func (c *Repository) CountEvents(ctx context.Context, sub Subject, sc Scope) (uint64, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return 0, nil
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var n uint64
	if err := c.conn.QueryRow(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE %s", c.table, where), args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("clickhouse count: %w", err)
	}
	return n, nil
}

// VolumeChart is PDF function B: events per day.
func (c *Repository) VolumeChart(ctx context.Context, sub Subject, sc Scope) ([]VolumePoint, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT toDate(event_time) AS date, count() AS count
		FROM %s WHERE %s GROUP BY date ORDER BY date`, c.table, where)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse volume: %w", err)
	}
	defer rows.Close()

	var out []VolumePoint
	for rows.Next() {
		var d time.Time
		var n uint64
		if err := rows.Scan(&d, &n); err != nil {
			return nil, fmt.Errorf("clickhouse volume scan: %w", err)
		}
		out = append(out, VolumePoint{Date: d.Format("2006-01-02"), Count: n})
	}
	return out, rows.Err()
}

// AccountVolume is the number of events per UTC day in the given accounts
// since start: the tenant-wide "event volume" trend.
func (c *Repository) AccountVolume(ctx context.Context, sc Scope) ([]VolumePoint, error) {
	if len(sc.Accounts) == 0 {
		return nil, nil
	}
	tc, ta := timeFilter(sc)
	q := fmt.Sprintf(`SELECT toDate(event_time) AS date, count() AS count
		FROM %s WHERE account_id IN {accounts:Array(String)}%s GROUP BY date ORDER BY date`, c.table, tc)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, append([]any{ch.Named("accounts", sc.Accounts)}, ta...)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse account volume: %w", err)
	}
	defer rows.Close()
	var out []VolumePoint
	for rows.Next() {
		var d time.Time
		var n uint64
		if err := rows.Scan(&d, &n); err != nil {
			return nil, fmt.Errorf("clickhouse account volume scan: %w", err)
		}
		out = append(out, VolumePoint{Date: d.Format("2006-01-02"), Count: n})
	}
	return out, rows.Err()
}

// IdentitiesUsingCredential is PDF function J: one row per caller that used
// the credential (the latest action only), most recent first.
func (c *Repository) IdentitiesUsingCredential(ctx context.Context, credentialARN string, sc Scope) ([]CallerUsage, error) {
	where, args, ok := buildFilter(Subject{Keys: []string{credentialARN}, IssuerOnly: true}, sc)
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT caller_arn,
			argMax(source_ip, event_time) AS source_ip,
			argMax(event_name, event_time) AS last_action,
			max(event_time) AS last_action_time
		FROM %s WHERE %s
		GROUP BY caller_arn ORDER BY last_action_time DESC LIMIT 100`, c.table, where)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse callers: %w", err)
	}
	defer rows.Close()

	var out []CallerUsage
	for rows.Next() {
		var u CallerUsage
		if err := rows.Scan(&u.CallerARN, &u.SourceIP, &u.LastAction, &u.LastActionTime); err != nil {
			return nil, fmt.Errorf("clickhouse callers scan: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
