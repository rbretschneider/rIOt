package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"
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
			a.triggerTelemetry()
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
		a.triggerTelemetry()
	}
	return status, msg
}

// dockerUpdateCompose updates Compose-managed containers by running
// `docker compose pull && docker compose up -d`.
// If service is non-empty, only that service is updated; otherwise the entire stack is updated.
//
// Before recreating, it stops containers using network_mode: container:<name> to prevent
// failures when the parent container is recreated (e.g. sonarr/radarr depending on gluetun).
func (a *Agent) dockerUpdateCompose(workDir, service string) (string, string) {
	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	stackName := filepath.Base(workDir)
	target := stackName + " stack"
	if service != "" {
		target = service
	}

	slog.Info("docker_update: compose path", "workDir", workDir, "service", service)
	a.sendDockerLifecycleEvent(updateCtx, target, "", "update_started")

	// Stop network_mode dependents before compose up to prevent failures when
	// a parent container (e.g. gluetun) is recreated and dependents lose their network namespace.
	stoppedDependents := a.stopNetworkDependents(updateCtx, workDir)
	if len(stoppedDependents) > 0 {
		slog.Info("docker_update: stopped network dependents before compose update",
			"stack", stackName, "stopped", stoppedDependents)
	}

	// Build pull command — try without --compatibility first (it doesn't help with validation)
	pullArgs := []string{"compose", "--project-directory", workDir, "pull"}
	if service != "" {
		pullArgs = append(pullArgs, service)
	}
	pullCmd := exec.CommandContext(updateCtx, "docker", pullArgs...)
	pullOut, err := pullCmd.CombinedOutput()

	// If pull failed due to compose file validation (e.g. deploy.resources not allowed),
	// sanitize the compose file by stripping unsupported keys and retry.
	var sanitizedFile string
	if err != nil && isComposeValidationError(string(pullOut)) {
		slog.Info("docker_update: compose validation failed, retrying with sanitized file",
			"stack", stackName, "error", string(pullOut))
		sf, serr := sanitizeComposeFile(workDir)
		if serr != nil {
			a.sendDockerLifecycleEvent(updateCtx, target, "", "update_failed")
			return "error", fmt.Sprintf("compose pull failed: %s\n%s\nsanitize fallback also failed: %s",
				err, truncateOutput(pullOut, 4000), serr)
		}
		sanitizedFile = sf
		defer os.Remove(sanitizedFile)

		pullArgs = []string{"compose", "-f", sanitizedFile, "--project-directory", workDir, "pull"}
		if service != "" {
			pullArgs = append(pullArgs, service)
		}
		pullCmd = exec.CommandContext(updateCtx, "docker", pullArgs...)
		pullOut, err = pullCmd.CombinedOutput()
	}

	if err != nil {
		a.sendDockerLifecycleEvent(updateCtx, target, "", "update_failed")
		return "error", fmt.Sprintf("compose pull failed: %s\n%s", err, truncateOutput(pullOut, 4000))
	}

	// Build up command — use sanitized file if we needed it for pull
	upArgs := []string{"compose"}
	if sanitizedFile != "" {
		upArgs = append(upArgs, "-f", sanitizedFile)
	}
	upArgs = append(upArgs, "--project-directory", workDir, "up", "-d")
	if service != "" {
		upArgs = append(upArgs, service)
	}
	upCmd := exec.CommandContext(updateCtx, "docker", upArgs...)
	upOut, err := upCmd.CombinedOutput()
	combined := append(pullOut, upOut...)
	if err != nil {
		a.sendDockerLifecycleEvent(updateCtx, target, "", "update_failed")
		return "error", fmt.Sprintf("compose up failed: %s\n%s", err, truncateOutput(combined, 4000))
	}

	a.sendDockerLifecycleEvent(updateCtx, target, "", "update_completed")

	label := stackName + " stack"
	if service != "" {
		label = "service " + service
	}
	return "success", fmt.Sprintf("updated compose %s\n%s", label, truncateOutput(combined, 4000))
}

