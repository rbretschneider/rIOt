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
	case "agent_update":
		status, message = a.handleTriggerUpdate(ctx)
	case "agent_uninstall":
		status, message = a.handleUninstall()
	default:
		status = "error"
		message = fmt.Sprintf("unknown action: %s", payload.Action)
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
		cmd = exec.Command("systemctl", "reboot")
	}

	if err := cmd.Start(); err != nil {
		return "error", fmt.Sprintf("reboot: %s", err)
	}
	return "success", "reboot initiated"
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

// handleTriggerUpdate triggers the agent's self-update mechanism.
func (a *Agent) handleTriggerUpdate(ctx context.Context) (string, string) {
	// Run update check in background — it will download and replace the binary
	go func() {
		time.Sleep(1 * time.Second) // small delay to let the result be sent first
		a.checkAndUpdate(ctx)
	}()
	return "success", "update check triggered"
}
