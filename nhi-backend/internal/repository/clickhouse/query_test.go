package clickhouse

import (
	"strings"
	"testing"
	"time"
)

func TestSubjectDropsBlankAndDuplicateKeys(t *testing.T) {
	s := NewSubject("a", "", "  ", "a", "b")
	if len(s.Keys) != 2 || s.Keys[0] != "a" || s.Keys[1] != "b" {
		t.Fatalf("keys = %v", s.Keys)
	}
	if !NewSubject("", " ").empty() {
		t.Fatal("blank-only subject must be empty")
	}
}

func TestBuildFilterMatchesAnyOfTheThreeFields(t *testing.T) {
	clause, args, ok := buildFilter(NewSubject("arn:aws:iam::1:user/a"), Scope{Accounts: []string{"111111111111"}})
	if !ok {
		t.Fatal("expected ok")
	}
	for _, want := range []string{"caller_arn IN", "session_issuer_arn IN", "access_key_id IN", " OR ", "account_id IN"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("filter missing %q: %s", want, clause)
		}
	}
	if len(args) != 2 { // keys + accounts
		t.Fatalf("args = %d, want 2", len(args))
	}
}

func TestBuildFilterIssuerOnly(t *testing.T) {
	clause, _, ok := buildFilter(Subject{Keys: []string{"arn:aws:iam::1:role/r"}, IssuerOnly: true}, Scope{Accounts: []string{"1"}})
	if !ok || strings.Contains(clause, "caller_arn") || strings.Contains(clause, "access_key_id") {
		t.Fatalf("issuer-only filter wrong: %s", clause)
	}
}

func TestBuildFilterServiceFilter(t *testing.T) {
	clause, args, _ := buildFilter(NewSubject("a"), Scope{Accounts: []string{"1"}, Service: " S3 "})
	if !strings.Contains(clause, "lower(event_source) IN") || len(args) != 3 {
		t.Fatalf("service filter missing: %s (%d args)", clause, len(args))
	}
	clause, _, _ = buildFilter(NewSubject("a"), Scope{Accounts: []string{"1"}})
	if strings.Contains(clause, "event_source") {
		t.Fatalf("unfiltered scope must not filter on service: %s", clause)
	}
}

func TestBuildFilterRefusesWithoutSubjectOrAccounts(t *testing.T) {
	if _, _, ok := buildFilter(Subject{}, Scope{Accounts: []string{"1"}}); ok {
		t.Fatal("empty subject must not query")
	}
	if _, _, ok := buildFilter(NewSubject(""), Scope{Accounts: []string{"1"}}); ok {
		t.Fatal("blank-only subject must not query")
	}
	if _, _, ok := buildFilter(NewSubject("a"), Scope{}); ok {
		t.Fatal("no accounts (tenant boundary) must not query")
	}
}

func TestBuildFilterTimeBounds(t *testing.T) {
	now := time.Now()
	clause, args, _ := buildFilter(NewSubject("a"), Scope{
		Accounts: []string{"1"}, Start: now.Add(-time.Hour), End: now,
	})
	if !strings.Contains(clause, "event_time >=") || !strings.Contains(clause, "event_time <=") {
		t.Fatalf("expected both bounds: %s", clause)
	}
	if len(args) != 4 { // keys, accounts, start, end
		t.Fatalf("args = %d, want 4", len(args))
	}
	clause, _, _ = buildFilter(NewSubject("a"), Scope{Accounts: []string{"1"}})
	if strings.Contains(clause, "event_time") {
		t.Fatalf("unbounded scope must not filter on time: %s", clause)
	}
}

func TestServiceName(t *testing.T) {
	for in, want := range map[string]string{
		"s3.amazonaws.com": "s3", "STS.amazonaws.com": "sts", "": "", "custom.example.com": "custom.example.com",
	} {
		if got := ServiceName(in); got != want {
			t.Errorf("ServiceName(%q) = %q, want %q", in, got, want)
		}
	}
}
