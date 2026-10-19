package v1

import "testing"

func TestEffectiveIdleDays(t *testing.T) {
	n := func(v int) *int { return &v }
	cases := []struct {
		name      string
		idle, age *int
		want      *int
	}{
		{"used recently", n(3), n(400), n(3)},
		{"never used, old", nil, n(400), n(400)},
		{"never used, age unknown", nil, nil, nil},
	}
	for _, c := range cases {
		got := effectiveIdleDays(c.idle, c.age)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
