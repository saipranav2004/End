package v1

import (
	"testing"

	repov1 "nhi-backend-api/internal/repository/v1"
)

// Fixture: a role shared by two EC2 instances, a lone Lambda role, an IAM user
// with an access key, and a terminated instance.
func testLookup() *repov1.IdentityLookup {
	return &repov1.IdentityLookup{
		Identities: []repov1.LookupIdentity{
			{ID: "i-a", ARN: "arn:aws:ec2:ap-south-1:111:instance/i-0aaa", Name: "web-1", PrivateIP: "10.0.0.5"},
			{ID: "i-b", ARN: "arn:aws:ec2:ap-south-1:111:instance/i-0bbb", Name: "web-2", PrivateIP: "10.0.0.6"},
			{ID: "i-dead", ARN: "arn:aws:ec2:ap-south-1:111:instance/i-0ccc", Name: "old", Terminated: true},
			{ID: "fn", ARN: "arn:aws:lambda:ap-south-1:111:function:report", Name: "report"},
			{ID: "usr", ARN: "arn:aws:iam::111:user/ispm-user", Name: "ispm-user"},
			{ID: "dup-1", ARN: "arn:aws:ec2:ap-south-1:111:instance/i-0d1", Name: "dup", PrivateIP: "10.9.9.9"},
			{ID: "dup-2", ARN: "arn:aws:ec2:ap-south-1:111:instance/i-0d2", Name: "dup", PrivateIP: "10.9.9.9"},
		},
		Credentials: []repov1.LookupCredential{
			{ID: "c-app", ARN: "arn:aws:iam::111:role/ec2-app-runtime-role", Name: "ec2-app-runtime-role", Type: "iam_role"},
			{ID: "c-fn", ARN: "arn:aws:iam::111:role/service-role/report-role", Name: "report-role", Type: "iam_role"},
			{ID: "c-key", ARN: "", Name: "AKIAEXAMPLE", Type: "access_key"},
			{ID: "c-old", ARN: "arn:aws:iam::111:role/old-role", Name: "old-role", Type: "iam_role", Terminated: true},
			{ID: "c-unbound", ARN: "arn:aws:iam::111:role/unbound", Name: "unbound", Type: "iam_role"},
			{ID: "c-dup", ARN: "arn:aws:iam::111:role/dup-role", Name: "dup-role", Type: "iam_role"},
		},
		Bindings: []repov1.LookupBinding{
			{IdentityID: "i-a", CredentialID: "c-app"},
			{IdentityID: "i-b", CredentialID: "c-app"},
			{IdentityID: "i-b", CredentialID: "c-app"}, // duplicate row must not double-count
			{IdentityID: "fn", CredentialID: "c-fn"},
			{IdentityID: "usr", CredentialID: "c-key"},
			{IdentityID: "i-dead", CredentialID: "c-old"},
			{IdentityID: "dup-1", CredentialID: "c-dup"},
			{IdentityID: "dup-2", CredentialID: "c-dup"},
		},
	}
}

