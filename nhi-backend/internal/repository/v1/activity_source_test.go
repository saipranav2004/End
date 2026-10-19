package v1

import (
	"testing"
	"time"

	chrepo "nhi-backend-api/internal/repository/clickhouse"
)

func TestGlobMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, s string
		want       bool
	}{
		{"getobject", "getobject", true},
		{"get*", "getobject", true},
		{"*", "anything", true},
		{"*object", "getobject", true},
		{"g?tobject", "getobject", true},
		{"get*", "putobject", false},
		{"getobject", "getobjectacl", false},
		{"", "", true},
		{"", "x", false},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
	} {
		if got := globMatch(tc.pattern, tc.s); got != tc.want {
			t.Errorf("globMatch(%q,%q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}

func TestMatchGranted(t *testing.T) {
	for _, tc := range []struct {
		gs, ga, us, ua string
		want           bool
	}{
		{"s3", "s3:GetObject", "s3", "GetObject", true},
		{"s3", "GetObject", "s3", "GetObject", true},  // no prefix on the action
		{"", "s3:GetObject", "s3", "GetObject", true}, // service only in the action
		{"s3", "s3:Get*", "s3", "GetObject", true},
		{"s3", "s3:*", "s3", "PutObject", true},
		{"*", "*", "ec2", "RunInstances", true},
		{"s3", "s3:GetObject", "ec2", "GetObject", false},
		{"s3", "s3:GetObject", "s3", "PutObject", false},
		{"S3", "S3:GETOBJECT", "s3", "GetObject", true}, // case-insensitive
		{"s3", "", "s3", "GetObject", false},
	} {
		if got := matchGranted(tc.gs, tc.ga, tc.us, tc.ua); got != tc.want {
			t.Errorf("matchGranted(%q,%q,%q,%q) = %v, want %v", tc.gs, tc.ga, tc.us, tc.ua, got, tc.want)
		}
	}
}

func TestUsageForAggregatesWildcardMatches(t *testing.T) {
	t1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(48 * time.Hour)
	used := []chrepo.UsedPermission{
		{Service: "s3", Action: "GetObject", LastUsedAt: t1, SourceIPs: []string{"1.1.1.1", ""}},
		{Service: "s3", Action: "GetBucketAcl", LastUsedAt: t2, SourceIPs: []string{"2.2.2.2", "1.1.1.1"}},
		{Service: "ec2", Action: "DescribeInstances", LastUsedAt: t2},
	}
	ok, last, ips := usageFor("s3", "s3:Get*", used)
	if !ok || last == nil || !last.Equal(t2) {
		t.Fatalf("ok=%v last=%v", ok, last)
	}
	if len(ips) != 2 || ips[0] != "1.1.1.1" || ips[1] != "2.2.2.2" {
		t.Fatalf("ips = %v", ips)
	}
	if ok, last, ips := usageFor("s3", "s3:PutObject", used); ok || last != nil || len(ips) != 0 {
		t.Fatalf("unused permission reported used: %v %v %v", ok, last, ips)
	}
}

func TestLatestOfAndIdleDays(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	seen := map[string]time.Time{"a": now.Add(-72 * time.Hour), "b": now.Add(-30 * time.Hour)}
	got := latestOf(seen, []string{"a", "b", "missing"})
	if got == nil || !got.Equal(now.Add(-30*time.Hour)) {
		t.Fatalf("latestOf = %v", got)
	}
	if latestOf(seen, []string{"missing"}) != nil || latestOf(nil, []string{"a"}) != nil {
		t.Fatal("no match must be nil")
	}
	if d := wholeDaysSince(got, now); d == nil || *d != 1 {
		t.Fatalf("idle days = %v, want 1", d)
	}
	if wholeDaysSince(nil, now) != nil {
		t.Fatal("nil time must give nil idle days")
	}
	future := now.Add(time.Hour)
	if d := wholeDaysSince(&future, now); d == nil || *d != 0 {
		t.Fatalf("future time = %v, want 0", d)
	}
}

func TestDistinctUsedCountUnionsAcrossKeys(t *testing.T) {
	sets := map[string][]chrepo.UsedAction{
		"role": {{Service: "s3", Action: "GetObject"}, {Service: "sts", Action: "AssumeRole"}},
		"key":  {{Service: "S3", Action: "getobject"}, {Service: "ec2", Action: "RunInstances"}},
	}
	if n := distinctUsedCount(sets, []string{"role", "key", "none"}); n != 3 {
		t.Fatalf("distinct = %d, want 3", n)
	}
	if n := distinctUsedCount(nil, []string{"role"}); n != 0 {
		t.Fatalf("nil sets = %d", n)
	}
}
