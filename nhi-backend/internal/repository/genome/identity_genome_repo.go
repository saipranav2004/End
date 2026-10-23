package genome

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// Readers behind GET /api/v1/identities/{source_type}/{identity_name}/genome
// (backend_api_contract.md §5.4 / §10.3). Every count is uniqExact(event_id)
// over the identity's events inside [from, to). ClickHouse only: §5.4 reads
// no PostgreSQL table once the identity is resolved.

// IdentityActionCount is one typical_actions group.
type IdentityActionCount struct {
	EventSource string
	EventName   string
	Calls       uint64
}

// TypicalActions groups the identity's events by event_source + event_name
// (§5.4 "aggregate event_name + event_source"), most-called first, limit rows.
func (e *MonitoringEvents) TypicalActions(ctx context.Context, accounts []string, sourceType, identityName string, from, to time.Time, limit int) ([]IdentityActionCount, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT event_source, event_name, uniqExact(event_id) AS calls
	      FROM ` + e.tbl(ctx) + `
	      WHERE` + identityWindowSQL + `
	      GROUP BY event_source, event_name
	      ORDER BY calls DESC, event_source, event_name
	      LIMIT {limit:UInt32}`
	args := append(identityWindowArgs(accounts, sourceType, identityName, from, to), ch.Named("limit", uint32(limit)))
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("log_events typical actions: %w", err)
	}
	defer rows.Close()
	out := []IdentityActionCount{}
	for rows.Next() {
		var a IdentityActionCount
		if err := rows.Scan(&a.EventSource, &a.EventName, &a.Calls); err != nil {
			return nil, fmt.Errorf("log_events typical actions: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_events typical actions: %w", err)
	}
	return out, nil
}

// IdentityResourceCount is one typical_resources row.
type IdentityResourceCount struct {
	ResourceARN  string
	Calls        uint64
	ResourceType string // most frequent dimensions_json.resource_type of the events touching the ARN; "" when the key is absent
}

// TypicalResources splits the comma-joined resource_arns of every event
// (§5.4 "split the comma-separated value before counting") and counts
// distinct events per ARN, most-used first, limit rows. resource_type is the
// dimensions_json.resource_type value seen most often with that ARN (§10.3).
func (e *MonitoringEvents) TypicalResources(ctx context.Context, accounts []string, sourceType, identityName string, from, to time.Time, limit int) ([]IdentityResourceCount, error) {
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
	              WHERE` + identityWindowSQL + `
	                AND resource_arns != ''
	          )
	          WHERE resource_arn != ''
	          GROUP BY resource_arn, resource_type
	      )
	      GROUP BY resource_arn
	      ORDER BY calls DESC, resource_arn
	      LIMIT {limit:UInt32}`
	args := append(identityWindowArgs(accounts, sourceType, identityName, from, to), ch.Named("limit", uint32(limit)))
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("log_events typical resources: %w", err)
	}
	defer rows.Close()
	out := []IdentityResourceCount{}
	for rows.Next() {
		var r IdentityResourceCount
		if err := rows.Scan(&r.ResourceARN, &r.Calls, &r.ResourceType); err != nil {
			return nil, fmt.Errorf("log_events typical resources: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_events typical resources: %w", err)
	}
	return out, nil
}

// IdentityScheduleCell is one activity_schedule cell.
type IdentityScheduleCell struct {
	DayOfWeek uint8 // toDayOfWeek: 1 = Monday … 7 = Sunday
	Hour      uint8 // toHour: 0–23 UTC
	Calls     uint64
}

// ActivitySchedule is the §5.4 schedule query (toDayOfWeek × toHour).
func (e *MonitoringEvents) ActivitySchedule(ctx context.Context, accounts []string, sourceType, identityName string, from, to time.Time) ([]IdentityScheduleCell, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	q := `SELECT toDayOfWeek(event_time) AS day_of_week, toHour(event_time) AS hour, uniqExact(event_id) AS calls
	      FROM ` + e.tbl(ctx) + `
	      WHERE` + identityWindowSQL + `
	      GROUP BY day_of_week, hour
	      ORDER BY day_of_week, hour`
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, identityWindowArgs(accounts, sourceType, identityName, from, to)...)
	if err != nil {
		return nil, fmt.Errorf("log_events activity schedule: %w", err)
	}
	defer rows.Close()
	out := []IdentityScheduleCell{}
	for rows.Next() {
		var c IdentityScheduleCell
		if err := rows.Scan(&c.DayOfWeek, &c.Hour, &c.Calls); err != nil {
			return nil, fmt.Errorf("log_events activity schedule: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_events activity schedule: %w", err)
	}
	return out, nil
}

// IdentityDimensionValueCount is one fingerprint point: distinct events whose
// dimensions_json has value for dimension.
type IdentityDimensionValueCount struct {
	Dimension string
	Value     string
	Calls     uint64
}

// Fingerprint parses dimensions_json (§5.4: the keys the API may parse are
// passed in dimensions) and counts distinct events per (dimension, value),
// keeping the perDimension most frequent values of each dimension. Numeric
// JSON values (e.g. hour_of_week_utc) are returned as their text form.
func (e *MonitoringEvents) Fingerprint(ctx context.Context, accounts []string, sourceType, identityName string, from, to time.Time, dimensions []string, perDimension int) ([]IdentityDimensionValueCount, error) {
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
	              WHERE` + identityWindowSQL + `
	          )
	          WHERE has({dimensions:Array(String)}, kv.1)
	      )
	      WHERE value != '' AND value != 'null'
	      GROUP BY dimension, value
	      ORDER BY dimension, calls DESC, value
	      LIMIT {per_dimension:UInt32} BY dimension`
	args := append(identityWindowArgs(accounts, sourceType, identityName, from, to),
		ch.Named("dimensions", dimensions), ch.Named("per_dimension", uint32(perDimension)))
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	rows, err := e.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("log_events fingerprint: %w", err)
	}
	defer rows.Close()
	out := []IdentityDimensionValueCount{}
	for rows.Next() {
		var p IdentityDimensionValueCount
		if err := rows.Scan(&p.Dimension, &p.Value, &p.Calls); err != nil {
			return nil, fmt.Errorf("log_events fingerprint: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_events fingerprint: %w", err)
	}
	return out, nil
}
