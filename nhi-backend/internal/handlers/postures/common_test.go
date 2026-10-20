package postures

import (
	"testing"
)

func TestSafeCell(t *testing.T) {
	for in, want := range map[string]string{"=1+1": "'=1+1", "+x": "'+x", "-x": "'-x", "@a": "'@a", "plain": "plain", "": ""} {
		if got := safeCell(in); got != want {
			t.Errorf("safeCell(%q)=%q want %q", in, got, want)
		}
	}
}
