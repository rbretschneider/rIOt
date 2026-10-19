package partition

import (
	"context"
	"sort"
	"time"
)

// EvictOldest shares the janitor's serialization and short lock policy. It
// never includes DEFAULT and always preserves the requested current-day floor.
func (m *Manager) EvictOldest(ctx context.Context, now time.Time, floorDays int, table string, limit int) (int, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	floor := dayFloor(now).AddDate(0, 0, -max(floorDays-1, 0))
	type candidate struct {
		table string
		p     PartitionInfo
	}
	items := []candidate{}
	for _, spec := range m.specs {
		if table != "" && table != spec.Name {
			continue
		}
		ps, err := m.listPartitions(ctx, spec)
		if err != nil {
			return 0, err
		}
		for _, p := range ps {
			if !p.IsDefault && !p.UpperBound.After(floor) && isDroppableName(spec.Name, p.Name) {
				items = append(items, candidate{spec.Name, p})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].p.UpperBound.Before(items[j].p.UpperBound) })
	count := 0
	for _, c := range items {
		if count >= min(limit, 24) {
			break
		}
		if err := m.exec.dropPartition(ctx, c.p.Name); err != nil {
			return count, err
		}
		m.log.Info("partition evicted under storage pressure", "table", c.table, "partition", c.p.Name)
		count++
	}
	return count, nil
}
