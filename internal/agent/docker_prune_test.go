package agent

import (
	"context"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"strings"
	"testing"
	"time"
)

type removalFake struct {
	*fakeDocker
	inspect         func(string) (image.InspectResponse, error)
	vol             volume.Volume
	net             network.Inspect
	removedImages   []string
	removedVolumes  []string
	removedNetworks []string
	t               *testing.T
}

func (f *removalFake) ImageInspect(ctx context.Context, ref string) (image.InspectResponse, error) {
	return f.inspect(ref)
}
func (f *removalFake) ImageRemove(ctx context.Context, ref string, opts image.RemoveOptions) ([]image.DeleteResponse, error) {
	if opts.Force || opts.PruneChildren {
		f.t.Fatal("remove must never force or prune children")
	}
	f.removedImages = append(f.removedImages, ref)
	return nil, nil
}
func (f *removalFake) VolumeInspect(context.Context, string) (volume.Volume, error) {
	return f.vol, nil
}
func (f *removalFake) VolumeRemove(ctx context.Context, name string, force bool) error {
	if force {
		f.t.Fatal("volume removal must not force")
	}
	f.removedVolumes = append(f.removedVolumes, name)
	return nil
}
func (f *removalFake) NetworkInspect(context.Context, string, network.InspectOptions) (network.Inspect, error) {
	return f.net, nil
}
func (f *removalFake) NetworkRemove(ctx context.Context, id string) error {
	f.removedNetworks = append(f.removedNetworks, id)
	return nil
}
func TestDUX_SEC007_TagMovedToOtherImageNotRemoved(t *testing.T) {
	h := newDockerHarness(t)
	id := "sha256:" + strings.Repeat("a", 64)
	f := &removalFake{fakeDocker: h.docker, t: t}
	f.inspect = func(ref string) (image.InspectResponse, error) {
		if ref == id {
			return image.InspectResponse{ID: id, RepoTags: []string{"app:latest"}}, nil
		}
		return image.InspectResponse{ID: "sha256:" + strings.Repeat("b", 64)}, nil
	}
	h.a.removalFactory = func() (dockerRemovalAPI, error) { return f, nil }
	res := h.run(idA, "docker_remove_items", map[string]interface{}{"item_kind": "image", "items": []interface{}{map[string]interface{}{"id": id, "tags": []string{"app:latest"}}}})
	if len(f.removedImages) != 0 || res.Status != "error" {
		t.Fatalf("moved tag removed: %+v %+v", res, f.removedImages)
	}
}
func TestDUX_SEC007_OnlyExactPreviewImageTagsRemoved(t *testing.T) {
	h := newDockerHarness(t)
	id := "sha256:" + strings.Repeat("a", 64)
	f := &removalFake{fakeDocker: h.docker, t: t}
	f.inspect = func(ref string) (image.InspectResponse, error) {
		return image.InspectResponse{ID: id, RepoTags: []string{"app:latest"}}, nil
	}
	h.a.removalFactory = func() (dockerRemovalAPI, error) { return f, nil }
	res := h.run(idA, "docker_remove_items", map[string]interface{}{"item_kind": "image", "items": []interface{}{map[string]interface{}{"id": id, "tags": []string{"app:latest"}}}})
	if res.Status != "success" || len(f.removedImages) != 2 || f.removedImages[0] != "app:latest" || f.removedImages[1] != id {
		t.Fatalf("unexpected image removal %+v %+v", res, f.removedImages)
	}
}
func TestDUX_SEC007_RecreatedVolumeNotRemoved(t *testing.T) {
	h := newDockerHarness(t)
	f := &removalFake{fakeDocker: h.docker, t: t, vol: volume.Volume{Name: "data", CreatedAt: "2026-10-19T00:00:00Z"}}
	h.a.removalFactory = func() (dockerRemovalAPI, error) { return f, nil }
	res := h.run(idA, "docker_remove_items", map[string]interface{}{"item_kind": "volume", "items": []interface{}{map[string]interface{}{"name": "data", "created_at": "2026-10-18T00:00:00Z"}}})
	if res.Status != "error" || len(f.removedVolumes) > 0 {
		t.Fatal("recreated volume removed")
	}
}
func TestDUX_SEC007_IngressNetworkNotRemoved(t *testing.T) {
	h := newDockerHarness(t)
	id := strings.Repeat("a", 64)
	stamp := time.Now().UTC()
	f := &removalFake{fakeDocker: h.docker, t: t, net: network.Inspect{ID: id, Name: "ingress", Ingress: true, Created: stamp}}
	h.a.removalFactory = func() (dockerRemovalAPI, error) { return f, nil }
	res := h.run(idA, "docker_remove_items", map[string]interface{}{"item_kind": "network", "items": []interface{}{map[string]interface{}{"id": id, "created_at": stamp}}})
	if res.Status != "error" || len(f.removedNetworks) > 0 {
		t.Fatal("ingress network removed")
	}
}
