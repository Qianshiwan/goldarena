// Package ratelimit provides a simple in-memory sliding-window rate limiter.
// It is used to throttle sensitive public endpoints (send-code, register) so
// that bots cannot mass-create accounts. State is not shared across replicas
// and resets on restart — acceptable for the single-process dev gateway.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter counts hits per key over a rolling window.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
	limit   int
	window  time.Duration
}

// New creates a limiter allowing `limit` hits per `window` for each key.
func New(limit int, window time.Duration) *Limiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{
		buckets: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
}

// Allow records a hit for key and returns true if the key is still within its
// quota. Returns false (and does not record an extra hit beyond the window) when
// the quota is already exhausted.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)

	hits := l.buckets[key]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	if len(kept) >= l.limit {
		l.buckets[key] = kept
		return false
	}
	kept = append(kept, now)
	l.buckets[key] = kept
	return true
}

// Remaining returns how many hits are still available in the current window.
func (l *Limiter) Remaining(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)

	n := 0
	for _, t := range l.buckets[key] {
		if t.After(cutoff) {
			n++
		}
	}
	r := l.limit - n
	if r < 0 {
		r = 0
	}
	return r
}
