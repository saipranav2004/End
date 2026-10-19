package clickhouse

import (
	"context"
	"fmt"
)

// AnalyticsRow is one raw aggregated row from ClickHouse. The service layer
// turns the rows into the analytics cards.
type AnalyticsRow struct {
	SourceIP    string
	Region      string
	EventSource string
	EventName   string
	ErrorCode   string
	Hour        uint8
	Count       uint64
}

// AnalyticsRows is PDF function C: the aggregation query.
func (c *Repository) AnalyticsRows(ctx context.Context, sub Subject, sc Scope) ([]AnalyticsRow, error) {
	where, args, ok := buildFilter(sub, sc)
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT source_ip, aws_region, event_source, event_name, error_code,
			toHour(event_time) AS hour, count() AS call_count
		FROM %s WHERE %s
		GROUP BY source_ip, aws_region, event_source, event_name, error_code, hour`, c.table, where)

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse analytics: %w", err)
	}
	defer rows.Close()

	var out []AnalyticsRow
	for rows.Next() {
		var r AnalyticsRow
		if err := rows.Scan(&r.SourceIP, &r.Region, &r.EventSource, &r.EventName, &r.ErrorCode, &r.Hour, &r.Count); err != nil {
			return nil, fmt.Errorf("clickhouse analytics scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
