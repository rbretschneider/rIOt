package collectors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// CronCollector gathers cron jobs and scheduled tasks.
type CronCollector struct{}

func (c *CronCollector) Name() string { return "cron" }

func (c *CronCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.CronInfo{}

	switch runtime.GOOS {
	case "linux":
		c.collectLinuxCrontabs(info)
		c.collectLinuxTimers(ctx, info)
	case "windows":
		c.collectWindowsTasks(ctx, info)
	}

	return info, nil
}

// collectLinuxCrontabs reads user and system crontab files.
func (c *CronCollector) collectLinuxCrontabs(info *models.CronInfo) {
	// User crontabs: /var/spool/cron/crontabs/<user>
	userDir := "/var/spool/cron/crontabs"
	if entries, err := os.ReadDir(userDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			user := entry.Name()
			path := filepath.Join(userDir, user)
			jobs := ParseCrontab(path, user, false)
			info.Jobs = append(info.Jobs, jobs...)
		}
	}

	// System crontab: /etc/crontab
	if jobs := ParseCrontab("/etc/crontab", "", true); len(jobs) > 0 {
		info.Jobs = append(info.Jobs, jobs...)
	}

	// System cron.d directory: /etc/cron.d/*
	cronDDir := "/etc/cron.d"
	if entries, err := os.ReadDir(cronDDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(cronDDir, entry.Name())
			jobs := ParseCrontab(path, "", true)
			info.Jobs = append(info.Jobs, jobs...)
		}
	}
}

// ParseCrontab reads a crontab file and returns parsed CronJob entries.
// If isSystem is true, the 6th field is the user (system crontab format).
// If isSystem is false, the user comes from the username parameter (user crontab format).
func ParseCrontab(path, username string, isSystem bool) []models.CronJob {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	return ParseCrontabReader(f, path, username, isSystem)
}

// ParseCrontabReader parses crontab content from a reader.
func ParseCrontabReader(r io.Reader, source, username string, isSystem bool) []models.CronJob {
	var jobs []models.CronJob
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines
		if line == "" {
			continue
		}

		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Skip environment variable assignments:
		// Lines with = but no spaces before the =
		if isEnvAssignment(line) {
			continue
		}

		// Parse the cron line
		fields := strings.Fields(line)

		if isSystem {
			// System crontab: min hour dom mon dow user command...
			if len(fields) < 7 {
				continue
			}
			schedule := strings.Join(fields[0:5], " ")
			user := fields[5]
			command := strings.Join(fields[6:], " ")
			jobs = append(jobs, models.CronJob{
				User:     user,
				Schedule: schedule,
				Command:  command,
				Source:   source,
				Enabled:  true,
			})
		} else {
			// User crontab: min hour dom mon dow command...
			if len(fields) < 6 {
				continue
			}
			schedule := strings.Join(fields[0:5], " ")
			command := strings.Join(fields[5:], " ")
			jobs = append(jobs, models.CronJob{
				User:     username,
				Schedule: schedule,
				Command:  command,
				Source:   source,
				Enabled:  true,
			})
		}
	}

	return jobs
}

// isEnvAssignment returns true if the line looks like a VAR=value assignment
// rather than a cron entry. An env assignment has = with no spaces before it.
func isEnvAssignment(line string) bool {
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return false
	}
	beforeEq := line[:eqIdx]
	// If there are no spaces before the =, it's an env assignment
	return !strings.Contains(beforeEq, " ")
}

// systemdTimer is the JSON structure from systemctl list-timers --output=json.
type systemdTimer struct {
	Next     uint64 `json:"next"`
	Left     uint64 `json:"left"`
	Last     uint64 `json:"last"`
	Passed   uint64 `json:"passed"`
	Unit     string `json:"unit"`
	Activates string `json:"activates"`
}

// collectLinuxTimers runs systemctl list-timers to get systemd timers.
func (c *CronCollector) collectLinuxTimers(ctx context.Context, info *models.CronInfo) {
	cmd := exec.CommandContext(ctx, "systemctl", "list-timers", "--all", "--output=json")
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("failed to list systemd timers", "error", err)
		return
	}

	var timers []systemdTimer
	if err := json.Unmarshal(out, &timers); err != nil {
		slog.Debug("failed to parse systemd timers JSON", "error", err)
		return
	}

	for _, t := range timers {
		timer := models.CronTimer{
			Name:    t.Unit,
			Unit:    t.Activates,
			Enabled: true,
		}

		// Get calendar info from systemctl show
		showCmd := exec.CommandContext(ctx, "systemctl", "show", t.Unit, "--property=TimersCalendar", "--no-pager")
		if showOut, err := showCmd.Output(); err == nil {
			line := strings.TrimSpace(string(showOut))
			if after, ok := strings.CutPrefix(line, "TimersCalendar="); ok {
				// Format: { OnCalendar=...; ... } — extract the OnCalendar part
				if idx := strings.Index(after, "OnCalendar="); idx >= 0 {
					cal := after[idx+len("OnCalendar="):]
					if semi := strings.Index(cal, " ;"); semi >= 0 {
						cal = cal[:semi]
					}
					timer.Calendar = cal
				} else {
					timer.Calendar = after
				}
			}
		}

		if t.Next > 0 {
			timer.NextRun = fmt.Sprintf("%d", t.Next)
		}
		if t.Last > 0 {
			timer.LastRun = fmt.Sprintf("%d", t.Last)
		}

		info.Timers = append(info.Timers, timer)
	}
}

// collectWindowsTasks runs schtasks and parses CSV output.
func (c *CronCollector) collectWindowsTasks(ctx context.Context, info *models.CronInfo) {
	cmd := exec.CommandContext(ctx, "schtasks", "/query", "/fo", "CSV", "/v")
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("failed to query scheduled tasks", "error", err)
		return
	}

	reader := csv.NewReader(bytes.NewReader(out))
	records, err := reader.ReadAll()
	if err != nil {
		slog.Debug("failed to parse schtasks CSV", "error", err)
		return
	}

	if len(records) < 2 {
		return
	}

	// Build column index map from header row
	header := records[0]
	colIdx := make(map[string]int)
	for i, col := range header {
		// Trim BOM if present
		col = strings.TrimPrefix(col, "\xef\xbb\xbf")
		colIdx[strings.TrimSpace(col)] = i
	}

	getCol := func(row []string, name string) string {
		if idx, ok := colIdx[name]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	for _, row := range records[1:] {
		taskName := getCol(row, "TaskName")

		// Filter out built-in Windows tasks
		if strings.HasPrefix(taskName, `\Microsoft\Windows\`) {
			continue
		}

		status := getCol(row, "Status")
		enabled := status != "Disabled"

		job := models.CronJob{
			TaskName: taskName,
			Command:  getCol(row, "Task To Run"),
			Schedule: getCol(row, "Scheduled Type"),
			User:     getCol(row, "Run As User"),
			NextRun:  getCol(row, "Next Run Time"),
			LastRun:  getCol(row, "Last Run Time"),
			Source:   "schtasks",
			Enabled:  enabled,
		}

		info.Jobs = append(info.Jobs, job)
	}
}
