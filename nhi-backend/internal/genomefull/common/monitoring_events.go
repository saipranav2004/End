// Package common: the read-only Genome ClickHouse reader shared by every
// endpoint that needs event data. Its table is configurable (default
// log_events), and its schema is the monitoring pipeline's event schema.
package common

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	chclient "nhi-backend-api/internal/clickhouse"
)

// MonitoringEvents reads the configured Genome ClickHouse table for the
// genome endpoints. The default table is log_events, using the monitoring
// pipeline's ReplacingMergeTree(ingested_at) ORDER BY event_id schema. It
// shares the backend's ClickHouse connection and configured database with
// the CloudTrail reader, but reads a separately configured table. Read-only.
//
// Because the table is a ReplacingMergeTree, several versions of one event
// can coexist until a merge runs. Every aggregate therefore counts
// uniqExact(event_id) and row reads use FINAL on a bounded time range.
type MonitoringEvents struct {
	conn    driver.Conn
	table   string // quoted configured `database`.`table`
	name    string // the bare table name, for per-tenant databases
	client  *chclient.Client
	timeout time.Duration
}

// tbl is the table for this request's tenant: `<tenant>_clickhouse`.`table`
// when the monitoring ClickHouse is configured per tenant, else the fixed one.
func (e *MonitoringEvents) tbl(ctx context.Context) string {
	if e.client != nil {
		if db := e.client.Database(ctx); db != "" {
			return "`" + db + "`.`" + e.name + "`"
		}
	}
	return e.table
}

// MonitoringEventsTable is the default Genome event table name. It can be
// overridden with NHI_CLICKHOUSE_GENOME_TABLE.
const MonitoringEventsTable = "log_events"

// ErrMonitoringEventsNotConfigured is returned by every reader when
// ClickHouse is not configured (NHI_CLICKHOUSE_HOST empty) or it was
// unreachable at start-up.
var ErrMonitoringEventsNotConfigured = errors.New("clickhouse is not configured")

// Event-store error classification. ClickHouse failures never fail a
// request (event-derived fields degrade to null/empty), but the log line
// must say what is wrong. The server error code travels in *ch.Exception.
var (
	// ErrEventsTableMissing: the configured database or Genome table does not
	// exist (UNKNOWN_TABLE 60 / UNKNOWN_DATABASE 81).
	ErrEventsTableMissing = errors.New("monitoring clickhouse table is not available")
	// ErrEventsSchemaMismatch: the table exists but a column the API reads
	// is missing or has another type.
	ErrEventsSchemaMismatch = errors.New("monitoring clickhouse table schema does not match the API")
	// ErrEventsAccessDenied: wrong credentials or missing grants.
	ErrEventsAccessDenied = errors.New("monitoring clickhouse access denied")
	// ErrEventsTimeout: the query exceeded NHI_CLICKHOUSE_TIMEOUT_SECONDS or
	// the request budget.
	ErrEventsTimeout = errors.New("monitoring clickhouse query timed out")
)

