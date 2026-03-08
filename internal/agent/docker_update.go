package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// dockerUpdate pulls a newer image and recreates the container.
// For Compose-managed containers it delegates to `docker compose`.
// For standalone containers it uses the Docker SDK directly.
//
// Supports two modes:
//   - Single container: params must include "container_id"
//   - Full compose stack: params must include "compose_work_dir" (updates all services)
func (a *Agent) dockerUpdate(ctx context.Context, payload models.CommandPayload) (string, string) {
	// Full-stack compose update — pull + up the entire project
	if workDir, _ := payload.Params["compose_work_dir"].(string); workDir != "" {
		status, msg := a.dockerUpdateCompose(workDir, "")
		if status == "success" {
			a.clearFreshnessCache()
		}
		return status, msg
	}

	containerID, _ := payload.Params["container_id"].(string)
	if containerID == "" {
		return "error", "container_id or compose_work_dir is required"
	}

	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		return "error", fmt.Sprintf("docker client: %s", err)
	}
	defer cli.Close()

	// Inspect container to determine update strategy
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "error", fmt.Sprintf("inspect container: %s", err)
	}

	// Check if this is a Compose-managed container
	composeProject := inspect.Config.Labels["com.docker.compose.project"]
	composeService := inspect.Config.Labels["com.docker.compose.service"]
	composeWorkDir := inspect.Config.Labels["com.docker.compose.project.working_dir"]

	var status, msg string
	if composeProject != "" && composeService != "" && composeWorkDir != "" {
		status, msg = a.dockerUpdateCompose(composeWorkDir, composeService)
	} else {
		status, msg = a.dockerUpdateStandalone(ctx, cli, inspect)
	}

	if status == "success" {
		a.clearFreshnessCache()
	}
	return status, msg
}

// dockerUpdateCompose updates Compose-managed containers by running
// `docker compose pull && docker compose up -d`.
// If service is non-empty, only that service is updated; otherwise the entire stack is updated.
func (a *Agent) dockerUpdateCompose(workDir, service string) (string, string) {
	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	slog.Info("docker_update: compose path", "workDir", workDir, "service", service)

	// Build pull command
	pullArgs := []string{"compose", "--project-directory", workDir, "pull"}
	if service != "" {
		pullArgs = append(pullArgs, service)
	}
	pullCmd := exec.CommandContext(updateCtx, "docker", pullArgs...)
	pullOut, err := pullCmd.CombinedOutput()
	if err != nil {
		return "error", fmt.Sprintf("compose pull failed: %s\n%s", err, truncateOutput(pullOut, 4000))
	}

	// Build up command
	upArgs := []string{"compose", "--project-directory", workDir, "up", "-d"}
	if service != "" {
		upArgs = append(upArgs, service)
	}
	upCmd := exec.CommandContext(updateCtx, "docker", upArgs...)
	upOut, err := upCmd.CombinedOutput()
	combined := append(pullOut, upOut...)
	if err != nil {
		return "error", fmt.Sprintf("compose up failed: %s\n%s", err, truncateOutput(combined, 4000))
	}

	target := "stack"
	if service != "" {
		target = "service " + service
	}
	return "success", fmt.Sprintf("updated compose %s\n%s", target, truncateOutput(combined, 4000))
}

// dockerUpdateStandalone updates a standalone container by pulling the new image,
// then stopping, removing, and recreating the container with the same configuration.
func (a *Agent) dockerUpdateStandalone(ctx context.Context, cli *client.Client, inspect container.InspectResponse) (string, string) {
	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	imageRef := inspect.Config.Image
	containerName := strings.TrimPrefix(inspect.Name, "/")
	oldID := inspect.ID

	slog.Info("docker_update: standalone path", "container", containerName, "image", imageRef)

	// Step 1: Pull new image BEFORE stopping (if pull fails, container is untouched)
	pullOut, err := cli.ImagePull(updateCtx, imageRef, image.PullOptions{})
	if err != nil {
		return "error", fmt.Sprintf("image pull failed: %s", err)
	}
	// Must drain the reader to complete the pull
	io.Copy(io.Discard, pullOut)
	pullOut.Close()

	// Step 2: Stop old container
	timeout := 30
	if err := cli.ContainerStop(updateCtx, oldID, container.StopOptions{Timeout: &timeout}); err != nil {
		return "error", fmt.Sprintf("stop failed: %s", err)
	}

	// Step 3: Remove old container
	if err := cli.ContainerRemove(updateCtx, oldID, container.RemoveOptions{}); err != nil {
		// Try to restart the old container if remove fails
		cli.ContainerStart(updateCtx, oldID, container.StartOptions{})
		return "error", fmt.Sprintf("remove failed (restarted old container): %s", err)
	}

	// Step 4: Create new container with same config
	var networkConfig *network.NetworkingConfig
	if inspect.NetworkSettings != nil && len(inspect.NetworkSettings.Networks) > 0 {
		networkConfig = &network.NetworkingConfig{
			EndpointsConfig: make(map[string]*network.EndpointSettings),
		}
		for netName, netSettings := range inspect.NetworkSettings.Networks {
			networkConfig.EndpointsConfig[netName] = &network.EndpointSettings{
				IPAMConfig:  netSettings.IPAMConfig,
				Links:       netSettings.Links,
				Aliases:     netSettings.Aliases,
				MacAddress:  netSettings.MacAddress,
				DriverOpts:  netSettings.DriverOpts,
				NetworkID:   netSettings.NetworkID,
				DNSNames:    netSettings.DNSNames,
			}
		}
	}

	newContainer, err := cli.ContainerCreate(updateCtx,
		inspect.Config,
		inspect.HostConfig,
		networkConfig,
		nil, // platform
		containerName,
	)
	if err != nil {
		return "error", fmt.Sprintf("create failed (old container removed): %s", err)
	}

	// Step 5: Start new container
	if err := cli.ContainerStart(updateCtx, newContainer.ID, container.StartOptions{}); err != nil {
		return "error", fmt.Sprintf("start failed: %s", err)
	}

	return "success", fmt.Sprintf("updated container %s (%s)", containerName, imageRef)
}

// clearFreshnessCache clears the docker collector's image freshness cache
// so the next telemetry push reflects the updated state.
func (a *Agent) clearFreshnessCache() {
	for _, c := range a.registry.Collectors() {
		if dc, ok := c.(*collectors.DockerCollector); ok {
			dc.ClearFreshnessCache()
			return
		}
	}
}
