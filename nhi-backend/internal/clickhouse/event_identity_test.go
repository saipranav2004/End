package clickhouse

import (
	"strings"
	"testing"
)

func TestIdentityEquals(t *testing.T) {
	if got := IdentityEquals("LAMBDA"); got != `identity_name = {identity_name:String}` {
		t.Fatalf("non-ECS must compare the plain column, got %q", got)
	}
	for _, st := range []string{"ECS", "ecs", " ECS "} {
		got := IdentityEquals(st)
		if !strings.Contains(got, "roleArn") || !strings.Contains(got, "OR identity_name = {identity_name:String}") {
			t.Fatalf("ECS must read roleArn, got %q", got)
		}
	}
	if !strings.Contains(EventIdentity, "source_type = 'ECS'") {
		t.Fatal("EventIdentity must branch on ECS")
	}
}
