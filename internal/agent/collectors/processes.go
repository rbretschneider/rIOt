package collectors

import (
	"context"
	"sort"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/process"
)

type ProcessesCollector struct{}

func (c *ProcessesCollector) Name() string { return "processes" }

func (c *ProcessesCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.ProcessInfo{}

	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return info, err
	}

	var entries []models.ProcessEntry
	for _, p := range procs {
		name, _ := p.NameWithContext(ctx)
		cpuPct, _ := p.CPUPercentWithContext(ctx)
		memPct, _ := p.MemoryPercentWithContext(ctx)
		memInfo, _ := p.MemoryInfoWithContext(ctx)
		user, _ := p.UsernameWithContext(ctx)
		cmdline, _ := p.CmdlineWithContext(ctx)

		var memMB float64
		if memInfo != nil {
			memMB = float64(memInfo.RSS) / 1024 / 1024
		}

		entries = append(entries, models.ProcessEntry{
			PID:     p.Pid,
			Name:    name,
			CPU:     cpuPct,
			MemPct:  float64(memPct),
			MemMB:   memMB,
			User:    user,
			Command: cmdline,
		})
	}

	// Top 15 by CPU
	sort.Slice(entries, func(i, j int) bool { return entries[i].CPU > entries[j].CPU })
	if len(entries) > 15 {
		info.TopByCPU = make([]models.ProcessEntry, 15)
		copy(info.TopByCPU, entries[:15])
	} else {
		info.TopByCPU = entries
	}

	// Top 15 by Memory
	sort.Slice(entries, func(i, j int) bool { return entries[i].MemMB > entries[j].MemMB })
	if len(entries) > 15 {
		info.TopByMemory = make([]models.ProcessEntry, 15)
		copy(info.TopByMemory, entries[:15])
	} else {
		info.TopByMemory = entries
	}

	return info, nil
}
