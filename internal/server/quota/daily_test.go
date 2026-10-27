package quota

import (
	"github.com/DesyncTheThird/rIOt/internal/models"
	"testing"
	"time"
)

// fixedNow is mid-minute and mid-day: stdout sampling resets when the minute
// changes and quotas when the UTC day changes, so time.Now() made these tests
// fail whenever they started in the last second of a minute.
var fixedNow = time.Date(2026, 10, 19, 12, 0, 30, 0, time.UTC)

func TestDUX_SEC047_PriorityLaneFairShare(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m := New(nil)
	d := m.get("host", now)
	d.Buckets["logs"].Level = 820
	d.Buckets["logs"].Degraded = true
	entries := []models.ContainerLogEntry{}
	for i := 0; i < 20; i++ {
		entries = append(entries, models.ContainerLogEntry{ContainerName: "noisy", Stream: "stderr", Line: "error"})
	}
	entries = append(entries, models.ContainerLogEntry{ContainerName: "quiet", Stream: "stderr", Line: "error"})
	kept, _ := m.FilterLogs("host", entries, now)
	counts := map[string]int{}
	for _, e := range kept {
		counts[e.ContainerName]++
	}
	if counts["quiet"] != 1 || counts["noisy"] != 3 {
		t.Fatalf("unfair priority allocation: %+v", counts)
	}
}
func TestDUX_AC165_QuotaStdoutSample(t *testing.T) {
	now := fixedNow
	m := New(nil)
	d := m.get("host", now)
	d.Buckets["logs"].Level = 10000
	d.Buckets["logs"].Degraded = true
	entries := make([]models.ContainerLogEntry, 10)
	for i := range entries {
		entries[i] = models.ContainerLogEntry{ContainerName: "web", Stream: "stdout", Line: "hello"}
	}
	out, drops := m.FilterLogs("host", entries, now)
	if len(out) != 5 || drops["web"] != 5 {
		t.Fatalf("stdout sample: %d %+v", len(out), drops)
	}
	out, _ = m.FilterLogs("host", entries, now.Add(time.Second))
	if len(out) != 0 {
		t.Fatal("sampling reset on each request")
	}
}
func TestDUX_AC164_QuotaRefillsWithoutMidnightReset(t *testing.T) {
	now := time.Date(2026, 10, 19, 23, 59, 59, 0, time.UTC)
	m := New(nil)
	d := m.get("host", now)
	d.Buckets["logs"].Level = 0
	d = m.get("host", now.Add(2*time.Second))
	want := float64(models.DeviceDailyLogBytes) * 2 / 86400
	if d.Buckets["logs"].Level != want {
		t.Fatalf("midnight reset allowance: got %f want %f", d.Buckets["logs"].Level, want)
	}
}
func TestDUX_AC166_DownsampleNeverStoresNothing(t *testing.T) {
	now := fixedNow
	m := New(nil)
	d := m.get("host", now)
	d.Buckets["telemetry"].Level = 0
	if store, trim := m.AllowSnapshot("host", 1000, now); !store || !trim {
		t.Fatal("empty quota dropped first snapshot")
	}
	if store, _ := m.AllowSnapshot("host", 1000, now.Add(time.Minute)); store {
		t.Fatal("too many snapshots")
	}
	if store, _ := m.AllowSnapshot("host", 1000, now.Add(15*time.Minute)); !store {
		t.Fatal("downsample failed to retain current snapshot")
	}
}
func TestDUX_AC168_CriticalStorageScope(t *testing.T) {
	now := fixedNow
	m := New(nil)
	m.SetMode("critical")
	entries := []models.ContainerLogEntry{{ContainerName: "web", Stream: "stdout", Line: "hello"}, {ContainerName: "web", Stream: "stderr", Line: "hello"}}
	out, _ := m.FilterLogs("host", entries, now)
	if len(out) != 1 || out[0].Stream != "stderr" {
		t.Fatal("critical mode stored non-priority output")
	}
	m.AllowSnapshot("host", 10, now)
	if store, _ := m.AllowSnapshot("host", 10, now.Add(30*time.Minute)); store {
		t.Fatal("critical mode snapshot cadence")
	}
}

func TestDUX_SEC043_DownsamplingKeepsLiveFreshness(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m := New(nil)
	if !m.AcceptTelemetry("host", now.Add(10*time.Minute), now.Add(10*time.Minute)) {
		t.Fatal("fresh snapshot refused")
	}
	if m.AcceptTelemetry("host", now.Add(6*time.Minute), now.Add(11*time.Minute)) {
		t.Fatal("buffered old snapshot rolled back live state")
	}
}
