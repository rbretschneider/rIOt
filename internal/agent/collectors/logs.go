package collectors

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// LogsCollector gathers recent journal entries (info and above).
// Linux-only; returns empty on other platforms.
type LogsCollector struct {
	mu          sync.Mutex
	lastSeen    time.Time
	authCounter *authFailureCounter
}

func (c *LogsCollector) Name() string { return "logs" }

func (c *LogsCollector) Collect(ctx context.Context) (interface{}, error) {
	if runtime.GOOS != "linux" {
		return []models.LogEntry{}, nil
	}

	c.mu.Lock()
	since := c.lastSeen
	c.mu.Unlock()

	// Record whether this is the first-ever scan (cursor was zero on entry).
	// We read this BEFORE the exec so that a subsequent mutation of c.lastSeen
	// cannot affect the first-interval gate decision (AD-001, note 3).
	firstScan := since.IsZero()

	if since.IsZero() {
		since = time.Now().Add(-5 * time.Minute)
	}

	sinceStr := since.Format("2006-01-02 15:04:05")
	out, err := exec.CommandContext(ctx, "journalctl",
		"--since", sinceStr,
		"--priority=0..6",
		"-o", "json",
		"--no-pager",
		"-n", "500",
	).Output()
	return c.collectFromOutput(out, err, firstScan)
}

// collectFromOutput handles the post-exec logic: on exec error it logs a
// warning and returns the fail-open empty slice (FR-006, ADD Section 9); on
// success it parses the output and conditionally marks the counter ready.
// Extracted as a named helper so unit tests can inject a pre-computed
// (output, error) tuple without spawning a real journalctl process.
func (c *LogsCollector) collectFromOutput(out []byte, err error, firstScan bool) (interface{}, error) {
	if err != nil {
		// Fail-open (FR-006): return empty log slice; do not call MarkReady;
		// SecurityCollector will drain 0 and report 0.
		slog.Warn("journalctl exec failed", "error", err)
		return []models.LogEntry{}, nil
	}

	entries, _ := c.parseAndCount(out)

	// After a real cursor-based scan (not the first-ever scan), mark the
	// counter ready so SecurityCollector reports the real count next tick.
	if !firstScan {
		c.authCounter.MarkReady()
	}

	return entries, nil
}

// parseAndCount parses the raw journalctl JSON output, builds the LogEntry
// slice, and increments the authFailureCounter for each line that passes the
// trusted-origin + content filters. It also advances c.lastSeen to the
// timestamp of the latest parsed entry.
//
// Extracted as a named helper so unit tests can feed raw JSON without
// spawning a real journalctl process (ADD Section 12, note 2).
func (c *LogsCollector) parseAndCount(out []byte) ([]models.LogEntry, time.Time) {
	var entries []models.LogEntry
	var latest time.Time

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		var ts time.Time
		if usecStr, ok := raw["__REALTIME_TIMESTAMP"].(string); ok {
			if usec, err := strconv.ParseInt(usecStr, 10, 64); err == nil {
				ts = time.Unix(usec/1_000_000, (usec%1_000_000)*1000)
			}
		}
		if ts.IsZero() {
			ts = time.Now()
		}

		priority := 4
		if p, ok := raw["PRIORITY"].(string); ok {
			if v, err := strconv.Atoi(p); err == nil {
				priority = v
			}
		}

		unit, _ := raw["_SYSTEMD_UNIT"].(string)
		if unit == "" {
			unit, _ = raw["SYSLOG_IDENTIFIER"].(string)
		}

		message, _ := raw["MESSAGE"].(string)

		entries = append(entries, models.LogEntry{
			Timestamp: ts,
			Priority:  priority,
			Unit:      unit,
			Message:   message,
		})

		if ts.After(latest) {
			latest = ts
		}

		// Increment the shared auth-failure counter for lines that pass
		// the trusted-origin filter AND a content pattern match (AD-001, AD-004).
		if matchesAuthFailure(raw) {
			c.authCounter.Add(1)
		}
	}

	if !latest.IsZero() {
		c.mu.Lock()
		c.lastSeen = latest.Add(time.Microsecond)
		c.mu.Unlock()
	}

	return entries, latest
}
