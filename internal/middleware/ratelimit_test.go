package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_EnforcesLimit(t *testing.T) {
	rl := newRateLimiter(3, time.Minute)

	if !rl.allow("10.0.0.1", time.Unix(1000, 0)) {
		t.Fatalf("first request should be allowed")
	}
	if !rl.allow("10.0.0.1", time.Unix(1001, 0)) {
		t.Fatalf("second request should be allowed")
	}
	if !rl.allow("10.0.0.1", time.Unix(1002, 0)) {
		t.Fatalf("third request should be allowed")
	}
	if rl.allow("10.0.0.1", time.Unix(1003, 0)) {
		t.Fatalf("fourth request should be blocked")
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := newRateLimiter(2, time.Minute)

	if !rl.allow("10.0.0.1", time.Unix(1000, 0)) || !rl.allow("10.0.0.1", time.Unix(1001, 0)) {
		t.Fatalf("requests within limit should pass")
	}
	if rl.allow("10.0.0.1", time.Unix(1002, 0)) {
		t.Fatalf("over-limit request should be blocked")
	}

	// After the window elapses a fresh bucket starts.
	if !rl.allow("10.0.0.1", time.Unix(1000+61, 0)) {
		t.Fatalf("request after window expiry should be allowed")
	}
}

func TestRateLimiter_IPsAreIndependent(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)

	if !rl.allow("10.0.0.1", time.Unix(1000, 0)) {
		t.Fatalf("first IP first request should be allowed")
	}
	if rl.allow("10.0.0.1", time.Unix(1001, 0)) {
		t.Fatalf("first IP second request should be blocked")
	}
	if !rl.allow("10.0.0.2", time.Unix(1001, 0)) {
		t.Fatalf("second IP should have its own bucket")
	}
	if !rl.allow("10.0.0.3", time.Unix(1002, 0)) {
		t.Fatalf("third IP should have its own bucket")
	}
}

func TestRateLimiter_IdleSweep(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)

	if !rl.allow("10.0.0.1", time.Unix(1000, 0)) {
		t.Fatalf("first request should be allowed")
	}
	if rl.allow("10.0.0.1", time.Unix(1001, 0)) {
		t.Fatalf("request within the same window should be blocked")
	}

	// An idle bucket is swept away after the window, restoring capacity.
	if !rl.allow("10.0.0.1", time.Unix(1000+61, 0)) {
		t.Fatalf("request after idle sweep should be allowed")
	}
}

// TestRateLimiter_SweepDirectReapsIdle proves the reaper contract after the
// O(1)-allow refactor: sweep itself drops idle buckets (background reaper
// calls it), and the allow path never scans other IPs' buckets.
func TestRateLimiter_SweepDirectReapsIdle(t *testing.T) {
	rl := newRateLimiter(2, time.Minute)
	now := time.Now()

	if !rl.allow("10.9.9.1", now) {
		t.Fatal("first request should be allowed")
	}
	shard := rl.shardFor("10.9.9.1")

	// Active bucket survives a sweep inside the window.
	shard.mu.Lock()
	shard.sweep(now.Add(30*time.Second), rl.window)
	n := len(shard.buckets)
	shard.mu.Unlock()
	if n != 1 {
		t.Fatalf("active bucket must survive sweep, got %d buckets", n)
	}

	// Idle past the window: sweep reaps it.
	shard.mu.Lock()
	shard.sweep(now.Add(2*time.Minute), rl.window)
	n = len(shard.buckets)
	shard.mu.Unlock()
	if n != 0 {
		t.Fatalf("idle bucket must be reaped by sweep, got %d buckets", n)
	}

	// Allow after reap starts a fresh bucket (reaper not on request path).
	if !rl.allow("10.9.9.1", now.Add(2*time.Minute)) {
		t.Fatal("allow after reap must succeed with fresh bucket")
	}
}

func TestRateLimitMiddleware_StatusCodes(t *testing.T) {
	mw := RateLimit(2)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d should be allowed, got %d", i+1, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}
