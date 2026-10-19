package v1

import (
	"testing"

	chrepo "nhi-backend-api/internal/repository/clickhouse"
)

func TestProcessActivityAnalyticsWeightsByCallCount(t *testing.T) {
	rows := []chrepo.AnalyticsRow{
		{SourceIP: "1.1.1.1", Region: "us-east-1", EventSource: "s3.amazonaws.com", EventName: "GetObject", Hour: 9, Count: 70},
		{SourceIP: "2.2.2.2", Region: "eu-west-1", EventSource: "ec2.amazonaws.com", EventName: "DescribeInstances", Hour: 9, Count: 20},
		{SourceIP: "1.1.1.1", Region: "us-east-1", EventSource: "s3.amazonaws.com", EventName: "PutObject", ErrorCode: "AccessDenied", Hour: 14, Count: 10},
	}
	a := ProcessActivityAnalytics(rows)
	if a.TotalCalls != 100 {
		t.Fatalf("total = %d", a.TotalCalls)
	}
	// Counting rows would give 1/3; weighting gives 10%.
	if a.ErrorRatePct == nil || *a.ErrorRatePct != 10 {
		t.Fatalf("error rate = %v, want 10", a.ErrorRatePct)
	}
	if a.AccessDeniedRatePct == nil || *a.AccessDeniedRatePct != 10 {
		t.Fatalf("access denied rate = %v, want 10", a.AccessDeniedRatePct)
	}
	if a.TopServices[0].Key != "s3" || a.TopServices[0].Pct != 80 {
		t.Fatalf("top service = %+v", a.TopServices[0])
	}
	if a.SourceIPs[0].Key != "1.1.1.1" || a.SourceIPs[0].Pct != 80 {
		t.Fatalf("top ip = %+v", a.SourceIPs[0])
	}
	if a.PeakHours[0].Hour != 9 || a.PeakHours[0].Count != 90 {
		t.Fatalf("peak hour = %+v", a.PeakHours[0])
	}
	if a.TopActions[0].Key != "GetObject" {
		t.Fatalf("top action = %+v", a.TopActions[0])
	}
}

func TestProcessActivityAnalyticsEmpty(t *testing.T) {
	a := ProcessActivityAnalytics(nil)
	if a.TotalCalls != 0 || a.ErrorRatePct != nil || a.AccessDeniedRatePct != nil {
		t.Fatalf("empty input must yield no rates: %+v", a)
	}
}

func TestProcessActivityAnalyticsSkipsBlankDimensions(t *testing.T) {
	a := ProcessActivityAnalytics([]chrepo.AnalyticsRow{{EventSource: "sts.amazonaws.com", EventName: "AssumeRole", Count: 3}})
	if len(a.SourceIPs) != 0 || len(a.ActiveRegions) != 0 {
		t.Fatalf("blank ip/region must be skipped: %+v", a)
	}
}

func TestActivityUsagePattern(t *testing.T) {
	for _, tc := range []struct {
		active, days int
		want         string
	}{{0, 30, "Dormant"}, {20, 30, "Continuous"}, {18, 30, "Continuous"}, {5, 30, "Intermittent"}, {1, 0, "Continuous"}} {
		if got := ActivityUsagePattern(tc.active, tc.days); got != tc.want {
			t.Errorf("ActivityUsagePattern(%d,%d) = %q, want %q", tc.active, tc.days, got, tc.want)
		}
	}
}
