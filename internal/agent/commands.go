package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
)

// handleCommand dispatches a remote command from the server.
func (a *Agent) handleCommand(ctx context.Context, msg AgentWSMessage) {
	var payload models.CommandPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		slog.Warn("command: invalid payload", "error", err)
		return
	}

	slog.Info("command: received", "id", payload.CommandID, "action", payload.Action)

	var status, message string
	switch payload.Action {
	case "docker_stop":
		status, message = a.dockerCommand(ctx, payload, "stop")
	case "docker_restart":
		status, message = a.dockerCommand(ctx, payload, "restart")
	case "docker_start":
		status, message = a.dockerCommand(ctx, payload, "start")
	case "reboot":
		status, message = a.handleReboot(payload)
	case "os_update":
		status, message = a.handleOSUpdate(ctx, payload)
	case "agent_update":
		status, message = a.handleTriggerUpdate(ctx)
	case "agent_uninstall":
		status, message = a.handleUninstall()
	default:
		status = "error"
		message = fmt.Sprintf("unknown action: %s", payload.Action)
	}

	if status == "error" {
		slog.Warn("command: failed", "id", payload.CommandID, "action", payload.Action, "error", message)
	} else {
		slog.Info("command: completed", "id", payload.CommandID, "action", payload.Action, "status", status)
	}

	// Send result back to server
	result := models.CommandResult{
		CommandID: payload.CommandID,
		Status:    status,
		Message:   message,
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

// handleOSUpdate runs package manager updates if allowed by config.
func (a *Agent) handleOSUpdate(ctx context.Context, payload models.CommandPayload) (string, string) {
	if !a.config.Commands.AllowPatching {
		return "error", "patching not allowed by agent config (set commands.allow_patching: true)"
	}
	if runtime.GOOS != "linux" {
		return "error", "os_update is only supported on Linux"
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
		return "error", "no supported package manager found (apt-get or dnf)"
	}

	slog.Info("os_update: refreshing package index", "mode", mode)
	refreshCmd := exec.CommandContext(updateCtx, refreshArgs[0], refreshArgs[1:]...)
	refreshOut, err := refreshCmd.CombinedOutput()
	if err != nil {
		return "error", fmt.Sprintf("package refresh failed: %s\n%s", err, truncateOutput(refreshOut, 4000))
	}

	slog.Info("os_update: running upgrade", "mode", mode)
	upgradeCmd := exec.CommandContext(updateCtx, upgradeArgs[0], upgradeArgs[1:]...)
	upgradeOut, err := upgradeCmd.CombinedOutput()
	combined := append(refreshOut, upgradeOut...)
	if err != nil {
		return "error", fmt.Sprintf("upgrade failed: %s\n%s", err, truncateOutput(combined, 4000))
	}

	return "success", truncateOutput(combined, 4000)
}

// truncateOutput returns the last maxLen characters of output.
func truncateOutput(data []byte, maxLen int) string {
	if len(data) <= maxLen {
		return string(data)
	}
	return "...(truncated)\n" + string(data[len(data)-maxLen:])
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

	// Restart after a short delay so the command result is sent first
	go func() {
		time.Sleep(1 * time.Second)
		a.restartSelf()
	}()

	return "success", fmt.Sprintf("updated %s → %s, restarting", a.version, resp.LatestVersion)
}
