package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterBurstAndRefill(t *testing.T) {
	l := New(20, 50)
	now := time.Now()
	for i := 0; i < 50; i++ {
		if ok, _ := l.Allow("t1", now); !ok {
			t.Fatalf("request %d inside the burst was refused", i)
		}
	}
	ok, wait := l.Allow("t1", now)
	if ok || wait <= 0 {
		t.Fatalf("51st request should be refused with a wait, got ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("t2", now); !ok {
		t.Error("another tenant must have its own bucket")
	}
	if ok, _ := l.Allow("t1", now.Add(100*time.Millisecond)); !ok {
		t.Error("tokens should refill over time")
	}
}
