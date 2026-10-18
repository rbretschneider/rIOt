package commands

import (
	"sync"
	"time"
)

// Report rate limit defaults (SEC C-1). A 50-container bulk update peaks at
// about 3 reports per second for a few seconds, which the burst absorbs;
// sustained legitimate traffic is far below 120 per minute.
const (
	ReportRatePerMinute = 120
	ReportBurst         = 60
	// LimiterIdleEviction is how long a bucket may sit unused before it is
	// dropped; eviction is lazy (every limiterEvictEvery calls).
	LimiterIdleEviction = 10 * time.Minute

	limiterEvictEvery = 256
)

type bucket struct {
	tokens float64
	last   time.Time
}

// DeviceLimiter is a token bucket keyed by the authenticated device ID. HTTP and
// WS reports share one instance per Service. It has an injected clock and no
// background goroutine, so it cannot leak in tests.
type DeviceLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	perSec  float64
	burst   float64
	buckets map[string]*bucket
	calls   int
}

// NewDeviceLimiter returns a limiter with the package defaults.
func NewDeviceLimiter(now func() time.Time) *DeviceLimiter {
	return &DeviceLimiter{
		now:     now,
		perSec:  float64(ReportRatePerMinute) / 60,
		burst:   ReportBurst,
		buckets: make(map[string]*bucket),
	}
}

// Allow takes one token for deviceID and reports whether one was available.
func (l *DeviceLimiter) Allow(deviceID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	l.calls++
	if l.calls%limiterEvictEvery == 0 {
		l.evictIdleLocked(now)
	}

	b, ok := l.buckets[deviceID]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[deviceID] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.perSec
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *DeviceLimiter) evictIdleLocked(now time.Time) {
	for id, b := range l.buckets {
		if now.Sub(b.last) > LimiterIdleEviction {
			delete(l.buckets, id)
		}
	}
}
