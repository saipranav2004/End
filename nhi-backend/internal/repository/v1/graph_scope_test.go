package v1

import (
	"testing"

	"nhi-backend-api/internal/scope"
)

func TestScopeAllowsARNRegionRules(t *testing.T) {
	s := &scope.DataScope{AccountIDs: []string{"111111111111"}, Regions: []string{"us-east-1", "global"}}
	cases := map[string]bool{
		"arn:aws:iam::111111111111:role/app":                   true,  // IAM: global, selected
		"arn:aws:lambda:us-east-1:111111111111:function:f":     true,  // in region
		"arn:aws:lambda:eu-west-1:111111111111:function:f":     false, // other region
		"arn:aws:iam::222222222222:role/app":                   false, // other account
		"arn:aws:s3:::bucket":                                  false, // no account: not traversable
		"lambda.amazonaws.com":                                 false,
		"arn:aws:dynamodb:us-east-1:111111111111:table/orders": true,
		"arn:aws:sts::111111111111:assumed-role/app/session":   true,
	}
	for arn, want := range cases {
		if got := scopeAllowsARN(s, arn); got != want {
			t.Errorf("scopeAllowsARN(%q) = %v, want %v", arn, got, want)
		}
	}

	// With "global" deselected, IAM ARNs drop out just as IAM rows drop out of the lists.
	noGlobal := &scope.DataScope{AccountIDs: []string{"111111111111"}, Regions: []string{"us-east-1"}}
	if scopeAllowsARN(noGlobal, "arn:aws:iam::111111111111:role/app") {
		t.Error("IAM ARN allowed although the global region is not selected")
	}
	if !scopeAllowsARN(noGlobal, "arn:aws:lambda:us-east-1:111111111111:function:f") {
		t.Error("regional ARN in a selected region was refused")
	}
}

func TestScopedEdgesKeepsServicePrincipalsAsLeaves(t *testing.T) {
	s := &scope.DataScope{AccountIDs: []string{"111111111111"}, Regions: []string{"global"}}
	role := "arn:aws:iam::111111111111:role/payments"
	outside := "arn:aws:iam::999999999999:role/other"
	edges := []graphEdgeRow{
		{Source: "lambda.amazonaws.com", Target: role, EdgeType: "service_trust"},
		{Source: "lambda.amazonaws.com", Target: outside, EdgeType: "service_trust"},
		{Source: "not a principal", Target: role, EdgeType: "trusts"},
	}
	visited := map[string]bool{role: true}
	kept, frontier, leaves := scopedEdges(s, edges, visited)
	if len(kept) != 1 || kept[0].Target != role {
		t.Fatalf("kept = %+v, want only the in-scope service trust", kept)
	}
	if len(frontier) != 0 {
		t.Errorf("frontier = %v: a service principal must never be walked through", frontier)
	}
	if len(leaves) != 1 || leaves[0] != "lambda.amazonaws.com" {
		t.Errorf("leaves = %v, want the service principal", leaves)
	}
}

func TestAllowsAccessTarget(t *testing.T) {
	s := &scope.DataScope{AccountIDs: []string{"111111111111"}, Regions: []string{"us-east-1", "global"}}
	root := "arn:aws:iam::111111111111:role/app"
	cases := map[string]bool{
		"arn:aws:s3:::acme-data-lake":                          true, // account-less bucket, root in scope
		"arn:aws:s3:::acme-data-lake/raw/2026/file.csv":        true,
		"arn:aws:dynamodb:us-east-1:111111111111:table/orders": true,
		"arn:aws:dynamodb:us-east-1:999999999999:table/orders": false, // another account's resource
		"lambda.amazonaws.com":                                 false,
		"not-an-arn":                                           false,
	}
	for target, want := range cases {
		if got := allowsAccessTarget(s, root, target); got != want {
			t.Errorf("allowsAccessTarget(%q) = %v, want %v", target, got, want)
		}
	}
	if allowsAccessTarget(s, "arn:aws:iam::999999999999:role/x", "arn:aws:s3:::bucket") {
		t.Error("an out-of-scope root must not get edges")
	}
}

func TestEscapeLikePattern(t *testing.T) {
	for in, want := range map[string]string{"_": `\_`, "50%": `50\%`, `a\b`: `a\\b`, "deploy-role": "deploy-role"} {
		if got := escapeLikePattern(in); got != want {
			t.Errorf("escapeLikePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseEdgeTypesIsCaseInsensitive(t *testing.T) {
	got := parseEdgeTypes(" ASSUME_ROLE, accesses ,,")
	if len(got) != 2 || got[0] != "assume_role" || got[1] != "accesses" {
		t.Errorf("parseEdgeTypes = %v", got)
	}
}
