package genome

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
)

func TestAnomalyCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 15, 22, 24, 40, 123456000, time.UTC)
	s := encodeAnomalyCursor(repo.AnomalyCursor{CreatedAt: at, ID: 36})
	got, err := decodeAnomalyCursor(*s)
	if err != nil || got == nil || !got.CreatedAt.Equal(at) || got.ID != 36 {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	for _, bad := range []string{"zzz", "a.", "a.!!", "a.e30", strings.Repeat("x", 10)} {
		if _, err := decodeAnomalyCursor(bad); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("decode(%q) err = %v, want ErrInvalidCursor", bad, err)
		}
	}
	if c, err := decodeAnomalyCursor(""); c != nil || err != nil {
		t.Errorf("empty cursor = %v, %v", c, err)
	}
}

// Validation runs before any store is touched, so a service without stores
// is enough to test it.
func TestSaveFeedbackValidation(t *testing.T) {
	s := &AnomaliesService{}
	yes := true
	long := strings.Repeat("x", maxFeedbackReason+1)
	cases := []struct {
		name string
		role string
		req  models.AnomalyFeedbackRequest
		want any // error sentinel, or *FeedbackValidationError
	}{
		{"viewer", "user", models.AnomalyFeedbackRequest{Decision: "malicious"}, ErrFeedbackForbidden},
		{"empty role", "", models.AnomalyFeedbackRequest{Decision: "malicious"}, ErrFeedbackForbidden},
		{"unknown decision", "analyst", models.AnomalyFeedbackRequest{Decision: "real"}, &FeedbackValidationError{}},
		{"approve without legitimate", "admin", models.AnomalyFeedbackRequest{Decision: "malicious", ApprovedForBaseline: &yes}, &FeedbackValidationError{}},
		{"reason too long", "Analyst", models.AnomalyFeedbackRequest{Decision: "unknown", Reason: &long}, &FeedbackValidationError{}},
	}
	for _, c := range cases {
		_, err := s.SaveFeedback(context.Background(), FeedbackCaller{Role: c.role}, 1, c.req)
		switch want := c.want.(type) {
		case *FeedbackValidationError:
			var got *FeedbackValidationError
			if !errors.As(err, &got) {
				t.Errorf("%s: err = %v, want a validation error", c.name, err)
			}
		case error:
			if !errors.Is(err, want) {
				t.Errorf("%s: err = %v, want %v", c.name, err, want)
			}
		}
	}
}

func TestAnomalyKeysScopeAndSharedWorkload(t *testing.T) {
	a := monitoredIdentity{Key: repo.MonitoringKey{SourceType: "ECS", IdentityName: "task-a"}, DisplayName: "orders", WorkloadKey: "ecs:orders"}
	b := monitoredIdentity{Key: repo.MonitoringKey{SourceType: "ECS", IdentityName: "task-b"}, DisplayName: "orders", WorkloadKey: "ecs:orders"}
	c := monitoredIdentity{Key: repo.MonitoringKey{SourceType: "LAMBDA", IdentityName: "fn"}, DisplayName: "billing"}
	fleet := []monitoredIdentity{a, b, c}

	keys, owners := anomalyKeys(fleet, AnomalyListParams{})
	if len(keys) != 4 { // task-a, ecs:orders, task-b, fn
		t.Fatalf("keys = %v", keys)
	}
	if owners[repo.MonitoringKey{SourceType: "ECS", IdentityName: "ecs:orders"}].Key != a.Key {
		t.Errorf("shared workload key should belong to the first identity")
	}
	keys, _ = anomalyKeys(fleet, AnomalyListParams{Search: "BILL"})
	if len(keys) != 1 || keys[0] != c.Key {
		t.Errorf("search keys = %v", keys)
	}
	keys, _ = anomalyKeys(fleet, AnomalyListParams{SourceType: "ECS", IdentityName: "task-b"})
	if len(keys) != 2 || keys[0] != b.Key {
		t.Errorf("identity keys = %v", keys)
	}
}
