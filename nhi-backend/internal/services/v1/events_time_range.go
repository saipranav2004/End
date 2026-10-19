package v1

import (
	"fmt"
	"strings"
	"time"
)

// EventsDefaultTimeRange is the window of the Identity Event Graph when
// ?time_range= is omitted.
const EventsDefaultTimeRange = "24h"

// eventsRanges are the rolling windows the Identity Event Graph accepts, in
// addition to "custom". They include hour-sized windows, which the shared
// ResolveTimeRange (whole days only) deliberately does not, so the other
// endpoints are unaffected.
var eventsRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

// EventsTimeRangeValues lists the accepted ?time_range= values.
const EventsTimeRangeValues = "1h, 6h, 24h, 7d, 30d, 90d, custom"

// ResolveEventsRange resolves ?time_range= (and start_date/end_date for
// "custom") for the Identity Event Graph. The returned Label is the canonical
// value to echo back, never the raw input.
func ResolveEventsRange(timeRange, startDate, endDate string) (*ResolvedTimeRange, error) {
	tr := strings.ToLower(strings.TrimSpace(timeRange))
	if tr == "" {
		tr = EventsDefaultTimeRange
	}
	if tr == "custom" {
		return ResolveCustomTimeRange(startDate, endDate)
	}
	d, ok := eventsRanges[tr]
	if !ok {
		return nil, &TimeRangeError{Msg: fmt.Sprintf("time_range %q is not valid: use one of %s", timeRange, EventsTimeRangeValues)}
	}
	now := time.Now().UTC()
	return &ResolvedTimeRange{
		Days:  int(d.Hours()/24) + 1,
		Start: now.Add(-d),
		End:   now,
		Label: tr,
	}, nil
}
