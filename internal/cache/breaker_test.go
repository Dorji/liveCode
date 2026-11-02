package cache

import (
	"testing"
	"time"
)

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	cb := NewCircuitBreaker(2, 30*time.Millisecond)

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

	if cb.Allow() {
		t.Fatal("Allow should be false while open")
	}

	time.Sleep(40 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("half-open should allow a probe")
	}
	if cb.Allow() {
		t.Fatal("half-open should allow only one probe")
	}

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

	cb.Allow()
	if !cb.Failure() {
		t.Fatal("first failure should open the breaker")
	}

	time.Sleep(25 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("half-open should allow a probe")
	}
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
