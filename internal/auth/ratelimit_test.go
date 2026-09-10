package auth

import (
	"testing"
	"time"
)

func TestRateLimiterTripsAndResets(t *testing.T) {
	now := time.Now()
	l := NewRateLimiter(10, 5*time.Minute)
	l.now = func() time.Time { return now }

	key := "1.2.3.4\x00alice"
	for i := 0; i < 10; i++ {
		if !l.Allowed(key) {
			t.Fatalf("attempt %d denied before the limit", i+1)
		}
		l.RecordFailure(key)
	}
	if l.Allowed(key) {
		t.Fatal("11th attempt allowed; limiter did not trip")
	}

	// A different username from the same IP is unaffected.
	if !l.Allowed("1.2.3.4\x00bob") {
		t.Fatal("unrelated key was blocked")
	}

	// A successful login resets the key.
	l.Reset(key)
	if !l.Allowed(key) {
		t.Fatal("key still blocked after reset")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	now := time.Now()
	l := NewRateLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	key := "ip\x00u"
	for i := 0; i < 3; i++ {
		l.RecordFailure(key)
	}
	if l.Allowed(key) {
		t.Fatal("limiter did not trip at 3 failures")
	}

	// Advance past the window; old failures age out.
	now = now.Add(61 * time.Second)
	if !l.Allowed(key) {
		t.Fatal("failures did not age out of the window")
	}
}
