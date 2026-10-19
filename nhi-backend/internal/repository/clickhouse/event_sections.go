package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// Queries for the Identity Event Graph endpoint. Every query goes through
// buildFilter, so the subject (the identity's keys) and the tenant account
// list always bound the result; EventFilter only narrows it further. All user
// supplied values are bound parameters. Only bounded integers (LIMIT/OFFSET)
// are written into the SQL text.

// EventsQueryTimeout bounds every query of the events endpoint. The driver's
// own read timeout (NHI_CLICKHOUSE_TIMEOUT_SECONDS) still applies underneath.
const EventsQueryTimeout = 10 * time.Second

const (
	maxEventsPageSize = 500
	maxResourceRows   = 500
	maxIPRows         = 500
	// MaxSessionEvents is how many raw events are read to build sessions.
	MaxSessionEvents = 50000
	heatmapActions   = 20
)

// EventFilter narrows an event query. Zero values mean "no filter". The
// service short name (Scope.Service) is applied by buildFilter.
type EventFilter struct {
	Action     string // exact event name, e.g. GetObject
	Resource   string // case-insensitive substring of resource_arn
	SourceIP   string
	Credential string // credential ARN (issuer or caller)
	Result     string // "success", "error" or ""/"all"
	ReadWrite  string // "read", "write" or ""/"all"
}

func (c *Repository) eventsCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, EventsQueryTimeout)
}

// eventsWhere builds the WHERE body for subject + scope + filter.
func eventsWhere(sub Subject, sc Scope, f EventFilter) (string, []any, bool) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return "", nil, false
	}
	if f.Action != "" {
		where += " AND event_name = {f_action:String}"
		args = append(args, ch.Named("f_action", f.Action))
	}
	if f.Resource != "" {
		where += " AND positionCaseInsensitive(resource_arn, {f_resource:String}) > 0"
		args = append(args, ch.Named("f_resource", f.Resource))
	}
	if f.SourceIP != "" {
		where += " AND source_ip = {f_ip:String}"
		args = append(args, ch.Named("f_ip", f.SourceIP))
	}
	if f.Credential != "" {
		where += " AND (session_issuer_arn = {f_cred:String} OR caller_arn = {f_cred:String})"
		args = append(args, ch.Named("f_cred", f.Credential))
	}
	switch f.Result {
	case "success":
		where += " AND error_code = ''"
	case "error":
		where += " AND error_code != ''"
	}
	switch f.ReadWrite {
	case "read":
		where += " AND is_read_only = 1"
	case "write":
		where += " AND is_read_only = 0"
	}
	return where, args, true
}

// EventsPage returns one page of events, newest first.
func (c *Repository) EventsPage(ctx context.Context, sub Subject, sc Scope, f EventFilter, limit, offset int) ([]Event, error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return nil, nil
	}
	if limit < 1 || limit > maxEventsPageSize {
		limit = maxEventsPageSize
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s
		ORDER BY event_time DESC, event_id
		LIMIT %d OFFSET %d`, eventColumns, c.table, where, limit, offset)
	return c.scanEvents(ctx, q, args)
}

// EventByID returns one event if (and only if) it belongs to the subject and
// the tenant's accounts; otherwise nil.
func (c *Repository) EventByID(ctx context.Context, sub Subject, sc Scope, eventID string) (*Event, error) {
	where, args, ok := eventsWhere(sub, sc, EventFilter{})
	if !ok || eventID == "" {
		return nil, nil
	}
	where += " AND event_id = {f_event_id:String}"
	args = append(args, ch.Named("f_event_id", eventID))
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s ORDER BY event_time DESC LIMIT 1`, eventColumns, c.table, where)
	rows, err := c.scanEvents(ctx, q, args)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (c *Repository) scanEvents(ctx context.Context, q string, args []any) ([]Event, error) {
	ctx, cancel := c.eventsCtx(ctx)
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

// CountFiltered counts the events matching subject, scope and filter.
func (c *Repository) CountFiltered(ctx context.Context, sub Subject, sc Scope, f EventFilter) (uint64, error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return 0, nil
	}
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	var n uint64
	if err := c.conn.QueryRow(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE %s", c.table, where), args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("clickhouse events count: %w", err)
	}
	return n, nil
}

