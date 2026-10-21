package v1

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestScanChangesCarryTheReferenceNames(t *testing.T) {
	at := time.Date(2026, 10, 16, 20, 16, 42, 0, time.UTC)
	cred := "c1"
	resp := ScanChangesResponse{
		Scan:    ScanInfo{ScanID: "scan-1", Status: "completed", CompletedAt: &at},
		Summary: ScanChangeSummary{Total: 3, ByChangeType: map[string]int{ChangeCreated: 2, ChangeModified: 1}, ByEntityType: map[string]int{EntityCredential: 3}},
		Changes: []ScanChange{{ChangeID: "x", EntityType: EntityCredential, CredentialID: &cred, ScanAt: &at,
			CloudTrailEvidence: &CloudTrailEvidence{CallerARN: "arn:caller", EventTime: at}}},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"scan":{`, `"scan_id":"scan-1"`, `"scan_time":"2026-10-16T20:16:42Z"`, `"created":2`, `"modified":1`,
		`"by_entity":{"credential":3}`, `"by_entity_type"`, `"id":"x"`, `"change_id":"x"`, `"entity_id":"c1"`,
		`"created_by":"arn:caller"`, `"caller_arn":"arn:caller"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}
