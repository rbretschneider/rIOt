package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
)

// maxOutputBytes is the maximum size of captured command output (256KB).
const maxOutputBytes = 256 * 1024

// truncateHeadTailBytes is the size kept from the head and tail when truncating (64KB).
const truncateHeadTailBytes = 64 * 1024

// commandExecResult holds the outcome of an executed command including timing and output.
type commandExecResult struct {
	// Detached keeps the durable in-flight marker and host reservation while
	// an independent root service owns the operation; no terminal report yet.
	Detached      bool
	TargetResults []models.DockerTargetResult
	Status        string
	Message       string
	ExitCode      *int
	DurationMs    *int64
	Output        string

	// PATCH-GATE (AD-008): internal-only fields for the post-result reboot
	// decision. The wire-format models.CommandResult is unchanged — the
	// reboot-class outcome is carried in Message (FR-026).
	RebootClassApplied []string
	RebootPending      bool
	RebootCommandID    string

	// CMD-LIFECYCLE-001: the failing (or last) step, the closed-enum reason and
	// the error text a failed result carries. Status stays success/error on the
	// wire (FR-081).
	Step   string
	Reason string
	Stderr string

	// postResult runs after the result is queued in the outbox (the
	// reboot-after-patch decision).
	postResult func()
}

// runCommand executes an external command, honoring the test runner when set.
func (a *Agent) runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if a.runner != nil {
		return a.runner(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// upgradeCancelGrace is how long a package-manager command is given to wind
// down after the context deadline fires before it is force-killed.
const upgradeCancelGrace = 90 * time.Second

// runUpgradeCommand runs a long-running package-manager command with graceful
// cancellation. Go's default for a context-bound command is an immediate
// SIGKILL on deadline, which — mid-`dpkg` configure — abandons the package
// database half-written and leaves apt refusing every future run. Instead we
// send SIGTERM (sudo forwards it to apt/dpkg, which then finishes or rolls
// back the in-flight transaction) and only escalate to SIGKILL after a grace
// period. Honors the injected test runner exactly like runCommand.
func (a *Agent) runUpgradeCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if a.runner != nil {
		return a.runner(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = upgradeCancelGrace
	return cmd.CombinedOutput()
}

// executeAction runs one command's action and returns its outcome. The
// lifecycle wrapper (submitCommand) has already deduplicated, marked and
// acknowledged the command; the result goes to the outbox from there.
func (a *Agent) executeAction(cr *cmdRun) commandExecResult {
	payload, ctx := cr.payload, cr.ctx
	if hostKeyFor(payload.Action) != nil {
		if blocker := a.maintenanceBlocker(ctx); blocker != "" && !a.canRequestMaintenanceRecovery(ctx, payload.Action) {
			return commandExecResult{Status: models.ResultStatusError, Step: models.StepPrecheck, Reason: models.ReasonConflict, Message: blocker + "; refusing conflicting host action"}
		}
	}

	// Host-scoped actions share one device-level lock (FR-054). agent_update and
	// agent_uninstall restart or remove the agent and so must never run while a
	// Docker update holds a lock: the restart would cut the recreate in half
	// (SEC C-4c).
	if keys := hostKeyFor(payload.Action); keys != nil {
		if key, holder, ok := cr.lock(keys...); !ok {
			if (payload.Action == models.ActionAgentUpdate || payload.Action == models.ActionAgentUninstall) && strings.HasPrefix(key, models.TargetKeyDockerPrefix) {
				return commandExecResult{Status: models.ResultStatusError, Step: models.StepPrecheck, Reason: models.ReasonConflict, Message: fmt.Sprintf("docker update in progress for %s (command %s); refusing agent restart", lockLabel(key), holder.CmdID)}
			}
			return conflictResult(key, holder)
		}
		if payload.Action == models.ActionAgentUpdate || payload.Action == models.ActionAgentUninstall {
			if key, holder, held := a.locks.HeldWithPrefix(models.TargetKeyDockerPrefix); held {
				return commandExecResult{
					Status: models.ResultStatusError,
					Step:   models.StepPrecheck,
					Reason: models.ReasonConflict,
					Message: fmt.Sprintf("docker update in progress for %s (command %s); refusing agent restart",
						lockLabel(key), holder.CmdID),
				}
			}
		}
	}

	var status, message string

	switch payload.Action {
	case "docker_stack_pull", "docker_stack_redeploy", "docker_stack_restart", "docker_stack_down", "docker_stack_up":
		return a.dockerStackAction(cr)
	case "docker_inventory_refresh":
		if a.registry != nil {
			for _, c := range a.registry.Collectors() {
				if inventory, ok := c.(*collectors.DockerInventoryCollector); ok {
					if err := inventory.ForceRefresh(cr.ctx); err != nil {
						return precheckFailure(err.Error())
					}
					a.triggerTelemetry()
					return commandExecResult{Status: "success", Message: "inventory refresh requested"}
				}
			}
		}
		return precheckFailure("docker_inventory collector is not enabled")
	case "docker_remove_items":
		return a.dockerRemoveItems(cr)
	case "docker_stop":
		return a.dockerCommand(cr, "stop")
	case "docker_restart":
		return a.dockerCommand(cr, "restart")
	case "docker_start":
		return a.dockerCommand(cr, "start")
	case "docker_update":
		return a.dockerUpdate(cr)
	case "docker_bulk_update":
		return a.dockerBulkUpdate(cr)
	case "docker_check_updates":
		if _, hasIDs := payload.Params["container_ids"]; hasIDs || payload.Params["compose_project"] != nil {
			if err := a.clearScopedFreshness(ctx, payload.Params); err != nil {
				return precheckFailure(err.Error())
			}
		} else {
			a.clearFreshnessCache()
		}
		a.triggerTelemetry()
		status, message = "success", "image freshness cache cleared, telemetry push triggered"
	case "reboot":
		status, message = a.handleReboot(payload)
	case "shutdown":
		status, message = a.handleShutdown(payload)
	case "os_update":
		r := a.handleOSUpdateWithOutput(ctx, payload)
		r.RebootCommandID = payload.CommandID
		r.postResult = func() { a.maybeRebootAfterPatch(r) }
		return r
	case "agent_update":
		status, message = a.handleTriggerUpdate(context.WithValue(ctx, updateCommandIDKey{}, payload.CommandID))
		if status == "pending" {
			return commandExecResult{Detached: true, Message: message}
		}
	case "agent_uninstall":
		status, message = a.handleUninstall()
	case "fetch_logs":
		status, message = a.handleFetchLogs(ctx, payload)
	case "enable_auto_updates":
		status, message = a.handleEnableAutoUpdates(ctx)
	case "run_device_probe":
		status, message = a.handleRunDeviceProbe(ctx, payload)
	default:
		status = "error"
		message = fmt.Sprintf("unknown action: %s", payload.Action)
	}

	return commandExecResult{Status: status, Message: message}
}

// maybeRebootAfterPatch acts on an os_update result that applied
// reboot-class packages (FR-024, FR-025): reboot when — and only when —
// commands.allow_reboot permits it (NFR-002); otherwise trigger an
// immediate telemetry push so the reboot-required state reaches the server
// and fires the FR-019 event.
func (a *Agent) maybeRebootAfterPatch(r commandExecResult) {
	if len(r.RebootClassApplied) == 0 {
		return
	}
	if a.config.Commands.AllowReboot {
		if a.locks == nil || a.maintenanceBusy(a.commandContext()) {
			a.triggerTelemetry()
			return
		}
		reservation := "patch-reboot:" + r.RebootCommandID
		if _, _, ok := a.locks.TryLockAll([]string{models.TargetKeyHost}, reservation, models.ActionReboot); !ok {
			a.triggerTelemetry()
			return
		}
		if !a.claimPatchReboot(r.RebootCommandID) {
			a.locks.Unlock([]string{models.TargetKeyHost}, reservation)
			a.triggerTelemetry()
			return
		}
		slog.Info("os_update: reboot-class packages applied, rebooting host",
			"packages", r.RebootClassApplied)
		delay := a.rebootDelay
		if delay == 0 {
			delay = 2 * time.Second
		}
		go func() {
			defer a.locks.Unlock([]string{models.TargetKeyHost}, reservation)
			if a.pause != nil {
				if !a.pause(a.commandContext(), delay) {
					return
				}
			} else {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-a.commandContext().Done():
					return
				}
			}
			if a.shuttingDown.Load() || a.maintenanceBusy(a.commandContext()) {
				a.triggerTelemetry()
				return
			}
			if err := a.execReboot(); err != nil {
				slog.Warn("os_update: post-patch reboot failed", "error", err)
			}
		}()
		return
	}
	slog.Info("os_update: reboot-class packages applied but reboot not permitted; reporting reboot-required",
		"packages", r.RebootClassApplied)
	a.triggerTelemetry()
}

// dockerCommand runs a docker stop/start/restart on the specified container.
// It takes no target lock but is refused while an update holds the container or
// its compose project, so it cannot cut a recreate in half (AD-012).
func (a *Agent) dockerCommand(cr *cmdRun, action string) commandExecResult {
	containerID, _ := cr.payload.Params["container_id"].(string)
	if containerID == "" {
		containerID, _ = cr.payload.Params["container"].(string)
	}
	if containerID == "" {
		return commandExecResult{Status: "error", Message: "container_id is required"}
	}

	api, err := a.newDocker()
	if err != nil {
		return commandExecResult{Status: "error", Message: fmt.Sprintf("docker client: %s", err)}
	}
	defer api.Close()

	if h, held := a.locks.Holder(models.TargetKeyHost); held && h.CmdID != cr.id {
		return conflictResult(models.TargetKeyHost, h)
	}
	ctx := cr.ctx
	if insp, ierr := api.ContainerInspect(ctx, containerID); ierr == nil {
		keys := []string{models.TargetKeyContainer(strings.TrimPrefix(insp.Name, "/"))}
		if insp.Config != nil {
			if wd := insp.Config.Labels[models.LabelComposeWorkingDir]; wd != "" {
				keys = append(keys, models.TargetKeyCompose(wd))
			}
		}
		if key, holder, ok := cr.lock(keys...); !ok {
			return conflictResult(key, holder)
		}
	}

	timeout := 30 // seconds

	switch action {
	case "stop":
		err = api.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
	case "restart":
		err = api.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &timeout})
	case "start":
		err = api.ContainerStart(ctx, containerID, container.StartOptions{})
	}

	if err != nil {
		return commandExecResult{Status: "error", Message: fmt.Sprintf("docker %s: %s", action, err)}
	}
	return commandExecResult{Status: "success", Message: fmt.Sprintf("container %s: %s ok", shortID(containerID), action)}
}

