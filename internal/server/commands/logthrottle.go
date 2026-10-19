package commands

import (
	"log/slog"
	"sync"
	"time"
)

// Rejection logs are persisted to Postgres at Warn level and above, so one
// device must not be able to flood server_logs (SEC C-1).
const (
	// RejectionLogsPerMinute is how many throttled records one device may emit
	// per one-minute window before the rest are suppressed.
	RejectionLogsPerMinute = 10

	logThrottleWindow = time.Minute
	// logThrottleEvictEvery bounds the work of expiring stale windows.
	logThrottleEvictEvery = 256
)

type throttleWindow struct {
	start      time.Time
	allowed    int
	suppressed int
}

// LogThrottle limits the Warn records a device can cause: a fixed one-minute
// window per device, up to RejectionLogsPerMinute records, then suppression. The
// first allowed call of the next window emits one summary Warn
// ("rejection logs suppressed", device_id, count) when the previous window
// suppressed anything. It has an injected clock and no goroutine.
type LogThrottle struct {
	mu      sync.Mutex
	now     func() time.Time
	log     *slog.Logger
	windows map[string]*throttleWindow
	calls   int
}

// NewLogThrottle returns a throttle that writes its summaries to logger.
func NewLogThrottle(now func() time.Time, logger *slog.Logger) *LogThrottle {
	return &LogThrottle{now: now, log: logger, windows: make(map[string]*throttleWindow)}
}

type suppressedSummary struct {
	deviceID string
	count    int
}

// Allow reports whether the caller may write a throttled record for deviceID.
func (t *LogThrottle) Allow(deviceID string) bool {
	t.mu.Lock()
	now := t.now()
	var summaries []suppressedSummary

	t.calls++
	if t.calls%logThrottleEvictEvery == 0 {
		summaries = append(summaries, t.expireLocked(now, deviceID)...)
	}

	w, ok := t.windows[deviceID]
	if ok && now.Sub(w.start) >= logThrottleWindow {
		if w.suppressed > 0 {
			summaries = append(summaries, suppressedSummary{deviceID, w.suppressed})
		}
		ok = false
	}
	if !ok {
		w = &throttleWindow{start: now}
		t.windows[deviceID] = w
	}
	allowed := w.allowed < RejectionLogsPerMinute
	if allowed {
		w.allowed++
	} else {
		w.suppressed++
	}
	t.mu.Unlock()

	for _, s := range summaries {
		t.log.Warn("rejection logs suppressed", "device_id", s.deviceID, "count", s.count)
	}
	return allowed
}

// expireLocked drops windows that ended, returning a summary for each one that
// suppressed records (except keep, which Allow handles itself).
func (t *LogThrottle) expireLocked(now time.Time, keep string) []suppressedSummary {
	var out []suppressedSummary
	for id, w := range t.windows {
		if id == keep || now.Sub(w.start) < logThrottleWindow {
			continue
		}
		if w.suppressed > 0 {
			out = append(out, suppressedSummary{id, w.suppressed})
		}
		delete(t.windows, id)
	}
	return out
}