// wrapEventsErr classifies a ClickHouse error and prefixes it with the
// operation name; the driver error stays in the chain.
func wrapEventsErr(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return fmt.Errorf("%s: %w: %w", op, ErrEventsTimeout, ctxErr)
		}
		return fmt.Errorf("%s: %w", op, ctxErr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", op, ErrEventsTimeout, err)
	}
	var chErr *ch.Exception
	if errors.As(err, &chErr) {
		switch chErr.Code {
		case 60, 81: // UNKNOWN_TABLE, UNKNOWN_DATABASE
			return fmt.Errorf("%s: %w: %w", op, ErrEventsTableMissing, err)
		case 16, 47, 43, 53: // NO_SUCH_COLUMN_IN_TABLE, UNKNOWN_IDENTIFIER, ILLEGAL_TYPE_OF_ARGUMENT, TYPE_MISMATCH
			return fmt.Errorf("%s: %w: %w", op, ErrEventsSchemaMismatch, err)
		case 192, 193, 194, 497, 516: // UNKNOWN_USER, WRONG_PASSWORD, REQUIRED_PASSWORD, ACCESS_DENIED, AUTHENTICATION_FAILED
			return fmt.Errorf("%s: %w: %w", op, ErrEventsAccessDenied, err)
		case 159, 209: // TIMEOUT_EXCEEDED, SOCKET_TIMEOUT
			return fmt.Errorf("%s: %w: %w", op, ErrEventsTimeout, err)
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

// NewMonitoringEvents wraps the backend's ClickHouse client. database is
// the configured ClickHouse database (the connection's default database
// when empty); tableName is the configured Genome table and defaults to
// MonitoringEventsTable when empty. A nil client yields a nil
// *MonitoringEvents, on which every method is safe to call and reports
// ErrMonitoringEventsNotConfigured, so callers degrade to null/empty fields.
func NewMonitoringEvents(client *chclient.Client, database, tableName string) *MonitoringEvents {
	if client == nil {
		return nil
	}
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = MonitoringEventsTable
	}
	table := "`" + tableName + "`"
	if database != "" {
		table = "`" + database + "`." + table
	}
	return &MonitoringEvents{conn: client.Conn(), table: table, name: tableName, client: client, timeout: client.Timeout()}
}

// Enabled reports whether the event table can be queried. Safe on nil.
func (e *MonitoringEvents) Enabled() bool { return e != nil }

// Table is the schema-qualified table the readers query (for logs/tests).
func (e *MonitoringEvents) Table() string {
	if e == nil {
		return ""
	}
	return e.table
}

// queryContext applies the configured per-query timeout.
func (e *MonitoringEvents) queryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, e.timeout)
}

// identityWindowSQL(sourceType) is the per-identity predicate: one identity, half-open
// [from, to) on event_time. Values are bound as server-side query
// parameters, never interpolated.
func identityWindowSQL(sourceType string) string {
	return ` source_type = {source_type:String}
	  AND ` + chclient.IdentityEquals(sourceType) + `
	  AND event_time >= {from:DateTime64(3)}
	  AND event_time < {to:DateTime64(3)} `
}

func identityWindowArgs(sourceType, identityName string, from, to time.Time) []any {
	return []any{
		ch.Named("source_type", sourceType),
		ch.Named("identity_name", identityName),
		ch.DateNamed("from", from.UTC(), ch.MilliSeconds),
		ch.DateNamed("to", to.UTC(), ch.MilliSeconds),
	}
}

// ---------------------------------------------------------------------------
// Totals and the alert event join
// ---------------------------------------------------------------------------

