package awsconnect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// fakeAWS answers the five calls the client makes, in each service's own wire
// protocol, and records what it was asked.
type fakeAWS struct {
	mu sync.Mutex

	assumeDenied bool
	rolesXML     string
	readDenied   bool
	busPolicy    string
	putErrCode   string
	events       bool

	assumeForm    url.Values
	listRolesKey  string // access key that signed ListRoles
	putBody       map[string]any
	putCalls      int
	removeCalls   int
	removeBody    map[string]any
	removeErrCode string
	filterBody    map[string]any
}

const oneRole = `<member><Path>/</Path><RoleName>r</RoleName><RoleId>AROAEXAMPLE</RoleId><Arn>arn:aws:iam::222222222222:role/r</Arn><CreateDate>2020-01-01T00:00:00Z</CreateDate></member>`

func (f *fakeAWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	target := r.Header.Get("X-Amz-Target")
	switch {
	case strings.HasPrefix(target, "AWSEvents."):
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		switch strings.TrimPrefix(target, "AWSEvents.") {
		case "DescribeEventBus":
			out := map[string]any{"Name": in["Name"], "Arn": "arn:aws:events:ap-south-1:999999999999:event-bus/nhi-central-bus"}
			if f.busPolicy != "" {
				out["Policy"] = f.busPolicy
			}
			_ = json.NewEncoder(w).Encode(out)
		case "RemovePermission":
			f.removeCalls++
			f.removeBody = in
			if f.removeErrCode != "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"`+f.removeErrCode+`","message":"x"}`)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		case "PutPermission":
			f.putCalls++
			f.putBody = in
			if f.putErrCode != "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"`+f.putErrCode+`","message":"x"}`)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		}
	case target == "Logs_20140328.FilterLogEvents":
		_ = json.Unmarshal(body, &f.filterBody)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if f.events {
			_, _ = io.WriteString(w, `{"events":[{"message":"{}","timestamp":1}]}`)
		} else {
			_, _ = io.WriteString(w, `{"events":[]}`)
		}
	default:
		form, _ := url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "text/xml")
		switch form.Get("Action") {
		case "AssumeRole":
			f.assumeForm = form
			if f.assumeDenied {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `<ErrorResponse><Error><Type>Sender</Type><Code>AccessDenied</Code><Message>not authorized</Message></Error><RequestId>1</RequestId></ErrorResponse>`)
				return
			}
			_, _ = io.WriteString(w, `<AssumeRoleResponse><AssumeRoleResult><Credentials><AccessKeyId>ASIACLIENT</AccessKeyId><SecretAccessKey>secret</SecretAccessKey><SessionToken>token</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration></Credentials><AssumedRoleUser><Arn>arn:aws:sts::222222222222:assumed-role/r/s</Arn><AssumedRoleId>AROA:s</AssumedRoleId></AssumedRoleUser></AssumeRoleResult></AssumeRoleResponse>`)
		case "ListRoles":
			auth := r.Header.Get("Authorization")
			if i := strings.Index(auth, "Credential="); i >= 0 {
				f.listRolesKey = strings.SplitN(auth[i+len("Credential="):], "/", 2)[0]
			}
			if f.readDenied {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `<ErrorResponse><Error><Type>Sender</Type><Code>AccessDenied</Code><Message>no</Message></Error><RequestId>1</RequestId></ErrorResponse>`)
				return
			}
			_, _ = io.WriteString(w, `<ListRolesResponse><ListRolesResult><IsTruncated>false</IsTruncated><Roles>`+f.rolesXML+`</Roles></ListRolesResult></ListRolesResponse>`)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func newTestClient(t *testing.T, f *fakeAWS, logGroup string) (*Client, Platform) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	base := aws.Config{
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("AKIAPLATFORM", "s", ""),
		BaseEndpoint:     aws.String(srv.URL),
		HTTPClient:       srv.Client(),
		RetryMaxAttempts: 1,
	}
	p, err := NewPlatform("999999999999", "ap-south-1",
		"arn:aws:events:ap-south-1:999999999999:event-bus/nhi-central-bus",
		"arn:aws:iam::999999999999:role/NHIPlatformAssumer")
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(base, false, logGroup), p
}

