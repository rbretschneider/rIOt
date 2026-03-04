package agent

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// dockerEventLoop watches Docker events and pushes them to the server.
// It reconnects with backoff on disconnects.
func (a *Agent) dockerEventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		a.watchDockerEvents(ctx)

		// Backoff before reconnect
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			slog.Info("reconnecting to Docker events stream")
		}
	}
}

func (a *Agent) watchDockerEvents(ctx context.Context) {
	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		slog.Debug("docker events: cannot connect", "error", err)
		return
	}
	defer cli.Close()

	// Only watch container events
	f := filters.NewArgs()
	f.Add("type", "container")

	msgCh, errCh := cli.Events(ctx, events.ListOptions{Filters: f})

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgCh:
			a.handleDockerEvent(ctx, msg)
		case err := <-errCh:
			if err != nil {
				slog.Warn("docker events stream error", "error", err)
			}
			return
		}
	}
}

func (a *Agent) handleDockerEvent(ctx context.Context, msg events.Message) {
	action := string(msg.Action)
	// Only forward interesting actions
	switch action {
	case "start", "stop", "die", "create", "destroy", "oom", "pause", "unpause":
	default:
		return
	}

	containerName := msg.Actor.Attributes["name"]
	image := msg.Actor.Attributes["image"]

	evt := &models.DockerEvent{
		ContainerID:   msg.Actor.ID,
		ContainerName: containerName,
		Action:        action,
		Image:         image,
	}

	deviceID := a.config.Agent.DeviceID
	if deviceID == "" {
		return
	}

	if err := a.client.SendDockerEvent(ctx, deviceID, evt); err != nil {
		slog.Warn("failed to send docker event", "action", action, "container", containerName, "error", err)
	}
}

func newDockerClient(socketPath string) (*client.Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if socketPath != "" {
		host := "unix://" + socketPath
		if runtime.GOOS == "windows" {
			host = "npipe://" + socketPath
		}
		opts = append(opts, client.WithHost(host))
	}
	return client.NewClientWithOpts(opts...)
}

// isDockerAvailable checks if Docker is reachable.
func isDockerAvailable(socketPath string) bool {
	cli, err := newDockerClient(socketPath)
	if err != nil {
		return false
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = cli.Ping(ctx)
	return err == nil
}

// shouldEnableDocker determines if Docker features should be enabled based on config.
func shouldEnableDocker(cfg DockerConfig) bool {
	switch strings.ToLower(cfg.Enabled) {
	case "true":
		return true
	case "false":
		return false
	default: // "auto"
		return isDockerAvailable(cfg.SocketPath)
	}
}
