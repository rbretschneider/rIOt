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

	// Collect all mount points (all=true) so FUSE-based pools like
	// fuse.mergerfs are included. Filter out virtual/pseudo filesystems
	// and deduplicate by mount point.
	skipFS := map[string]bool{
		"tmpfs": true, "devtmpfs": true, "sysfs": true, "proc": true,
		"devpts": true, "securityfs": true, "cgroup": true, "cgroup2": true,
		"pstore": true, "debugfs": true, "tracefs": true, "configfs": true,
		"fusectl": true, "mqueue": true, "hugetlbfs": true, "binfmt_misc": true,
		"autofs": true, "efivarfs": true, "ramfs": true, "overlay": true,
		"nsfs": true, "fuse.lxcfs": true, "fuse.gvfsd-fuse": true,
	}
	netFS := map[string]bool{
		"nfs": true, "nfs4": true, "cifs": true, "smb": true,
		"sshfs": true, "fuse.sshfs": true,
	}
	seen := make(map[string]bool)
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err == nil {
		for _, p := range partitions {
			if skipFS[p.Fstype] || seen[p.Mountpoint] {
				continue
			}
			if strings.HasPrefix(p.Mountpoint, "/proc") ||
				strings.HasPrefix(p.Mountpoint, "/sys") ||
				strings.HasPrefix(p.Mountpoint, "/dev") ||
				strings.HasPrefix(p.Mountpoint, "/run/lock") ||
				strings.HasPrefix(p.Mountpoint, "/snap/") {
				continue
			}
			usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil || usage.Total == 0 {
				continue
			}
			seen[p.Mountpoint] = true
			info.Filesystems = append(info.Filesystems, models.Filesystem{
				MountPoint:     p.Mountpoint,
				Device:         p.Device,
				FSType:         p.Fstype,
				TotalGB:        float64(usage.Total) / 1024 / 1024 / 1024,
				UsedGB:         float64(usage.Used) / 1024 / 1024 / 1024,
				FreeGB:         float64(usage.Free) / 1024 / 1024 / 1024,
				UsagePercent:   usage.UsedPercent,
				MountOptions:   strings.Join(p.Opts, ","),
				IsNetworkMount: netFS[p.Fstype],
				IsPool:         models.IsPoolFilesystem(p.Fstype, p.Device),
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
