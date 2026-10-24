package genome

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// Readers behind GET /api/v1/identities/{source_type}/{identity_name}/activity
// (backend_api_contract.md §5.3 / §10.3). The SQL is the contract's, with the
// table name taken from configuration. total_calls reuses TotalCalls from
// monitoring_events.go. ClickHouse only: §5.3 reads no PostgreSQL table once
// the identity is resolved.

// VolumeBucket selects the call-volume bucket function.
type VolumeBucket string

const (
	VolumeBucketDay  VolumeBucket = "day"  // toStartOfDay(event_time)
	VolumeBucketHour VolumeBucket = "hour" // toStartOfHour(event_time)
)

// IdentityVolumePoint is one call-volume bucket.
type IdentityVolumePoint struct {
	Bucket time.Time
	Calls  uint64
}

// Volume is the §5.3 call-volume query: uniqExact(event_id) per day or hour
// bucket of event_time inside [from, to), oldest bucket first.
func (e *MonitoringEvents) Volume(ctx context.Context, accounts []string, sourceType, identityName string, from, to time.Time, bucket VolumeBucket) ([]IdentityVolumePoint, error) {
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
	rows, err := e.conn.Query(ctx, q, identityWindowArgs(accounts, sourceType, identityName, from, to)...)
	if err != nil {
		return nil, fmt.Errorf("log_events volume: %w", err)
	}
	defer rows.Close()
	out := []IdentityVolumePoint{}
	for rows.Next() {
		var p IdentityVolumePoint
		if err := rows.Scan(&p.Bucket, &p.Calls); err != nil {
			return nil, fmt.Errorf("log_events volume: %w", err)
		}
		p.Bucket = p.Bucket.UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_events volume: %w", err)
	}
	return out, nil
}

// IdentityRecentCall is one row of the recent-calls table — only the
// columns the §5.3 response uses (never request_parameters).
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

// RecentCalls is the §5.3 recent-calls query: the identity's events inside
// [from, to), newest first (event_time DESC, event_id DESC), read with FINAL
// on that bounded range so each event appears once with its latest version.
// before, when set, continues after a previous page; limit rows are returned
// (callers ask for one more than the page size to detect a next page).
func (e *MonitoringEvents) RecentCalls(ctx context.Context, accounts []string, sourceType, identityName string, from, to time.Time, before *RecentCallsCursor, limit int) ([]IdentityRecentCall, error) {
	if !e.Enabled() {
		return nil, ErrMonitoringEventsNotConfigured
	}
	if limit < 1 {
		return []IdentityRecentCall{}, nil
	}
	args := identityWindowArgs(accounts, sourceType, identityName, from, to)
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
		return nil, fmt.Errorf("log_events recent calls: %w", err)
	}
	defer rows.Close()
	out := []IdentityRecentCall{}
	for rows.Next() {
		var r IdentityRecentCall
		if err := rows.Scan(&r.EventID, &r.EventTime, &r.EventName, &r.EventSource, &r.SourceIP, &r.Region, &r.ResourceARNs, &r.RiskStatus, &r.EventRiskScore); err != nil {
			return nil, fmt.Errorf("log_events recent calls: %w", err)
		}
		r.EventTime = r.EventTime.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("log_events recent calls: %w", err)
	}
	return out, nil
}