func TestResolveSingleIdentityBehindIssuer(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{
		CallerARN:        "arn:aws:sts::111:assumed-role/report-role/report",
		SessionIssuerARN: "arn:aws:iam::111:role/service-role/report-role",
	})
	if m.IdentityID != "fn" || m.CredentialID != "c-fn" || m.Reason != "session_issuer" || m.Ambiguous {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveSharedCredentialBySessionNameSuffix(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{
		CallerARN:        "arn:aws:sts::111:assumed-role/ec2-app-runtime-role/i-0bbb",
		SessionIssuerARN: "arn:aws:iam::111:role/ec2-app-runtime-role",
	})
	if m.IdentityID != "i-b" || m.CredentialID != "c-app" || m.Ambiguous {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveSharedCredentialWithoutHintIsAmbiguous(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{SessionIssuerARN: "arn:aws:iam::111:role/ec2-app-runtime-role"})
	if m.IdentityID != "" || m.CredentialID != "c-app" || !m.Ambiguous {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveSharedCredentialByUniquePrivateIP(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{
		SessionIssuerARN: "arn:aws:iam::111:role/ec2-app-runtime-role",
		SourceIP:         "10.0.0.6",
	})
	if m.IdentityID != "i-b" {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveIgnoresPublicIPAndServiceNames(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	for _, ip := range []string{"8.8.8.8", "resource-explorer-2.amazonaws.com", ""} {
		m := r.Resolve(ResolveInput{SessionIssuerARN: "arn:aws:iam::111:role/ec2-app-runtime-role", SourceIP: ip})
		if m.IdentityID != "" || !m.Ambiguous {
			t.Fatalf("ip %q matched: %+v", ip, m)
		}
	}
}

func TestResolveSharedPrivateIPIsNotGuessed(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{SessionIssuerARN: "arn:aws:iam::111:role/dup-role", SourceIP: "10.9.9.9"})
	if m.IdentityID != "" || !m.Ambiguous {
		t.Fatalf("ambiguous IP must not pick an identity: %+v", m)
	}
	// Same for the identity-only fallback.
	m = r.Resolve(ResolveInput{SourceIP: "10.9.9.9"})
	if m.Matched() {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveIAMUserByCallerARN(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{CallerARN: "arn:aws:iam::111:user/ispm-user"})
	if m.IdentityID != "usr" || m.CredentialID != "c-key" || m.Reason != "caller_arn" {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveByAccessKey(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{AccessKeyID: "AKIAEXAMPLE"})
	if m.IdentityID != "usr" || m.CredentialID != "c-key" || m.Reason != "access_key" {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveAssumedRoleWithoutIssuerIgnoresPath(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{CallerARN: "arn:aws:sts::111:assumed-role/report-role/anything"})
	if m.CredentialID != "c-fn" || m.IdentityID != "fn" || m.Reason != "assumed_role" {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveTerminatedRowsStillMatch(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{SessionIssuerARN: "arn:aws:iam::111:role/old-role"})
	if m.IdentityID != "i-dead" || !m.IdentityTerminated || !m.CredentialTerminated {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveUnboundRoleFallsBackToSessionNameThenIP(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	// c-unbound has no binding row, but the session is named after instance i-0aaa.
	m := r.Resolve(ResolveInput{
		CallerARN:        "arn:aws:sts::111:assumed-role/unbound/i-0aaa",
		SessionIssuerARN: "arn:aws:iam::111:role/unbound",
	})
	if m.CredentialID != "c-unbound" || m.IdentityID != "i-a" {
		t.Fatalf("session name fallback: %+v", m)
	}
	m = r.Resolve(ResolveInput{SessionIssuerARN: "arn:aws:iam::111:role/unbound", SourceIP: "10.0.0.5"})
	if m.CredentialID != "c-unbound" || m.IdentityID != "i-a" {
		t.Fatalf("ip fallback: %+v", m)
	}
}

func TestResolveCredentialWithNoBoundIdentity(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{SessionIssuerARN: "arn:aws:iam::111:role/unbound"})
	if m.CredentialID != "c-unbound" || m.IdentityID != "" || m.Ambiguous {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveSessionNameWithoutAnyCredential(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	m := r.Resolve(ResolveInput{CallerARN: "arn:aws:sts::111:assumed-role/unknown-role/i-0aaa"})
	if m.IdentityID != "i-a" || m.Reason != "session_name" || m.CredentialID != "c-app" {
		t.Fatalf("got %+v", m)
	}
}

func TestResolveUnmatchedEventsAreEmptyNotDropped(t *testing.T) {
	r := NewIdentityResolver(testLookup())
	for _, in := range []ResolveInput{
		{CallerARN: "resource-explorer-2.amazonaws.com", SourceIP: "resource-explorer-2.amazonaws.com"},
		{CallerARN: "arn:aws:iam::999:user/stranger"},
		{},
	} {
		if m := r.Resolve(in); m.Matched() {
			t.Fatalf("%+v should not match, got %+v", in, m)
		}
	}
}

func TestResolverWithNilLookup(t *testing.T) {
	if m := NewIdentityResolver(nil).Resolve(ResolveInput{CallerARN: "arn:aws:iam::1:user/x"}); m.Matched() {
		t.Fatalf("got %+v", m)
	}
}

func TestParseAssumedRoleAndRoleKey(t *testing.T) {
	s := parseAssumedRole("arn:aws:sts::123:assumed-role/my-role/i-0a1b2c3d")
	if !s.ok || s.account != "123" || s.role != "my-role" || s.session != "i-0a1b2c3d" {
		t.Fatalf("got %+v", s)
	}
	for _, bad := range []string{"", "arn:aws:iam::123:user/x", "svc.amazonaws.com", "arn:aws:sts::123:assumed-role/only-role"} {
		if parseAssumedRole(bad).ok {
			t.Errorf("%q should not parse", bad)
		}
	}
	if k := roleKeyFromRoleARN("arn:aws:iam::123:role/service-role/my-role"); k != "123:my-role" {
		t.Fatalf("role key = %q", k)
	}
	if roleKeyFromRoleARN("arn:aws:iam::123:user/x") != "" {
		t.Fatal("non-role ARN must have no role key")
	}
}