// stopNetworkDependents finds and stops containers in a compose project that use
// network_mode: container:<name>, so that the parent can be safely recreated.
// Returns the names of containers that were stopped.
func (a *Agent) stopNetworkDependents(ctx context.Context, workDir string) []string {
	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		return nil
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil
	}

	// Find containers in this compose project
	var projectContainers []container.Summary
	for _, c := range containers {
		if c.Labels["com.docker.compose.project.working_dir"] == workDir {
			projectContainers = append(projectContainers, c)
		}
	}

	// Find which container names exist in this project
	projectNames := make(map[string]bool)
	for _, c := range projectContainers {
		for _, name := range c.Names {
			projectNames[strings.TrimPrefix(name, "/")] = true
		}
	}

	// Stop running containers that use network_mode: container:<name> where <name> is in this project
	var stopped []string
	for _, c := range projectContainers {
		if c.State != "running" {
			continue
		}
		// Need to inspect to get network mode
		inspect, err := cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}
		if inspect.HostConfig == nil {
			continue
		}
		nm := string(inspect.HostConfig.NetworkMode)
		if !strings.HasPrefix(nm, "container:") {
			continue
		}
		parentName := strings.TrimPrefix(nm, "container:")
		if !projectNames[parentName] {
			continue
		}
		// This container depends on another container's network — stop it first
		containerName := strings.TrimPrefix(inspect.Name, "/")
		timeout := 30
		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			slog.Warn("docker_update: failed to stop network dependent", "container", containerName, "error", err)
			continue
		}
		stopped = append(stopped, containerName)
	}

	return stopped
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
	a.sendDockerLifecycleEvent(updateCtx, containerName, imageRef, "update_started")

	// Step 1: Pull new image BEFORE stopping (if pull fails, container is untouched)
	pullOut, err := cli.ImagePull(updateCtx, imageRef, image.PullOptions{})
	if err != nil {
		a.sendDockerLifecycleEvent(updateCtx, containerName, imageRef, "update_failed")
		return "error", fmt.Sprintf("image pull failed: %s", err)
	}
	// Must drain the reader to complete the pull
	io.Copy(io.Discard, pullOut)
	pullOut.Close()

	// Step 2: Stop old container
	timeout := 30
	if err := cli.ContainerStop(updateCtx, oldID, container.StopOptions{Timeout: &timeout}); err != nil {
		a.sendDockerLifecycleEvent(updateCtx, containerName, imageRef, "update_failed")
		return "error", fmt.Sprintf("stop failed: %s", err)
	}

	// Step 3: Remove old container
	if err := cli.ContainerRemove(updateCtx, oldID, container.RemoveOptions{}); err != nil {
		// Try to restart the old container if remove fails
		cli.ContainerStart(updateCtx, oldID, container.StartOptions{})
		a.sendDockerLifecycleEvent(updateCtx, containerName, imageRef, "update_failed")
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
		a.sendDockerLifecycleEvent(updateCtx, containerName, imageRef, "update_failed")
		return "error", fmt.Sprintf("create failed (old container removed): %s", err)
	}

	// Step 5: Start new container
	if err := cli.ContainerStart(updateCtx, newContainer.ID, container.StartOptions{}); err != nil {
		a.sendDockerLifecycleEvent(updateCtx, containerName, imageRef, "update_failed")
		return "error", fmt.Sprintf("start failed: %s", err)
	}

	a.sendDockerLifecycleEvent(updateCtx, containerName, imageRef, "update_completed")
	return "success", fmt.Sprintf("updated container %s (%s)", containerName, imageRef)
}

// sendDockerLifecycleEvent sends a synthetic docker event to the server for update lifecycle tracking.
func (a *Agent) sendDockerLifecycleEvent(ctx context.Context, containerName, image, action string) {
	if a.client == nil {
		return
	}
	deviceID := a.config.Agent.DeviceID
	if deviceID == "" {
		return
	}
	evt := &models.DockerEvent{
		ContainerName: containerName,
		Action:        action,
		Image:         image,
	}
	if err := a.client.SendDockerEvent(ctx, deviceID, evt); err != nil {
		slog.Warn("failed to send docker lifecycle event", "action", action, "container", containerName, "error", err)
	}
}

