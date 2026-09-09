package auth

import (
	"sync"
	"time"
)

// loginLimiter is a small in-memory limiter keyed by "ip\x00username". It allows
// up to maxFailures failed login attempts within window; the next attempt is
// refused until enough time passes for older failures to age out. A successful
// login clears the key.
type loginLimiter struct {
	mu          sync.Mutex
	failures    map[string][]time.Time
	maxFailures int
	window      time.Duration
	now         func() time.Time // overridable in tests
}

func newLoginLimiter(maxFailures int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		failures:    make(map[string][]time.Time),
		maxFailures: maxFailures,
		window:      window,
		now:         time.Now,
	}
}

func (l *loginLimiter) prune(key string, cutoff time.Time) {
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

// allowed reports whether another login attempt for key may proceed.
func (l *loginLimiter) allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key, l.now().Add(-l.window))
	return len(l.failures[key]) < l.maxFailures
}

// recordFailure notes a failed attempt for key.
func (l *loginLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.prune(key, now.Add(-l.window))
	l.failures[key] = append(l.failures[key], now)
}

// reset clears the failure history for key (called after a successful login).
func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
