package middleware

import (
	"net/http"
	"sync"
	"time"
)

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter implements an in-memory per-IP token bucket rate limiter.
type RateLimiter struct {
	rate     float64 // tokens per second
	burst    int
	visitors map[string]*visitor
	mu       sync.Mutex
}

// NewRateLimiter creates a rate limiter. rate is requests per minute, burst is max burst size.
func NewRateLimiter(ratePerMin float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		rate:     ratePerMin / 60.0, // convert to per-second
		burst:    burst,
		visitors: make(map[string]*visitor),
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	now := time.Now()
	if !exists {
		rl.visitors[ip] = &visitor{tokens: float64(rl.burst) - 1, lastSeen: now}
		return true
	}

	elapsed := now.Sub(v.lastSeen).Seconds()
	v.lastSeen = now
	v.tokens += elapsed * rl.rate
	if v.tokens > float64(rl.burst) {
		v.tokens = float64(rl.burst)
	}

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for ip, v := range rl.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns an HTTP middleware that rate limits by client IP.
// Keyed on middleware.ClientIP(r) (AD-020) — the resolved TCP peer address
// unless rewritten by a configured trusted proxy — so the key cannot be
// forged by an arbitrary request header (SEC-002).
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)
			if !rl.allow(ip) {
				http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MiddlewareRedirect returns an HTTP middleware that rate limits by client IP
// identically to Middleware, but on rejection responds with a 302 redirect
// to the given location instead of a 429 JSON body. Used by the OIDC-001
// browser-facing endpoints (AD-013) so a throttled navigation never dead-ends
// on a raw JSON response (AD-010).
func (rl *RateLimiter) MiddlewareRedirect(location string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)
			if !rl.allow(ip) {
				http.Redirect(w, r, location, http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
