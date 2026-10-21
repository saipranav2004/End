package v1

import (
	"context"
	"testing"
	"time"

	chrepo "nhi-backend-api/internal/repository/clickhouse"
)

func tp(t time.Time) *time.Time { return &t }

func TestClassifyIdentitiesStaleUsesActivityThenCreationDate(t *testing.T) {
	old := testNow.Add(-200 * 24 * time.Hour)
	recent := testNow.Add(-5 * 24 * time.Hour)
	rows := []identityActivityRow{
		// old creation but recent CloudTrail activity through its role -> active
		{arn: "arn:i/active", baseline: tp(old), keys: []string{"arn:i/active", "arn:role/r"}},
		// old creation, no activity ever -> stale (never-used baseline)
		{arn: "arn:i/never", baseline: tp(old), keys: []string{"arn:i/never"}},
		// new, never active -> not stale
		{arn: "arn:i/new", baseline: tp(recent), keys: []string{"arn:i/new"}},
		// last activity long ago -> stale even though created recently is impossible; use old activity
		{arn: "arn:i/dormant", baseline: tp(recent), keys: []string{"arn:i/dormant"}},
		// no timestamp at all -> never asserted stale
		{arn: "arn:i/unknown", keys: []string{"arn:i/unknown"}},
	}
	src := &fakeActivity{available: true, seen: map[string]time.Time{
		"arn:role/r":    recent,
		"arn:i/dormant": testNow.Add(-120 * 24 * time.Hour),
	}}
	stale, _ := classifyIdentities(context.Background(), rows, src, testNow)
	if stale != 2 { // never + dormant
		t.Fatalf("stale = %d, want 2", stale)
	}
}

func TestClassifyIdentitiesWithoutCloudTrailFallsBackToCreationDate(t *testing.T) {
	old := testNow.Add(-200 * 24 * time.Hour)
	rows := []identityActivityRow{
		{arn: "a", baseline: tp(old), keys: []string{"a"}},
		{arn: "b", baseline: tp(testNow.Add(-time.Hour)), keys: []string{"b"}},
	}
	stale, _ := classifyIdentities(context.Background(), rows, &fakeActivity{available: false}, testNow)
	if stale != 1 {
		t.Fatalf("stale = %d, want 1", stale)
	}
}

func TestClassifyIdentitiesShadowNeedsNoOwnerAndNoKnownCreator(t *testing.T) {
	rows := []identityActivityRow{
		{arn: "arn:i/owned", keys: []string{"arn:i/owned"}},
		{arn: "arn:i/nobody", ownerBlank: true, keys: []string{"arn:i/nobody"}},
		{arn: "arn:i/madeby", ownerBlank: true, keys: []string{"arn:i/madeby"}},
	}
	src := &fakeActivity{available: true, creators: map[string]string{"arn:i/madeby": "arn:user/admin"}}
	if _, shadow := classifyIdentities(context.Background(), rows, src, testNow); shadow != 1 {
		t.Fatalf("shadow = %d, want 1 (owner-less with no known creator)", shadow)
	}
	// Without CloudTrail the owner-missing proxy applies alone.
	if _, shadow := classifyIdentities(context.Background(), rows, &fakeActivity{available: false}, testNow); shadow != 2 {
		t.Fatalf("shadow without cloudtrail = %d, want 2", shadow)
	}
}

func TestClassifyCredentialsStaleAndUsedActions(t *testing.T) {
	old := testNow.Add(-300 * 24 * time.Hour)
	rows := []credentialActivityRow{
		{baseline: tp(old), keys: []string{"arn:role/used"}},
		{baseline: tp(old), keys: []string{"arn:role/idle"}},
		{baseline: tp(testNow), keys: []string{"arn:role/fresh"}},
		{baseline: tp(old), keys: []string{"arn:aws:iam::1:access-key/AKIAX", "AKIAX"}},
	}
	src := &fakeActivity{
		available: true,
		seen:      map[string]time.Time{"arn:role/used": testNow.Add(-time.Hour), "AKIAX": testNow.Add(-100 * 24 * time.Hour)},
		used: map[string][]chrepo.UsedAction{
			"arn:role/used": {{Service: "s3", Action: "GetObject"}, {Service: "s3", Action: "PutObject"}},
			"AKIAX":         {{Service: "ec2", Action: "RunInstances"}},
		},
	}
	stale, used, utilization := classifyCredentials(context.Background(), rows, src, testNow)
	if len(utilization) != 5 || utilization[0].Bucket != "used_7d" {
		t.Fatalf("utilization buckets = %+v", utilization)
	}
	if stale != 2 { // idle (never used, old) + access key (last used 100d ago)
		t.Fatalf("stale = %d, want 2", stale)
	}
	if used != 3 {
		t.Fatalf("used actions = %d, want 3", used)
	}
}
