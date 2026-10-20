package v1

import "testing"

func TestValidateTokens(t *testing.T) {
	if bad, ok := validateTokens([]string{"login_profile", "Prod-EU", "ec2_instance", "a.b:c d"}); !ok {
		t.Fatalf("rejected %q", bad)
	}
	for _, v := range []string{"", "x;drop", "-lead", "a'b", string(make([]byte, 65))} {
		if _, ok := validateTokens([]string{v}); ok {
			t.Errorf("accepted %q", v)
		}
	}
}
