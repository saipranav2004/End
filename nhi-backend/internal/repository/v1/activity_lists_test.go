package v1

import (
	"context"
	"testing"
	"time"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
)

var testNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func idRow(name, arn string, credKeys []string, granted int64) identityListRow {
	return identityListRow{
		item:    models.IdentityListItem{Name: name, ARN: arn},
		keys:    append([]string{arn}, credKeys...),
		credKey: credKeys,
		granted: granted,
	}
}

func TestAttachIdentityActivityUsesNewestKeyAndBulkCalls(t *testing.T) {
	src := &fakeActivity{
		available: true,
		seen: map[string]time.Time{
			"arn:user/a": testNow.Add(-10 * 24 * time.Hour),
			"arn:role/r": testNow.Add(-2 * 24 * time.Hour), // newer: through its credential
			"arn:user/b": testNow.Add(-40 * time.Hour),
		},
		creators: map[string]string{"arn:user/a": "arn:user/admin"},
		used: map[string][]chrepo.UsedAction{
			"arn:role/r": {{Service: "s3", Action: "GetObject"}, {Service: "sts", Action: "AssumeRole"}},
			"AKIAKEY":    {{Service: "s3", Action: "GetObject"}, {Service: "ec2", Action: "RunInstances"}},
		},
	}
	rows := []identityListRow{
		idRow("a", "arn:user/a", []string{"arn:role/r", "AKIAKEY"}, 10),
		idRow("b", "arn:user/b", nil, 0),
		idRow("c", "arn:user/c", nil, 5),
	}
	attachIdentityActivity(context.Background(), rows, src, testNow)

	a := rows[0].item
	if a.LastActivityAt == nil || !a.LastActivityAt.Equal(testNow.Add(-2*24*time.Hour)) {
		t.Fatalf("a last activity = %v", a.LastActivityAt)
	}
	if a.IdleDays == nil || *a.IdleDays != 2 {
		t.Fatalf("a idle days = %v", a.IdleDays)
	}
	if a.CreatedBy == nil || *a.CreatedBy != "arn:user/admin" {
		t.Fatalf("a created by = %v", a.CreatedBy)
	}
	// 3 distinct actions (s3:GetObject shared, AssumeRole, RunInstances) of 10 granted.
	if a.PermissionUtilizationPct == nil || *a.PermissionUtilizationPct != 30 {
		t.Fatalf("a utilisation = %v, want 30", a.PermissionUtilizationPct)
	}
	if b := rows[1].item; b.PermissionUtilizationPct != nil || b.LastActivityAt == nil {
		t.Fatalf("b: granted 0 must give nil utilisation, last activity set: %+v", b)
	}
	if c := rows[2].item; c.LastActivityAt != nil || c.IdleDays != nil || c.PermissionUtilizationPct == nil || *c.PermissionUtilizationPct != 0 {
		t.Fatalf("c: no events -> nil activity, 0%% utilisation: %+v", c)
	}
	if src.seenCalls != 1 {
		t.Fatalf("LastSeen called %d times, want one bulk call", src.seenCalls)
	}
	// Utilisation is measured over the last 90 days, not all time.
	if want := testNow.AddDate(0, 0, -90); !src.lastSince.Equal(want) {
		t.Fatalf("used actions window starts %v, want %v", src.lastSince, want)
	}
}

func TestAttachIdentityActivityWhenUnavailableLeavesFieldsNil(t *testing.T) {
	rows := []identityListRow{idRow("a", "arn:user/a", nil, 10)}
	attachIdentityActivity(context.Background(), rows, &fakeActivity{available: false}, testNow)
	if it := rows[0].item; it.LastActivityAt != nil || it.IdleDays != nil || it.CreatedBy != nil || it.PermissionUtilizationPct != nil {
		t.Fatalf("unavailable source must leave activity unknown: %+v", it)
	}
	attachIdentityActivity(context.Background(), rows, noActivity{}, testNow) // must not panic
}

func intp(v int) *int { return &v }