// shortID is the first 12 characters of a container ID.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// handleReboot triggers a system reboot if allowed by config.
func (a *Agent) handleReboot(payload models.CommandPayload) (string, string) {
	if !a.config.Commands.AllowReboot {
		return "error", "reboot not allowed by agent config (set commands.allow_reboot: true)"
	}

	if err := a.execReboot(); err != nil {
		return "error", fmt.Sprintf("reboot: %s", err)
	}
	return "success", "reboot initiated"
}

// execReboot starts the OS reboot command. Both call sites (handleReboot
// and maybeRebootAfterPatch) gate on a.config.Commands.AllowReboot before
// calling — no other path may reach this (NFR-002, ADD Note #6).
func (a *Agent) execReboot() error {
	if a.runner != nil {
		_, err := a.runner(context.Background(), "sudo", "systemctl", "reboot")
		return err
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("shutdown", "/r", "/t", "5")
	} else {
		cmd = exec.Command("sudo", "systemctl", "reboot")
	}
	return cmd.Start()
}

// handleShutdown triggers a system shutdown if allowed by config.
func (a *Agent) handleShutdown(payload models.CommandPayload) (string, string) {
	if !a.config.Commands.AllowShutdown {
		return "error", "shutdown not allowed by agent config (set commands.allow_shutdown: true)"
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("shutdown", "/s", "/t", "5")
	} else {
		cmd = exec.Command("sudo", "systemctl", "poweroff")
	}

	if err := cmd.Start(); err != nil {
		return "error", fmt.Sprintf("shutdown: %s", err)
	}
	return "success", "shutdown initiated"
}