// TotalCalls is uniqExact(event_id) for the identity inside [from, to).
func (e *MonitoringEvents) TotalCalls(ctx context.Context, sourceType, identityName string, from, to time.Time) (uint64, error) {
	if !e.Enabled() {
		return 0, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT uniqExact(event_id) FROM ` + e.tbl(ctx) + ` WHERE` + identityWindowSQL(sourceType)
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	var total uint64
	if err := e.conn.QueryRow(ctx, q, identityWindowArgs(sourceType, identityName, from, to)...).Scan(&total); err != nil {
		return 0, wrapEventsErr(ctx, "Genome ClickHouse events total calls", err)
	}
	return total, nil
}

// AlertEventRow is the log_events row of one alerts.event_id — the columns
// the anomaly endpoints use. request_parameters is never read.
type AlertEventRow struct {
	EventID              string
	EventTime            time.Time
	SourceType           string
	IdentityName         string
	AccountID            string
	Region               string
	SourceIP             string
	UserAgent            string
	EventSource          string
	EventName            string
	RequestID            string
	ResourceARNs         string // joined string as stored; the service splits it
	ProvenanceConfidence uint8
	EventRiskScore       *uint8 // Nullable
	EventRiskSeverity    string
	RiskStatus           string
	RiskReasonsJSON      string
	RiskComponentsJSON   string
}

// EventsByID reads the latest version (FINAL on the event_id key) of every
// requested event in ONE query and returns them keyed by event_id; events
// that are not in the table are simply absent. It is how the anomaly
// endpoints join alerts to log_events without a ClickHouse query per alert.
func (e *MonitoringEvents) EventsByID(ctx context.Context, eventIDs []string) (map[string]AlertEventRow, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	out := map[string]AlertEventRow{}
	if len(eventIDs) == 0 {
		return out, nil
	}
	q := `SELECT event_id, event_time, source_type, identity_name, account_id, region, source_ip, user_agent,
	             event_source, event_name, request_id, resource_arns, provenance_confidence, event_risk_score,
	             event_risk_severity, risk_status, risk_reasons_json, risk_components_json
	      FROM ` + e.tbl(ctx) + ` FINAL
	      WHERE event_id IN {ids:Array(String)}`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, ch.Named("ids", eventIDs))
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events by id", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r AlertEventRow
		if err := rows.Scan(&r.EventID, &r.EventTime, &r.SourceType, &r.IdentityName, &r.AccountID, &r.Region, &r.SourceIP, &r.UserAgent,
			&r.EventSource, &r.EventName, &r.RequestID, &r.ResourceARNs, &r.ProvenanceConfidence, &r.EventRiskScore,
			&r.EventRiskSeverity, &r.RiskStatus, &r.RiskReasonsJSON, &r.RiskComponentsJSON); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events by id", err)
		}
		r.EventTime = r.EventTime.UTC()
		out[r.EventID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events by id", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Dashboard fleet volume
// ---------------------------------------------------------------------------

// IdentityVolumePoint is one call-volume bucket.
type IdentityVolumePoint struct {
	Bucket time.Time
	Calls  uint64
}

// FleetVolumeFilter narrows the dashboard activity call-volume series.
// Accounts/Regions are the caller's tenant scope (events outside them are
// never counted); SourceType, AccountID and Region are the optional exact
// query filters.
type FleetVolumeFilter struct {
	Accounts   []string
	Regions    []string
	SourceType string
	AccountID  string
	Region     string
}

// FleetVolume is uniqExact(event_id) per toStartOfDay(event_time) over
// resolved events (is_resolved = 1) inside [from, to), oldest bucket first.
func (e *MonitoringEvents) FleetVolume(ctx context.Context, f FleetVolumeFilter, from, to time.Time) ([]IdentityVolumePoint, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	out := []IdentityVolumePoint{}
	if len(f.Accounts) == 0 || len(f.Regions) == 0 {
		return out, nil // nothing is in scope
	}
	where := ` event_time >= {from:DateTime64(3)}
	  AND event_time < {to:DateTime64(3)}
	  AND is_resolved = 1
	  AND has({accounts:Array(String)}, account_id)
	  AND has({regions:Array(String)}, region)`
	args := []any{
		ch.DateNamed("from", from.UTC(), ch.MilliSeconds),
		ch.DateNamed("to", to.UTC(), ch.MilliSeconds),
		ch.Named("accounts", f.Accounts),
		ch.Named("regions", f.Regions),
	}
	if f.SourceType != "" {
		where += ` AND source_type = {source_type:String}`
		args = append(args, ch.Named("source_type", f.SourceType))
	}
	if f.AccountID != "" {
		where += ` AND account_id = {account_id:String}`
		args = append(args, ch.Named("account_id", f.AccountID))
	}
	if f.Region != "" {
		where += ` AND region = {region:String}`
		args = append(args, ch.Named("region", f.Region))
	}
	q := `SELECT toStartOfDay(event_time) AS bucket, uniqExact(event_id) AS calls
	      FROM ` + e.tbl(ctx) + `
	      WHERE` + where + `
	      GROUP BY bucket
	      ORDER BY bucket`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events fleet volume", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p IdentityVolumePoint
		if err := rows.Scan(&p.Bucket, &p.Calls); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events fleet volume", err)
		}
		p.Bucket = p.Bucket.UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events fleet volume", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Identity list / summary: last_seen
// ---------------------------------------------------------------------------

// LastSeenByIdentity is last_seen for several identities at once: per key,
// MAX(event_time) over the latest row per event_id, resolved events only
// (is_resolved = 1), across all stored history. Keys without a resolved
// event are absent from the map.
func (e *MonitoringEvents) LastSeenByIdentity(ctx context.Context, keys []MonitoringKey) (map[MonitoringKey]time.Time, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	out := map[MonitoringKey]time.Time{}
	if len(keys) == 0 {
		return out, nil
	}
	q := `SELECT source_type, ident, max(event_time)
	      FROM (
	          SELECT source_type, ` + chclient.EventIdentity + ` AS ident, event_id,
	                 argMax(event_time, ingested_at)  AS event_time,
	                 argMax(is_resolved, ingested_at) AS is_resolved
	          FROM ` + e.tbl(ctx) + `
	          WHERE` + KeyFilterSQL + `
	          GROUP BY source_type, ident, event_id
	      )
	      WHERE is_resolved = 1
	      GROUP BY source_type, ident`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, KeyFilterArgs(keys)...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events last seen by identity", err)
	}
	defer rows.Close()
	want := KeySet(keys)
	for rows.Next() {
		var (
			k    MonitoringKey
			last time.Time
		)
		if err := rows.Scan(&k.SourceType, &k.IdentityName, &last); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events last seen by identity", err)
		}
		if _, ok := want[k]; ok {
			out[k] = last.UTC()
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events last seen by identity", err)
	}
	return out, nil
}

// LastSeen is MAX(event_time) over the latest row per event_id of the
// identity, resolved events only (is_resolved = 1), across all stored
// history. nil when the identity has no resolved event.
func (e *MonitoringEvents) LastSeen(ctx context.Context, sourceType, identityName string) (*time.Time, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT maxOrNull(event_time)
	      FROM (
	          SELECT event_id,
	                 argMax(event_time, ingested_at)  AS event_time,
	                 argMax(is_resolved, ingested_at) AS is_resolved
	          FROM ` + e.tbl(ctx) + `
	          WHERE source_type = {source_type:String} AND ` + chclient.IdentityEquals(sourceType) + `
	          GROUP BY event_id
	      )
	      WHERE is_resolved = 1`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	var last *time.Time
	if err := e.conn.QueryRow(ctx, q, ch.Named("source_type", sourceType), ch.Named("identity_name", identityName)).Scan(&last); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events last seen", err)
	}
	if last == nil {
		return nil, nil
	}
	t := last.UTC()
	return &t, nil
}

