package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
)

// maxOutputBytes is the maximum size of captured command output (256KB).
const maxOutputBytes = 256 * 1024

// truncateHeadTailBytes is the size kept from the head and tail when truncating (64KB).
const truncateHeadTailBytes = 64 * 1024

// commandExecResult holds the outcome of an executed command including timing and output.
type commandExecResult struct {
	Status     string
	Message    string
	ExitCode   *int
	DurationMs *int64
	Output     string
}

// handleCommand dispatches a remote command from the server.
func (a *Agent) handleCommand(ctx context.Context, msg AgentWSMessage) {
	var payload models.CommandPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		slog.Warn("command: invalid payload", "error", err)
		return
	}

	slog.Info("command: received", "id", payload.CommandID, "action", payload.Action)

	startTime := time.Now()

	var status, message string
	var execOutput string
	var exitCode *int

	switch payload.Action {
	case "docker_stop":
		status, message = a.dockerCommand(ctx, payload, "stop")
	case "docker_restart":
		status, message = a.dockerCommand(ctx, payload, "restart")
	case "docker_start":
		status, message = a.dockerCommand(ctx, payload, "start")
	case "docker_update":
		status, message = a.dockerUpdate(ctx, payload)
	case "docker_bulk_update":
		status, message = a.dockerBulkUpdate(ctx, payload)
	case "docker_check_updates":
		a.clearFreshnessCache()
		status, message = "success", "image freshness cache cleared, will re-check on next telemetry cycle"
	case "reboot":
		status, message = a.handleReboot(payload)
	case "shutdown":
		status, message = a.handleShutdown(payload)
	case "os_update":
		r := a.handleOSUpdateWithOutput(ctx, payload)
		status, message, execOutput, exitCode = r.Status, r.Message, r.Output, r.ExitCode
	case "agent_update":
		status, message = a.handleTriggerUpdate(ctx)
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

	durationMs := time.Since(startTime).Milliseconds()

	if status == "error" {
		slog.Warn("command: failed", "id", payload.CommandID, "action", payload.Action, "error", message)
		// Report command failure as a server event so it appears in the dashboard
		a.reportUpdateEvent(ctx, models.EventCommandCompleted, models.SeverityWarning,
			fmt.Sprintf("Command %s failed: %s", payload.Action, message))
	} else {
		slog.Info("command: completed", "id", payload.CommandID, "action", payload.Action, "status", status)
	}

	// Send result back to server
	result := models.CommandResult{
		CommandID:  payload.CommandID,
		Status:     status,
		Message:    message,
		ExitCode:   exitCode,
		DurationMs: &durationMs,
		Output:     TruncateOutputSmart(execOutput, maxOutputBytes),
	}
	resultJSON, _ := json.Marshal(result)
	if a.wsClient != nil {
		a.wsClient.send(AgentWSMessage{
			Type: "command_result",
			Data: resultJSON,
		})
	}
}

// dockerCommand runs a docker stop/start/restart on the specified container.
func (a *Agent) dockerCommand(ctx context.Context, payload models.CommandPayload, action string) (string, string) {
	containerID, _ := payload.Params["container_id"].(string)
	if containerID == "" {
		return "error", "container_id is required"
	}

	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		return "error", fmt.Sprintf("docker client: %s", err)
	}
	defer cli.Close()

	timeout := 30 // seconds

	switch action {
	case "stop":
		err = cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
	case "restart":
		err = cli.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &timeout})
	case "start":
		err = cli.ContainerStart(ctx, containerID, container.StartOptions{})
	}

	if err != nil {
		return "error", fmt.Sprintf("docker %s: %s", action, err)
	}
	return "success", fmt.Sprintf("container %s: %s ok", containerID[:12], action)
}