// handleOSUpdateWithOutput runs package manager updates and captures full output.
func (a *Agent) handleOSUpdateWithOutput(ctx context.Context, payload models.CommandPayload) commandExecResult {
	if !a.config.Commands.AllowPatching {
		return commandExecResult{Status: "error", Message: "patching not allowed by agent config (set commands.allow_patching: true)"}
	}
	if runtime.GOOS != "linux" {
		return commandExecResult{Status: "error", Message: "os_update is only supported on Linux"}
	}

	mode, modeOK := payload.Params["mode"].(string)
	if value, present := payload.Params["mode"]; present && (!modeOK || value == nil) {
		return precheckFailure("OS update mode must be a string")
	}
	if mode == "" {
		mode = "full"
	}
	if mode != "full" && mode != "security" && mode != "repair" {
		return precheckFailure("OS update mode must be full, security or repair")
	}
	// Absent ⇒ false (FR-020 validation rule). Only in-window automated
	// dispatches ever carry this param (server strips manual dispatches).
	includeRC, _ := payload.Params["include_reboot_class"].(bool)

	// Use a long timeout independent of the WS read loop
	updateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Detect package manager
	aptPath, aptErr := exec.LookPath("apt-get")
	dnfPath, dnfErr := exec.LookPath("dnf")

	var plan osUpdatePlan
	plan.Mode = mode
	plan.IncludeRebootClass = includeRC

	switch {
	case aptErr == nil:
		_ = aptPath
		return a.runDetachedMaintenance(a.commandContext(), payload, mode, includeRC && a.holdMgr != nil && a.holdMgr.Enabled)
	case dnfErr == nil:
		if mode == "repair" {
			return precheckFailure("bounded repair is supported only on apt/systemd hosts")
		}
		plan.PM = "dnf"
		plan.RefreshArgs = []string{"sudo", dnfPath, "makecache"}
		if mode == "security" {
			plan.UpgradeArgs = []string{"sudo", dnfPath, "-y", "--security", "update"}
		} else {
			plan.UpgradeArgs = []string{"sudo", dnfPath, "-y", "update"}
		}
	default:
		return commandExecResult{Status: "error", Message: "no supported package manager found (apt-get or dnf)"}
	}

	return a.runOSUpdate(updateCtx, payload.CommandID, plan)
}

