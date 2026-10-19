package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/DesyncTheThird/rIOt/internal/server/quota"
)

var logContainerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)
var logContainerID = regexp.MustCompile(`^(?:[a-fA-F0-9]{12}|[a-fA-F0-9]{64})$`)

type logUse struct {
	at           time.Time
	lines, bytes int
}
type logWindow struct {
	events []logUse
	last   time.Time
}

func (w *logWindow) usage(now time.Time) (int, int) {
	i := 0
	for i < len(w.events) && !w.events[i].at.After(now.Add(-time.Minute)) {
		i++
	}
	w.events = w.events[i:]
	lines, bytes := 0, 0
	for _, e := range w.events {
		lines += e.lines
		bytes += e.bytes
	}
	w.last = now
	return lines, bytes
}
func (w *logWindow) take(now time.Time, lines, bytes, maxLines, maxBytes int) bool {
	l, b := w.usage(now)
	if l+lines > maxLines || b+bytes > maxBytes {
		return false
	}
	if len(w.events) > 0 && w.events[len(w.events)-1].at.Equal(now) {
		e := &w.events[len(w.events)-1]
		e.lines += lines
		e.bytes += bytes
	} else {
		w.events = append(w.events, logUse{now, lines, bytes})
	}
	return true
}

type logHour struct {
	hour                int64
	rejected, restamped int
}
type deviceLogState struct {
	containers                                     map[string]*logWindow
	device, unknown, gaps                          logWindow
	hours                                          [24]logHour
	lastUsed, lastRestamp, lastWarning, cleanSince time.Time
	lastSkew                                       int64
	consecutive                                    int
	skewActive                                     bool
}
type containerLogIngest struct {
	mu      sync.Mutex
	devices map[string]*deviceLogState
}

func newContainerLogIngest() *containerLogIngest {
	return &containerLogIngest{devices: map[string]*deviceLogState{}}
}
func truncateLogLine(line string) string {
	if len(line) <= models.StoredLogMaxLineBytes {
		return line
	}
	const suffix = " [truncated]"
	n := models.StoredLogMaxLineBytes - len(suffix)
	for n > 0 && !utf8.RuneStart(line[n]) {
		n--
	}
	return line[:n] + suffix
}

func applyLogWindow(e models.ContainerLogEntry, now time.Time, retention int, index int) models.ContainerLogEntry {
	// An agent may not claim the server-owned skew marker.
	e.AgentTS = nil
	if e.Timestamp.Before(now.AddDate(0, 0, -retention)) || e.Timestamp.After(now.Add(models.TelemetryFutureTolerance)) {
		ts := e.Timestamp
		e.AgentTS = &ts
		e.Timestamp = now.Truncate(time.Microsecond).Add(time.Duration(index) * time.Microsecond)
	}
	return e
}

