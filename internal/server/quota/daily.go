// Package quota implements continuously refilling storage allowances. Exhaustion
// reduces fidelity while retaining priority output and periodic device state.
package quota

import (
	"context"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

var ErrorLinePatterns = regexp.MustCompile(`(?i)\b(err|error|fatal|panic|exception|traceback|crit|critical|emerg|alert|fail|failed|failure)\b`)

func Priority(e models.ContainerLogEntry) bool {
	return e.StoredPriority || e.Stream == "stderr" || ErrorLinePatterns.MatchString(e.Line)
}

type Bucket struct {
	Notified  bool    `json:"notified"`
	Level     float64 `json:"level"`
	Allowance int64   `json:"allowance"`
	Used      int64   `json:"used"`
	Degraded  bool    `json:"degraded"`
}
type Record struct {
	LatestTelemetry time.Time          `json:"latest_telemetry"`
	DeviceID        string             `json:"device_id"`
	Day             string             `json:"day"`
	At              time.Time          `json:"at"`
	Buckets         map[string]*Bucket `json:"buckets"`
	LastSnapshot    time.Time          `json:"last_snapshot"`
}
type Repository interface {
	Load(context.Context) ([]Record, error)
	Save(context.Context, Record) error
}
type device struct {
	Record
	sampleMinute                            int64
	samples                                 map[string]int
	deviceSamples                           int
	emergencyLogBytes, emergencyDeviceBytes int64
	dirty                                   bool
	lastUsed                                time.Time
}
type Manager struct {
	mu      sync.Mutex
	devices map[string]*device
	repo    Repository
	mode    string
}

func New(repo Repository) *Manager { return &Manager{devices: make(map[string]*device), repo: repo} }

func (m *Manager) Seed(ctx context.Context) error {
	if m.repo == nil {
		return nil
	}
	records, err := m.repo.Load(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range records {
		m.devices[r.DeviceID] = &device{Record: r, samples: map[string]int{}, lastUsed: r.At}
	}
	return nil
}
func defaults() map[string]int64 {
	return map[string]int64{"logs": models.DeviceDailyLogBytes, "telemetry": models.DeviceDailyTelemetryBytes, "device_logs": models.DeviceDailyDeviceLogBytes, "commands": models.DeviceDailyCommandBytes}
}
func (m *Manager) get(id string, now time.Time) *device {
	d := m.devices[id]
	if d == nil {
		d = &device{Record: Record{DeviceID: id, Day: now.UTC().Format("2006-01-02"), At: now, Buckets: map[string]*Bucket{}}, samples: map[string]int{}}
		m.devices[id] = d
	}
	if d.Buckets == nil {
		d.Buckets = map[string]*Bucket{}
	}
	elapsed := now.Sub(d.At).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	for class, q := range defaults() {
		b := d.Buckets[class]
		if b == nil {
			b = &Bucket{Level: float64(q) / 4, Allowance: q}
			d.Buckets[class] = b
		}
		capacity := float64(b.Allowance) / 4
		b.Level = min(capacity, b.Level+elapsed*float64(b.Allowance)/86400)
		if b.Degraded {
			if b.Level > capacity*.5 {
				b.Degraded = false
				b.Notified = false
			}
		} else if b.Level <= capacity*.25 {
			b.Degraded = true
		}
		if d.Day != now.UTC().Format("2006-01-02") {
			b.Used = 0
		}
	}
	d.Day = now.UTC().Format("2006-01-02")
	d.At = now
	d.lastUsed = now
	minute := now.Unix() / 60
	if d.sampleMinute != minute {
		d.samples = map[string]int{}
		d.deviceSamples = 0
		d.emergencyLogBytes = 0
		d.emergencyDeviceBytes = 0
		d.sampleMinute = minute
	}
	return d
}
func (m *Manager) SetMode(mode string) { m.mu.Lock(); m.mode = mode; m.mu.Unlock() }

// AcceptTelemetry keeps freshness independent of the downsampled storage clock.
// A buffered old snapshot must not roll back automation or live fleet state.
func (m *Manager) AcceptTelemetry(id string, collectedAt, receivedAt time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.get(id, receivedAt)
	if !d.LatestTelemetry.IsZero() && !collectedAt.After(d.LatestTelemetry) {
		return false
	}
	d.LatestTelemetry = collectedAt
	d.dirty = true
	return true
}

type Transition struct{ DeviceID, Class string }

func (m *Manager) DrainTransitions(now time.Time) []Transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Transition{}
	for id := range m.devices {
		d := m.get(id, now)
		for class, b := range d.Buckets {
			if b.Degraded && !b.Notified {
				out = append(out, Transition{id, class})
				b.Notified = true
				d.dirty = true
			}
		}
	}
	return out
}

func (m *Manager) CommandOutput(id, text string, now time.Time) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.get(id, now)
	b := d.Buckets["commands"]
	cost := float64(len(text))
	if b.Level >= cost {
		b.Level -= cost
		b.Used += int64(cost)
		d.dirty = true
		return text, false
	}
	const marker = "\nOutput truncated (storage quota)\n"
	text = models.TruncateHeadTail(text, 1024-len(marker))
	text = marker + text
	b.Used += int64(len(text))
	d.dirty = true
	return text, true
}
func (m *Manager) SetLogAllowance(id string, q int64, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.get(id, now)
	b := d.Buckets["logs"]
	q = max(q, 32<<20)
	b.Allowance = q
	b.Level = min(b.Level, float64(q)/4)
	d.dirty = true
}

