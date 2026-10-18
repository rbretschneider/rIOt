package commands

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// captureLogger returns a text logger writing to the returned buffer.
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestCL_SEC001_LogThrottleSummary(t *testing.T) {
	t.Run("ten pass per minute, the rest are suppressed", func(t *testing.T) {
		clock := newFakeClock()
		log, _ := captureLogger()
		th := NewLogThrottle(clock.Now, log)
		allowed := 0
		for i := 0; i < 15; i++ {
			if th.Allow("dev-a") {
				allowed++
			}
		}
		assert.Equal(t, RejectionLogsPerMinute, allowed, "[SEC-001] at most 10 records per device per minute")
	})

	t.Run("next window emits one summary with the suppressed count", func(t *testing.T) {
		clock := newFakeClock()
		log, buf := captureLogger()
		th := NewLogThrottle(clock.Now, log)
		for i := 0; i < 15; i++ {
			th.Allow("dev-a")
		}
		assert.NotContains(t, buf.String(), "rejection logs suppressed", "no summary inside the window")

		clock.Advance(61 * time.Second)
		assert.True(t, th.Allow("dev-a"), "a new window allows records again")

		out := buf.String()
		assert.Equal(t, 1, strings.Count(out, "rejection logs suppressed"), "exactly one summary line")
		assert.Contains(t, out, "device_id=dev-a")
		assert.Contains(t, out, "count=5", "the summary carries the number of suppressed records")
		assert.Contains(t, out, "level=WARN")
	})

	t.Run("no summary when nothing was suppressed", func(t *testing.T) {
		clock := newFakeClock()
		log, buf := captureLogger()
		th := NewLogThrottle(clock.Now, log)
		th.Allow("dev-a")
		clock.Advance(2 * time.Minute)
		th.Allow("dev-a")
		assert.Empty(t, buf.String())
	})

	t.Run("devices have independent windows", func(t *testing.T) {
		clock := newFakeClock()
		log, _ := captureLogger()
		th := NewLogThrottle(clock.Now, log)
		for i := 0; i < 15; i++ {
			th.Allow("dev-a")
		}
		assert.True(t, th.Allow("dev-b"), "device B has its own budget")
	})

	t.Run("a window that expires unseen is summarised by lazy eviction", func(t *testing.T) {
		clock := newFakeClock()
		log, buf := captureLogger()
		th := NewLogThrottle(clock.Now, log)
		for i := 0; i < 13; i++ {
			th.Allow("quiet-device")
		}
		clock.Advance(5 * time.Minute)
		for i := 0; i < logThrottleEvictEvery; i++ {
			th.Allow("busy-device")
		}
		assert.Contains(t, buf.String(), "device_id=quiet-device")
		assert.Contains(t, buf.String(), "count=3")
	})
}
