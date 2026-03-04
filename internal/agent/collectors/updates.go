package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type UpdatesCollector struct{}

func (c *UpdatesCollector) Name() string { return "updates" }

func (c *UpdatesCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.UpdateInfo{}

	if runtime.GOOS != "linux" {
		return info, nil
	}

	// Detect package manager
	if _, err := exec.LookPath("apt"); err == nil {
		info.PackageManager = "apt"
		c.collectAPT(ctx, info)
	} else if _, err := exec.LookPath("dnf"); err == nil {
		info.PackageManager = "dnf"
		c.collectDNF(ctx, info)
	} else if _, err := exec.LookPath("pacman"); err == nil {
		info.PackageManager = "pacman"
	} else if _, err := exec.LookPath("apk"); err == nil {
		info.PackageManager = "apk"
	}

	return info, nil
}

func (c *UpdatesCollector) collectAPT(ctx context.Context, info *models.UpdateInfo) {
	// Count installed packages
	out, err := exec.CommandContext(ctx, "dpkg", "--get-selections").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		info.TotalInstalled = len(lines)
	}

	// Check for updates (requires apt update to have been run)
	out, err = exec.CommandContext(ctx, "apt", "list", "--upgradable").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			if strings.Contains(line, "upgradable") || strings.Contains(line, "/") {
				if strings.Contains(line, "Listing") {
					continue
				}
				info.PendingUpdates++
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					name := strings.Split(parts[0], "/")[0]
					info.Updates = append(info.Updates, models.PendingUpdate{
						Name:   name,
						NewVer: parts[1],
					})
				}
				if strings.Contains(line, "-security") {
					info.PendingSecurityCount++
				}
			}
		}
	}
}

func (c *UpdatesCollector) collectDNF(ctx context.Context, info *models.UpdateInfo) {
	out, err := exec.CommandContext(ctx, "rpm", "-qa").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		info.TotalInstalled = len(lines)
	}

	out, err = exec.CommandContext(ctx, "dnf", "check-update", "-q").Output()
	if err == nil || err.(*exec.ExitError) != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			info.PendingUpdates++
		}
	}
}
