package partition

import (
	"context"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// StartupEnsure fills the entire retained log window before requests begin.
func (m *Manager) StartupEnsure(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	now := time.Now().UTC()
	for _, spec := range m.specs {
		previous := m.behind
		if spec.Name == "container_logs" {
			m.behind = min(90, int(spec.Retention/(24*time.Hour)))
		}
		err := m.ensureTable(ctx, spec, now)
		m.behind = previous
		if err != nil {
			return err
		}
	}
	return nil
}

// GuardLogRows singleflights missing-day recovery across all devices. When a
// partition cannot be created, data falls back to an existing current partition
// and keeps its original timestamp. The DEFAULT partition is never a fallback.
func (m *Manager) GuardLogRows(ctx context.Context, entries []models.ContainerLogEntry, now time.Time) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.guardDays == nil {
		m.guardDays = map[string]time.Time{}
	}
	for key, at := range m.guardDays {
		if now.Sub(at) > 5*time.Minute {
			delete(m.guardDays, key)
		}
	}
	cutoff, err := m.exec.readCutoff(ctx, "container_logs")
	if err != nil {
		return err
	}
	ensure := func(ts time.Time) error {
		if ts.Before(cutoff) {
			return nil
		}
		day := dayFloor(ts)
		key := PartitionName("container_logs", day)
		if at, ok := m.guardDays[key]; ok && now.Sub(at) < 5*time.Minute {
			return nil
		}
		if err := m.exec.createPartition(ctx, "container_logs", day, day.AddDate(0, 0, 1)); err != nil {
			return err
		}
		m.guardDays[key] = now
		return nil
	}
	for i := range entries {
		if err := ensure(entries[i].Timestamp); err == nil {
			continue
		}
		placed := false
		for _, fallback := range []time.Time{now, dayFloor(now).Add(-time.Microsecond)} {
			if err := ensure(fallback); err != nil {
				continue
			}
			if entries[i].AgentTS == nil {
				original := entries[i].Timestamp
				entries[i].AgentTS = &original
			}
			entries[i].Timestamp = fallback
			placed = true
			break
		}
		if !placed {
			return fmt.Errorf("no writable retained container log partition")
		}
	}
	return nil
}
