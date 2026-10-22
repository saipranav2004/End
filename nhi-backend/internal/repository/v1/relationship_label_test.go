package v1

import "testing"

func TestRelationshipLabelsAreReadable(t *testing.T) {
	for _, tc := range []struct{ edge, dir, want string }{
		{"bound_to", "outbound", "Uses"},
		{"bound_to", "inbound", "Used by"},
		{"cross_account", "outbound", "Trusts another account"},
		{"pass_role", "outbound", "Can hand a role to a service"},
		{"federated_saml", "inbound", "Signed in through SAML (company single sign-on)"},
		{"service_trust", "inbound", "Trusted service"},
	} {
		if got := relationshipLabel(tc.edge, tc.dir); got != tc.want {
			t.Errorf("relationshipLabel(%s, %s) = %q, want %q", tc.edge, tc.dir, got, tc.want)
		}
	}
}
