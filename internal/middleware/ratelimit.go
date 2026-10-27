package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter is an in-memory per-client (IP) token-bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*entry
	rps      rate.Limit
	burst    int

	stopOnce sync.Once
	stop     chan struct{}
}

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*entry),
		rps:      rate.Limit(rps),
		burst:    burst,
		stop:     make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Stop shuts down the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stop) })
}

// Middleware rejects requests that exceed the per-client limit with 429.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.limiterFor(clientIP(r)).Allow() {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) limiterFor(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e, ok := rl.limiters[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.limiters[key] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

// cleanup periodically drops limiters idle for longer than idleTimeout so the
// map does not grow without bound as clients come and go.
func (rl *RateLimiter) cleanup() {
	const idleTimeout = 5 * time.Minute
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-idleTimeout)
			rl.mu.Lock()
			for key, e := range rl.limiters {
				if e.lastSeen.Before(cutoff) {
					delete(rl.limiters, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}

// clientIP returns the client IP, preferring the first X-Forwarded-For entry
// when the service runs behind a proxy.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
