package cache

import (
	"sync"
	"time"
)

// State is the circuit breaker state.
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker is a simple closed/open/half-open breaker that protects an
// external dependency (Redis) from being hammered while it is failing.
type CircuitBreaker struct {
	mu        sync.Mutex
	state     State
	failures  int
	threshold int
	cooldown  time.Duration
	lastOpen  time.Time
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 1
	}
	return &CircuitBreaker{state: StateClosed, threshold: threshold, cooldown: cooldown}
}

// Allow reports whether a call may proceed. In half-open it permits exactly one
// probe; further calls are rejected until that probe resolves.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastOpen) >= cb.cooldown {
			cb.state = StateHalfOpen
			return true
		}
		return false
	default: // half-open: a single probe is already in flight
		return false
	}
}

// Success closes the breaker.
func (cb *CircuitBreaker) Success() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.failures = 0
}

// Failure records a failed call and reports whether it just opened the breaker.
func (cb *CircuitBreaker) Failure() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateHalfOpen:
		cb.state = StateOpen
		cb.lastOpen = time.Now()
		return true
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.threshold {
			cb.state = StateOpen
			cb.lastOpen = time.Now()
			return true
		}
	}
	return false
}

// State returns the current state (for observability).
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
