package v1

import (
	"math"
	"sort"
	"strings"

	chrepo "nhi-backend-api/internal/repository/clickhouse"
)

type CountItem struct {
	Key   string
	Count uint64
}

type PctItem struct {
	Key   string
	Count uint64
	Pct   float64
}

type HourItem struct {
	Hour  int
	Count uint64
}

// ActivityAnalytics is the processed result of the ClickHouse aggregation.
type ActivityAnalytics struct {
	TotalCalls          uint64
	SourceIPs           []PctItem
	ActiveRegions       []CountItem
	TopServices         []PctItem // Key is the short service name ("s3")
	TopActions          []CountItem
	ErrorRatePct        *float64 // nil when there are no calls
	AccessDeniedRatePct *float64
	PeakHours           []HourItem
}

// ProcessActivityAnalytics turns raw rows into the analytics cards. Every
// figure is weighted by the row's call count (rows are already aggregated, so
// counting rows would be wrong).
func ProcessActivityAnalytics(rows []chrepo.AnalyticsRow) ActivityAnalytics {
	var a ActivityAnalytics
	ips := map[string]uint64{}
	regions := map[string]uint64{}
	services := map[string]uint64{}
	actions := map[string]uint64{}
	hours := map[int]uint64{}
	var errs, denied uint64

	for _, r := range rows {
		a.TotalCalls += r.Count
		if r.SourceIP != "" {
			ips[r.SourceIP] += r.Count
		}
		if r.Region != "" {
			regions[r.Region] += r.Count
		}
		if svc := chrepo.ServiceName(r.EventSource); svc != "" {
			services[svc] += r.Count
		}
		if r.EventName != "" {
			actions[r.EventName] += r.Count
		}
		hours[int(r.Hour)] += r.Count
		if r.ErrorCode != "" {
			errs += r.Count
			if strings.HasPrefix(r.ErrorCode, "AccessDenied") {
				denied += r.Count
			}
		}
	}
	if a.TotalCalls == 0 {
		return a
	}

	total := float64(a.TotalCalls)
	a.SourceIPs = pctItems(ips, total)
	a.TopServices = pctItems(services, total)
	a.ActiveRegions = countItems(regions)
	a.TopActions = countItems(actions)
	for h, n := range hours {
		a.PeakHours = append(a.PeakHours, HourItem{Hour: h, Count: n})
	}
	sort.Slice(a.PeakHours, func(i, j int) bool {
		if a.PeakHours[i].Count != a.PeakHours[j].Count {
			return a.PeakHours[i].Count > a.PeakHours[j].Count
		}
		return a.PeakHours[i].Hour < a.PeakHours[j].Hour
	})
	er := roundTenth(float64(errs) / total * 100)
	dr := roundTenth(float64(denied) / total * 100)
	a.ErrorRatePct, a.AccessDeniedRatePct = &er, &dr
	return a
}

func countItems(m map[string]uint64) []CountItem {
	out := make([]CountItem, 0, len(m))
	for k, n := range m {
		out = append(out, CountItem{Key: k, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func pctItems(m map[string]uint64, total float64) []PctItem {
	items := countItems(m)
	out := make([]PctItem, len(items))
	for i, it := range items {
		out[i] = PctItem{Key: it.Key, Count: it.Count, Pct: roundTenth(float64(it.Count) / total * 100)}
	}
	return out
}

func roundTenth(f float64) float64 { return math.Round(f*10) / 10 }

// ActivityUsagePattern keeps the labels the frontend already knows. activeDays
// is the number of days with at least one event (the length of the volume
// chart), rangeDays the length of the window.
func ActivityUsagePattern(activeDays, rangeDays int) string {
	if rangeDays < 1 {
		rangeDays = 1
	}
	switch {
	case activeDays == 0:
		return "Dormant"
	case float64(activeDays)/float64(rangeDays) >= 0.6:
		return "Continuous"
	default:
		return "Intermittent"
	}
}
