package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// AUTOUPD-HARDEN-001 AD-011: a compose stack whose containers report more than
// one working directory is not dispatched. The server cannot tell which is
// genuine (provenance is an agent-side fact), and a hostile image that copies
// the project label of a policy would otherwise silently stop that stack from
// updating, so the skip is raised as a warning event the dashboard shows.

const (
	// stackSkipMinWindow is the least time between two skip events for one
	// policy. The window is the maintenance cooldown when that is longer.
	stackSkipMinWindow = 6 * time.Hour
	// maxSkipEventDirs, maxSkipValueBytes and maxSkipEventBytes bound what one
	// event message names.
	maxSkipEventDirs  = 5
	maxSkipValueBytes = 200
	maxSkipEventBytes = 700
)

// composeProjectWorkDirs maps each compose project to the distinct working
// directories its containers report (compared by target key, so a trailing
// slash does not count as a second directory), the first spelling kept, sorted.
func composeProjectWorkDirs(containers []models.ContainerInfo) map[string][]string {
	seen := map[string]map[string]string{} // project -> key -> label value
	for _, c := range containers {
		project := c.Labels[models.LabelComposeProject]
		wd := c.Labels[models.LabelComposeWorkingDir]
		if project == "" || wd == "" {
			continue
		}
		if seen[project] == nil {
			seen[project] = map[string]string{}
		}
		if _, ok := seen[project][models.TargetKeyCompose(wd)]; !ok {
			seen[project][models.TargetKeyCompose(wd)] = wd
		}
	}
	out := make(map[string][]string, len(seen))
	for project, dirs := range seen {
		list := make([]string, 0, len(dirs))
		for _, wd := range dirs {
			list = append(list, wd)
		}
		sort.Strings(list)
		out[project] = list
	}
	return out
}

// skipLimiter allows one skip event per key per window. Its state is bounded by
// the number of (device, stack policy) pairs and swept, and is lost on restart,
// which at worst repeats one event.
type skipLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newSkipLimiter() *skipLimiter { return &skipLimiter{last: map[string]time.Time{}} }

// allow reports whether an event for key may be raised at now, and records it.
// A nil limiter always allows, so a Handlers built without one still works.
func (l *skipLimiter) allow(key string, now time.Time, window time.Duration) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, t := range l.last {
		if now.Sub(t) >= window {
			delete(l.last, k)
		}
	}
	if t, ok := l.last[key]; ok && now.Sub(t) < window {
		return false
	}
	l.last[key] = now
	return true
}

// quoteCapped quotes a label-derived value after cutting it to a fixed length,
// which also escapes any control character.
func quoteCapped(s string) string {
	return strconv.Quote(models.TruncateHead(s, maxSkipValueBytes))
}

// noteStackSkipped records that a stack was not dispatched because it reports
// several working directories: a Warn log on every observation, and a warning
// event at most once per policy per window.
func (h *Handlers) noteStackSkipped(ctx context.Context, deviceID string, pol *models.AutoUpdatePolicy, project string, dirs []string, now time.Time, cooldown time.Duration) {
	slog.Warn("auto-update: stack skipped, its containers report more than one working directory",
		"device_id", loggableDeviceID(deviceID), "project", quoteCapped(project), "working_dirs", len(dirs))

	window := stackSkipMinWindow
	if cooldown > window {
		window = cooldown
	}
	if !h.skipEvents.allow(fmt.Sprintf("%s|%d|%s", deviceID, pol.ID, pol.Target), now, window) || h.events == nil {
		return
	}

	host := deviceID
	if len(host) > 8 {
		host = host[:8]
	}
	if h.devices != nil {
		if d, err := h.devices.GetByID(ctx, deviceID); err == nil && d != nil && d.Hostname != "" {
			host = models.TruncateHead(d.Hostname, maxSkipValueBytes)
		}
	}
	shown := dirs
	more := ""
	if len(shown) > maxSkipEventDirs {
		more = fmt.Sprintf(" and %d more", len(shown)-maxSkipEventDirs)
		shown = shown[:maxSkipEventDirs]
	}
	quoted := make([]string, len(shown))
	for i, d := range shown {
		quoted[i] = quoteCapped(d)
	}
	msg := fmt.Sprintf("Auto-update skipped for stack %s on %s: its containers report more than one working directory (%s%s). "+
		"A container may have copied the project label; the stack is not updated until this is resolved.",
		quoteCapped(project), host, strings.Join(quoted, ", "), more)
	evt := &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventStackUpdateSkipped,
		Severity:  models.SeverityWarning,
		Message:   models.TruncateHead(msg, maxSkipEventBytes),
		CreatedAt: now.UTC(),
	}
	if err := h.events.Create(ctx, evt); err != nil {
		slog.Error("create stack skip event", "error", models.TruncateHead(err.Error(), maxSkipValueBytes))
		return
	}
	if h.hub != nil {
		h.hub.BroadcastEvent(evt)
	}
}
