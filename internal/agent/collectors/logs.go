package collectors

import (
	"context"
	"encoding/json"
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
	mu       sync.Mutex
	lastSeen time.Time
}

func (c *LogsCollector) Name() string { return "logs" }

func (c *LogsCollector) Collect(ctx context.Context) (interface{}, error) {
	if runtime.GOOS != "linux" {
		return []models.LogEntry{}, nil
	}

	c.mu.Lock()
	since := c.lastSeen
	c.mu.Unlock()

	if since.IsZero() {
		since = time.Now().Add(-5 * time.Minute)
	}

	sinceStr := since.UTC().Format("2006-01-02 15:04:05")
	out, err := exec.CommandContext(ctx, "journalctl",
		"--since", sinceStr,
		"--priority=0..6",
		"-o", "json",
		"--no-pager",
		"-n", "500",
	).Output()
	if err != nil {
		return []models.LogEntry{}, nil
	}

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
	}

	if !latest.IsZero() {
		c.mu.Lock()
		c.lastSeen = latest.Add(time.Microsecond)
		c.mu.Unlock()
	}

	return entries, nil
}
