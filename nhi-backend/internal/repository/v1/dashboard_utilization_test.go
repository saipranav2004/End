package v1

import (
	"testing"
	"time"
)

func TestUtilizationBucket(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	at := func(days int) *time.Time { v := now.AddDate(0, 0, -days); return &v }
	for _, tc := range []struct {
		last *time.Time
		want string
	}{
		{nil, "never_used"}, {at(1), "used_7d"}, {at(8), "used_30d"}, {at(45), "used_90d"}, {at(91), "idle_90d"},
	} {
		if got := utilizationBucket(tc.last, now); got != tc.want {
			t.Errorf("utilizationBucket(%v) = %s, want %s", tc.last, got, tc.want)
		}
	}
}