// CredentialUsageRow is one credential (session issuer, access key or bare
// caller) and what it did in the window.
type CredentialUsageRow struct {
	Key             string // the issuer ARN, access key id or caller ARN
	Kind            string // "issuer", "access_key" or "caller"
	Count           uint64
	First, Last     time.Time
	Actions         []string
	UniqueResources uint64
}

// CredentialUsage groups events by the credential that made them: the session
// issuer when there is one, else the access key, else the caller itself.
func (c *Repository) CredentialUsage(ctx context.Context, sub Subject, sc Scope, f EventFilter) ([]CredentialUsageRow, error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT
			multiIf(session_issuer_arn != '', session_issuer_arn, access_key_id != '', access_key_id, caller_arn) AS ckey,
			multiIf(session_issuer_arn != '', 'issuer', access_key_id != '', 'access_key', 'caller') AS kind,
			count() AS n, min(event_time), max(event_time),
			groupUniqArray(20)(event_name), uniqExact(resource_arn)
		FROM %s WHERE %s
		GROUP BY ckey, kind
		ORDER BY n DESC
		LIMIT 200`, c.table, where)
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse credential usage: %w", err)
	}
	defer rows.Close()
	var out []CredentialUsageRow
	for rows.Next() {
		var r CredentialUsageRow
		if err := rows.Scan(&r.Key, &r.Kind, &r.Count, &r.First, &r.Last, &r.Actions, &r.UniqueResources); err != nil {
			return nil, fmt.Errorf("clickhouse credential usage scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResourceRow is one resource the subject touched.
type ResourceRow struct {
	ARN         string
	Type        string
	Actions     []string
	Reads       uint64
	Writes      uint64
	Errors      uint64
	First, Last time.Time
}

// ResourceAccess returns the busiest resources (at most 500) and the total
// number of distinct resources in the window.
func (c *Repository) ResourceAccess(ctx context.Context, sub Subject, sc Scope, f EventFilter) ([]ResourceRow, uint64, error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return nil, 0, nil
	}
	where += " AND resource_arn != ''"
	q := fmt.Sprintf(`SELECT resource_arn, any(resource_type), groupUniqArray(20)(event_name),
			countIf(is_read_only = 1), countIf(is_read_only = 0), countIf(error_code != ''),
			min(event_time), max(event_time)
		FROM %s WHERE %s
		GROUP BY resource_arn
		ORDER BY count() DESC, resource_arn
		LIMIT %d`, c.table, where, maxResourceRows)
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse resources: %w", err)
	}
	var out []ResourceRow
	for rows.Next() {
		var r ResourceRow
		if err := rows.Scan(&r.ARN, &r.Type, &r.Actions, &r.Reads, &r.Writes, &r.Errors, &r.First, &r.Last); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("clickhouse resources scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()

	var total uint64
	if err := c.conn.QueryRow(ctx, fmt.Sprintf("SELECT uniqExact(resource_arn) FROM %s WHERE %s", c.table, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("clickhouse resources total: %w", err)
	}
	return out, total, nil
}

// HeatCell is the number of times an action ran in one UTC hour of the day.
type HeatCell struct {
	Action string
	Hour   uint8
	Count  uint64
}

// Heatmap returns action x hour-of-day counts for the 20 most frequent actions.
func (c *Repository) Heatmap(ctx context.Context, sub Subject, sc Scope, f EventFilter) ([]HeatCell, error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT event_name, toHour(event_time) AS h, count()
		FROM %[1]s
		WHERE %[2]s AND event_name IN (
			SELECT event_name FROM %[1]s WHERE %[2]s
			GROUP BY event_name ORDER BY count() DESC, event_name LIMIT %[3]d)
		GROUP BY event_name, h
		ORDER BY event_name, h`, c.table, where, heatmapActions)
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse heatmap: %w", err)
	}
	defer rows.Close()
	var out []HeatCell
	for rows.Next() {
		var h HeatCell
		if err := rows.Scan(&h.Action, &h.Hour, &h.Count); err != nil {
			return nil, fmt.Errorf("clickhouse heatmap scan: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SessionEvent is the slice of an event needed to build sessions.
type SessionEvent struct {
	CallerARN        string
	SessionIssuerARN string
	AccessKeyID      string
	EventTime        time.Time
	EventName        string
	EventSource      string
	SourceIP         string
}

// SessionEvents reads up to MaxSessionEvents raw events ordered by caller and
// time, so the caller can window them into sessions. truncated is true when
// more events matched than were read.
func (c *Repository) SessionEvents(ctx context.Context, sub Subject, sc Scope, f EventFilter) (rows []SessionEvent, truncated bool, err error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return nil, false, nil
	}
	q := fmt.Sprintf(`SELECT caller_arn, session_issuer_arn, access_key_id, event_time, event_name, event_source, source_ip
		FROM %s WHERE %s
		ORDER BY caller_arn, event_time, event_id
		LIMIT %d`, c.table, where, MaxSessionEvents+1)
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	res, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse session events: %w", err)
	}
	defer res.Close()
	for res.Next() {
		var e SessionEvent
		if err := res.Scan(&e.CallerARN, &e.SessionIssuerARN, &e.AccessKeyID, &e.EventTime, &e.EventName, &e.EventSource, &e.SourceIP); err != nil {
			return nil, false, fmt.Errorf("clickhouse session events scan: %w", err)
		}
		rows = append(rows, e)
	}
	if err := res.Err(); err != nil {
		return nil, false, err
	}
	if len(rows) > MaxSessionEvents {
		rows, truncated = rows[:MaxSessionEvents], true
	}
	return rows, truncated, nil
}

// ErrorCount is the number of failures with one error code.
type ErrorCount struct {
	Code  string
	Count uint64
}

// DeniedAction / DeniedResource are the top access-denied action / resource.
type DeniedAction struct {
	EventSource string
	EventName   string
	Count       uint64
}
type DeniedResource struct {
	ARN   string
	Count uint64
}

// ErrorSummaryResult is the aggregate part of the errors section.
type ErrorSummaryResult struct {
	ByCode        []ErrorCount
	MostDeniedAct *DeniedAction
	MostDeniedRes *DeniedResource
	TotalErrors   uint64
}

// deniedClause matches access-denied style error codes.
const deniedClause = "(error_code LIKE '%AccessDenied%' OR error_code LIKE '%Unauthorized%' OR error_code LIKE '%Denied%')"

// ErrorSummary aggregates the failed calls of the window.
func (c *Repository) ErrorSummary(ctx context.Context, sub Subject, sc Scope, f EventFilter) (*ErrorSummaryResult, error) {
	f.Result = "error"
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return &ErrorSummaryResult{}, nil
	}
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	out := &ErrorSummaryResult{}

	rows, err := c.conn.Query(ctx, fmt.Sprintf(`SELECT error_code, count() AS n FROM %s WHERE %s
		GROUP BY error_code ORDER BY n DESC, error_code LIMIT 50`, c.table, where), args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse error codes: %w", err)
	}
	for rows.Next() {
		var e ErrorCount
		if err := rows.Scan(&e.Code, &e.Count); err != nil {
			rows.Close()
			return nil, fmt.Errorf("clickhouse error codes scan: %w", err)
		}
		out.ByCode = append(out.ByCode, e)
		out.TotalErrors += e.Count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if out.TotalErrors == 0 {
		return out, nil
	}
	// The listed codes are capped at 50; take the exact total separately.
	if err := c.conn.QueryRow(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE %s", c.table, where), args...).Scan(&out.TotalErrors); err != nil {
		return nil, fmt.Errorf("clickhouse error total: %w", err)
	}

	var a DeniedAction
	err = c.conn.QueryRow(ctx, fmt.Sprintf(`SELECT event_source, event_name, count() AS n FROM %s WHERE %s AND %s
		GROUP BY event_source, event_name ORDER BY n DESC, event_name LIMIT 1`, c.table, where, deniedClause), args...).
		Scan(&a.EventSource, &a.EventName, &a.Count)
	if err == nil {
		out.MostDeniedAct = &a
	} else if !isNoRows(err) {
		return nil, fmt.Errorf("clickhouse denied action: %w", err)
	}

	var r DeniedResource
	err = c.conn.QueryRow(ctx, fmt.Sprintf(`SELECT resource_arn, count() AS n FROM %s WHERE %s AND %s AND resource_arn != ''
		GROUP BY resource_arn ORDER BY n DESC, resource_arn LIMIT 1`, c.table, where, deniedClause), args...).
		Scan(&r.ARN, &r.Count)
	if err == nil {
		out.MostDeniedRes = &r
	} else if !isNoRows(err) {
		return nil, fmt.Errorf("clickhouse denied resource: %w", err)
	}
	return out, nil
}

// CrossAccountRow is one resource in another AWS account.
type CrossAccountRow struct {
	TargetAccount string
	ResourceARN   string
	Actions       []string
	Count         uint64
	LastSeen      time.Time
}

// CrossAccount returns events whose resource lives in an AWS account other
// than homeAccount (the account is read from the resource ARN; resources whose
// ARN has no account, such as S3 buckets, are never cross-account).
func (c *Repository) CrossAccount(ctx context.Context, sub Subject, sc Scope, f EventFilter, homeAccount string) ([]CrossAccountRow, uint64, error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok || homeAccount == "" {
		return nil, 0, nil
	}
	where += " AND match(splitByChar(':', resource_arn)[5], '^[0-9]{12}$') AND splitByChar(':', resource_arn)[5] != {f_home:String}"
	args = append(args, ch.Named("f_home", homeAccount))
	q := fmt.Sprintf(`SELECT splitByChar(':', resource_arn)[5] AS acct, resource_arn,
			groupUniqArray(20)(event_name), count() AS n, max(event_time)
		FROM %s WHERE %s
		GROUP BY acct, resource_arn
		ORDER BY n DESC, resource_arn
		LIMIT %d`, c.table, where, maxResourceRows)
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse cross account: %w", err)
	}
	var out []CrossAccountRow
	for rows.Next() {
		var r CrossAccountRow
		if err := rows.Scan(&r.TargetAccount, &r.ResourceARN, &r.Actions, &r.Count, &r.LastSeen); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("clickhouse cross account scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	var total uint64
	if err := c.conn.QueryRow(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE %s", c.table, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("clickhouse cross account total: %w", err)
	}
	return out, total, nil
}

// IPRow is one source address the subject called from.
type IPRow struct {
	IP          string
	Count       uint64
	First, Last time.Time
}

// IPStats returns the busiest source addresses (at most 500) and the number of
// distinct addresses.
func (c *Repository) IPStats(ctx context.Context, sub Subject, sc Scope, f EventFilter) ([]IPRow, uint64, error) {
	where, args, ok := eventsWhere(sub, sc, f)
	if !ok {
		return nil, 0, nil
	}
	where += " AND source_ip != ''"
	q := fmt.Sprintf(`SELECT source_ip, count() AS n, min(event_time), max(event_time)
		FROM %s WHERE %s
		GROUP BY source_ip
		ORDER BY n DESC, source_ip
		LIMIT %d`, c.table, where, maxIPRows)
	ctx, cancel := c.eventsCtx(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse ips: %w", err)
	}
	var out []IPRow
	for rows.Next() {
		var r IPRow
		if err := rows.Scan(&r.IP, &r.Count, &r.First, &r.Last); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("clickhouse ips scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	var total uint64
	if err := c.conn.QueryRow(ctx, fmt.Sprintf("SELECT uniqExact(source_ip) FROM %s WHERE %s", c.table, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("clickhouse ips total: %w", err)
	}
	return out, total, nil
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