// ---------------------------------------------------------------------------
// Identity activity: volume buckets and the recent-calls keyset page
// ---------------------------------------------------------------------------

// VolumeBucket selects the call-volume bucket function.
type VolumeBucket string

const (
	VolumeBucketDay  VolumeBucket = "day"  // toStartOfDay(event_time)
	VolumeBucketHour VolumeBucket = "hour" // toStartOfHour(event_time)
)

// Volume is the identity activity call-volume query: uniqExact(event_id)
// per day or hour bucket of event_time inside [from, to), oldest bucket
// first.
func (e *MonitoringEvents) Volume(ctx context.Context, sourceType, identityName string, from, to time.Time, bucket VolumeBucket) ([]IdentityVolumePoint, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	fn := "toStartOfDay"
	if bucket == VolumeBucketHour {
		fn = "toStartOfHour"
	}
	q := `SELECT ` + fn + `(event_time) AS bucket, uniqExact(event_id) AS calls
	      FROM ` + e.tbl(ctx) + `
	      WHERE` + identityWindowSQL(sourceType) + `
	      GROUP BY bucket
	      ORDER BY bucket`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, identityWindowArgs(sourceType, identityName, from, to)...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events volume", err)
	}
	defer rows.Close()
	out := []IdentityVolumePoint{}
	for rows.Next() {
		var p IdentityVolumePoint
		if err := rows.Scan(&p.Bucket, &p.Calls); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events volume", err)
		}
		p.Bucket = p.Bucket.UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events volume", err)
	}
	return out, nil
}

// IdentityRecentCall is one row of the recent-calls table — only the
// columns the response uses (never request_parameters).
type IdentityRecentCall struct {
	EventID        string
	EventTime      time.Time
	EventName      string
	EventSource    string
	SourceIP       string
	Region         string
	ResourceARNs   string // joined string as stored; the service splits it
	RiskStatus     string
	EventRiskScore *uint8 // event_risk_score, Nullable
}

