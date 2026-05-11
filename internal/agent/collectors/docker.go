package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DockerCollector collects container and daemon info via the Docker SDK.
type DockerCollector struct {
	CollectStats bool
	SocketPath   string
	CheckUpdates bool
	CachePath    string

	// Image freshness cache
	cacheMu     sync.RWMutex
	digestCache map[string]*imageFreshness
	cacheOnce   sync.Once
}

const freshnessCacheTTL = 30 * time.Minute

// perCallTimeout is the per-call timeout applied to every individual Docker
// API call and subprocess invocation (FR-019).
const perCallTimeout = 10 * time.Second

// Worker pool size limits (FR-002, FR-006, FR-010).
const (
	statsWorkerLimit       = 10
	freshnessWorkerLimit   = 5
	networkModeWorkerLimit = 10
)

// imageFreshness caches the result of a single image freshness check.
// Fields are exported so encoding/json can serialize them for disk persistence
// (AD-004). The struct remains unexported; only its fields are exported.
type imageFreshness struct {
	UpdateAvailable *bool     `json:"update_available"`
	CheckedAt       time.Time `json:"checked_at"`
}

func (c *DockerCollector) Name() string { return "docker" }

func (c *DockerCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.DockerInfo{}

	cli, err := c.newClient()
	if err != nil {
		// Docker not available — graceful degradation
		return info, nil
	}
	defer cli.Close()

	// Ping to verify daemon is running
	if _, err := cli.Ping(ctx); err != nil {
		return info, nil
	}
	info.Available = true

	// Daemon info
	sysInfo, err := cli.Info(ctx)
	if err == nil {
		fillDaemonInfo(info, sysInfo)
	}

	// Docker version
	ver, err := cli.ServerVersion(ctx)
	if err == nil {
		info.Version = ver.Version
		info.APIVersion = ver.APIVersion
	}

	// List all containers
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		slog.Warn("docker: failed to list containers", "error", err)
		return info, nil
	}

	info.TotalContainers = len(containers)
	for _, c := range containers {
		ci := containerFromAPI(c)
		switch ci.State {
		case "running":
			info.Running++
		case "paused":
			info.Paused++
		default:
			info.Stopped++
		}
		info.Containers = append(info.Containers, ci)
	}

	// Phase 1: Collect CPU/mem stats for running containers (BR-004)
	if c.CollectStats {
		c.collectStats(ctx, cli, info.Containers)
	}

	// Phase 2: Check for image updates (BR-004)
	if c.CheckUpdates {
		c.checkImageUpdates(ctx, cli, info.Containers)
	}

	// Phase 3: Collect network modes for dependency awareness (BR-004)
	c.collectNetworkModes(ctx, cli, info.Containers)

	return info, nil
}

func (c *DockerCollector) newClient() (*client.Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if c.SocketPath != "" {
		host := "unix://" + c.SocketPath
		if runtime.GOOS == "windows" {
			host = "npipe://" + c.SocketPath
		}
		opts = append(opts, client.WithHost(host))
	}
	return client.NewClientWithOpts(opts...)
}

func fillDaemonInfo(info *models.DockerInfo, sys system.Info) {
	info.ImagesTotal = sys.Images
	info.StorageDriver = sys.Driver
	info.DockerRootDir = sys.DockerRootDir
}

func containerFromAPI(c container.Summary) models.ContainerInfo {
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	shortID := c.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	// Deduplicate port mappings
	seen := make(map[string]struct{})
	var ports []models.PortMapping
	for _, p := range c.Ports {
		if p.PublicPort == 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		ports = append(ports, models.PortMapping{
			ContainerPort: portStr(p.PrivatePort, p.Type),
			HostPort:      portStr(p.PublicPort, ""),
			Protocol:      p.Type,
			HostIP:        p.IP,
		})
	}

	// Parse mounts
	var mounts []models.MountInfo
	for _, m := range c.Mounts {
		mounts = append(mounts, models.MountInfo{
			Type:        string(m.Type),
			Source:      m.Source,
			Destination: m.Destination,
			ReadOnly:    !m.RW,
		})
	}

	// Parse networks
	var networks []models.NetworkAttach
	if c.NetworkSettings != nil {
		for netName, net := range c.NetworkSettings.Networks {
			networks = append(networks, models.NetworkAttach{
				Name:      netName,
				IPAddress: net.IPAddress,
				Gateway:   net.Gateway,
				MacAddr:   net.MacAddress,
			})
		}
	}

	riot := ParseRiotLabels(c.Labels)
	repoURL := InferRepoURL(c.Labels, c.Image)

	ci := models.ContainerInfo{
		ID:       c.ID,
		ShortID:  shortID,
		Name:     name,
		Image:    c.Image,
		State:    c.State,
		Status:   c.Status,
		Created:  c.Created,
		Ports:    ports,
		Labels:   c.Labels,
		Mounts:   mounts,
		Networks: networks,
		RepoURL:  repoURL,
		Riot:     riot,
	}

	return ci
}

