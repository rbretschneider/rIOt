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
	// Detect unattended-upgrades
	if out, err := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}", "unattended-upgrades").Output(); err == nil {
		if strings.Contains(string(out), "install ok installed") {
			info.UnattendedUpgrades = true
		}
	}

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

	// Get all pending updates with package details.
	// dnf check-update exits 100 when updates are available, 0 when up to date.
	out, err = exec.CommandContext(ctx, "dnf", "check-update", "-q").Output()
	if err != nil || len(out) > 0 {
		info.Updates = parseDNFCheckUpdate(string(out))
		info.PendingUpdates = len(info.Updates)
	}

	// Check for security updates specifically.
	secOut, secErr := exec.CommandContext(ctx, "dnf", "check-update", "-q", "--security").Output()
	if secErr != nil || len(secOut) > 0 {
		secPkgs := parseDNFCheckUpdate(string(secOut))
		info.PendingSecurityCount = len(secPkgs)

		// Mark security packages in the main update list
		secSet := make(map[string]bool, len(secPkgs))
		for _, p := range secPkgs {
			secSet[p.Name] = true
		}
		for i := range info.Updates {
			if secSet[info.Updates[i].Name] {
				info.Updates[i].IsSecurity = true
			}
		}
	}
}

// parseDNFCheckUpdate parses output from `dnf check-update -q`.
// Each update line has the format: package-name.arch   version   repository
func parseDNFCheckUpdate(output string) []models.PendingUpdate {
	var updates []models.PendingUpdate
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// First field is "name.arch" — strip the arch suffix
		nameArch := fields[0]
		name := nameArch
		if idx := strings.LastIndex(nameArch, "."); idx > 0 {
			name = nameArch[:idx]
		}
		updates = append(updates, models.PendingUpdate{
			Name:   name,
			NewVer: fields[1],
		})
	}
	return updates
}
