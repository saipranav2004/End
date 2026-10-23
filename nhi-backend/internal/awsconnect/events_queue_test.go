package awsconnect

import "testing"

func TestBodyAccount(t *testing.T) {
	for body, want := range map[string]string{
		`{"version":"0","account":"123456789012","detail-type":"AWS API Call via CloudTrail"}`: "123456789012",
		`{"Message":"{\"account\":\"210987654321\"}"}`:                                         "210987654321",
		`not json`: "",
		`{}`:       "",
	} {
		if got := bodyAccount(body); got != want {
			t.Errorf("%s: got %q want %q", body, got, want)
		}
	}
}