// RecentCallsCursor is the keyset position of a recent-calls page: rows
// strictly before (EventTime, EventID) in the DESC ordering are returned.
type RecentCallsCursor struct {
	EventTime time.Time
	EventID   string
}

// RecentCalls is the identity activity recent-calls query: the identity's
// events inside [from, to), newest first (event_time DESC, event_id DESC),
// read with FINAL on that bounded range so each event appears once with its
// latest version. before, when set, continues after a previous page; limit
// rows are returned (callers ask for one more than the page size to detect
// a next page).
func (e *MonitoringEvents) RecentCalls(ctx context.Context, sourceType, identityName string, from, to time.Time, before *RecentCallsCursor, limit int) ([]IdentityRecentCall, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	if limit < 1 {
		return []IdentityRecentCall{}, nil
	}
	args := identityWindowArgs(sourceType, identityName, from, to)
	cursor := ""
	if before != nil {
		cursor = ` AND (event_time, event_id) < ({before_time:DateTime64(3)}, {before_id:String})`
		args = append(args, ch.DateNamed("before_time", before.EventTime.UTC(), ch.MilliSeconds), ch.Named("before_id", before.EventID))
	}
	args = append(args, ch.Named("limit", uint32(limit)))
	q := `SELECT event_id, event_time, event_name, event_source, source_ip, region, resource_arns, risk_status, event_risk_score
	      FROM ` + e.tbl(ctx) + ` FINAL
	      WHERE` + identityWindowSQL(sourceType) + cursor + `
	      ORDER BY event_time DESC, event_id DESC
	      LIMIT {limit:UInt32}`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events recent calls", err)
	}
	defer rows.Close()
	out := []IdentityRecentCall{}
	for rows.Next() {
		var r IdentityRecentCall
		if err := rows.Scan(&r.EventID, &r.EventTime, &r.EventName, &r.EventSource, &r.SourceIP, &r.Region, &r.ResourceARNs, &r.RiskStatus, &r.EventRiskScore); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events recent calls", err)
		}
		r.EventTime = r.EventTime.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events recent calls", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Identity genome: typical actions/resources, schedule, fingerprint
// ---------------------------------------------------------------------------

// IdentityActionCount is one typical_actions group.
type IdentityActionCount struct {
	EventSource string
	EventName   string
	Calls       uint64
}

// TypicalActions groups the identity's events by event_source + event_name,
// most-called first, limit rows.
func (e *MonitoringEvents) TypicalActions(ctx context.Context, sourceType, identityName string, from, to time.Time, limit int) ([]IdentityActionCount, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT event_source, event_name, uniqExact(event_id) AS calls
	      FROM ` + e.tbl(ctx) + `
	      WHERE` + identityWindowSQL(sourceType) + `
	      GROUP BY event_source, event_name
	      ORDER BY calls DESC, event_source, event_name
	      LIMIT {limit:UInt32}`
	args := append(identityWindowArgs(sourceType, identityName, from, to), ch.Named("limit", uint32(limit)))
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events typical actions", err)
	}
	defer rows.Close()
	out := []IdentityActionCount{}
	for rows.Next() {
		var a IdentityActionCount
		if err := rows.Scan(&a.EventSource, &a.EventName, &a.Calls); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events typical actions", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events typical actions", err)
	}
	return out, nil
}

// IdentityResourceCount is one typical_resources row.
type IdentityResourceCount struct {
	ResourceARN  string
	Calls        uint64
	ResourceType string // most frequent dimensions_json.resource_type of the events touching the ARN; "" when the key is absent
}

// TypicalResources splits the comma-joined resource_arns of every event and
// counts distinct events per ARN, most-used first, limit rows.
// resource_type is the dimensions_json.resource_type value seen most often
// with that ARN.
func (e *MonitoringEvents) TypicalResources(ctx context.Context, sourceType, identityName string, from, to time.Time, limit int) ([]IdentityResourceCount, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT resource_arn, sum(type_calls) AS calls, argMax(resource_type, type_calls) AS resource_type
	      FROM (
	          SELECT resource_arn, resource_type, uniqExact(event_id) AS type_calls
	          FROM (
	              SELECT event_id,
	                     trim(arrayJoin(splitByChar(',', resource_arns))) AS resource_arn,
	                     JSONExtractString(dimensions_json, 'resource_type') AS resource_type
	              FROM ` + e.tbl(ctx) + `
	              WHERE` + identityWindowSQL(sourceType) + `
	                AND resource_arns != ''
	          )
	          WHERE resource_arn != ''
	          GROUP BY resource_arn, resource_type
	      )
	      GROUP BY resource_arn
	      ORDER BY calls DESC, resource_arn
	      LIMIT {limit:UInt32}`
	args := append(identityWindowArgs(sourceType, identityName, from, to), ch.Named("limit", uint32(limit)))
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events typical resources", err)
	}
	defer rows.Close()
	out := []IdentityResourceCount{}
	for rows.Next() {
		var r IdentityResourceCount
		if err := rows.Scan(&r.ResourceARN, &r.Calls, &r.ResourceType); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events typical resources", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events typical resources", err)
	}
	return out, nil
}

