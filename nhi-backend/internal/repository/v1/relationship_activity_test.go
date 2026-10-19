package v1

import (
	"context"
	"testing"
	"time"

	chrepo "nhi-backend-api/internal/repository/clickhouse"
	"nhi-backend-api/internal/scope"
)

func inScope() *scope.DataScope {
	return &scope.DataScope{AccountIDs: []string{"111111111111"}, Regions: []string{"us-east-1", "global"}}
}

const rootARN = "arn:aws:iam::111111111111:role/app"

func TestActivityGraphEdgesAccessesAndCreator(t *testing.T) {
	seen := tabTime(2026, 10, 1)
	src := &fakeActivity{
		available: true,
		creators:  map[string]string{rootARN: "arn:aws:iam::111111111111:user/admin"},
		edges: []chrepo.AccessEdge{
			{TargetARN: "arn:aws:s3:::bucket", TargetType: "AWS::S3::Bucket", LastObservedAt: seen},                   // no account in ARN: the root's own call, kept
			{TargetARN: "arn:aws:iam::111111111111:role/other", TargetType: "AWS::IAM::Role", LastObservedAt: seen},   // in scope
			{TargetARN: "arn:aws:iam::999999999999:role/foreign", TargetType: "AWS::IAM::Role", LastObservedAt: seen}, // other account
			{TargetARN: rootARN, LastObservedAt: seen},                                                                // self
		},
	}
	edges, hints := activityGraphEdges(context.Background(), inScope(), src, rootARN, []string{rootARN}, true, nil, time.Time{}, time.Time{})

	types := map[string]string{}
	for _, e := range edges {
		types[e.EdgeType+"->"+e.Target] = e.Source
	}
	if len(edges) != 3 {
		t.Fatalf("want accesses(bucket) + accesses(other) + created_by(admin), got %+v", edges)
	}
	if _, ok := types["accesses->arn:aws:s3:::bucket"]; !ok {
		t.Fatalf("missing S3 accesses edge: %+v", edges)
	}
	if _, ok := types["accesses->arn:aws:iam::111111111111:role/other"]; !ok {
		t.Fatalf("missing in-scope accesses edge: %+v", edges)
	}
	if _, ok := types["created_by->arn:aws:iam::111111111111:user/admin"]; !ok {
		t.Fatalf("missing created_by edge: %+v", edges)
	}
	if h := hints["arn:aws:iam::111111111111:role/other"]; h.nodeType != "resource" || h.subtype != "AWS::IAM::Role" {
		t.Fatalf("hint = %+v", h)
	}
	for _, e := range edges {
		if e.EdgeType == "created_by" && !e.CreatedAt.IsZero() {
			t.Fatal("created_by has no event time")
		}
	}
}

func TestActivityGraphEdgesHonoursEdgeTypeFilterAndAvailability(t *testing.T) {
	src := &fakeActivity{
		available: true,
		creators:  map[string]string{rootARN: "arn:aws:iam::111111111111:user/admin"},
		edges:     []chrepo.AccessEdge{{TargetARN: "arn:aws:iam::111111111111:role/other"}},
	}
	only, _ := activityGraphEdges(context.Background(), inScope(), src, rootARN, []string{rootARN}, true, []string{"CREATED_BY"}, time.Time{}, time.Time{})
	if len(only) != 1 || only[0].EdgeType != "created_by" {
		t.Fatalf("filter created_by: %+v", only)
	}
	none, _ := activityGraphEdges(context.Background(), inScope(), src, rootARN, []string{rootARN}, true, []string{"assume_role"}, time.Time{}, time.Time{})
	if len(none) != 0 {
		t.Fatalf("filter assume_role must drop ClickHouse edges: %+v", none)
	}
	noCreator, _ := activityGraphEdges(context.Background(), inScope(), src, rootARN, []string{rootARN}, false, nil, time.Time{}, time.Time{})
	for _, e := range noCreator {
		if e.EdgeType == "created_by" {
			t.Fatal("credentials must not get created_by edges")
		}
	}
	off, _ := activityGraphEdges(context.Background(), inScope(), &fakeActivity{available: false}, rootARN, []string{rootARN}, true, nil, time.Time{}, time.Time{})
	if len(off) != 0 {
		t.Fatalf("unavailable source: %+v", off)
	}
	empty, _ := activityGraphEdges(context.Background(), &scope.DataScope{}, src, rootARN, []string{rootARN}, true, nil, time.Time{}, time.Time{})
	if len(empty) != 0 {
		t.Fatalf("empty scope must keep every edge out: %+v", empty)
	}
}