// fairShares allocates bytes max-min fairly, including within the priority lane.
func fairShares(demand map[string]int64, budget int64) map[string]int64 {
	out := map[string]int64{}
	keys := make([]string, 0, len(demand))
	for key := range demand {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for len(keys) > 0 && budget > 0 {
		share := max(int64(1), budget/int64(len(keys)))
		next := keys[:0]
		for _, key := range keys {
			n := min(share, demand[key]-out[key], budget)
			out[key] += n
			budget -= n
			if out[key] < demand[key] {
				next = append(next, key)
			}
		}
		keys = next
	}
	return out
}

func (m *Manager) FilterLogs(id string, entries []models.ContainerLogEntry, now time.Time) ([]models.ContainerLogEntry, map[string]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.get(id, now)
	b := d.Buckets["logs"]
	drops := map[string]int{}
	keep := make([]bool, len(entries))
	sampled := b.Degraded || m.mode == "emergency" || m.mode == "critical"
	budget := int64(b.Level)
	if m.mode == "emergency" || m.mode == "critical" {
		budget = min(budget, max(0, (64<<10)-d.emergencyLogBytes))
	}
	for _, priority := range []bool{true, false} {
		demand := map[string]int64{}
		candidates := map[string][]int{}
		for i, e := range entries {
			if Priority(e) != priority {
				continue
			}
			if !priority && m.mode == "critical" {
				continue
			}
			if !priority && sampled {
				limit := models.StdoutSampleLinesPerContainerPerMinute
				if m.mode == "emergency" {
					limit = 1
				}
				if d.samples[e.ContainerName] >= limit {
					continue
				}
				d.samples[e.ContainerName]++
			}
			demand[e.ContainerName] += int64(len(e.Line) + 200)
			candidates[e.ContainerName] = append(candidates[e.ContainerName], i)
		}
		shares := fairShares(demand, budget)
		for name, indexes := range candidates {
			for _, i := range indexes {
				cost := int64(len(entries[i].Line) + 200)
				if cost <= shares[name] {
					keep[i] = true
					shares[name] -= cost
					budget -= cost
					b.Level -= float64(cost)
					b.Used += cost
					if m.mode == "emergency" || m.mode == "critical" {
						d.emergencyLogBytes += cost
					}
				}
			}
		}
	}
	out := make([]models.ContainerLogEntry, 0, len(entries))
	for i, e := range entries {
		if keep[i] {
			out = append(out, e)
		} else {
			drops[e.ContainerName]++
		}
	}
	if b.Level <= float64(b.Allowance)/16 {
		b.Degraded = true
	}
	d.dirty = true
	return out, drops
}

func (m *Manager) AllowSnapshot(id string, size int64, now time.Time) (store, trim bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.get(id, now)
	b := d.Buckets["telemetry"]
	degraded := b.Degraded || b.Level < float64(size) || m.mode == "emergency" || m.mode == "critical"
	interval := models.TelemetryQuotaDownsampleInterval
	if m.mode == "critical" {
		interval = time.Hour
	}
	if degraded && !d.LastSnapshot.IsZero() && now.Sub(d.LastSnapshot) < interval {
		return false, true
	}
	d.LastSnapshot = now
	b.Used += size
	b.Level = max(0, b.Level-float64(size))
	d.dirty = true
	return true, degraded
}

func (m *Manager) FilterDeviceLogs(id string, entries []models.LogEntry, now time.Time) []models.LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.get(id, now)
	b := d.Buckets["device_logs"]
	out := []models.LogEntry{}
	for _, priority := range []bool{true, false} {
		for _, e := range entries {
			if (e.Priority <= 3) != priority {
				continue
			}
			if !priority && (m.mode == "emergency" || m.mode == "critical") {
				continue
			}
			if !priority && b.Degraded {
				if d.deviceSamples >= 20 {
					continue
				}
				d.deviceSamples++
			}
			cost := int64(len(e.Message) + 200)
			if (m.mode == "emergency" || m.mode == "critical") && d.emergencyDeviceBytes+cost > 16<<10 {
				continue
			}
			if b.Level < float64(cost) {
				continue
			}
			out = append(out, e)
			b.Level -= float64(cost)
			b.Used += cost
			if m.mode == "emergency" || m.mode == "critical" {
				d.emergencyDeviceBytes += cost
			}
		}
	}
	d.dirty = true
	return out
}