func TestClientAssumeRoleAndRead(t *testing.T) {
	f := &fakeAWS{rolesXML: oneRole}
	c, p := newTestClient(t, f, "")
	role, _ := ParseRoleARN("arn:aws:iam::222222222222:role/NHIPlatformCollector")
	ctx := context.Background()

	session, err := c.AssumeRole(ctx, p, role, "ext-abc")
	if err != nil {
		t.Fatalf("AssumeRole: %v", err)
	}
	if f.assumeForm.Get("RoleArn") != role.ARN || f.assumeForm.Get("ExternalId") != "ext-abc" || f.assumeForm.Get("DurationSeconds") != "900" {
		t.Fatalf("AssumeRole sent %v", f.assumeForm)
	}
	if err := c.TestRead(ctx, session); err != nil {
		t.Fatalf("TestRead: %v", err)
	}
	if f.listRolesKey != "ASIACLIENT" {
		t.Fatalf("ListRoles was signed with %q, want the assumed role's key", f.listRolesKey)
	}

	f.rolesXML = ""
	if err := c.TestRead(ctx, session); !errors.Is(err, ErrNothingRead) {
		t.Fatalf("empty ListRoles = %v, want ErrNothingRead", err)
	}
	f.readDenied = true
	if err := c.TestRead(ctx, session); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("denied ListRoles = %v, want ErrAccessDenied", err)
	}

	f.assumeDenied = true
	if _, err := c.AssumeRole(ctx, p, role, "wrong"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("denied AssumeRole = %v, want ErrAccessDenied", err)
	}
}

func TestClientAllowAccountOnBus(t *testing.T) {
	f := &fakeAWS{}
	c, p := newTestClient(t, f, "")
	ctx := context.Background()

	if err := c.AllowAccountOnBus(ctx, p, "222222222222"); err != nil {
		t.Fatalf("AllowAccountOnBus: %v", err)
	}
	if f.putCalls != 1 || f.putBody["Principal"] != "222222222222" || f.putBody["Action"] != "events:PutEvents" ||
		f.putBody["StatementId"] != "nhi-client-222222222222" || f.putBody["EventBusName"] != "nhi-central-bus" {
		t.Fatalf("PutPermission sent %v", f.putBody)
	}

	// Already allowed: no second statement.
	f.busPolicy = `{"Version":"2012-10-17","Statement":[{"Sid":"nhi-client-222222222222","Effect":"Allow"}]}`
	if err := c.AllowAccountOnBus(ctx, p, "222222222222"); err != nil || f.putCalls != 1 {
		t.Fatalf("existing statement: err %v, PutPermission calls %d", err, f.putCalls)
	}

	// Lost a race to a concurrent check: still fine.
	f.busPolicy = ""
	f.putErrCode = "ResourceAlreadyExistsException"
	if err := c.AllowAccountOnBus(ctx, p, "333333333333"); err != nil {
		t.Fatalf("ResourceAlreadyExists should be success, got %v", err)
	}
	f.putErrCode = "PolicyLengthExceededException"
	if err := c.AllowAccountOnBus(ctx, p, "444444444444"); err == nil {
		t.Fatal("policy too large should be an error")
	}
	if err := c.AllowAccountOnBus(ctx, p, "12"); err == nil {
		t.Fatal("malformed account id accepted")
	}
}

func TestClientHasEventsFrom(t *testing.T) {
	ctx := context.Background()
	since := time.Unix(1_700_000_000, 0)

	c, p := newTestClient(t, &fakeAWS{}, "")
	if _, err := c.HasEventsFrom(ctx, p, "222222222222", since); !errors.Is(err, ErrEventSourceNotConfigured) {
		t.Fatalf("no log group = %v, want ErrEventSourceNotConfigured", err)
	}

	f := &fakeAWS{events: true}
	c, p = newTestClient(t, f, "/aws/events/nhi-central-bus")
	found, err := c.HasEventsFrom(ctx, p, "222222222222", since)
	if err != nil || !found {
		t.Fatalf("HasEventsFrom = %v, %v", found, err)
	}
	if f.filterBody["logGroupName"] != "/aws/events/nhi-central-bus" ||
		f.filterBody["filterPattern"] != `{ $.account = "222222222222" }` ||
		f.filterBody["startTime"] != float64(since.UnixMilli()) {
		t.Fatalf("FilterLogEvents sent %v", f.filterBody)
	}

	f.events = false
	if found, err := c.HasEventsFrom(ctx, p, "222222222222", since); err != nil || found {
		t.Fatalf("no events = %v, %v", found, err)
	}
}

func TestClientRevokeAccountOnBus(t *testing.T) {
	f := &fakeAWS{}
	c, p := newTestClient(t, f, "")
	ctx := context.Background()
	if err := c.RevokeAccountOnBus(ctx, p, "222222222222"); err != nil {
		t.Fatalf("RevokeAccountOnBus: %v", err)
	}
	if f.removeCalls != 1 || f.removeBody["StatementId"] != "nhi-client-222222222222" || f.removeBody["EventBusName"] != "nhi-central-bus" {
		t.Fatalf("RemovePermission sent %v", f.removeBody)
	}
	f.removeErrCode = "ResourceNotFoundException"
	if err := c.RevokeAccountOnBus(ctx, p, "222222222222"); err != nil {
		t.Fatalf("already removed should be success, got %v", err)
	}
	f.removeErrCode = "AccessDeniedException"
	if err := c.RevokeAccountOnBus(ctx, p, "222222222222"); err == nil {
		t.Fatal("access denied should be an error")
	}
	if err := c.RevokeAccountOnBus(ctx, p, "12"); err == nil {
		t.Fatal("malformed account id accepted")
	}
}
