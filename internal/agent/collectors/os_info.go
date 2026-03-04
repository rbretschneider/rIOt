package collectors

import (
	"context"
	"runtime"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/host"
)

type OSInfoCollector struct{}

func (c *OSInfoCollector) Name() string { return "os_info" }

func (c *OSInfoCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.OSInfo{
		KernelArch: runtime.GOARCH,
	}

	if hostInfo, err := host.InfoWithContext(ctx); err == nil {
		info.Name = hostInfo.Platform + " " + hostInfo.PlatformVersion
		info.ID = hostInfo.Platform
		info.Version = hostInfo.PlatformVersion
		info.Kernel = hostInfo.KernelVersion
		info.KernelArch = hostInfo.KernelArch
		info.Uptime = hostInfo.Uptime
		info.BootTime = int64(hostInfo.BootTime)
	}

	info.Timezone, _ = time.Now().Zone()

	return info, nil
}