// osUpdatePlan describes one os_update run: the resolved package-manager
// commands plus the reboot-class gating input (AD-008).
type osUpdatePlan struct {
	Mode               string
	IncludeRebootClass bool
	PM                 string // "apt" | "dnf"
	RefreshArgs        []string
	UpgradeArgs        []string
	// RepairArgs, when non-empty, is run between refresh and upgrade to
	// clear a dpkg transaction left half-finished by an earlier interruption
	// (`dpkg --configure -a`). Empty for package managers that have no such
	// state (dnf). A healthy system makes it a fast no-op.
	RepairArgs []string
}

// runOSUpdate executes the refresh + upgrade with hold release/re-apply
// scoped to exactly this run. Split from handleOSUpdateWithOutput so the
// hold and reboot-decision mechanics are testable on any platform.
func (a *Agent) runOSUpdate(updateCtx context.Context, commandID string, plan osUpdatePlan) commandExecResult {
	mode := plan.Mode
	includeRC := plan.IncludeRebootClass
	refreshArgs, upgradeArgs := plan.RefreshArgs, plan.UpgradeArgs

	// Release rIOt-managed holds for exactly this run (FR-015, AD-008).
	// The deferred re-apply covers success, refresh failure, upgrade
	// failure, partial apply, and panic (NFR-001) and uses its own
	// background context — updateCtx may be dead on those paths.
	var released []string
	var before map[string]string
	if includeRC && a.holdMgr != nil && a.holdMgr.Enabled {
		released = a.holdMgr.ReleaseForRun(updateCtx, commandID)
		if len(released) > 0 {
			defer a.holdMgr.ReapplyAfterRun(commandID)
			before = a.installedVersions(updateCtx, plan.PM, released)
		}
	}
	holdsSkipped := includeRC && a.holdMgr != nil && a.holdMgr.Enabled &&
		len(released) == 0 && len(a.holdMgr.HeldPackages()) > 0

	slog.Info("os_update: refreshing package index", "mode", mode)
	refreshOut, err := a.runUpgradeCommand(updateCtx, refreshArgs[0], refreshArgs[1:]...)
	if err != nil {
		ec := exitCodeFromError(err)
		return commandExecResult{
			Status:   "error",
			Message:  fmt.Sprintf("package refresh failed (exit %d): %s", ec, lastMeaningfulLines(string(refreshOut), 10)),
			ExitCode: &ec,
			Output:   string(refreshOut),
		}
	}

	// Repair an interrupted dpkg state before upgrading (no-op when clean).
	// A failure here is logged but not fatal: the upgrade below will surface
	// the authoritative error, and we never want a repair hiccup to mask an
	// otherwise-working upgrade.
	var repairOut []byte
	if len(plan.RepairArgs) > 0 {
		slog.Info("os_update: repairing any interrupted dpkg state", "mode", mode)
		var rerr error
		repairOut, rerr = a.runUpgradeCommand(updateCtx, plan.RepairArgs[0], plan.RepairArgs[1:]...)
		if rerr != nil {
			slog.Warn("os_update: dpkg repair returned an error; continuing to upgrade",
				"error", rerr, "output", lastMeaningfulLines(string(repairOut), 5))
		}
	}

	slog.Info("os_update: running upgrade", "mode", mode)
	upgradeOut, err := a.runUpgradeCommand(updateCtx, upgradeArgs[0], upgradeArgs[1:]...)
	combinedOut := string(refreshOut) + string(repairOut) + string(upgradeOut)
	if err != nil {
		ec := exitCodeFromError(err)
		return commandExecResult{
			Status:   "error",
			Message:  fmt.Sprintf("upgrade failed (exit %d): %s", ec, lastMeaningfulLines(string(upgradeOut), 10)),
			ExitCode: &ec,
			Output:   combinedOut,
		}
	}

	// "Actually upgraded" detection via before/after version snapshot
	// (FR-024/FR-025). A failed snapshot yields an empty applied set ⇒ no
	// reboot — the fail-safe direction (§7C).
	var applied []string
	if len(released) > 0 {
		after := a.installedVersions(updateCtx, plan.PM, released)
		applied = changedVersions(before, after, released)
	}

	ec := 0
	summary := parseOSUpdateSummary(string(upgradeOut), plan.PM == "apt")
	switch {
	case len(applied) > 0 && a.config.Commands.AllowReboot:
		summary += fmt.Sprintf(" (reboot-class applied: %s; reboot initiated)", strings.Join(applied, ", "))
	case len(applied) > 0:
		summary += fmt.Sprintf(" (reboot-class applied: %s; reboot required but not permitted (commands.allow_reboot: false))", strings.Join(applied, ", "))
	case holdsSkipped:
		summary += " (reboot-class packages skipped: holds not released)"
	}
	return commandExecResult{
		Status:             "success",
		Message:            summary,
		ExitCode:           &ec,
		Output:             combinedOut,
		RebootClassApplied: applied,
		RebootPending:      len(applied) > 0 && a.config.Commands.AllowReboot,
		RebootCommandID:    commandID,
	}
}

