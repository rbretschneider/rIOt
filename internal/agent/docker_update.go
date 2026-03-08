package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// dockerUpdate pulls a newer image and recreates the container.
// For Compose-managed containers it delegates to `docker compose`.
// For standalone containers it uses the Docker SDK directly.
func (a *Agent) dockerUpdate(ctx context.Context, payload models.CommandPayload) (string, string) {
	containerID, _ := payload.Params["container_id"].(string)
	if containerID == "" {
		return "error", "container_id is required"
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

	if composeProject != "" && composeService != "" && composeWorkDir != "" {
		return a.dockerUpdateCompose(composeWorkDir, composeService)
	}

	return a.dockerUpdateStandalone(ctx, cli, inspect)
}

// dockerUpdateCompose updates a Compose-managed container by running
// `docker compose pull <service> && docker compose up -d <service>`.
func (a *Agent) dockerUpdateCompose(workDir, service string) (string, string) {
	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	slog.Info("docker_update: compose path", "workDir", workDir, "service", service)

	// Pull new image
	pullCmd := exec.CommandContext(updateCtx, "docker", "compose",
		"--project-directory", workDir, "pull", service)
	pullOut, err := pullCmd.CombinedOutput()
	if err != nil {
		return "error", fmt.Sprintf("compose pull failed: %s\n%s", err, truncateOutput(pullOut, 4000))
	}

	// Recreate with new image
	upCmd := exec.CommandContext(updateCtx, "docker", "compose",
		"--project-directory", workDir, "up", "-d", service)
	upOut, err := upCmd.CombinedOutput()
	combined := append(pullOut, upOut...)
	if err != nil {
		return "error", fmt.Sprintf("compose up failed: %s\n%s", err, truncateOutput(combined, 4000))
	}

	return "success", fmt.Sprintf("updated compose service %s\n%s", service, truncateOutput(combined, 4000))
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