// apply validates, windows and caps before any potentially expensive masking.
func (s *containerLogIngest) apply(deviceID string, entries []models.ContainerLogEntry, gaps []models.ContainerLogGap, docker *models.DockerInfo, now time.Time, retention int) ([]models.ContainerLogEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if retention < 1 || retention > 90 {
		retention = 7
	}
	for id, d := range s.devices {
		if now.Sub(d.lastUsed) > 24*time.Hour {
			delete(s.devices, id)
		}
	}
	d := s.devices[deviceID]
	if d == nil {
		d = &deviceLogState{containers: map[string]*logWindow{}}
		s.devices[deviceID] = d
	}
	d.lastUsed = now
	hour := now.Unix() / 3600
	slot := hour % 24
	if slot < 0 {
		slot = 0
	}
	stats := &d.hours[slot]
	if stats.hour != hour {
		*stats = logHour{hour: hour}
	}
	known := map[string]string{}
	if docker != nil {
		for _, c := range docker.Containers {
			known[c.Name] = c.ID
		}
	}
	maxLines := min(max(len(known), 1)*1000, models.StoredLogDeviceMaxLines)
	maxBytes := min(max(len(known), 1)*models.StoredLogMaxBytes, models.StoredLogDeviceMaxBytes)
	for key, w := range d.containers {
		if now.Sub(w.last) > models.StoredLogLimiterIdle {
			delete(d.containers, key)
		}
	}
	dropped := map[string]int{}
	marker := map[string]models.ContainerLogEntry{}
	kept := make([]models.ContainerLogEntry, 0, min(len(entries), maxLines))
	hadSkew := false
	if len(entries) > 20000 {
		stats.rejected += len(entries) - 20000
		entries = entries[len(entries)-20000:]
	}
	// Reverse traversal keeps the newest output when one ingest exceeds a cap.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if !logContainerName.MatchString(e.ContainerName) || !logContainerID.MatchString(e.ContainerID) || (e.Stream != "stdout" && e.Stream != "stderr") {
			stats.rejected++
			continue
		}
		e.DeviceID = deviceID
		e.ID = 0
		e.Line = truncateLogLine(models.SanitizeText(e.Line))
		e.StoredPriority = quota.Priority(e)
		e = applyLogWindow(e, now, retention, i)
		if e.AgentTS != nil {
			hadSkew = true
			stats.restamped++
			d.lastRestamp = now
			d.lastSkew = int64(e.AgentTS.Sub(now).Seconds())
		}
		if _, ok := known[e.ContainerName]; !ok {
			if !d.unknown.take(now, 1, len(e.Line), models.StoredLogUnknownMaxLines, models.StoredLogUnknownMaxBytes) {
				stats.rejected++
				continue
			}
		}
		w := d.containers[e.ContainerName]
		if w == nil {
			if len(d.containers) >= models.StoredLogContainerLimiterMax {
				oldest := ""
				var last time.Time
				for key, entry := range d.containers {
					if oldest == "" || entry.last.Before(last) {
						oldest = key
						last = entry.last
					}
				}
				delete(d.containers, oldest)
			}
			w = &logWindow{}
			d.containers[e.ContainerName] = w
		}
		l, b := w.usage(now)
		dl, db := d.device.usage(now)
		if l >= models.StoredLogMaxLines || b+len(e.Line) > models.StoredLogMaxBytes || dl >= maxLines || db+len(e.Line) > maxBytes {
			dropped[e.ContainerName]++
			if _, exists := marker[e.ContainerName]; !exists {
				marker[e.ContainerName] = e
			}
			continue
		}
		e.Line, _ = redact.MaskLine(e.Line)
		e.Line = truncateLogLine(e.Line)
		if b+len(e.Line) > models.StoredLogMaxBytes || db+len(e.Line) > maxBytes {
			dropped[e.ContainerName]++
			if _, exists := marker[e.ContainerName]; !exists {
				marker[e.ContainerName] = e
			}
			continue
		}
		w.take(now, 1, len(e.Line), models.StoredLogMaxLines, models.StoredLogMaxBytes)
		d.device.take(now, 1, len(e.Line), maxLines, maxBytes)
		kept = append(kept, e)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	for name, count := range dropped {
		if !d.gaps.take(now, 1, 0, models.StoredLogGapRowsPerDevice, 0) {
			stats.rejected += count
			continue
		}
		e := marker[name]
		e.Stream = "gap_server"
		e.Line = strconv.Itoa(count)
		kept = append(kept, e)
	}
	seen := map[string]bool{}
	for i, g := range gaps {
		if i >= len(known) {
			stats.rejected += len(gaps) - i
			break
		}
		_, exists := known[g.ContainerName]
		if !exists || seen[g.ContainerName] || !logContainerName.MatchString(g.ContainerName) || !logContainerID.MatchString(g.ContainerID) || g.Count < 1 || g.Count > 10000000 || g.Reason != "agent_cap" {
			stats.rejected++
			continue
		}
		seen[g.ContainerName] = true
		if !d.gaps.take(now, 1, 0, models.StoredLogGapRowsPerDevice, 0) {
			stats.rejected++
			continue
		}
		line := strconv.FormatInt(g.Count, 10)
		if g.LowerBound {
			line += "+"
		}
		e := models.ContainerLogEntry{DeviceID: deviceID, ContainerID: g.ContainerID, ContainerName: g.ContainerName, Timestamp: g.Timestamp, Stream: "gap_agent", Line: line}
		e = applyLogWindow(e, now, retention, len(entries)+i)
		kept = append(kept, e)
		if e.AgentTS != nil {
			hadSkew = true
			stats.restamped++
			d.lastRestamp = now
			d.lastSkew = int64(e.AgentTS.Sub(now).Seconds())
		}
	}
	fire := false
	if hadSkew {
		d.consecutive++
		d.cleanSince = time.Time{}
		if d.consecutive >= 3 && !d.skewActive {
			d.skewActive = true
			fire = true
		}
	} else {
		d.consecutive = 0
		if d.cleanSince.IsZero() {
			d.cleanSince = now
		}
		if now.Sub(d.cleanSince) >= time.Hour {
			d.skewActive = false
		}
	}
	return kept, fire
}

