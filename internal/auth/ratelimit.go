package auth

import (
	"sync"
	"time"
)

// RateLimiter is a small in-memory limiter keyed by an arbitrary string (an
// "ip\x00username" pair for the web login, a chat id for the bot). It allows
// up to maxFailures failed attempts within window; the next attempt is refused
// until enough time passes for older failures to age out. A success clears the
// key. Used for both the web login and the bot's /login and /admin rate
// limiting.
type RateLimiter struct {
	mu          sync.Mutex
	failures    map[string][]time.Time
	maxFailures int
	window      time.Duration
	now         func() time.Time // overridable in tests
}

// NewRateLimiter returns a RateLimiter allowing maxFailures failed attempts
// per window, per key.
func NewRateLimiter(maxFailures int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		failures:    make(map[string][]time.Time),
		maxFailures: maxFailures,
		window:      window,
		now:         time.Now,
	}
}

func (l *RateLimiter) prune(key string, cutoff time.Time) {
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
	} else {
		l.failures[key] = kept
	}
}

// Allowed reports whether another attempt for key may proceed.
func (l *RateLimiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key, l.now().Add(-l.window))
	return len(l.failures[key]) < l.maxFailures
}

// RecordFailure notes a failed attempt for key.
func (l *RateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.prune(key, now.Add(-l.window))
	l.failures[key] = append(l.failures[key], now)
}

// Reset clears the failure history for key (called after a success).
func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
