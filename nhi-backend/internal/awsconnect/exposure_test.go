package awsconnect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// fakeExposureAWS answers CodeCommit and Secrets Manager. The region of a
// call is read from its SigV4 credential scope.
type fakeExposureAWS struct {
	mu         sync.Mutex
	repos      map[string][]string // region -> repository names
	denied     map[string]bool     // region -> AccessDenied
	unknown    map[string]bool     // region -> CodeCommit not offered
	denyAction map[string]bool     // "GetBlob" -> AccessDenied
	pages      int                 // ListRepositories calls
	secret     string
	secretCode string // error __type for GetSecretValue
	signedWith []string
}

func regionOf(r *http.Request) (region, key string) {
	auth := r.Header.Get("Authorization")
	i := strings.Index(auth, "Credential=")
	if i < 0 {
		return "", ""
	}
	parts := strings.Split(strings.SplitN(auth[i+len("Credential="):], ",", 2)[0], "/")
	if len(parts) < 3 {
		return "", ""
	}
	return parts[2], parts[0]
}

func writeErr(w http.ResponseWriter, code string) {
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(w, `{"__type":"`+code+`","message":"x"}`)
}

func (f *fakeExposureAWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	var in map[string]any
	_ = json.Unmarshal(body, &in)
	region, key := regionOf(r)
	f.signedWith = append(f.signedWith, key)
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	switch r.Header.Get("X-Amz-Target") {
	case "CodeCommit_20150413.ListRepositories":
		f.pages++
		switch {
		case f.denied[region]:
			writeErr(w, "AccessDeniedException")
		case f.unknown[region]:
			writeErr(w, "UnrecognizedClientException")
		default:
			names := f.repos[region]
			// Page one repository at a time to exercise NextToken.
			start := 0
			if tok, _ := in["nextToken"].(string); tok != "" {
				start = len(tok)
			}
			out := map[string]any{"repositories": []map[string]string{}}
			if start < len(names) {
				out["repositories"] = []map[string]string{{"repositoryName": names[start], "repositoryId": "id"}}
				if start+1 < len(names) {
					out["nextToken"] = strings.Repeat("x", start+1)
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		}
	case "CodeCommit_20150413.GetRepository":
		_, _ = io.WriteString(w, `{"repositoryMetadata":{"repositoryName":"`+in["repositoryName"].(string)+`","defaultBranch":"main"}}`)
	case "CodeCommit_20150413.GetBranch":
		if f.denyAction["GetBranch"] {
			writeErr(w, "AccessDeniedException")
			return
		}
		if in["branchName"] != "main" {
			writeErr(w, "BranchDoesNotExistException")
			return
		}
		_, _ = io.WriteString(w, `{"branch":{"branchName":"main","commitId":"c1"}}`)
	case "CodeCommit_20150413.GetCommit":
		if f.denyAction["GetCommit"] {
			writeErr(w, "AccessDeniedException")
			return
		}
		_, _ = io.WriteString(w, `{"commit":{"commitId":"c1","parents":["c0"]}}`)
	case "CodeCommit_20150413.GetDifferences":
		if f.denyAction["GetDifferences"] {
			writeErr(w, "AccessDeniedException")
			return
		}
		_, _ = io.WriteString(w, `{"differences":[{"afterBlob":{"blobId":"b1","path":"a.env"}}]}`)
	case "CodeCommit_20150413.GetBlob":
		if f.denyAction["GetBlob"] {
			writeErr(w, "AccessDeniedException")
			return
		}
		_, _ = io.WriteString(w, `{"content":"ZGF0YQ=="}`)
	case "secretsmanager.GetSecretValue":
		if f.secretCode != "" {
			writeErr(w, f.secretCode)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ARN": in["SecretId"], "SecretString": f.secret})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newExposureSession(t *testing.T, f *fakeExposureAWS) (*Client, *Session) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg := aws.Config{
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("ASIACLIENT", "s", "t"),
		BaseEndpoint:     aws.String(srv.URL),
		HTTPClient:       srv.Client(),
		RetryMaxAttempts: 1,
	}
	return NewClient(cfg, false, ""), &Session{config: cfg}
}

func TestListCodeCommit(t *testing.T) {
	f := &fakeExposureAWS{
		repos:   map[string][]string{"us-east-1": {"api", "web", "infra"}, "eu-west-1": {}},
		unknown: map[string]bool{"ap-east-1": true},
	}
	c, s := newExposureSession(t, f)
	res, err := c.ListCodeCommit(context.Background(), s, []string{"us-east-1", "eu-west-1", "ap-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Repositories != 3 || len(res.Regions) != 2 || strings.Join(res.Unavailable, ",") != "ap-east-1" {
		t.Fatalf("got %+v", res)
	}
	if res.SampleRepository != "api" || res.SampleRegion != "us-east-1" {
		t.Fatalf("sample %q in %q", res.SampleRepository, res.SampleRegion)
	}
	for _, key := range f.signedWith {
		if key != "ASIACLIENT" {
			t.Fatalf("signed with %q, want the client session", key)
		}
	}

	f.denied = map[string]bool{"eu-west-1": true}
	if _, err := c.ListCodeCommit(context.Background(), s, []string{"us-east-1", "eu-west-1"}); !errors.Is(err, ErrAccessDenied) || !strings.Contains(err.Error(), "eu-west-1") {
		t.Fatalf("denied region: %v", err)
	}
	if _, err := c.ListCodeCommit(context.Background(), s, []string{"ap-east-1"}); err == nil {
		t.Fatal("no reachable region should be an error")
	}
}

func TestProbeCodeCommit(t *testing.T) {
	f := &fakeExposureAWS{}
	c, s := newExposureSession(t, f)
	p := c.ProbeCodeCommit(context.Background(), s, "us-east-1", "api")
	if !p.OK() || p.Files != 1 || p.Branch != "main" {
		t.Fatalf("all four allowed: %+v", p)
	}
	f.denyAction = map[string]bool{"GetBlob": true, "GetDifferences": true}
	p = c.ProbeCodeCommit(context.Background(), s, "us-east-1", "api")
	if p.OK() || strings.Join(p.Denied, ",") != "codecommit:GetDifferences,codecommit:GetBlob" && strings.Join(p.Denied, ",") != "codecommit:GetDifferences" {
		t.Fatalf("denied: %+v", p)
	}
	f.denyAction = map[string]bool{"GetBranch": true}
	if p = c.ProbeCodeCommit(context.Background(), s, "us-east-1", "api"); len(p.Denied) != 1 || p.Denied[0] != "codecommit:GetBranch" {
		t.Fatalf("GetBranch denied: %+v", p)
	}
}

func TestReadSecretToken(t *testing.T) {
	arn, err := ParseSecretARN("arn:aws:secretsmanager:eu-west-1:222222222222:secret:nhi/github-AbCdEf", "222222222222", "nhi/github")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		secret, code, want string
		err                error
	}{
		{secret: "ghp_plain", want: "ghp_plain"},
		{secret: `{"token":"github_pat_x"}`, want: "github_pat_x"},
		{secret: `{"github_token":" ghp_y "}`, want: "ghp_y"},
		{secret: `{"other":"z"}`, err: ErrSecretEmpty},
		{secret: "   ", err: ErrSecretEmpty},
		{code: "ResourceNotFoundException", err: ErrSecretNotFound},
		{code: "DecryptionFailure", err: ErrKMSDenied},
		{code: "AccessDeniedException", err: ErrAccessDenied},
	}
	for _, tc := range cases {
		f := &fakeExposureAWS{secret: tc.secret, secretCode: tc.code}
		c, s := newExposureSession(t, f)
		got, err := c.ReadSecretToken(context.Background(), s, arn)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Fatalf("%q/%q: err %v, want %v", tc.secret, tc.code, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%q: got %q, %v", tc.secret, got, err)
		}
	}
}

func TestParseSecretARN(t *testing.T) {
	good := "arn:aws:secretsmanager:ap-south-1:222222222222:secret:nhi/github/prod-AbCdEf"
	arn, err := ParseSecretARN(" "+good+" ", "222222222222", "nhi/github")
	if err != nil || arn.Region != "ap-south-1" || arn.AccountID != "222222222222" || arn.ARN != good {
		t.Fatalf("got %+v, %v", arn, err)
	}
	var invalid *ErrInvalidSecretARN
	for _, bad := range []string{
		"",
		"nhi/github",
		"arn:aws:secretsmanager:ap-south-1:22222222222:secret:nhi/github-x",
		"arn:aws:secretsmanager:ap-south-1:333333333333:secret:nhi/github-x",
		"arn:aws:secretsmanager:ap-south-1:222222222222:secret:other/github-x",
		"arn:aws:ssm:ap-south-1:222222222222:parameter/nhi/github",
	} {
		if _, err := ParseSecretARN(bad, "222222222222", "nhi/github"); !errors.As(err, &invalid) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}
