package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiterAllowsThenBlocks(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	defer rl.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := rl.Middleware(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil)
	req.RemoteAddr = "192.0.2.1:1234"

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
}

func TestRateLimiterIsPerClient(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	defer rl.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := rl.Middleware(next)

	reqA := httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil)
	reqA.RemoteAddr = "192.0.2.1:1234"
	reqB := httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil)
	reqB.RemoteAddr = "192.0.2.2:1234"

	h.ServeHTTP(httptest.NewRecorder(), reqA)
	rrB := httptest.NewRecorder()
	h.ServeHTTP(rrB, reqB)
	if rrB.Code != http.StatusOK {
		t.Fatalf("other client = %d, want 200", rrB.Code)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:9999"
	if got := clientIP(r); got != "10.0.0.5" {
		t.Fatalf("clientIP(RemoteAddr) = %q, want 10.0.0.5", got)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(r2); got != "203.0.113.7" {
		t.Fatalf("clientIP(X-Forwarded-For) = %q, want 203.0.113.7", got)
	}
}
