package v1

import (
	"testing"

	models "nhi-backend-api/internal/models/v1"
)

func TestHighlightsOrderAndLabels(t *testing.T) {
	got := Highlights(map[changePair]int{
		{models.ChangeCreated, models.EntityIdentity}:    5,
		{models.ChangeDeleted, models.EntityCredential}:  3,
		{models.ChangeModified, models.EntityPermission}: 6,
		{models.ChangeCreated, models.EntityTrustEdge}:   1,
		{models.ChangeDeleted, models.EntityIdentity}:    0,
	})
	want := []string{
		"6 permission changes detected",
		"5 new identities discovered",
		"3 credentials removed",
		"1 trust relationship added",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d highlights, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Label != w {
			t.Errorf("highlight %d = %q, want %q", i, got[i].Label, w)
		}
	}
	if one := highlightLabel(models.ChangeCreated, models.EntityIdentity, 1); one != "1 new identity discovered" {
		t.Errorf("singular label = %q", one)
	}
}

func TestDescribeChangeFallback(t *testing.T) {
	cases := map[string][4]string{
		"Identity deploy-bot was added":                 {models.ChangeCreated, models.EntityIdentity, "deploy-bot", ""},
		"Credential AKIA1 was removed":                  {models.ChangeDeleted, models.EntityCredential, "", "arn:aws:iam::1:user/x/accesskey/AKIA1"},
		"Permission of ci-deployer was changed":         {models.ChangeModified, models.EntityPermission, "ci-deployer", ""},
		"Trust relationship of deploy-role was removed": {models.ChangeDeleted, models.EntityTrustEdge, "deploy-role", ""},
	}
	for want, in := range cases {
		if got := DescribeChange(in[0], in[1], in[2], in[3]); got != want {
			t.Errorf("DescribeChange(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestUUIDLike(t *testing.T) {
	if !uuidLike("62676623-7294-50b9-9533-37b161ba75a4") || uuidLike("not-a-uuid") || uuidLike("62676623x7294-50b9-9533-37b161ba75a4") {
		t.Error("uuidLike misclassifies")
	}
}
