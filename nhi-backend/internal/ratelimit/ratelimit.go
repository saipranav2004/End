// Package ratelimit is an in-process token bucket per key (a tenant, an IP, an
// email). ponytail: per process, so N replicas allow N times the rate; move it
// to a shared store if the API is ever scaled out behind one address.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter refills Rate tokens per second up to Burst, per key.
type Limiter struct {
	mu    sync.Mutex
	rate  float64
	burst float64
	m     map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func New(rate, burst float64) *Limiter {
	return &Limiter{rate: rate, burst: burst, m: map[string]*bucket{}}
}

// Allow takes one token for the key. When none is left it reports how long to
// wait for the next one.
func (l *Limiter) Allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.m[key]
	if !ok {
		if len(l.m) > 5000 { // drop idle keys
			for k, v := range l.m {
				if now.Sub(v.last) > time.Duration(l.burst/l.rate*float64(time.Second))+time.Minute {
					delete(l.m, k)
				}
			}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.m[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}
