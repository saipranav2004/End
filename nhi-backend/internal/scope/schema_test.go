package scope

import "testing"

func TestDataScopeQualifiesSchema(t *testing.T) {
	s := &DataScope{Schema: "tenant_a"}
	if got, want := s.QSchema(), `"tenant_a"`; got != want {
		t.Errorf("QSchema() = %s, want %s", got, want)
	}
	if got, want := s.Table("identities"), `"tenant_a"."identities"`; got != want {
		t.Errorf("Table() = %s, want %s", got, want)
	}
	// Defensive: embedded quotes are escaped rather than breaking out.
	evil := &DataScope{Schema: `a"b`}
	if got, want := evil.QSchema(), `"a""b"`; got != want {
		t.Errorf("QSchema() = %s, want %s", got, want)
	}
}

func TestQueryArgsAndPredicatesHaveNoScan(t *testing.T) {
	s := &DataScope{AccountIDs: []string{"111111111111"}, Regions: []string{"us-east-1"}}
	if got := len(s.QueryArgs()); got != 2 {
		t.Fatalf("QueryArgs() has %d args, want 2 (accounts, regions)", got)
	}
	if got := len((*DataScope)(nil).QueryArgs()); got != 2 {
		t.Fatalf("nil QueryArgs() has %d args, want 2", got)
	}
	p := s.Predicates("i")
	want := []string{"i.account_id = ANY($1::text[])", "i.region = ANY($2::text[])"}
	if len(p) != len(want) || p[0] != want[0] || p[1] != want[1] {
		t.Fatalf("Predicates(i) = %v, want %v", p, want)
	}
}
