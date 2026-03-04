package collectors

import (
	"context"
	"os"
	"runtime"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

type SystemCollector struct{}

func (c *SystemCollector) Name() string { return "system" }

func (c *SystemCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.SystemInfo{
		Arch: runtime.GOARCH,
	}

	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}

	if cpuInfo, err := cpu.InfoWithContext(ctx); err == nil && len(cpuInfo) > 0 {
		info.CPUModel = cpuInfo[0].ModelName
		info.CPUCores = int(cpuInfo[0].Cores)
	}

	if counts, err := cpu.CountsWithContext(ctx, true); err == nil {
		info.CPUThreads = counts
	}
	if counts, err := cpu.CountsWithContext(ctx, false); err == nil {
		info.CPUCores = counts
	}

	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		info.TotalRAMMB = int64(v.Total / 1024 / 1024)
	}

	if hostInfo, err := host.InfoWithContext(ctx); err == nil {
		info.Virtualization = hostInfo.VirtualizationSystem
		if info.Hostname == "" {
			info.Hostname = hostInfo.Hostname
		}
	}

	return info, nil
}

// GetArch returns the runtime architecture string.
func (c *SystemCollector) GetArch() string {
	return runtime.GOARCH
}
