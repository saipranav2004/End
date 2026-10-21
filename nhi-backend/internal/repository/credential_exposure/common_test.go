package credential_exposure

import "testing"

func TestScannerTimeAddsTheMissingOffset(t *testing.T) {
	for in, want := range map[string]string{
		"2026-09-18 12:17:39":       "2026-09-18T12:17:39+00:00", // pre-migration rows: UTC, no zone
		"2026-09-23T11:12:25+00:00": "2026-09-23T11:12:25+00:00",
		"2026-10-02T06:51:13Z":      "2026-10-02T06:51:13Z",
		"1790079297 +0530":          "1790079297 +0530", // not ours to reinterpret
	} {
		if got := scannerTime(in); got != want {
			t.Errorf("scannerTime(%q) = %q, want %q", in, got, want)
		}
	}
}