func TestMergeGraphEdgesDedupesAndAddsEndpoints(t *testing.T) {
	existing := []graphEdgeRow{{Source: "a", Target: "b", EdgeType: "assume_role"}}
	extra := []graphEdgeRow{
		{Source: "a", Target: "b", EdgeType: "assume_role"}, // duplicate
		{Source: "a", Target: "c", EdgeType: "accesses"},
	}
	edges, arns := mergeGraphEdges(existing, []string{"a", "b"}, extra)
	if len(edges) != 2 {
		t.Fatalf("edges = %+v", edges)
	}
	if len(arns) != 3 || arns[2] != "c" {
		t.Fatalf("arns = %v", arns)
	}
}

func TestObservedAtAndResourceNodeInfo(t *testing.T) {
	if observedAt(time.Time{}) != nil {
		t.Fatal("zero time must be nil")
	}
	now := time.Now()
	if p := observedAt(now); p == nil || !p.Equal(now) {
		t.Fatal("real time must pass through")
	}
	n := resourceNodeInfo("arn:aws:ec2:us-east-1:111111111111:instance/i-0abc")
	if n.name != "i-0abc" || n.nodeType != "unknown" || n.subtype != "ec2:instance" {
		t.Fatalf("got %+v", n)
	}
	if n := resourceNodeInfo("arn:aws:s3:::bucket"); n.subtype != "s3" {
		t.Fatalf("bucket subtype = %q", n.subtype)
	}
}

func TestSummarizeUsedPermissionsGroupsByService(t *testing.T) {
	used := []chrepo.UsedPermission{
		{Service: "s3", Action: "GetObject", ResourceARN: "arn:aws:s3:::a"},
		{Service: "s3", Action: "getobject", ResourceARN: "arn:aws:s3:::b"}, // same action, other resource
		{Service: "s3", Action: "PutObject", ResourceARN: "arn:aws:s3:::a"},
		{Service: "sts", Action: "AssumeRole", ResourceARN: "arn:aws:iam::1:role/r"},
		{Service: "", Action: "Mystery"},
	}
	sum := summarizeUsedPermissions(used)
	got := map[string][2]interface{}{}
	for _, s := range sum {
		got[s.Service] = [2]interface{}{s.AllowedActions, s.ResourceScope}
	}
	if got["s3"][0] != 2 || got["s3"][1] != "Scoped" {
		t.Fatalf("s3 = %v", got["s3"])
	}
	if got["sts"][0] != 1 || got["sts"][1] != "arn:aws:iam::1:role/r" {
		t.Fatalf("sts = %v", got["sts"])
	}
	if got["unknown"][0] != 1 || got["unknown"][1] != "Unknown" {
		t.Fatalf("unknown = %v", got["unknown"])
	}
	if len(sum) != 3 || sum[0].Service != "s3" { // sorted by service name
		t.Fatalf("order: %+v", sum)
	}
	if len(summarizeUsedPermissions(nil)) != 0 {
		t.Fatal("nothing used -> empty summary")
	}
}

func TestLastAssumedAt(t *testing.T) {
	t1, t2 := tabTime(2026, 9, 1), tabTime(2026, 10, 1)
	used := []chrepo.UsedPermission{
		{Service: "sts", Action: "AssumeRole", ResourceARN: "arn:r", LastUsedAt: t1},
		{Service: "sts", Action: "AssumeRoleWithWebIdentity", ResourceARN: "arn:r", LastUsedAt: t2},
		{Service: "s3", Action: "AssumeRole", ResourceARN: "arn:r", LastUsedAt: tabTime(2027, 1, 1)}, // wrong service
		{Service: "sts", Action: "AssumeRole", ResourceARN: "arn:other", LastUsedAt: tabTime(2027, 1, 1)},
	}
	if got := lastAssumedAt(used, "arn:r"); got == nil || !got.Equal(t2) {
		t.Fatalf("got %v, want %v", got, t2)
	}
	if lastAssumedAt(used, "arn:none") != nil || lastAssumedAt(nil, "arn:r") != nil {
		t.Fatal("no assume event must be nil")
	}
}

func tabTime(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