func TestSortIdentityRowsNullsLastBothDirections(t *testing.T) {
	mk := func(name string, idle *int) identityListRow {
		return identityListRow{item: models.IdentityListItem{Name: name, IdleDays: idle}}
	}
	rows := []identityListRow{mk("n1", nil), mk("old", intp(50)), mk("new", intp(1)), mk("n0", nil), mk("mid", intp(10))}

	sortIdentityRows(rows, "idle_days", true) // desc
	want := []string{"old", "mid", "new", "n0", "n1"}
	for i, w := range want {
		if rows[i].item.Name != w {
			t.Fatalf("desc order[%d] = %s, want %s (%v)", i, rows[i].item.Name, w, names(rows))
		}
	}
	sortIdentityRows(rows, "idle_days", false) // asc
	want = []string{"new", "mid", "old", "n0", "n1"}
	for i, w := range want {
		if rows[i].item.Name != w {
			t.Fatalf("asc order[%d] = %s, want %s (%v)", i, rows[i].item.Name, w, names(rows))
		}
	}
}

func names(rows []identityListRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.item.Name)
	}
	return out
}

func TestSortIdentityRowsByLastActivityAndUtilisation(t *testing.T) {
	t1, t2 := testNow.Add(-time.Hour), testNow.Add(-48*time.Hour)
	rows := []identityListRow{
		{item: models.IdentityListItem{Name: "b", LastActivityAt: &t2, PermissionUtilizationPct: intp(5)}},
		{item: models.IdentityListItem{Name: "a", LastActivityAt: &t1, PermissionUtilizationPct: intp(50)}},
		{item: models.IdentityListItem{Name: "z"}},
	}
	sortIdentityRows(rows, "last_activity_at", true)
	if rows[0].item.Name != "a" || rows[1].item.Name != "b" || rows[2].item.Name != "z" {
		t.Fatalf("last_activity desc: %v", names(rows))
	}
	sortIdentityRows(rows, "permission_utilization_pct", false)
	if rows[0].item.Name != "b" || rows[1].item.Name != "a" || rows[2].item.Name != "z" {
		t.Fatalf("utilisation asc: %v", names(rows))
	}
}

func TestCredentialActivityKeysAndAttach(t *testing.T) {
	if k := credentialActivityKeys("arn:role/r", "r", "iam_role"); len(k) != 1 || k[0] != "arn:role/r" {
		t.Fatalf("role keys = %v", k)
	}
	k := credentialActivityKeys("arn:aws:iam::1:access-key/AKIAX", "AKIAX", "access_key")
	if len(k) != 2 || k[1] != "AKIAX" {
		t.Fatalf("access key keys = %v", k)
	}
	if k := credentialActivityKeys("", "", "iam_role"); len(k) != 0 {
		t.Fatalf("empty = %v", k)
	}

	src := &fakeActivity{available: true, seen: map[string]time.Time{"AKIAX": testNow.Add(-5 * 24 * time.Hour)}}
	rows := []credentialListRow{
		{item: models.CredentialListItem{Name: "AKIAX"}, keys: k},
		{item: models.CredentialListItem{Name: "never"}, keys: []string{"arn:role/never"}},
	}
	attachCredentialActivity(context.Background(), rows, src, testNow)
	if rows[0].item.IdleDays == nil || *rows[0].item.IdleDays != 5 || rows[0].item.LastUsedAt == nil {
		t.Fatalf("used key: %+v", rows[0].item)
	}
	if rows[1].item.LastUsedAt != nil || rows[1].item.IdleDays != nil {
		t.Fatalf("never used must stay nil: %+v", rows[1].item)
	}
}

func TestSortCredentialRows(t *testing.T) {
	t1, t2 := testNow.Add(-time.Hour), testNow.Add(-72*time.Hour)
	rows := []credentialListRow{
		{item: models.CredentialListItem{Name: "none"}},
		{item: models.CredentialListItem{Name: "old", LastUsedAt: &t2, IdleDays: intp(3)}},
		{item: models.CredentialListItem{Name: "new", LastUsedAt: &t1, IdleDays: intp(0)}},
	}
	sortCredentialRows(rows, "last_used_at", true)
	if rows[0].item.Name != "new" || rows[1].item.Name != "old" || rows[2].item.Name != "none" {
		t.Fatalf("last_used desc: %v / %v / %v", rows[0].item.Name, rows[1].item.Name, rows[2].item.Name)
	}
	sortCredentialRows(rows, "idle_days", true)
	if rows[0].item.Name != "old" || rows[2].item.Name != "none" {
		t.Fatalf("idle desc: %v / %v / %v", rows[0].item.Name, rows[1].item.Name, rows[2].item.Name)
	}
}
