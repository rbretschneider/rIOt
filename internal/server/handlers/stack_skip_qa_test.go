package handlers

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests added by QA Engineer (AUTOUPD-HARDEN-001), covering the standing
// requirement that no in-memory map grows without bound.

// Covers the standing bounds requirement. The skip limiter drops entries older
// than the window on every call, so a burst of distinct keys does not stay in
// memory once its window has passed. Added by QA Engineer.
func TestQA_SkipLimiterEvictsExpiredKeys(t *testing.T) {
	l := newSkipLimiter()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	window := 6 * time.Hour

	for i := 0; i < 5000; i++ {
		assert.True(t, l.allow(fmt.Sprintf("dev-%d|1|stack", i), start, window))
	}
	assert.Len(t, l.last, 5000, "all keys are live inside the window")

	// Inside the window the same keys are refused and nothing new is stored.
	assert.False(t, l.allow("dev-0|1|stack", start.Add(window-time.Second), window))
	assert.Len(t, l.last, 5000)

	// One call after the window sweeps every expired key.
	assert.True(t, l.allow("dev-new|1|stack", start.Add(window), window))
	assert.Len(t, l.last, 1, "expired keys are evicted, only the new key remains")
}

// Covers the standing bounds requirement. A nil limiter holds no state at all.
// Added by QA Engineer.
func TestQA_SkipLimiterNilHoldsNothing(t *testing.T) {
	var l *skipLimiter
	assert.True(t, l.allow("k", time.Now(), time.Hour))
	assert.True(t, l.allow("k", time.Now(), time.Hour))
}
