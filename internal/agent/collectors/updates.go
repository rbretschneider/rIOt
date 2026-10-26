package collectors

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Default apt reboot-required marker paths (PATCH-GATE FR-017).
const (
	defaultRebootRequiredPath     = "/var/run/reboot-required"
	defaultRebootRequiredPkgsPath = "/var/run/reboot-required.pkgs"
)

type UpdatesCollector struct {
	// holdMgr enforces OS-level reboot-class holds; wired via
	// Registry.SetHoldManager. Nil in tests that don't exercise holds.
	holdMgr *HoldManager

	// Test injection points (ADD Implementation Note #2).
	exitRun                exitRunner // dnf needs-restarting exit-code runner; nil = real exec
	rebootRequiredPath     string     // apt marker file override
	rebootRequiredPkgsPath string     // apt package-list file override

	// PATCH-FLOW-001 seams; the zero values mean production ("/" roots, wall clock).
	kernelRoot string           // root for /boot and /proc kernel comparison
	aptRoot    string           // root for /etc/apt and /var/lib/apt freshness
	listsNow   func() time.Time // clock for the freshness clock-skew guard
}

// exitRunner runs a command and reports its exit code. err is non-nil only
// when the command could not run at all (missing binary, context error).
type exitRunner func(ctx context.Context, name string, args ...string) (int, error)

func defaultExitRunner(ctx context.Context, name string, args ...string) (int, error) {
	err := exec.CommandContext(ctx, name, args...).Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

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
		info.ReleaseUpgrade = defaultReleaseChecker.Check(ctx)
	} else if _, err := exec.LookPath("dnf"); err == nil {
		info.PackageManager = "dnf"
		c.collectDNF(ctx, info)
		info.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusUnavailable}
	} else if _, err := exec.LookPath("pacman"); err == nil {
		info.PackageManager = "pacman"
	} else if _, err := exec.LookPath("apk"); err == nil {
		info.PackageManager = "apk"
	}

	// Platform updaters outside the package manager (EXTRA-UPD-001). They ride
	// the existing updates collector, so no collector.enabled change is needed.
	info.ExtraUpdaters = c.detectExtraUpdaters(ctx)

	return info, nil
}

// extraRootFS is the filesystem view extra-updater detection reads: the real
// root, or a fixture tree when a test sets the root.
func extraRootFS(root string) extraupd.FS {
	if root == "" {
		return extraupd.OSRoot()
	}
	return extraupd.DirFS(root)
}

// detectExtraUpdaters reports every present extra updater, at most eight. It
// reads files only and never starts a process (FR-003). The kernel root seam
// doubles as its root, so one fixture tree serves both.
func (c *UpdatesCollector) detectExtraUpdaters(ctx context.Context) []models.ExtraUpdater {
	return extraupd.DetectAll(ctx, extraRootFS(c.kernelRoot))
}

func (c *UpdatesCollector) collectAPT(ctx context.Context, info *models.UpdateInfo) {
	// Detect unattended-upgrades
	if out, err := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}", "unattended-upgrades").Output(); err == nil {
		if strings.Contains(string(out), "install ok installed") {
			info.UnattendedUpgrades = true
		}
	}

	// Count installed packages; the parsed names also feed hold
	// reconciliation (NFR-004 — no extra package-manager invocation).
	var installedNames []string
	out, err := exec.CommandContext(ctx, "dpkg", "--get-selections").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		info.TotalInstalled = len(lines)
		installedNames = parseDpkgSelections(string(out))
	}
	c.collectAptHolds(info, string(out), err)

	c.reconcileHolds(ctx, info, "apt", installedNames)

	// Check for updates (requires apt update to have been run)
	out, err = exec.CommandContext(ctx, "apt", "list", "--upgradable").Output()
	if err == nil {
		parseAptUpgradable(string(out), info)
	}

	classifyUpdates(info)
	c.applyListsFreshness(info)
	c.detectRebootRequiredAPT(info)
}

