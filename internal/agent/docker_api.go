package agent

import (
	"context"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// dockerAPI is the subset of the Docker SDK the update, recovery and container
// control paths use. It lets tests drive them with a fake daemon.
type dockerAPI interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRename(ctx context.Context, containerID, newName string) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string) (container.CreateResponse, error)
	ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
	ImageInspect(ctx context.Context, imageRef string) (image.InspectResponse, error)
	Info(ctx context.Context) (system.Info, error)
	VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
	Close() error
}

// sdkDocker adapts *client.Client to dockerAPI. Two methods differ from the SDK
// signatures: ContainerCreate drops the platform argument (the agent never sets
// one, and naming its type would pull a transitive module into go.mod) and
// ImageInspect drops the variadic options.
type sdkDocker struct{ *client.Client }

var _ dockerAPI = sdkDocker{}

func (s sdkDocker) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string) (container.CreateResponse, error) {
	return s.Client.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, containerName)
}

func (s sdkDocker) ImageInspect(ctx context.Context, imageRef string) (image.InspectResponse, error) {
	return s.Client.ImageInspect(ctx, imageRef)
}

// newDocker returns a Docker API handle: the injected factory in tests, the
// real SDK client otherwise. The caller closes it.
func (a *Agent) newDocker() (dockerAPI, error) {
	if a.dockerFactory != nil {
		return a.dockerFactory()
	}
	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		return nil, err
	}
	return sdkDocker{cli}, nil
}

// newProc builds an unstarted child process: the injected factory in tests, a
// real exec.Cmd wrapper otherwise.
func (a *Agent) newProc(name string, args ...string) limitedProcess {
	if a.procFactory != nil {
		return a.procFactory(name, args...)
	}
	return newExecProcess(name, args...)
}

// pauseFor waits d unless ctx ends first, and reports whether the full wait
// elapsed. It is the verification-window wait; tests replace a.pause with an
// instant version.
func (a *Agent) pauseFor(ctx context.Context, d time.Duration) bool {
	if a.pause != nil {
		return a.pause(ctx, d)
	}
	return sleepContext(ctx, d)
}
