package handlers

import (
	"log/slog"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

// REG-HARDEN-001: agent-supplied snapshot and heartbeat timestamps are trusted
// for the past and up to db.FutureSkewAllowance into the future, and clamped to
// the server receive time beyond that. Telemetry is never rejected because of a
// timestamp: the agent's clock may be wrong, but its data is still wanted.

// Kinds of clamped ingest, used in the clamp log entry and as limiter keys.
const (
	clampKindSnapshot  = "snapshot"
	clampKindHeartbeat = "heartbeat"
)

// clampLogWindow is the minimum gap between two clamp log entries for the same
// device and kind.
const clampLogWindow = time.Hour

// clampEntryMaxAge is how long a limiter entry with a pending suppressed count
// is kept without a new clamp before the sweep drops it.
const clampEntryMaxAge = 24 * time.Hour

// clampFutureTimestamp applies the ingest timestamp rule.
//
//   - A zero timestamp defaults to now (not reported as a clamp).
//   - A timestamp later than now + db.FutureSkewAllowance becomes now (clamped).
//   - Anything else, including every past timestamp and exactly now+allowance,
//     is returned unchanged.
func clampFutureTimestamp(ts, now time.Time) (time.Time, bool) {
	if ts.IsZero() {
		return now, false
	}
	if ts.After(now.Add(db.FutureSkewAllowance)) {
		return now, true
	}
	return ts, false
}

// ingestNow is the server receive time for telemetry and heartbeats. Tests set
// ingestClock, or the handler-wide clock (h.now); with neither (the production
// default) it is time.Now.
func (h *Handlers) ingestNow() time.Time {
	if h.ingestClock != nil {
		return h.ingestClock().UTC()
	}
	return h.clock()
}

// noteClamp records that a timestamp was clamped. The log entry is debounced to
// at most one per device and kind per clampLogWindow and carries the number of
// clamps since the previous entry. It never logs request content.
func (h *Handlers) noteClamp(deviceID, kind string, sent, received time.Time) {
	emit, n := h.clampLog.observe(deviceID, kind, received)
	if !emit {
		return
	}
	skew := sent.Sub(received)
	slog.Warn("future-dated timestamp clamped",
		"device_id", loggableDeviceID(deviceID),
		"kind", kind,
		"skew", skew.String(),
		"skew_seconds", int64(skew/time.Second),
		"clamps_since_last", n,
	)
}

type clampKey struct {
	deviceID string
	kind     string
}

type clampEntry struct {
	lastLogged time.Time
	suppressed int
}

// clampLogLimiter debounces clamp log entries. Its state is bounded by the
// number of (device, kind) pairs, which come from authenticated device IDs,
// and is swept so entries for deleted or long-quiet devices do not accumulate.
// It is lost on restart, which is acceptable.
type clampLogLimiter struct {
	mu        sync.Mutex
	entries   map[clampKey]*clampEntry
	lastSweep time.Time
}

func newClampLogLimiter() *clampLogLimiter {
	return &clampLogLimiter{entries: make(map[clampKey]*clampEntry)}
}

// observe registers one clamp at time now. It reports whether a log entry
// should be emitted and, if so, how many clamps (including this one) happened
// since the previous entry. A nil limiter always emits with a count of 1, so a
// Handlers built without one still works.
func (l *clampLogLimiter) observe(deviceID, kind string, now time.Time) (emit bool, clampsSinceLast int) {
	if l == nil {
		return true, 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	key := clampKey{deviceID: deviceID, kind: kind}
	e, ok := l.entries[key]
	switch {
	case !ok:
		l.entries[key] = &clampEntry{lastLogged: now}
		emit, clampsSinceLast = true, 1
	case now.Sub(e.lastLogged) >= clampLogWindow:
		emit, clampsSinceLast = true, e.suppressed+1
		e.suppressed = 0
		e.lastLogged = now
	default:
		e.suppressed++
	}

	l.sweepLocked(now)
	return emit, clampsSinceLast
}

// sweepLocked drops stale entries, at most once per clampLogWindow. An entry is
// stale when it has nothing pending and its window has passed, or when it has
// been idle for clampEntryMaxAge. The caller holds l.mu.
func (l *clampLogLimiter) sweepLocked(now time.Time) {
	if l.lastSweep.IsZero() {
		l.lastSweep = now
		return
	}
	if now.Sub(l.lastSweep) < clampLogWindow {
		return
	}
	l.lastSweep = now
	for k, e := range l.entries {
		age := now.Sub(e.lastLogged)
		if (e.suppressed == 0 && age >= clampLogWindow) || age >= clampEntryMaxAge {
			delete(l.entries, k)
		}
	}
}