func (h *Handlers) prepareContainerLogs(id string, entries []models.ContainerLogEntry, gaps []models.ContainerLogGap, docker *models.DockerInfo, now time.Time) []models.ContainerLogEntry {
	if h.logIngest == nil {
		h.logIngest = newContainerLogIngest()
	}
	entries, skew := h.logIngest.apply(id, entries, gaps, docker, now, h.containerLogRetentionDays)
	if h.storageQuota != nil {
		ordinary := make([]models.ContainerLogEntry, 0, len(entries))
		markers := []models.ContainerLogEntry{}
		template := map[string]models.ContainerLogEntry{}
		for _, e := range entries {
			if strings.HasPrefix(e.Stream, "gap_") {
				markers = append(markers, e)
			} else {
				ordinary = append(ordinary, e)
				template[e.ContainerName] = e
			}
		}
		kept, drops := h.storageQuota.FilterLogs(id, ordinary, now)
		entries = append(kept, markers...)
		h.logIngest.mu.Lock()
		d := h.logIngest.devices[id]
		for name, count := range drops {
			if d != nil && d.gaps.take(now, 1, 0, models.StoredLogGapRowsPerDevice, 0) {
				e := template[name]
				e.Stream = "gap_quota"
				e.Line = strconv.Itoa(count)
				entries = append(entries, e)
			}
		}
		h.logIngest.mu.Unlock()
	}

	if skew && h.events != nil {
		h.logIngest.mu.Lock()
		skewSeconds := h.logIngest.devices[id].lastSkew
		h.logIngest.mu.Unlock()
		e := &models.Event{DeviceID: id, Type: "clock_skew", Severity: "warning", Message: fmt.Sprintf("Container log timestamps repeatedly differ from server time (skew %+d seconds)", skewSeconds), CreatedAt: now}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := h.events.Create(ctx, e); err == nil && h.hub != nil {
			h.hub.BroadcastEvent(e)
		}
		cancel()
	}
	return entries
}

func (h *Handlers) dockerLogHostState(id string) map[string]interface{} {
	out := map[string]interface{}{}
	now := h.clock()
	if h.logIngest != nil {
		h.logIngest.mu.Lock()
		d := h.logIngest.devices[id]
		if d != nil {
			restamped, rejected := 0, 0
			for _, slot := range d.hours {
				if now.Unix()/3600-slot.hour < 24 {
					restamped += slot.restamped
					rejected += slot.rejected
				}
			}
			out["log_clock_skew"] = map[string]interface{}{"restamped_24h": restamped, "last_skew_s": d.lastSkew, "last_restamp_at": d.lastRestamp}
			out["log_ingest"] = map[string]int{"rejected_24h": rejected}
		}
		h.logIngest.mu.Unlock()
	}
	if h.storageQuota != nil {
		out["storage_quota"] = h.storageQuota.Status(id, now)
	}
	return out
}
