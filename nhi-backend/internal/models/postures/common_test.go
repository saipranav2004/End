package postures

import "testing"

func TestParseWindow(t *testing.T) {
	for _, in := range []string{"", "24h", "3d", "7d", "30d", "60d", "90d", " 7D "} {
		if _, ok := ParseWindow(in); !ok {
			t.Errorf("%q should be accepted", in)
		}
	}
	for _, in := range []string{"1d", "14d", "abc", "365d"} {
		if _, ok := ParseWindow(in); ok {
			t.Errorf("%q should be rejected", in)
		}
	}
}
