package collectors

import (
	"context"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/mem"
)

type MemoryCollector struct{}

func (c *MemoryCollector) Name() string { return "memory" }

func (c *MemoryCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.MemoryInfo{}

	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		info.TotalMB = int64(v.Total / 1024 / 1024)
		info.UsedMB = int64(v.Used / 1024 / 1024)
		info.FreeMB = int64(v.Free / 1024 / 1024)
		info.CachedMB = int64(v.Cached / 1024 / 1024)
		info.BuffersMB = int64(v.Buffers / 1024 / 1024)
		info.UsagePercent = v.UsedPercent
	}

	if s, err := mem.SwapMemoryWithContext(ctx); err == nil {
		info.SwapTotalMB = int64(s.Total / 1024 / 1024)
		info.SwapUsedMB = int64(s.Used / 1024 / 1024)
	}

	return info, nil
}