// installedVersions returns name → installed version for the given
// packages via one batched query (AD-008 steps b/e). Returns nil on error
// so applied-detection fails safe (no reboot).
func (a *Agent) installedVersions(ctx context.Context, pm string, pkgs []string) map[string]string {
	if len(pkgs) == 0 {
		return nil
	}
	var out []byte
	var err error
	if pm == "dnf" {
		args := append([]string{"-q", "--qf", "%{NAME} %{VERSION}-%{RELEASE}\n"}, pkgs...)
		out, err = a.runCommand(ctx, "rpm", args...)
	} else {
		args := append([]string{"-W", "-f=${Package} ${Version}\n"}, pkgs...)
		out, err = a.runCommand(ctx, "dpkg-query", args...)
	}
	if err != nil {
		slog.Warn("os_update: version snapshot failed; treating as no reboot-class change", "error", err)
		return nil
	}
	versions := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			versions[fields[0]] = fields[1]
		}
	}
	return versions
}

// changedVersions returns the released packages whose installed version
// changed between the before and after snapshots. Either snapshot missing
// (query failure) yields an empty result — fail-safe, no reboot (FR-025).
func changedVersions(before, after map[string]string, released []string) []string {
	if len(before) == 0 || len(after) == 0 {
		return nil
	}
	var changed []string
	for _, p := range released {
		if bv, ok := before[p]; ok {
			if av, ok := after[p]; ok && av != bv {
				changed = append(changed, p)
			}
		}
	}
	return changed
}

// handleOSUpdate is kept for backward compatibility (wraps handleOSUpdateWithOutput).
func (a *Agent) handleOSUpdate(ctx context.Context, payload models.CommandPayload) (string, string) {
	r := a.handleOSUpdateWithOutput(ctx, payload)
	return r.Status, r.Message
}

// parseOSUpdateSummary parses apt/dnf output to produce a human-readable summary.
func parseOSUpdateSummary(output string, isApt bool) string {
	if isApt {
		return parseAptSummary(output)
	}
	return parseDnfSummary(output)
}

var aptUpgradedRe = regexp.MustCompile(`(\d+)\s+upgraded`)
var aptInstalledRe = regexp.MustCompile(`(\d+)\s+newly installed`)
var aptRemovedRe = regexp.MustCompile(`(\d+)\s+to remove`)
var aptHeldRe = regexp.MustCompile(`(\d+)\s+not upgraded`)

