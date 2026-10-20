package collectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"sort"
	"strings"
	"sync"
	"time"
)

type DockerInventoryCollector struct {
	SocketPath string
	mu         sync.Mutex
	next       time.Time
	lastHash   string
	lastForced time.Time
	pending    *models.DockerInventory
}

func (c *DockerInventoryCollector) Name() string { return "docker_inventory" }
func (c *DockerInventoryCollector) Collect(ctx context.Context) (interface{}, error) {
	if !c.mu.TryLock() {
		return nil, nil
	}
	defer c.mu.Unlock()
	if c.pending != nil {
		out := c.pending
		c.pending = nil
		return out, nil
	}
	if time.Now().Before(c.next) {
		return nil, nil
	}
	out := &models.DockerInventory{CollectedAt: time.Now().UTC(), Images: []models.DockerImage{}, Volumes: []models.DockerVolume{}, Networks: []models.DockerNetwork{}, DiskUsage: map[string]models.DockerDiskUsage{}}
	cli, err := (&DockerCollector{SocketPath: c.SocketPath}).newClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	imagesUsed := map[string]int64{}
	volumesUsed := map[string][]string{}
	networksUsed := map[string][]string{}
	for _, ctr := range containers {
		imagesUsed[ctr.ImageID]++
		name := ctr.ID
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}
		for _, m := range ctr.Mounts {
			if m.Name != "" {
				volumesUsed[m.Name] = append(volumesUsed[m.Name], name)
			}
		}
		if ctr.NetworkSettings != nil {
			for _, n := range ctr.NetworkSettings.Networks {
				networksUsed[n.NetworkID] = append(networksUsed[n.NetworkID], name)
			}
		}
	}
	imgs, err := cli.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return nil, err
	}
	sort.Slice(imgs, func(i, j int) bool {
		if imgs[i].Created != imgs[j].Created {
			return imgs[i].Created > imgs[j].Created
		}
		return imgs[i].ID < imgs[j].ID
	})
	for i, img := range imgs {
		if i >= 2000 {
			out.Truncated = true
			out.TruncatedKinds.Images = true
			break
		}
		tags := img.RepoTags
		if len(tags) > 32 {
			tags = tags[:32]
			out.Truncated = true
		}
		digests := img.RepoDigests
		if len(digests) > 32 {
			digests = digests[:32]
			out.Truncated = true
		}
		out.Images = append(out.Images, models.DockerImage{ID: img.ID, Tags: tags, Digests: digests, Dangling: len(tags) == 0, Size: img.Size, CreatedAt: time.Unix(img.Created, 0).UTC(), InUse: imagesUsed[img.ID] > 0, Containers: imagesUsed[img.ID]})
		usage := out.DiskUsage["images"]
		usage.Total += max(img.Size, 0)
		if imagesUsed[img.ID] == 0 {
			usage.Reclaimable += max(img.Size, 0)
		}
		out.DiskUsage["images"] = usage
	}
	vols, err := cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	sort.Slice(vols.Volumes, func(i, j int) bool { return vols.Volumes[i].Name < vols.Volumes[j].Name })
	for i, v := range vols.Volumes {
		if i >= 2000 {
			out.Truncated = true
			out.TruncatedKinds.Volumes = true
			break
		}
		used := volumesUsed[v.Name]
		sort.Strings(used)
		out.Volumes = append(out.Volumes, models.DockerVolume{Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint, Project: v.Labels["com.docker.compose.project"], UsedBy: used, CreatedAt: v.CreatedAt, InUse: len(used) > 0, Anonymous: len(v.Name) == 64, Size: -1})
	}
	nets, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}
	sort.Slice(nets, func(i, j int) bool { return nets[i].Name < nets[j].Name })
	for i, n := range nets {
		if i >= 500 {
			out.Truncated = true
			out.TruncatedKinds.Networks = true
			break
		}
		used := networksUsed[n.ID]
		sort.Strings(used)
		entry := models.DockerNetwork{ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, CreatedAt: n.Created, Containers: used, Internal: n.Internal, InUse: len(used) > 0, Builtin: n.Name == "bridge" || n.Name == "host" || n.Name == "none" || n.Ingress}
		for _, subnet := range n.IPAM.Config {
			if len(entry.Subnets) >= 32 {
				break
			}
			entry.Subnets = append(entry.Subnets, models.DockerSubnet{Subnet: subnet.Subnet, Gateway: subnet.Gateway})
		}
		out.Networks = append(out.Networks, entry)
	}
	diskCtx, diskCancel := context.WithTimeout(ctx, 15*time.Second)
	usage, err := cli.DiskUsage(diskCtx, types.DiskUsageOptions{Types: []types.DiskUsageObject{types.ImageObject, types.VolumeObject, types.BuildCacheObject}})
	diskCancel()
	if err == nil {
		out.SizesAvailable = true
		volSizes := map[string]int64{}
		volUsage := models.DockerDiskUsage{}
		for _, v := range usage.Volumes {
			if v.UsageData != nil && v.UsageData.Size >= 0 {
				volSizes[v.Name] = v.UsageData.Size
				volUsage.Total += v.UsageData.Size
				if len(volumesUsed[v.Name]) == 0 {
					volUsage.Reclaimable += v.UsageData.Size
				}
			}
		}
		for i := range out.Volumes {
			if size, ok := volSizes[out.Volumes[i].Name]; ok {
				out.Volumes[i].Size = size
			}
		}
		out.DiskUsage["volumes"] = volUsage
		buildUsage := models.DockerDiskUsage{}
		for _, cache := range usage.BuildCache {
			buildUsage.Total += max(cache.Size, 0)
			if !cache.InUse {
				buildUsage.Reclaimable += max(cache.Size, 0)
			}
		}
		out.DiskUsage["build_cache"] = buildUsage
	}
	stamp := out.CollectedAt
	out.CollectedAt = time.Time{}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	out.CollectedAt = stamp
	out.Hash = hash
	c.next = time.Now().Add(15 * time.Minute)
	if hash == c.lastHash {
		return &models.DockerInventory{CollectedAt: stamp, Hash: hash, Unchanged: true}, nil
	}
	c.lastHash = hash
	return out, nil
}
func (c *DockerInventoryCollector) ForceRefresh(ctx context.Context) error {
	c.mu.Lock()
	if time.Since(c.lastForced) < time.Minute {
		c.mu.Unlock()
		return fmt.Errorf("refresh too frequent")
	}
	c.lastForced = time.Now()
	c.next = time.Time{}
	c.mu.Unlock()
	result, err := c.Collect(ctx)
	if err != nil {
		return err
	}
	out, ok := result.(*models.DockerInventory)
	if !ok || out == nil {
		return fmt.Errorf("inventory collection is already running")
	}
	c.mu.Lock()
	c.pending = out
	c.mu.Unlock()
	return nil
}
func (c *DockerInventoryCollector) Refresh() { c.mu.Lock(); c.next = time.Time{}; c.mu.Unlock() }
