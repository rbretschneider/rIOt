package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestRateLimiter(burst int) *RateLimiter {
	// Don't use NewRateLimiter (starts cleanup goroutine), construct directly
	return &RateLimiter{
		rate:     1.0, // 1 token per second
		burst:    burst,
		visitors: make(map[string]*visitor),
	}
}

func TestRateLimiter_BurstAllowed(t *testing.T) {
	rl := newTestRateLimiter(3)

	assert.True(t, rl.allow("1.2.3.4"), "first request should be allowed")
	assert.True(t, rl.allow("1.2.3.4"), "second request within burst")
	assert.True(t, rl.allow("1.2.3.4"), "third request within burst")
}

func TestRateLimiter_ExhaustBurst(t *testing.T) {
	rl := newTestRateLimiter(2)

	assert.True(t, rl.allow("1.2.3.4"))
	assert.True(t, rl.allow("1.2.3.4"))
	assert.False(t, rl.allow("1.2.3.4"), "should be rate limited after burst exhausted")
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	rl := newTestRateLimiter(1)

	assert.True(t, rl.allow("1.1.1.1"))
	assert.False(t, rl.allow("1.1.1.1"))
	// Different IP should have its own bucket
	assert.True(t, rl.allow("2.2.2.2"))
}

func TestRateLimiter_Middleware(t *testing.T) {
	rl := newTestRateLimiter(1)

	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should pass
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Second request should be rate limited
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestRateLimiter_Middleware_NoPort(t *testing.T) {
	rl := newTestRateLimiter(1)

	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4" // no port
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