func parseAptSummary(output string) string {
	upgraded := extractInt(aptUpgradedRe, output)
	installed := extractInt(aptInstalledRe, output)
	removed := extractInt(aptRemovedRe, output)
	held := extractInt(aptHeldRe, output)

	if upgraded == 0 && installed == 0 && removed == 0 {
		return "System is up to date"
	}

	total := upgraded + installed
	parts := []string{fmt.Sprintf("Updated %d packages", total)}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", removed))
	}
	if held > 0 {
		parts = append(parts, fmt.Sprintf("%d held", held))
	}
	return strings.Join(parts, " (") + strings.Repeat(")", len(parts)-1)
}

var dnfUpgradedRe = regexp.MustCompile(`(?i)Upgraded:\s*(\d+)`)
var dnfInstalledRe = regexp.MustCompile(`(?i)Installed:\s*(\d+)`)
var dnfRemovedRe = regexp.MustCompile(`(?i)Removed:\s*(\d+)`)

func parseDnfSummary(output string) string {
	upgraded := extractInt(dnfUpgradedRe, output)
	installed := extractInt(dnfInstalledRe, output)
	removed := extractInt(dnfRemovedRe, output)

	if upgraded == 0 && installed == 0 && removed == 0 {
		// dnf often says "Nothing to do" or "No packages marked for update"
		if strings.Contains(output, "Nothing to do") || strings.Contains(output, "No packages marked") {
			return "System is up to date"
		}
		return "Update completed"
	}

	total := upgraded + installed
	parts := []string{fmt.Sprintf("Updated %d packages", total)}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", removed))
	}
	return strings.Join(parts, " (") + strings.Repeat(")", len(parts)-1)
}

func extractInt(re *regexp.Regexp, s string) int {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	return v
}

// exitCodeFromError extracts the exit code from an exec.ExitError.
func exitCodeFromError(err error) int {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// lastMeaningfulLines returns the last N non-empty lines of output.
func lastMeaningfulLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var meaningful []string
	for i := len(lines) - 1; i >= 0 && len(meaningful) < n; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			meaningful = append([]string{line}, meaningful...)
		}
	}
	return strings.Join(meaningful, "\n")
}

// handleEnableAutoUpdates installs and configures unattended-upgrades (Debian/Ubuntu)
// or dnf-automatic (RHEL/Fedora) for automatic security patching.
func (a *Agent) handleEnableAutoUpdates(ctx context.Context) (string, string) {
	if !a.config.Commands.AllowPatching {
		return "error", "patching not allowed by agent config (set commands.allow_patching: true)"
	}
	if runtime.GOOS != "linux" {
		return "error", "enable_auto_updates is only supported on Linux"
	}

	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	aptPath, aptErr := exec.LookPath("apt-get")
	dnfPath, dnfErr := exec.LookPath("dnf")

	switch {
	case aptErr == nil:
		return a.enableUnattendedUpgrades(cmdCtx, aptPath)
	case dnfErr == nil:
		return a.enableDNFAutomatic(cmdCtx, dnfPath)
	default:
		return "error", "no supported package manager found (apt-get or dnf)"
	}
}

func (a *Agent) enableUnattendedUpgrades(ctx context.Context, aptPath string) (string, string) {
	// Install unattended-upgrades package
	slog.Info("enable_auto_updates: installing unattended-upgrades")
	out, err := exec.CommandContext(ctx, "sudo", aptPath, "-y", "install", "unattended-upgrades").CombinedOutput()
	if err != nil {
		return "error", fmt.Sprintf("failed to install unattended-upgrades: %s\n%s", err, truncateOutput(out, 2000))
	}

	// Enable via dpkg-reconfigure (non-interactive)
	slog.Info("enable_auto_updates: enabling via dpkg-reconfigure")
	reconfigOut, err := exec.CommandContext(ctx, "sudo", "dpkg-reconfigure", "-plow", "unattended-upgrades").CombinedOutput()
	if err != nil {
		// Fallback: write the config file directly
		slog.Warn("enable_auto_updates: dpkg-reconfigure failed, writing config directly", "error", err)
		configContent := `APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
`
		writeCmd := exec.CommandContext(ctx, "sudo", "tee", "/etc/apt/apt.conf.d/20auto-upgrades")
		writeCmd.Stdin = strings.NewReader(configContent)
		if wout, werr := writeCmd.CombinedOutput(); werr != nil {
			return "error", fmt.Sprintf("failed to write auto-upgrades config: %s\n%s", werr, truncateOutput(wout, 2000))
		}
	}
	_ = reconfigOut

	// Verify it's enabled
	checkOut, _ := exec.CommandContext(ctx, "apt-config", "dump", "APT::Periodic::Unattended-Upgrade").Output()
	if strings.Contains(string(checkOut), `"1"`) {
		return "success", "unattended-upgrades installed and enabled"
	}
	return "success", "unattended-upgrades installed; verify /etc/apt/apt.conf.d/20auto-upgrades"
}