// dockerBulkUpdate handles ordered container updates with network dependency awareness.
// It receives a list of container IDs and updates them, handling network_mode: container:<parent>
// dependencies by stopping dependents first, updating the parent, then updating dependents.
func (a *Agent) dockerBulkUpdate(ctx context.Context, payload models.CommandPayload) (string, string) {
	containerIDs, ok := payload.Params["container_ids"].([]interface{})
	if !ok || len(containerIDs) == 0 {
		return "error", "container_ids is required"
	}

	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		return "error", fmt.Sprintf("docker client: %s", err)
	}
	defer cli.Close()

	// Build dependency graph: child -> parent
	type containerNode struct {
		id     string
		parent string // container name from network_mode: container:<name>
	}

	var nodes []containerNode
	nameToID := make(map[string]string)

	for _, rawID := range containerIDs {
		cid, _ := rawID.(string)
		if cid == "" {
			continue
		}
		inspect, err := cli.ContainerInspect(ctx, cid)
		if err != nil {
			continue
		}
		name := strings.TrimPrefix(inspect.Name, "/")
		nameToID[name] = cid

		node := containerNode{id: cid}
		if inspect.HostConfig != nil {
			nm := string(inspect.HostConfig.NetworkMode)
			if strings.HasPrefix(nm, "container:") {
				node.parent = strings.TrimPrefix(nm, "container:")
			}
		}
		nodes = append(nodes, node)
	}

	// Separate into: parents (containers that others depend on) and children
	parentSet := make(map[string]bool)
	for _, n := range nodes {
		if n.parent != "" {
			parentSet[n.parent] = true
		}
	}

	// Update order: children first, then parents
	var children, parents []containerNode
	for _, n := range nodes {
		name := ""
		for k, v := range nameToID {
			if v == n.id {
				name = k
				break
			}
		}
		if parentSet[name] || parentSet[n.id] {
			parents = append(parents, n)
		} else if n.parent != "" {
			children = append(children, n)
		} else {
			// Independent container — just update
			children = append(children, n)
		}
	}

	var results []string
	var failed int

	// First: update children (dependents)
	for _, n := range children {
		p := models.CommandPayload{Params: map[string]interface{}{"container_id": n.id}}
		status, msg := a.dockerUpdate(ctx, p)
		results = append(results, fmt.Sprintf("%s: %s", n.id[:12], msg))
		if status == "error" {
			failed++
		}
	}

	// Then: update parents (network providers)
	for _, n := range parents {
		p := models.CommandPayload{Params: map[string]interface{}{"container_id": n.id}}
		status, msg := a.dockerUpdate(ctx, p)
		results = append(results, fmt.Sprintf("%s: %s", n.id[:12], msg))
		if status == "error" {
			failed++
		}
	}

	a.clearFreshnessCache()
	a.triggerTelemetry()

	summary := fmt.Sprintf("bulk update: %d/%d succeeded\n%s", len(nodes)-failed, len(nodes), strings.Join(results, "\n"))
	if failed > 0 {
		return "error", summary
	}
	return "success", summary
}

// triggerTelemetry signals the telemetry loop to send immediately so the
// dashboard reflects updated container state without waiting for the next cycle.
func (a *Agent) triggerTelemetry() {
	select {
	case a.telemetryNow <- struct{}{}:
	default:
		// Already pending — no need to queue another
	}
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

// isComposeValidationError checks if compose output indicates a file validation error
// (e.g. "services.deploy additional properties 'resources' not allowed").
func isComposeValidationError(output string) bool {
	return strings.Contains(output, "validating") && strings.Contains(output, "not allowed")
}

// sanitizeComposeFile creates a temporary copy of the compose file in workDir with
// problematic keys removed (version, deploy sections) so that Docker Compose doesn't
// reject files with Swarm-only config in non-Swarm environments.
func sanitizeComposeFile(workDir string) (string, error) {
	// Find compose file — Docker Compose checks these names in order
	var composeFile string
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		candidate := filepath.Join(workDir, name)
		if _, err := os.Stat(candidate); err == nil {
			composeFile = candidate
			break
		}
	}
	if composeFile == "" {
		return "", fmt.Errorf("no compose file found in %s", workDir)
	}

	data, err := os.ReadFile(composeFile)
	if err != nil {
		return "", err
	}

	var compose map[string]interface{}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return "", fmt.Errorf("parse compose file: %w", err)
	}

	// Remove version key — without it, Docker Compose uses compose-spec which is more lenient
	delete(compose, "version")

	// Remove deploy sections from all services — these are Swarm-only and cause
	// validation errors in non-Swarm Docker Compose environments
	if services, ok := compose["services"].(map[string]interface{}); ok {
		for svcName, svc := range services {
			if svcMap, ok := svc.(map[string]interface{}); ok {
				delete(svcMap, "deploy")
				services[svcName] = svcMap
			}
		}
	}

	out, err := yaml.Marshal(compose)
	if err != nil {
		return "", fmt.Errorf("marshal sanitized compose: %w", err)
	}

	tmpFile := filepath.Join(workDir, ".compose.riot-sanitized.yml")
	if err := os.WriteFile(tmpFile, out, 0644); err != nil {
		return "", err
	}

	slog.Info("docker_update: created sanitized compose file", "original", composeFile, "sanitized", tmpFile)
	return tmpFile, nil
}
