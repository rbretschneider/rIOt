package collectors

import (
	"context"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/disk"
)

type DiskCollector struct{}

func (c *DiskCollector) Name() string { return "disk" }

func (c *DiskCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.DiskInfo{}

	// Mounted filesystems
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err == nil {
		for _, p := range partitions {
			usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil || usage.Total == 0 {
				continue
			}

			isNetwork := false
			netFS := []string{"nfs", "nfs4", "cifs", "smb", "sshfs", "fuse.sshfs"}
			for _, nf := range netFS {
				if p.Fstype == nf {
					isNetwork = true
					break
				}
			}

			info.Filesystems = append(info.Filesystems, models.Filesystem{
				MountPoint:     p.Mountpoint,
				Device:         p.Device,
				FSType:         p.Fstype,
				TotalGB:        float64(usage.Total) / 1024 / 1024 / 1024,
				UsedGB:         float64(usage.Used) / 1024 / 1024 / 1024,
				FreeGB:         float64(usage.Free) / 1024 / 1024 / 1024,
				UsagePercent:   usage.UsedPercent,
				MountOptions:   strings.Join(p.Opts, ","),
				IsNetworkMount: isNetwork,
				IsPool:         models.IsPoolFSType(p.Fstype),
			})
		}
	}

	// Block devices (IOCounters gives us device names)
	ioCounters, err := disk.IOCountersWithContext(ctx)
	if err == nil {
		for name := range ioCounters {
			info.BlockDevices = append(info.BlockDevices, models.BlockDevice{
				Name: name,
			})
		}
	}

	return info, nil
}