func (a *Agent) enableDNFAutomatic(ctx context.Context, dnfPath string) (string, string) {
	// Install dnf-automatic
	slog.Info("enable_auto_updates: installing dnf-automatic")
	out, err := exec.CommandContext(ctx, "sudo", dnfPath, "-y", "install", "dnf-automatic").CombinedOutput()
	if err != nil {
		return "error", fmt.Sprintf("failed to install dnf-automatic: %s\n%s", err, truncateOutput(out, 2000))
	}

	// Enable and start the timer
	slog.Info("enable_auto_updates: enabling dnf-automatic timer")
	if enableOut, err := exec.CommandContext(ctx, "sudo", "systemctl", "enable", "--now", "dnf-automatic.timer").CombinedOutput(); err != nil {
		return "error", fmt.Sprintf("failed to enable dnf-automatic timer: %s\n%s", err, truncateOutput(enableOut, 2000))
	}

	return "success", "dnf-automatic installed and timer enabled"
}

// truncateOutput returns the last maxLen characters of output.
func truncateOutput(data []byte, maxLen int) string {
	if len(data) <= maxLen {
		return string(data)
	}
	return "...(truncated)\n" + string(data[len(data)-maxLen:])
}

// TruncateOutputSmart truncates output to maxBytes using a head+tail strategy.
// If the output is within the limit, it is returned as-is.
// Otherwise, the first headSize bytes and last tailSize bytes are kept with a truncation marker.
func TruncateOutputSmart(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	headSize := truncateHeadTailBytes
	tailSize := truncateHeadTailBytes
	if headSize+tailSize >= maxBytes {
		headSize = maxBytes / 2
		tailSize = maxBytes / 2
	}
	return s[:headSize] + "\n... [truncated] ...\n" + s[len(s)-tailSize:]
}

// handleUninstall initiates agent self-removal from the host.
func (a *Agent) handleUninstall() (string, string) {
	go func() {
		time.Sleep(1 * time.Second) // let the result message flush
		a.performUninstall()
	}()
	return "success", "uninstall initiated"
}

// performUninstall removes the agent binary, config, data, and service, then exits.
func (a *Agent) performUninstall() {
	slog.Info("uninstall: beginning self-removal")

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Warn("uninstall: command failed", "cmd", name, "args", args, "error", err, "output", string(out))
		}
	}

	if runtime.GOOS == "windows" {
		run("sc", "stop", "riot-agent")
		run("sc", "delete", "riot-agent")
		os.RemoveAll(os.Getenv("PROGRAMDATA") + `\riot`)
		// Remove binary last
		exe, _ := os.Executable()
		if exe != "" {
			os.Remove(exe)
		}
	} else {
		run("systemctl", "stop", "riot-agent")
		run("systemctl", "disable", "riot-agent")
		os.Remove("/etc/systemd/system/riot-agent.service")
		run("systemctl", "daemon-reload")
		os.Remove("/usr/local/bin/riot-agent")
		os.RemoveAll("/etc/riot")
		os.RemoveAll("/var/lib/riot")
	}

	slog.Info("uninstall: cleanup complete, exiting")
	os.Exit(0)
}

