// Package ratelimit provides in-memory token-bucket limiters keyed by an
// arbitrary string (client IP, email). Limits apply per replica: behind N
// replicas the effective allowance is N times the configured rate.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   rate.Limit
	burst   int
	idleTTL time.Duration
	ops     uint64
}

type bucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// New returns a limiter allowing perMinute sustained events per key with the
// given burst size.
func New(perMinute int, burst int) *Limiter {
	if perMinute < 1 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		buckets: make(map[string]*bucket),
		limit:   rate.Limit(float64(perMinute) / 60),
		burst:   burst,
		idleTTL: 10 * time.Minute,
	}
}

// Allow consumes one token for key. It reports whether the request may
// proceed and, when not, how long the caller should wait before retrying.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.ops++
	if l.ops%256 == 0 {
		l.sweep(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	r := b.lim.ReserveN(now, 1)
	if !r.OK() {
		return false, time.Minute
	}
	if delay := r.DelayFrom(now); delay > 0 {
		r.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// sweep drops buckets idle beyond idleTTL so the map cannot grow unbounded.
func (l *Limiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > l.idleTTL {
			delete(l.buckets, key)
		}
	}
}