func (m *Manager) Status(id string, now time.Time) map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.get(id, now)
	used := map[string]int64{}
	limits := map[string]int64{}
	level := map[string]float64{}
	mode := map[string]string{}
	for class, b := range d.Buckets {
		used[class] = b.Used
		limits[class] = b.Allowance
		level[class] = b.Level
		mode[class] = "normal"
		if b.Degraded {
			mode[class] = "sampled"
			if class == "telemetry" {
				mode[class] = "downsampled"
			}
			if class == "commands" {
				mode[class] = "stub"
			}
		}
	}
	return map[string]interface{}{"day": d.Day, "used": used, "quota": limits, "level": level, "mode": mode, "fairness": map[string]interface{}{"limited": d.Buckets["logs"].Allowance < models.DeviceDailyLogBytes, "logs_allowance_per_day": d.Buckets["logs"].Allowance}}
}

func (m *Manager) Flush(ctx context.Context, now time.Time) error {
	if m.repo == nil {
		return nil
	}
	m.mu.Lock()
	records := []Record{}
	for id, d := range m.devices {
		if d.dirty {
			r := d.Record
			r.Buckets = map[string]*Bucket{}
			for k, b := range d.Buckets {
				copy := *b
				r.Buckets[k] = &copy
			}
			records = append(records, r)
			d.dirty = false
		}
		if now.Sub(d.lastUsed) > 24*time.Hour {
			delete(m.devices, id)
		}
	}
	m.mu.Unlock()
	for index, r := range records {
		if err := m.repo.Save(ctx, r); err != nil {
			m.mu.Lock()
			for _, unsaved := range records[index:] {
				if d := m.devices[unsaved.DeviceID]; d != nil {
					d.dirty = true
				}
			}
			m.mu.Unlock()
			return err
		}
	}
	return nil
}