// handleFetchLogs runs journalctl with the requested parameters and pushes log entries to the server.
func (a *Agent) handleFetchLogs(ctx context.Context, payload models.CommandPayload) (string, string) {
	if runtime.GOOS != "linux" {
		return "error", "fetch_logs is only supported on Linux"
	}

	// Parse parameters with defaults
	hours := 24.0
	if h, ok := payload.Params["hours"].(float64); ok && h > 0 {
		hours = h
	}
	maxPriority := 6 // info level
	if p, ok := payload.Params["priority"].(float64); ok {
		maxPriority = int(p)
	}
	limit := 1000
	if l, ok := payload.Params["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	if limit > 5000 {
		limit = 5000
	}

	since := time.Now().Add(-time.Duration(hours * float64(time.Hour)))
	sinceStr := since.Format("2006-01-02 15:04:05")

	priorityArg := fmt.Sprintf("--priority=0..%d", maxPriority)
	limitArg := fmt.Sprintf("%d", limit)

	fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(fetchCtx, "journalctl",
		"--since", sinceStr,
		priorityArg,
		"-o", "json",
		"--no-pager",
		"-n", limitArg,
	).Output()
	if err != nil {
		return "error", fmt.Sprintf("journalctl: %s", err)
	}

	// fetch_logs applies the same per-message truncation as the logs collector.
	maxLineBytes := a.config.Limits().JournalMaxLineBytes

	var entries []models.LogEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		var ts time.Time
		if usecStr, ok := raw["__REALTIME_TIMESTAMP"].(string); ok {
			if usec, err := strconv.ParseInt(usecStr, 10, 64); err == nil {
				ts = time.Unix(usec/1_000_000, (usec%1_000_000)*1000)
			}
		}
		if ts.IsZero() {
			ts = time.Now()
		}

		priority := 6
		if p, ok := raw["PRIORITY"].(string); ok {
			if v, err := strconv.Atoi(p); err == nil {
				priority = v
			}
		}

		unit, _ := raw["_SYSTEMD_UNIT"].(string)
		if unit == "" {
			unit, _ = raw["SYSLOG_IDENTIFIER"].(string)
		}
		message, _ := raw["MESSAGE"].(string)

		entries = append(entries, models.LogEntry{
			Timestamp: ts,
			Priority:  priority,
			Unit:      collectors.TruncateLogLine(unit, 256),
			Message:   collectors.TruncateLogLine(message, maxLineBytes),
		})
	}

	if len(entries) == 0 {
		return "success", "no log entries found for the requested time range"
	}

	// Push to server
	if err := a.client.SendDeviceLogs(ctx, a.config.Agent.DeviceID, entries); err != nil {
		return "error", fmt.Sprintf("failed to push logs: %s", err)
	}

	return "success", fmt.Sprintf("fetched and pushed %d log entries", len(entries))
}

// handleTriggerUpdate runs the agent's self-update synchronously and returns the real result.
// On success the agent restarts after the command result is sent (handled by the caller).
func (a *Agent) handleTriggerUpdate(ctx context.Context) (string, string) {
	if a.version == "dev" {
		return "error", "cannot auto-update dev builds"
	}

	if a.config.Agent.AutoUpdate != nil && !*a.config.Agent.AutoUpdate {
		return "error", "auto-update disabled by agent config"
	}

	goarm := ""
	if runtime.GOARCH == "arm" {
		goarm = goarmVersion()
	}

	resp, err := a.client.CheckForUpdate(ctx, a.version, runtime.GOOS, runtime.GOARCH, goarm)
	if err != nil {
		return "error", fmt.Sprintf("update check failed: %v", err)
	}
	// Empty LatestVersion means the server has not been able to reach the
	// release feed yet — we cannot tell whether this agent is current.
	// Returning "success" here would mask the real failure (the server's
	// GitHub DNS / network problem) as a completed update on the dashboard.
	if resp.LatestVersion == "" {
		return "error", "server has not reached release feed yet; cannot verify update"
	}
	if !resp.UpdateAvail {
		return "success", fmt.Sprintf("agent is already up to date (%s)", a.version)
	}

	// AD-010 best-effort downgrade guard (see runUpdateCheck for its limits).
	if !isNewerVersion(resp.LatestVersion, a.version) {
		return "error", fmt.Sprintf("refusing update: advertised version %s is not newer than %s", resp.LatestVersion, a.version)
	}

	suffix := platformSuffix(goarm)
	downloadURL, ok := resp.Assets[suffix]
	if !ok {
		return "error", fmt.Sprintf("no binary available for platform %s", suffix)
	}

	a.reportUpdateEvent(ctx, models.EventAgentUpdateStarted, models.SeverityInfo,
		fmt.Sprintf("Agent update started: %s → %s", a.version, resp.LatestVersion))

	if err := a.performUpdate(ctx, downloadURL, resp.ChecksumURL, resp.SignatureURL, suffix); err != nil {
		if errors.Is(err, errUpdateScheduled) {
			return "pending", "signed update accepted; privileged installation and restart are pending"
		}
		a.reportUpdateEvent(ctx, models.EventAgentUpdateFailed, models.SeverityWarning,
			fmt.Sprintf("Agent update failed: %v", err))
		return "error", fmt.Sprintf("update failed: %v", err)
	}

	a.reportUpdateEvent(ctx, models.EventAgentUpdateCompleted, models.SeverityInfo,
		fmt.Sprintf("Agent updated to %s", resp.LatestVersion))

	// Restart after a short delay so the command result is sent first.
	go func() {
		time.Sleep(1 * time.Second)
		a.restartSelf()
	}()

	return "success", fmt.Sprintf("updated %s → %s, restarting", a.version, resp.LatestVersion)
}
