package v1

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TimeRangeError is a client mistake in ?time_range= (HTTP 400). Each handler
// converts it into its own JSON error envelope.
type TimeRangeError struct{ Msg string }

func (e *TimeRangeError) Error() string { return e.Msg }

// ResolvedTimeRange is the normalised result of resolving ?time_range=.
//
// The window is rolling and ends now (UTC). Label is the canonical value that
// must be echoed back in the response — never the raw query parameter, which
// would let a client inject arbitrary strings into the payload.
type ResolvedTimeRange struct {
	Days  int
	Start time.Time
	End   time.Time
	Label string // "90d", "7d", "all", …
}

const maxRangeDays = 36500 // ~100 years — "no lower bound"

// ResolveTimeRange is shared by Identity Detail and Credential Detail so both
// endpoints behave identically for the same input.
//
//	""          → 90 days (the documented default)
//	"1", "55"   → that many days (bare number)
//	"7d", "30d" → that many days (optional "d" suffix)
//	"all"       → everything
//
// Anything else (0, negative, text, > 36500) is a TimeRangeError so the caller
// gets an explicit 400 rather than silently receiving a different window than
// the one it asked for.
func ResolveTimeRange(timeRange string) (*ResolvedTimeRange, error) {
	now := time.Now().UTC()
	tr := strings.ToLower(strings.TrimSpace(timeRange))
	if tr == "" {
		tr = "90"
	}
	if tr == "all" {
		return &ResolvedTimeRange{
			Days:  maxRangeDays,
			Start: now.AddDate(0, 0, -maxRangeDays),
			End:   now,
			Label: "all",
		}, nil
	}
	n, err := strconv.Atoi(strings.TrimSuffix(tr, "d"))
	if err != nil || n < 1 || n > maxRangeDays {
		return nil, &TimeRangeError{Msg: fmt.Sprintf(
			"time_range %q is not valid: use a number of days (e.g. 1, 7, 55) or \"all\"", timeRange)}
	}
	return &ResolvedTimeRange{
		Days:  n,
		Start: now.AddDate(0, 0, -n),
		End:   now,
		Label: fmt.Sprintf("%dd", n),
	}, nil
}

// ResolveCustomTimeRange resolves ?time_range=custom with start_date and
// end_date (YYYY-MM-DD or RFC3339). A bare end date covers that whole day. The
// end is clamped to now and the start must precede it.
func ResolveCustomTimeRange(startDate, endDate string) (*ResolvedTimeRange, error) {
	now := time.Now().UTC()
	start, _, err := parseRangeBound(startDate, false)
	if err != nil {
		return nil, &TimeRangeError{Msg: "start_date is not valid: use YYYY-MM-DD or RFC3339"}
	}
	end, dateOnly, err := parseRangeBound(endDate, true)
	if err != nil {
		return nil, &TimeRangeError{Msg: "end_date is not valid: use YYYY-MM-DD or RFC3339"}
	}
	if dateOnly {
		end = end.AddDate(0, 0, 1).Add(-time.Nanosecond) // inclusive of the whole end day
	}
	if end.After(now) {
		end = now
	}
	if !start.Before(end) {
		return nil, &TimeRangeError{Msg: "start_date must be before end_date"}
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days > maxRangeDays {
		return nil, &TimeRangeError{Msg: "custom time range is too long"}
	}
	return &ResolvedTimeRange{Days: days, Start: start, End: end, Label: "custom"}, nil
}

// resolveRange picks the standard or custom resolver for a request.
func resolveRange(timeRange, startDate, endDate string) (*ResolvedTimeRange, error) {
	if strings.EqualFold(strings.TrimSpace(timeRange), "custom") {
		return ResolveCustomTimeRange(startDate, endDate)
	}
	return ResolveTimeRange(timeRange)
}

func parseRangeBound(s string, _ bool) (t time.Time, dateOnly bool, err error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05"} {
		if t, err = time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC(), false, nil
		}
	}
	if t, err = time.ParseInLocation("2006-01-02", s, time.UTC); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, err
}