// collectStats gathers CPU/mem for running containers using a bounded worker
// pool (FR-001, FR-002). Each Docker API call has its own per-call timeout
// (FR-003, FR-004, FR-019). A failure for one container does not affect others
// (FR-018).
func (c *DockerCollector) collectStats(ctx context.Context, cli *client.Client, containers []models.ContainerInfo) {
	sem := make(chan struct{}, statsWorkerLimit)
	var wg sync.WaitGroup

	for i := range containers {
		if containers[i].State != "running" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			callCtx, cancel := context.WithTimeout(ctx, perCallTimeout)
			defer cancel()

			resp, err := cli.ContainerStats(callCtx, containers[idx].ID, false)
			if err != nil {
				slog.Debug("docker: ContainerStats failed", "container", containers[idx].ID, "error", err)
				return
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || len(body) == 0 {
				return
			}

			var stats struct {
				Read    time.Time `json:"read"`
				PreRead time.Time `json:"preread"`
				CPUStats struct {
					CPUUsage struct {
						TotalUsage uint64 `json:"total_usage"`
					} `json:"cpu_usage"`
					SystemCPUUsage uint64 `json:"system_cpu_usage"`
					OnlineCPUs     uint32 `json:"online_cpus"`
				} `json:"cpu_stats"`
				PreCPUStats struct {
					CPUUsage struct {
						TotalUsage uint64 `json:"total_usage"`
					} `json:"cpu_usage"`
					SystemCPUUsage uint64 `json:"system_cpu_usage"`
				} `json:"precpu_stats"`
				MemoryStats struct {
					Usage uint64 `json:"usage"`
					Limit uint64 `json:"limit"`
					Stats struct {
						Cache        uint64 `json:"cache"`         // cgroup v1
						InactiveFile uint64 `json:"inactive_file"` // cgroup v2
					} `json:"stats"`
				} `json:"memory_stats"`
			}
			if err := json.Unmarshal(body, &stats); err != nil {
				return
			}

			// CPU percent calculation — use wall-clock time delta from Docker's
			// read/preread timestamps instead of system_cpu_usage, which has unit
			// inconsistencies on cgroup v2 hosts that inflate readings by 10-100x.
			cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
			wallDelta := stats.Read.Sub(stats.PreRead).Nanoseconds()
			if wallDelta > 0 && cpuDelta > 0 {
				containers[idx].CPUPercent = (cpuDelta / float64(wallDelta)) * 100.0
			}

			// Subtract filesystem cache from memory usage to match docker stats.
			// cgroup v1 reports "cache", cgroup v2 reports "inactive_file".
			cacheBytes := stats.MemoryStats.Stats.Cache
			if cacheBytes == 0 {
				cacheBytes = stats.MemoryStats.Stats.InactiveFile
			}
			containers[idx].MemUsage = int64(stats.MemoryStats.Usage - cacheBytes)
			containers[idx].MemLimit = int64(stats.MemoryStats.Limit)

			// Read CPU limit (NanoCPUs) and network mode from container inspect.
			// Use the same callCtx so this call is also bounded by perCallTimeout.
			inspect, err := cli.ContainerInspect(callCtx, containers[idx].ID)
			if err != nil {
				slog.Debug("docker: ContainerInspect failed in stats phase", "container", containers[idx].ID, "error", err)
				return
			}
			if inspect.HostConfig != nil && inspect.HostConfig.NanoCPUs > 0 {
				containers[idx].CPULimit = inspect.HostConfig.NanoCPUs
			}
			if inspect.HostConfig != nil {
				containers[idx].NetworkMode = string(inspect.HostConfig.NetworkMode)
			}
			if inspect.State != nil && inspect.State.Health != nil {
				containers[idx].HealthStatus = inspect.State.Health.Status
			}
		}(i)
	}

	wg.Wait()
}

