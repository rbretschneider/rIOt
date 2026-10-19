package agent

import (
	"context"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
)

type dockerRemovalAPI interface {
	ContainerList(context.Context, container.ListOptions) ([]container.Summary, error)
	ImageInspect(context.Context, string) (image.InspectResponse, error)
	ImageRemove(context.Context, string, image.RemoveOptions) ([]image.DeleteResponse, error)
	VolumeInspect(context.Context, string) (volume.Volume, error)
	VolumeRemove(context.Context, string, bool) error
	NetworkInspect(context.Context, string, network.InspectOptions) (network.Inspect, error)
	NetworkRemove(context.Context, string) error
	Close() error
}

func (a *Agent) newRemovalDocker() (dockerRemovalAPI, error) {
	if a.removalFactory != nil {
		return a.removalFactory()
	}
	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		return nil, err
	}
	return sdkDocker{cli}, nil
}
