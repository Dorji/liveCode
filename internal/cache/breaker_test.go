package cache

import (
	"testing"
	"time"
)

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	cb := NewCircuitBreaker(2, 30*time.Millisecond)

	// Closed: allows calls; one failure does not yet open.
	if !cb.Allow() {
		t.Fatal("first Allow should be true")
	}
	cb.Failure()
	if cb.State() != StateClosed {
		t.Fatalf("after 1 failure state = %v, want closed", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("second Allow should be true")
	}
	if !cb.Failure() {
		t.Fatal("second failure should open the breaker")
	}
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}

	// Open: rejects calls without touching the dependency.
	if cb.Allow() {
		t.Fatal("Allow should be false while open")
	}

	// After cooldown: half-open allows exactly one probe.
	time.Sleep(40 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("half-open should allow a probe")
	}
	if cb.Allow() {
		t.Fatal("half-open should allow only one probe")
	}

	// A successful probe closes the breaker.
	cb.Success()
	if cb.State() != StateClosed {
		t.Fatalf("state = %v, want closed after success", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("Allow should be true after closing")
	}
}

func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(1, 20*time.Millisecond)

	// Open immediately (threshold 1).
	cb.Allow()
	if !cb.Failure() {
		t.Fatal("first failure should open the breaker")
	}

	time.Sleep(25 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("half-open should allow a probe")
	}
	// The probe fails -> reopen.
	if !cb.Failure() {
		t.Fatal("half-open failure should reopen the breaker")
	}
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}
	if cb.Allow() {
		t.Fatal("Allow should be false after reopening")
	}
}