func portStr(port uint16, proto string) string {
	if port == 0 {
		return ""
	}
	if proto != "" {
		return fmt.Sprintf("%d/%s", port, proto)
	}
	return fmt.Sprintf("%d", port)
}

// collectNetworkModes reads HostConfig.NetworkMode for containers that did not
// already have their NetworkMode set by collectStats. Uses a bounded worker
// pool (FR-009, FR-010). Each ContainerInspect call has a per-call timeout
// (FR-011, FR-019). A failure for one container does not affect others (FR-018).
func (c *DockerCollector) collectNetworkModes(ctx context.Context, cli *client.Client, containers []models.ContainerInfo) {
	sem := make(chan struct{}, networkModeWorkerLimit)
	var wg sync.WaitGroup

	for i := range containers {
		// Running containers already have NetworkMode from collectStats inspect (AC-012)
		if containers[i].NetworkMode != "" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			callCtx, cancel := context.WithTimeout(ctx, perCallTimeout)
			defer cancel()

			inspect, err := cli.ContainerInspect(callCtx, containers[idx].ID)
			if err != nil {
				slog.Debug("docker: ContainerInspect failed in network mode phase", "container", containers[idx].ID, "error", err)
				return
			}
			if inspect.HostConfig != nil {
				containers[idx].NetworkMode = string(inspect.HostConfig.NetworkMode)
				if inspect.HostConfig.RestartPolicy.Name != "" {
					containers[idx].RestartPolicy = string(inspect.HostConfig.RestartPolicy.Name)
				}
			}
			if inspect.State != nil && inspect.State.Health != nil {
				containers[idx].HealthStatus = inspect.State.Health.Status
			}
		}(i)
	}

	wg.Wait()
}

// ClearFreshnessCache clears the image freshness cache, forcing the next
// telemetry cycle to re-check all images against their registries. Also removes
// the on-disk cache file so a restart does not reload stale entries (AD-006).
func (c *DockerCollector) ClearFreshnessCache() {
	c.cacheMu.Lock()
	c.digestCache = nil
	c.cacheMu.Unlock()

	if c.CachePath != "" {
		if err := removeFreshnessCacheFile(c.CachePath); err != nil {
			slog.Warn("docker: failed to remove freshness cache file", "path", c.CachePath, "error", err)
		}
	}
}

// checkImageUpdates queries registries to determine if running container images
// have newer versions available. Results are cached for 30 minutes in-memory
// and on disk. Uses a bounded worker pool (FR-005, FR-006). Each individual
// registry API call has a per-call timeout (FR-007, FR-008, FR-019). A failure
// for one image does not affect others (FR-018).
func (c *DockerCollector) checkImageUpdates(ctx context.Context, cli *client.Client, containers []models.ContainerInfo) {
	// Load the on-disk cache exactly once across all Collect() calls (AD-006).
	c.cacheOnce.Do(func() { c.loadFreshnessCache() })

	sem := make(chan struct{}, freshnessWorkerLimit)
	var wg sync.WaitGroup

	for i := range containers {
		if containers[i].State != "running" {
			continue
		}
		imageRef := containers[i].Image
		if imageRef == "" {
			continue
		}

		// Check in-memory cache before spawning a goroutine. Cache hits never
		// consume a semaphore slot (AD-001, section 7.3 note).
		c.cacheMu.RLock()
		if c.digestCache != nil {
			if cached, ok := c.digestCache[imageRef]; ok && time.Since(cached.CheckedAt) < freshnessCacheTTL {
				containers[i].UpdateAvailable = cached.UpdateAvailable
				c.cacheMu.RUnlock()
				continue
			}
		}
		c.cacheMu.RUnlock()

		wg.Add(1)
		go func(idx int, ref string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			callCtx, cancel := context.WithTimeout(ctx, perCallTimeout)
			defer cancel()

			result := c.checkSingleImage(callCtx, cli, ref)

			c.cacheMu.Lock()
			if c.digestCache == nil {
				c.digestCache = make(map[string]*imageFreshness)
			}
			c.digestCache[ref] = &imageFreshness{
				UpdateAvailable: result,
				CheckedAt:       time.Now(),
			}
			c.cacheMu.Unlock()

			containers[idx].UpdateAvailable = result
		}(i, imageRef)
	}

	wg.Wait()

	// Persist the updated cache to disk once after the entire phase completes
	// (AD-003, section 7.6).
	c.saveFreshnessCache()
}

