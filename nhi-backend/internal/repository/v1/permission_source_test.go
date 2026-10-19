package v1

import "testing"

// Entries use the shape found in the real effective_permissions documents.
func realEntries() []map[string]interface{} {
	e := func(svc, action, effect, source, ptype, res string) map[string]interface{} {
		return map[string]interface{}{"service": svc, "action": action, "effect": effect, "policy_source": source, "policy_type": ptype, "resource_arn": res}
	}
	return []map[string]interface{}{
		e("ec2", "ec2:Describe*", "Allow", "AmazonECSServiceRolePolicy", "managed", "*"),
		e("ec2", "ec2:AttachNetworkInterface", "Allow", "AmazonECSServiceRolePolicy", "managed", "*"),
		e("s3", "s3:GetObject", "Deny", "DenyReads", "inline", "arn:aws:s3:::secret/*"),
		e("s3", "s3:Get*", "Allow", "S3ReadOnly", "managed", "*"),
		e("s3", "s3:GetObject", "Allow", "AppPolicy", "inline", "arn:aws:s3:::app/*"),
	}
}

func TestBestGrantWildcardMatchesRealShape(t *testing.T) {
	g := bestGrant(realEntries(), "ec2", "DescribeInstances")
	if g == nil || g.policySource != "AmazonECSServiceRolePolicy" || g.policyType != "managed" || g.action != "ec2:Describe*" {
		t.Fatalf("got %+v", g)
	}
}

func TestBestGrantExactBeatsWildcardAndAllowBeatsDeny(t *testing.T) {
	// s3:GetObject is granted exactly (Allow, AppPolicy), by wildcard (S3ReadOnly)
	// and denied by DenyReads: the exact Allow explains the call.
	g := bestGrant(realEntries(), "s3", "GetObject")
	if g == nil || g.policySource != "AppPolicy" || g.effect != "Allow" || g.resourceARN != "arn:aws:s3:::app/*" {
		t.Fatalf("got %+v", g)
	}
	// Only the wildcard covers GetBucketAcl.
	if g := bestGrant(realEntries(), "s3", "GetBucketAcl"); g == nil || g.policySource != "S3ReadOnly" {
		t.Fatalf("got %+v", g)
	}
}

func TestBestGrantOnlyDenyIsReportedAsDeny(t *testing.T) {
	only := []map[string]interface{}{realEntries()[2]}
	g := bestGrant(only, "s3", "GetObject")
	if g == nil || g.effect != "Deny" || g.policySource != "DenyReads" {
		t.Fatalf("got %+v", g)
	}
}

func TestBestGrantNoMatchAndEmpty(t *testing.T) {
	if g := bestGrant(realEntries(), "sts", "AssumeRole"); g != nil {
		t.Fatalf("no grant covers sts:AssumeRole: %+v", g)
	}
	if bestGrant(nil, "s3", "GetObject") != nil {
		t.Fatal("no entries -> nil")
	}
	// An entry with no effect is an Allow; capitalised keys are tolerated.
	g := bestGrant([]map[string]interface{}{{"Service": "iam", "Action": "iam:ListRoles", "PolicySource": "Admin"}}, "iam", "ListRoles")
	if g == nil || g.effect != "Allow" || g.policySource != "Admin" {
		t.Fatalf("got %+v", g)
	}
}

func TestGrantResponseShapes(t *testing.T) {
	r := grantResponse(&grant{policySource: "AppPolicy", policyType: "inline", effect: "Allow", resourceARN: "*", action: "s3:GetObject"})
	if !r.Matched || *r.PolicySource != "AppPolicy" || *r.PolicyType != "inline" || *r.MatchedAction != "s3:GetObject" || r.Note != nil {
		t.Fatalf("matched response: %+v", r)
	}
	none := grantResponse(nil)
	if none.Matched || none.PolicySource != nil || none.PolicyType != nil || none.Note == nil || *none.Note != "Policy may have changed since this event" {
		t.Fatalf("unmatched response: %+v", none)
	}
}
