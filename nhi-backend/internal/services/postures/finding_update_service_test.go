package postures

import (
	"testing"
	"time"

	models "nhi-backend-api/internal/models/postures"
)

func sp(s string) *string { return &s }

func TestValidateFindingUpdate(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	past := now.Add(-time.Hour)
	cases := []struct {
		name string
		req  models.FindingUpdateRequest
		ok   bool
	}{
		{"empty", models.FindingUpdateRequest{}, false},
		{"note only", models.FindingUpdateRequest{Note: sp("hi")}, true},
		{"assign only", models.FindingUpdateRequest{AssignedTo: sp("bob")}, true},
		{"accept with reason", models.FindingUpdateRequest{Status: sp("ACCEPTED"), AcceptedReason: sp("why")}, true},
		{"accept no reason", models.FindingUpdateRequest{Status: sp("ACCEPTED")}, false},
		{"suppress future", models.FindingUpdateRequest{Status: sp("SUPPRESSED"), SuppressedUntil: &future}, true},
		{"suppress past", models.FindingUpdateRequest{Status: sp("SUPPRESSED"), SuppressedUntil: &past}, false},
		{"suppress no date", models.FindingUpdateRequest{Status: sp("SUPPRESSED")}, false},
		{"reopen", models.FindingUpdateRequest{Status: sp("OPEN")}, true},
		{"resolved", models.FindingUpdateRequest{Status: sp("RESOLVED")}, false},
		{"bogus status", models.FindingUpdateRequest{Status: sp("DONE")}, false},
	}
	for _, c := range cases {
		req := c.req
		if err := validateFindingUpdate(&req, now); (err == nil) != c.ok {
			t.Errorf("%s: err=%v wantOK=%v", c.name, err, c.ok)
		}
	}
}
