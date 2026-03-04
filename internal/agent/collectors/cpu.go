package collectors

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
)

type CPUCollector struct{}

func (c *CPUCollector) Name() string { return "cpu" }

func (c *CPUCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.CPUInfo{}

	// Overall usage
	if percents, err := cpu.PercentWithContext(ctx, time.Second, false); err == nil && len(percents) > 0 {
		info.UsagePercent = percents[0]
	}

	// Per-core usage
	if percents, err := cpu.PercentWithContext(ctx, 0, true); err == nil {
		info.PerCore = percents
	}

	// Load average
	if l, err := load.AvgWithContext(ctx); err == nil {
		info.LoadAvg1m = l.Load1
		info.LoadAvg5m = l.Load5
		info.LoadAvg15m = l.Load15
	}

	// Temperature
	if temps, err := host.SensorsTemperaturesWithContext(ctx); err == nil {
		for _, t := range temps {
			if t.Temperature > 0 {
				temp := t.Temperature
				info.Temperature = &temp
				break
			}
		}
	}

	// Frequency
	if cpuInfo, err := cpu.InfoWithContext(ctx); err == nil && len(cpuInfo) > 0 {
		if cpuInfo[0].Mhz > 0 {
			mhz := cpuInfo[0].Mhz
			info.FreqCurrent = &mhz
		}
	}

	return info, nil
}