// checkSingleImage compares the local image digest with the remote registry digest.
// Returns nil if the check fails (unknown), *true if update available, *false if up to date.
func (c *DockerCollector) checkSingleImage(ctx context.Context, cli *client.Client, imageRef string) *bool {
	// Skip images pinned by digest — they are immutable
	if strings.Contains(imageRef, "@sha256:") {
		f := false
		return &f
	}

	// Query registry for remote digest (empty auth works for public registries)
	distInfo, err := cli.DistributionInspect(ctx, imageRef, "")
	if err != nil {
		slog.Debug("image freshness: registry check failed", "image", imageRef, "error", err)
		return nil // unknown
	}
	remoteDigest := distInfo.Descriptor.Digest.String()

	// Get local image digests
	localInfo, _, err := cli.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		slog.Debug("image freshness: local inspect failed", "image", imageRef, "error", err)
		return nil
	}

	// Strategy 1: Direct comparison — RepoDigests contains "repo@sha256:..." strings.
	// This works when the registry manifest digest matches the pulled image digest
	// (single-platform images built without provenance attestations).
	for _, rd := range localInfo.RepoDigests {
		if atIdx := strings.LastIndex(rd, "@"); atIdx != -1 {
			localDigest := rd[atIdx+1:]
			if localDigest == remoteDigest {
				f := false
				return &f // up to date
			}
		}
	}

	// If the remote is a plain image manifest (not a manifest list/OCI index),
	// the direct digest comparison is reliable — a mismatch means a genuine update.
	// The manifest list fallback is only needed for buildx provenance images where
	// the top-level digest is an index that wraps the actual image manifest.
	mt := string(distInfo.Descriptor.MediaType)
	if mt == "application/vnd.docker.distribution.manifest.v2+json" ||
		mt == "application/vnd.oci.image.manifest.v1+json" ||
		mt == "" {
		// Single image manifest — direct comparison is authoritative.
		t := true
		return &t // update available
	}

	// Strategy 2: Manifest list fallback — images built with buildx (provenance: true)
	// wrap the image in an OCI index whose digest differs from the per-platform
	// image manifest digest stored in RepoDigests.
	if resolved := c.checkViaManifestInspect(ctx, imageRef, localInfo.RepoDigests); resolved != nil {
		return resolved
	}

	// Manifest list but couldn't resolve sub-manifests — trust the mismatch.
	t := true
	return &t // update available
}

// checkViaManifestInspect shells out to `docker manifest inspect` to get the
// full manifest list contents, then checks if any per-platform sub-manifest
// digest matches a local RepoDigest entry. Returns nil if the command fails
// or the output can't be parsed (caller should fall back).
func (c *DockerCollector) checkViaManifestInspect(ctx context.Context, imageRef string, repoDigests []string) *bool {
	out, err := exec.CommandContext(ctx, "docker", "manifest", "inspect", imageRef).Output()
	if err != nil {
		return nil
	}

	var idx struct {
		MediaType string `json:"mediaType"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform *struct {
				Architecture string `json:"architecture"`
				OS           string `json:"os"`
			} `json:"platform,omitempty"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(out, &idx); err != nil {
		return nil
	}
	if len(idx.Manifests) == 0 {
		return nil
	}

	// Build set of local digests for fast lookup
	localDigests := make(map[string]bool, len(repoDigests))
	for _, rd := range repoDigests {
		if atIdx := strings.LastIndex(rd, "@"); atIdx != -1 {
			localDigests[rd[atIdx+1:]] = true
		}
	}

	// Check if any platform manifest in the index matches a local digest.
	// Skip attestation manifests (they have unknown/empty platform).
	for _, m := range idx.Manifests {
		if m.Platform != nil && m.Platform.Architecture == "unknown" {
			continue
		}
		if localDigests[m.Digest] {
			f := false
			return &f // a platform manifest matches local — up to date
		}
	}

	t := true
	return &t // manifest list exists but no sub-manifest matches — genuine update
}
