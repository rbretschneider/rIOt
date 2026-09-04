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

// [AC-027] SSO attempts are rate limited by IP: MiddlewareRedirect throttles
// after the burst and answers 302 to the given location, not 429.
func TestAC027_MiddlewareRedirect_ThrottlesToLocation(t *testing.T) {
	rl := newTestRateLimiter(1)
	handler := rl.MiddlewareRedirect("/?sso_error=sso_failed")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "9.9.9.9:1111"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "first request within burst should pass")

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusFound, rec.Code, "throttled request must redirect, not 429")
	assert.Equal(t, "/?sso_error=sso_failed", rec.Header().Get("Location"))
}

// [AC-027] A different client IP is unaffected by another IP's throttling.
func TestAC027_MiddlewareRedirect_DifferentIPUnaffected(t *testing.T) {
	rl := newTestRateLimiter(1)
	handler := rl.MiddlewareRedirect("/?sso_error=sso_failed")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req1.RemoteAddr = "9.9.9.9:1111"
	handler.ServeHTTP(httptest.NewRecorder(), req1)
	handler.ServeHTTP(httptest.NewRecorder(), req1) // exhausts 9.9.9.9's bucket

	req2 := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req2.RemoteAddr = "8.8.8.8:2222"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req2)
	assert.Equal(t, http.StatusOK, rec.Code, "a different client IP must have its own bucket")
}

// [SEC-002] A spoofed X-Forwarded-For from an untrusted peer does not create
// a fresh rate-limit bucket. Chains RealIP (with no trusted proxies
// configured, the production default) in front of Middleware — the same
// arrangement router.go wires — so the assertion covers the real header path,
// not just ClientIP in isolation.
func TestSEC002_Middleware_SpoofedXFF_DoesNotOpenFreshBucket(t *testing.T) {
	rl := newTestRateLimiter(1)
	tp := ParseTrustedProxies("") // trust nobody — production default
	handler := RealIP(tp)(rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/api/v1/auth/login", nil)
	req.RemoteAddr = "9.9.9.9:1111"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Same peer, but spoofing a fresh X-Forwarded-For per request — since
	// there is no configured trusted proxy, this header must be ignored and
	// the bucket must still be keyed on the TCP peer.
	req2 := httptest.NewRequest("GET", "/api/v1/auth/login", nil)
	req2.RemoteAddr = "9.9.9.9:1111"
	req2.Header.Set("X-Forwarded-For", "10.0.0.1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code, "a spoofed X-Forwarded-For must not open a fresh bucket")
}