// handleReboot triggers a system reboot if allowed by config.
func (a *Agent) handleReboot(payload models.CommandPayload) (string, string) {
	if !a.config.Commands.AllowReboot {
		return "error", "reboot not allowed by agent config (set commands.allow_reboot: true)"
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("shutdown", "/r", "/t", "5")
	} else {
		cmd = exec.Command("sudo", "systemctl", "reboot")
	}

	if err := cmd.Start(); err != nil {
		return "error", fmt.Sprintf("reboot: %s", err)
	}
	return "success", "reboot initiated"
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

	mode, _ := payload.Params["mode"].(string)
	if mode == "" {
		mode = "full"
	}

	// Use a long timeout independent of the WS read loop
	updateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Detect package manager
	aptPath, aptErr := exec.LookPath("apt-get")
	dnfPath, dnfErr := exec.LookPath("dnf")

	var refreshArgs, upgradeArgs []string

	switch {
	case aptErr == nil:
		refreshArgs = []string{"sudo", aptPath, "update"}
		if mode == "security" {
			upgradeArgs = []string{"sudo", aptPath, "-y", "upgrade",
				"-o", "Dpkg::Options::=--force-confold",
				"-o", "Dpkg::Options::=--force-confdef"}
		} else {
			upgradeArgs = []string{"sudo", aptPath, "-y", "dist-upgrade",
				"-o", "Dpkg::Options::=--force-confold",
				"-o", "Dpkg::Options::=--force-confdef"}
		}
	case dnfErr == nil:
		refreshArgs = []string{"sudo", dnfPath, "makecache"}
		if mode == "security" {
			upgradeArgs = []string{"sudo", dnfPath, "-y", "--security", "update"}
		} else {
			upgradeArgs = []string{"sudo", dnfPath, "-y", "update"}
		}
	default:
		return commandExecResult{Status: "error", Message: "no supported package manager found (apt-get or dnf)"}
	}

	slog.Info("os_update: refreshing package index", "mode", mode)
	refreshCmd := exec.CommandContext(updateCtx, refreshArgs[0], refreshArgs[1:]...)
	refreshOut, err := refreshCmd.CombinedOutput()
	if err != nil {
		ec := exitCodeFromError(err)
		return commandExecResult{
			Status:   "error",
			Message:  fmt.Sprintf("package refresh failed (exit %d): %s", ec, lastMeaningfulLines(string(refreshOut), 10)),
			ExitCode: &ec,
			Output:   string(refreshOut),
		}
	}

	slog.Info("os_update: running upgrade", "mode", mode)
	upgradeCmd := exec.CommandContext(updateCtx, upgradeArgs[0], upgradeArgs[1:]...)
	upgradeOut, err := upgradeCmd.CombinedOutput()
	combinedOut := string(refreshOut) + string(upgradeOut)
	if err != nil {
		ec := exitCodeFromError(err)
		return commandExecResult{
			Status:   "error",
			Message:  fmt.Sprintf("upgrade failed (exit %d): %s", ec, lastMeaningfulLines(string(upgradeOut), 10)),
			ExitCode: &ec,
			Output:   combinedOut,
		}
	}

	ec := 0
	summary := parseOSUpdateSummary(string(upgradeOut), aptErr == nil)
	return commandExecResult{
		Status:   "success",
		Message:  summary,
		ExitCode: &ec,
		Output:   combinedOut,
	}
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
			Unit:      unit,
			Message:   message,
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
	if !resp.UpdateAvail {
		return "success", fmt.Sprintf("agent is already up to date (%s)", a.version)
	}

	suffix := platformSuffix(goarm)
	downloadURL, ok := resp.Assets[suffix]
	if !ok {
		return "error", fmt.Sprintf("no binary available for platform %s", suffix)
	}

	a.reportUpdateEvent(ctx, models.EventAgentUpdateStarted, models.SeverityInfo,
		fmt.Sprintf("Agent update started: %s → %s", a.version, resp.LatestVersion))

	if err := a.performUpdate(ctx, downloadURL, resp.ChecksumURL, suffix); err != nil {
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