// parseAptUpgradable counts and lists the entries of `apt list --upgradable`
// output into info. The raw list is reported even when the package lists are
// stale (PATCH-FLOW-001 FR-032).
func parseAptUpgradable(output string, info *models.UpdateInfo) {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if !strings.Contains(line, "upgradable") && !strings.Contains(line, "/") {
			continue
		}
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

func (c *UpdatesCollector) collectDNF(ctx context.Context, info *models.UpdateInfo) {
	// Installed names (not name-version-release) so the same invocation
	// feeds both the installed count and hold reconciliation (AD-010).
	var installedNames []string
	out, err := exec.CommandContext(ctx, "rpm", "-qa", "--qf", "%{NAME}\n").Output()
	if err == nil {
		installedNames = nonEmptyLines(string(out))
		info.TotalInstalled = len(installedNames)
	}

	c.reconcileHolds(ctx, info, "dnf", installedNames)
	c.collectDnfHolds(info)

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

	classifyUpdates(info)
	c.detectRebootRequiredDNF(ctx, info)
}

// managedHolds returns the holds rIOt placed. With no hold manager there are
// none to subtract.
func (c *UpdatesCollector) managedHolds() ([]string, error) {
	if c.holdMgr == nil {
		return nil, nil
	}
	return c.holdMgr.ManagedHolds()
}

// collectAptHolds reports apt holds that rIOt did not place. selectionsErr is
// the error from `dpkg --get-selections`; when it failed the fact is
// unavailable.
func (c *UpdatesCollector) collectAptHolds(info *models.UpdateInfo, selections string, selectionsErr error) {
	if selectionsErr != nil {
		info.OtherHolds = &models.OtherHolds{Status: models.FactStatusUnavailable}
		return
	}
	managed, err := c.managedHolds()
	info.OtherHolds = aptOtherHolds(selections, managed, err)
}

// collectDnfHolds reports dnf excludes and version locks that rIOt did not place.
func (c *UpdatesCollector) collectDnfHolds(info *models.UpdateInfo) {
	managed, err := c.managedHolds()
	if err != nil {
		info.OtherHolds = &models.OtherHolds{Status: models.FactStatusUnavailable}
		return
	}
	info.OtherHolds = dnfOtherHolds(osFactFS, managed)
}

// reconcileHolds runs the per-cycle hold reconciliation (AD-007) and
// reports hold state in telemetry (FR-016, AD-015). A hold failure never
// prevents the rest of the cycle (NFR-005) — Reconcile only WARNs.
func (c *UpdatesCollector) reconcileHolds(ctx context.Context, info *models.UpdateInfo, pm string, installedNames []string) {
	if c.holdMgr == nil {
		return
	}
	c.holdMgr.Reconcile(ctx, pm, installedNames)
	info.HeldPackages = c.holdMgr.HeldPackages()
	info.HoldEnforcement = c.holdMgr.Status()
}

// classifyUpdates classifies every pending update (FR-001), populates the
// previously dead kernel fields (FR-008), and the aggregate reboot-class
// count (FR-009). Pure — shared by the apt and dnf paths.
func classifyUpdates(info *models.UpdateInfo) {
	for i := range info.Updates {
		info.Updates[i].Class = ClassifyPackage(info.Updates[i].Name)
		if info.Updates[i].Class != "" {
			info.PendingRebootClassCount++
		}
	}
	_, version := SelectPrimaryKernel(info.Updates)
	info.PendingKernelUpdate = version != ""
	info.PendingKernelVersion = version
}

// detectRebootRequiredAPT reports whether a reboot is required now: the apt
// reboot-required marker exists (PATCH-GATE FR-017) or the newest installed
// kernel is newer than the running one (PATCH-FLOW-001 FR-002). Stat/read
// errors degrade to false with debug-only logging.
func (c *UpdatesCollector) detectRebootRequiredAPT(info *models.UpdateInfo) {
	if c.detectRebootMarker(info) {
		info.RebootRequired = true
	}
	if newer, installed, running, ok := kernelNewerThanRunning(c.kernelRoot); ok && newer {
		info.RebootRequired = true
		info.RebootRequiredReasons = append(info.RebootRequiredReasons, kernelReason(installed, running))
	}
	// DietPi's own test: a running kernel without its modules directory needs a
	// reboot (EXTRA-UPD-001 FR-016). It is false on every other host.
	if missing, release := extraupd.ModulesMissing(extraRootFS(c.kernelRoot)); missing {
		info.RebootRequired = true
		info.RebootRequiredReasons = append(info.RebootRequiredReasons, extraupd.ModulesMissingReason(release))
	}
}

// kernelReason is the single reason line added when the kernel source fires.
// Both inputs already passed the release charset filter.
func kernelReason(installed, running string) string {
	reason := "kernel " + installed + " installed; running " + running
	if len(reason) > maxKernelReasonLen {
		reason = reason[:maxKernelReasonLen]
	}
	return reason
}

// detectRebootMarker reports whether the apt marker file exists and, when it
// does, appends the deduplicated package reasons from the .pkgs file.
func (c *UpdatesCollector) detectRebootMarker(info *models.UpdateInfo) bool {
	markerPath := c.rebootRequiredPath
	if markerPath == "" {
		markerPath = defaultRebootRequiredPath
	}
	pkgsPath := c.rebootRequiredPkgsPath
	if pkgsPath == "" {
		pkgsPath = defaultRebootRequiredPkgsPath
	}

	if _, err := os.Stat(markerPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("updates: reboot-required stat failed", "path", markerPath, "error", err)
		}
		return false
	}

	data, err := os.ReadFile(pkgsPath)
	if err != nil {
		return true // marker alone is enough; reasons are optional
	}
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line = strings.TrimSpace(line); line != "" && !seen[line] {
			seen[line] = true
			info.RebootRequiredReasons = append(info.RebootRequiredReasons, line)
		}
	}
	return true
}

// detectRebootRequiredDNF maps `dnf needs-restarting -r` exit codes
// (FR-017): exit 1 = reboot required, exit 0 = not required, anything else
// (missing plugin, error) degrades to false with debug-only logging (AC-018
// no-spam requirement). Runs unprivileged — no sudo rule exists for it.
func (c *UpdatesCollector) detectRebootRequiredDNF(ctx context.Context, info *models.UpdateInfo) {
	run := c.exitRun
	if run == nil {
		run = defaultExitRunner
	}
	code, err := run(ctx, "dnf", "needs-restarting", "-r")
	switch {
	case err != nil:
		slog.Debug("updates: dnf needs-restarting unavailable", "error", err)
	case code == 1:
		info.RebootRequired = true
	case code != 0:
		slog.Debug("updates: dnf needs-restarting unexpected exit", "code", code)
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
