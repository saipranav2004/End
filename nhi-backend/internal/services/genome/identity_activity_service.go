package genome

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
)

// IdentityActivityService implements
// GET /api/v1/identities/{source_type}/{identity_name}/activity
// (backend_api_contract.md §5.3 / §10.3) — the Activity tab:
//
//   - volume        uniqExact(event_id) per day/hour bucket  (ClickHouse log_events)
//   - recent_calls  newest events of the window, keyset-paged (ClickHouse log_events, FINAL)
//   - total_calls   uniqExact(event_id) over the window        (ClickHouse log_events)
//
// The identity itself is resolved from the tenant's discovery table first
// (unknown → ErrIdentityNotFound). Nothing is invented: while the monitoring
// ClickHouse is not configured or cannot be read the lists are empty and
// total_calls is null (logged server-side).
type IdentityActivityService struct {
	lookup *MonitoredIdentityLookup
	events *repo.MonitoringEvents // nil when no monitoring ClickHouse is configured
}

// NewIdentityActivityService wires the service.
func NewIdentityActivityService(lookup *MonitoredIdentityLookup, events *repo.MonitoringEvents) *IdentityActivityService {
	return &IdentityActivityService{lookup: lookup, events: events}
}

// Volume bucket values of the §5.3 bucket parameter.
const (
	ActivityBucketDay  = "day"
	ActivityBucketHour = "hour"
)

// IdentityActivityParams are the parsed §5.3 query parameters.
type IdentityActivityParams struct {
	Window       TimeWindow // §2 from (inclusive) / to (exclusive)
	Bucket       string     // ActivityBucketDay | ActivityBucketHour
	RecentLimit  int        // recent_limit (page size of recent_calls, §2: default 25, max 100)
	RecentCursor string     // recent_cursor (opaque, from a previous meta.next_cursor); "" for the first page
}

// ErrInvalidCursor reports a recent_cursor that was not produced by this
// endpoint; handlers answer 400 VALIDATION_ERROR.
var ErrInvalidCursor = errors.New("cursor is invalid")

// GetActivity builds the §5.3 response and the meta.next_cursor of the
// recent-calls page (nil when the page is the last one).
func (s *IdentityActivityService) GetActivity(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, p IdentityActivityParams) (*models.IdentityActivityResponse, *string, error) {
	before, err := decodeRecentCursor(p.RecentCursor)
	if err != nil {
		return nil, nil, err
	}
	res, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName)
	if err != nil {
		return nil, nil, err
	}
	key := res.identity.Key

	out := &models.IdentityActivityResponse{
		Volume:      []models.IdentityVolumePoint{},
		RecentCalls: []models.IdentityRecentCall{},
	}

	bucket := repo.VolumeBucketDay
	if p.Bucket == ActivityBucketHour {
		bucket = repo.VolumeBucketHour
	}
	if points, err := s.events.Volume(ctx, key.SourceType, key.IdentityName, p.Window.From, p.Window.To, bucket); eventsReadOK("identity activity volume", err) {
		for _, pt := range points {
			out.Volume = append(out.Volume, models.IdentityVolumePoint{Bucket: formatVolumeBucket(pt.Bucket, bucket), Calls: pt.Calls})
		}
	}

	var next *string
	if rows, err := s.events.RecentCalls(ctx, key.SourceType, key.IdentityName, p.Window.From, p.Window.To, before, p.RecentLimit+1); eventsReadOK("identity recent calls", err) {
		if len(rows) > p.RecentLimit { // one extra row was requested to detect a next page
			rows = rows[:p.RecentLimit]
			last := rows[len(rows)-1]
			next = encodeRecentCursor(repo.RecentCallsCursor{EventTime: last.EventTime, EventID: last.EventID})
		}
		for _, r := range rows {
			out.RecentCalls = append(out.RecentCalls, models.IdentityRecentCall{
				EventID:    r.EventID,
				EventTime:  r.EventTime,
				APICall:    apiOperation(r.EventSource, r.EventName),
				Resource:   firstResourceARN(r.ResourceARNs),
				SourceIP:   r.SourceIP,
				Region:     r.Region,
				RiskStatus: r.RiskStatus,
				RiskScore:  r.EventRiskScore,
			})
		}
	}

	if total, err := s.events.TotalCalls(ctx, key.SourceType, key.IdentityName, p.Window.From, p.Window.To); eventsReadOK("identity total calls", err) {
		out.TotalCalls = &total
	}
	return out, next, nil
}

// formatVolumeBucket renders a bucket as the contract shows it: the date
// for day buckets ("2026-10-01"), the full UTC timestamp for hour buckets.
func formatVolumeBucket(t time.Time, bucket repo.VolumeBucket) string {
	if bucket == repo.VolumeBucketHour {
		return t.UTC().Format(time.RFC3339)
	}
	return t.UTC().Format("2006-01-02")
}

// apiOperation is the §10.3 formula event_source + ':' + event_name, with
// the CloudTrail ".amazonaws.com" host suffix of event_source removed so the
// value reads like the contract's examples ("dynamodb:Query").
func apiOperation(eventSource, eventName string) string {
	return strings.TrimSuffix(strings.TrimSpace(eventSource), ".amazonaws.com") + ":" + strings.TrimSpace(eventName)
}

// splitResourceARNs splits the comma-joined log_events.resource_arns
// (§5.4 "split the comma-separated value"), dropping blanks.
func splitResourceARNs(joined string) []string {
	var out []string
	for _, part := range strings.Split(joined, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// firstResourceARN is the §5.3 recent_calls[].resource: the first ARN the
// event touched ("" when the event has none).
func firstResourceARN(joined string) string {
	if arns := splitResourceARNs(joined); len(arns) > 0 {
		return arns[0]
	}
	return ""
}

// ---- opaque keyset cursor of recent_calls -----------------------------------
//
// The cursor is the (event_time, event_id) of the last row of the page,
// encoded as base64url("<unix milliseconds>|<event_id>"). It carries no
// tenant data and is validated on the way back in.

func encodeRecentCursor(c repo.RecentCallsCursor) *string {
	s := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.EventTime.UTC().UnixMilli(), 10) + "|" + c.EventID))
	return &s
}

func decodeRecentCursor(s string) (*repo.RecentCallsCursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	millis, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return nil, ErrInvalidCursor
	}
	ms, err := strconv.ParseInt(millis, 10, 64)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	return &repo.RecentCallsCursor{EventTime: time.UnixMilli(ms).UTC(), EventID: id}, nil
}
