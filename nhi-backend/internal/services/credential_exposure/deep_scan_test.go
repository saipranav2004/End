package credential_exposure

import (
	"errors"
	"testing"
)

func TestPickPlatform(t *testing.T) {
	both := []string{"codecommit", "github"}
	cases := []struct {
		asked, repo string
		known       []string
		want        string
		err         error
	}{
		{"github", "owner/repo", both, "github", nil},
		{"github", "owner/repo", []string{"codecommit"}, "", ErrDeepScanClientNotFound},
		{"", "anything", []string{"codecommit"}, "codecommit", nil},
		{"", "owner/repo", nil, "", ErrDeepScanClientNotFound},
		{"", "owner/repo", both, "github", nil},
		{"", "plain-name", both, "codecommit", nil},
	}
	for _, tc := range cases {
		got, err := pickPlatform(tc.asked, tc.repo, tc.known)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("pickPlatform(%q, %q, %v) = %q, %v; want %q, %v", tc.asked, tc.repo, tc.known, got, err, tc.want, tc.err)
		}
	}
}
