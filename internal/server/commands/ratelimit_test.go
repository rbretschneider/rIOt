package commands

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeClock is a settable clock shared by the limiter and throttle tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestCL_SEC001_DeviceLimiterTokenBucket(t *testing.T) {
	t.Run("burst of 60 is allowed then the 61st is refused", func(t *testing.T) {
		clock := newFakeClock()
		l := NewDeviceLimiter(clock.Now)
		for i := 0; i < ReportBurst; i++ {
			assert.True(t, l.Allow("dev-a"), "[SEC-001] call %d is inside the burst", i+1)
		}
		assert.False(t, l.Allow("dev-a"), "[SEC-001] the 61st immediate call must be refused")
	})

	t.Run("refills at 120 per minute", func(t *testing.T) {
		clock := newFakeClock()
		l := NewDeviceLimiter(clock.Now)
		for i := 0; i < ReportBurst; i++ {
			l.Allow("dev-a")
		}
		clock.Advance(time.Second) // 2 tokens
		assert.True(t, l.Allow("dev-a"))
		assert.True(t, l.Allow("dev-a"))
		assert.False(t, l.Allow("dev-a"), "only 2 tokens accrue per second")
	})

	t.Run("refill is capped at the burst", func(t *testing.T) {
		clock := newFakeClock()
		l := NewDeviceLimiter(clock.Now)
		l.Allow("dev-a")
		clock.Advance(time.Hour)
		allowed := 0
		for i := 0; i < ReportBurst+20; i++ {
			if l.Allow("dev-a") {
				allowed++
			}
		}
		assert.Equal(t, ReportBurst, allowed, "an idle hour must not bank more than the burst")
	})

	t.Run("buckets are keyed by device", func(t *testing.T) {
		clock := newFakeClock()
		l := NewDeviceLimiter(clock.Now)
		for i := 0; i < ReportBurst; i++ {
			l.Allow("dev-a")
		}
		assert.False(t, l.Allow("dev-a"), "device A is limited")
		assert.True(t, l.Allow("dev-b"), "[SEC-001] device B must be unaffected while A is limited")
	})

	t.Run("a clock that moves backwards does not mint tokens", func(t *testing.T) {
		clock := newFakeClock()
		l := NewDeviceLimiter(clock.Now)
		for i := 0; i < ReportBurst; i++ {
			l.Allow("dev-a")
		}
		clock.Advance(-time.Hour)
		assert.False(t, l.Allow("dev-a"))
	})

	t.Run("idle buckets are evicted lazily", func(t *testing.T) {
		clock := newFakeClock()
		l := NewDeviceLimiter(clock.Now)
		l.Allow("idle-device")
		clock.Advance(LimiterIdleEviction + time.Minute)
		for i := 0; i < limiterEvictEvery; i++ {
			l.Allow("busy-device")
		}
		l.mu.Lock()
		_, idleKept := l.buckets["idle-device"]
		_, busyKept := l.buckets["busy-device"]
		l.mu.Unlock()
		assert.False(t, idleKept, "a bucket idle past 10m must be evicted")
		assert.True(t, busyKept)
	})
}