// IdentityScheduleCell is one activity_schedule cell.
type IdentityScheduleCell struct {
	DayOfWeek uint8 // toDayOfWeek: 1 = Monday … 7 = Sunday
	Hour      uint8 // toHour: 0–23 UTC
	Calls     uint64
}

// ActivitySchedule is the toDayOfWeek × toHour schedule query.
func (e *MonitoringEvents) ActivitySchedule(ctx context.Context, sourceType, identityName string, from, to time.Time) ([]IdentityScheduleCell, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT toDayOfWeek(event_time) AS day_of_week, toHour(event_time) AS hour, uniqExact(event_id) AS calls
	      FROM ` + e.tbl(ctx) + `
	      WHERE` + identityWindowSQL(sourceType) + `
	      GROUP BY day_of_week, hour
	      ORDER BY day_of_week, hour`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, identityWindowArgs(sourceType, identityName, from, to)...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events activity schedule", err)
	}
	defer rows.Close()
	out := []IdentityScheduleCell{}
	for rows.Next() {
		var c IdentityScheduleCell
		if err := rows.Scan(&c.DayOfWeek, &c.Hour, &c.Calls); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events activity schedule", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events activity schedule", err)
	}
	return out, nil
}

// IdentityDimensionValueCount is one fingerprint point: distinct events
// whose dimensions_json has value for dimension.
type IdentityDimensionValueCount struct {
	Dimension string
	Value     string
	Calls     uint64
}

// Fingerprint parses dimensions_json (only the keys in dimensions are
// parsed) and counts distinct events per (dimension, value), keeping the
// perDimension most frequent values of each dimension. Numeric JSON values
// (e.g. hour_of_week_utc) are returned as their text form.
func (e *MonitoringEvents) Fingerprint(ctx context.Context, sourceType, identityName string, from, to time.Time, dimensions []string, perDimension int) ([]IdentityDimensionValueCount, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	if len(dimensions) == 0 || perDimension < 1 {
		return []IdentityDimensionValueCount{}, nil
	}
	q := `SELECT dimension, value, uniqExact(event_id) AS calls
	      FROM (
	          SELECT event_id, kv.1 AS dimension, kv.2 AS raw,
	                 if(startsWith(raw, '"'), JSONExtractString(raw), raw) AS value
	          FROM (
	              SELECT event_id, arrayJoin(JSONExtractKeysAndValuesRaw(dimensions_json)) AS kv
	              FROM ` + e.tbl(ctx) + `
	              WHERE` + identityWindowSQL(sourceType) + `
	          )
	          WHERE has({dimensions:Array(String)}, kv.1)
	      )
	      WHERE value != '' AND value != 'null'
	      GROUP BY dimension, value
	      ORDER BY dimension, calls DESC, value
	      LIMIT {per_dimension:UInt32} BY dimension`
	args := append(identityWindowArgs(sourceType, identityName, from, to),
		ch.Named("dimensions", dimensions), ch.Named("per_dimension", uint32(perDimension)))
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events fingerprint", err)
	}
	defer rows.Close()
	out := []IdentityDimensionValueCount{}
	for rows.Next() {
		var p IdentityDimensionValueCount
		if err := rows.Scan(&p.Dimension, &p.Value, &p.Calls); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events fingerprint", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events fingerprint", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Identity peers: grouped call counts
// ---------------------------------------------------------------------------

// CallCountsByIdentity is the grouped uniqExact(event_id) per key inside
// [from, to) — one query for the whole peer set. Keys without events in the
// window are absent from the map.
func (e *MonitoringEvents) CallCountsByIdentity(ctx context.Context, keys []MonitoringKey, from, to time.Time) (map[MonitoringKey]uint64, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	out := map[MonitoringKey]uint64{}
	if len(keys) == 0 {
		return out, nil
	}
	q := `SELECT source_type, ` + chclient.EventIdentity + ` AS ident, uniqExact(event_id)
	      FROM ` + e.tbl(ctx) + `
	      WHERE` + KeyFilterSQL + `
	        AND event_time >= {from:DateTime64(3)}
	        AND event_time < {to:DateTime64(3)}
	      GROUP BY source_type, ident`
	args := append(KeyFilterArgs(keys),
		ch.DateNamed("from", from.UTC(), ch.MilliSeconds),
		ch.DateNamed("to", to.UTC(), ch.MilliSeconds))
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events call counts by identity", err)
	}
	defer rows.Close()
	want := KeySet(keys)
	for rows.Next() {
		var (
			k MonitoringKey
			n uint64
		)
		if err := rows.Scan(&k.SourceType, &k.IdentityName, &n); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events call counts by identity", err)
		}
		if _, ok := want[k]; ok {
			out[k] = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events call counts by identity", err)
	}
	return out, nil
}

// MeanProvenanceConfidence returns the event-weighted mean of the latest
// log_events.provenance_confidence value in the requested time window. It is
// scoped to the caller's account/region selection and exact normalized keys.
// A nil result means there were no matching resolved events.
func (e *MonitoringEvents) MeanProvenanceConfidence(ctx context.Context, keys []MonitoringKey, accounts, regions []string, from, to time.Time) (*float64, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	if len(keys) == 0 || len(accounts) == 0 || len(regions) == 0 {
		return nil, nil
	}
	q := `SELECT source_type, ident, avg(confidence), count()
	      FROM (
	          SELECT event_id, source_type, ` + chclient.EventIdentity + ` AS ident,
	                 argMax(provenance_confidence, ingested_at) AS confidence,
	                 argMax(is_resolved, ingested_at) AS is_resolved
	          FROM ` + e.tbl(ctx) + `
	          WHERE event_time >= {from:DateTime64(3)}
	            AND event_time < {to:DateTime64(3)}
	            AND has({accounts:Array(String)}, account_id)
	            AND has({regions:Array(String)}, region)
	            AND` + KeyFilterSQL + `
	          GROUP BY event_id, source_type, ident
	      )
	      WHERE is_resolved = 1
	      GROUP BY source_type, ident`
	args := []any{
		ch.DateNamed("from", from.UTC(), ch.MilliSeconds),
		ch.DateNamed("to", to.UTC(), ch.MilliSeconds),
		ch.Named("accounts", accounts),
		ch.Named("regions", regions),
	}
	args = append(args, KeyFilterArgs(keys)...)
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events mean provenance confidence", err)
	}
	defer rows.Close()
	want := KeySet(keys)
	var weighted float64
	var count uint64
	for rows.Next() {
		var key MonitoringKey
		var mean float64
		var n uint64
		if err := rows.Scan(&key.SourceType, &key.IdentityName, &mean, &n); err != nil {
			return nil, wrapEventsErr(ctx, "Genome ClickHouse events mean provenance confidence", err)
		}
		if _, ok := want[key]; ok {
			weighted += mean * float64(n)
			count += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapEventsErr(ctx, "Genome ClickHouse events mean provenance confidence", err)
	}
	if count == 0 {
		return nil, nil
	}
	mean := weighted / float64(count)
	return &mean, nil
}
