package auth

import (
	"testing"
	"time"
)

func TestLoginLimiterTripsAndResets(t *testing.T) {
	now := time.Now()
	l := newLoginLimiter(10, 5*time.Minute)
	l.now = func() time.Time { return now }

	key := "1.2.3.4\x00alice"
	for i := 0; i < 10; i++ {
		if !l.allowed(key) {
			t.Fatalf("attempt %d denied before the limit", i+1)
		}
		l.recordFailure(key)
	}
	if l.allowed(key) {
		t.Fatal("11th attempt allowed; limiter did not trip")
	}

	// A different username from the same IP is unaffected.
	if !l.allowed("1.2.3.4\x00bob") {
		t.Fatal("unrelated key was blocked")
	}

	// A successful login resets the key.
	l.reset(key)
	if !l.allowed(key) {
		t.Fatal("key still blocked after reset")
	}
}

func TestLoginLimiterWindowExpiry(t *testing.T) {
	now := time.Now()
	l := newLoginLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	key := "ip\x00u"
	for i := 0; i < 3; i++ {
		l.recordFailure(key)
	}
	if l.allowed(key) {
		t.Fatal("limiter did not trip at 3 failures")
	}

	// Advance past the window; old failures age out.
	now = now.Add(61 * time.Second)
	if !l.allowed(key) {
		t.Fatal("failures did not age out of the window")
	}
}
