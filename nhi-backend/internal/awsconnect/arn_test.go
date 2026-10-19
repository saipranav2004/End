package awsconnect

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRoleARN(t *testing.T) {
	valid := map[string]RoleARN{
		"arn:aws:iam::123456789012:role/NHIPlatformCollector":          {AccountID: "123456789012", Name: "NHIPlatformCollector"},
		"  arn:aws:iam::123456789012:role/NHIPlatformCollector  ":      {AccountID: "123456789012", Name: "NHIPlatformCollector"},
		"arn:aws:iam::123456789012:role/security/NHIPlatformCollector": {AccountID: "123456789012", Name: "NHIPlatformCollector"},
		"arn:aws:iam::123456789012:role/a/b/c/name+=,.@-_":             {AccountID: "123456789012", Name: "name+=,.@-_"},
	}
	for in, want := range valid {
		got, err := ParseRoleARN(in)
		if err != nil {
			t.Fatalf("ParseRoleARN(%q) error: %v", in, err)
		}
		if got.AccountID != want.AccountID || got.Name != want.Name || got.ARN != strings.TrimSpace(in) {
			t.Fatalf("ParseRoleARN(%q) = %+v, want %+v", in, got, want)
		}
	}

	invalid := map[string]string{
		"":                                "Paste the ARN",
		"   ":                             "Paste the ARN",
		"NHIPlatformCollector":            "not a role ARN",
		"arn:aws:iam::12345678901:role/x": "not a role ARN",
		"arn:aws:iam::123456789012:role/": "not a role ARN",
		"arn:aws:iam::123456789012:role/" + strings.Repeat("a", 65):   "not a role ARN",
		"arn:aws:iam::123456789012:instance-profile/NHI":              "instance profile",
		"arn:aws:iam::123456789012:user/alice":                        "IAM user",
		"arn:aws:sts::123456789012:assumed-role/NHI/session":          "not a role ARN",
		"arn:aws-cn:iam::123456789012:role/NHI":                       "standard AWS partition",
		"arn:aws:iam::123456789012:role/NHI\nx":                       "not a role ARN",
		"arn:aws:iam::123456789012:role/" + strings.Repeat("p/", 600): "too long",
	}
	for in, wantText := range invalid {
		_, err := ParseRoleARN(in)
		var target *ErrInvalidRoleARN
		if !errors.As(err, &target) {
			t.Fatalf("ParseRoleARN(%q) = %v, want ErrInvalidRoleARN", in, err)
		}
		if !strings.Contains(target.Message, wantText) {
			t.Fatalf("ParseRoleARN(%q) message %q, want it to mention %q", in, target.Message, wantText)
		}
	}
}

func TestNewPlatform(t *testing.T) {
	p, err := NewPlatform("123456789012", "ap-south-1",
		"arn:aws:events:ap-south-1:123456789012:event-bus/nhi-central-bus",
		"arn:aws:iam::123456789012:role/NHIPlatformAssumer")
	if err != nil {
		t.Fatalf("NewPlatform: %v", err)
	}
	if p.EventBusName != "nhi-central-bus" {
		t.Fatalf("EventBusName = %q", p.EventBusName)
	}

	bad := [][4]string{
		{"12345678901", "ap-south-1", "arn:aws:events:ap-south-1:12345678901:event-bus/b", "arn:aws:iam::12345678901:role/r"},
		{"123456789012", "ap-south-1", "arn:aws:events:us-east-1:123456789012:event-bus/b", "arn:aws:iam::123456789012:role/r"},
		{"123456789012", "ap-south-1", "arn:aws:events:ap-south-1:999999999999:event-bus/b", "arn:aws:iam::123456789012:role/r"},
		{"123456789012", "ap-south-1", "arn:aws:events:ap-south-1:123456789012:event-bus/b", "arn:aws:iam::999999999999:role/r"},
		{"123456789012", "ap-south-1", "arn:aws:events:ap-south-1:123456789012:rule/b", "arn:aws:iam::123456789012:role/r"},
		{"123456789012", "ap-south-1", "arn:aws:events:ap-south-1:123456789012:event-bus/b", "not-an-arn"},
	}
	for _, c := range bad {
		if _, err := NewPlatform(c[0], c[1], c[2], c[3]); !errors.Is(err, ErrPlatformMisconfigured) {
			t.Fatalf("NewPlatform(%v) = %v, want ErrPlatformMisconfigured", c, err)
		}
	}
}

func TestPolicyHasStatement(t *testing.T) {
	policy := `{"Version":"2012-10-17","Statement":[{"Sid":"nhi-client-111111111111","Effect":"Allow"}]}`
	if !policyHasStatement(policy, BusStatementID("111111111111")) {
		t.Fatal("existing statement not found")
	}
	if policyHasStatement(policy, BusStatementID("222222222222")) {
		t.Fatal("absent statement reported present")
	}
	if policyHasStatement("not json", "x") {
		t.Fatal("malformed policy reported a statement")
	}
}
