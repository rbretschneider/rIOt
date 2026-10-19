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
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
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

	// Test seams: the clock and the manifest-list lookup. Nil = real.
	now             func() time.Time
	manifestInspect func(ctx context.Context, ref string) ([]byte, error)
}

// clockNow returns the collector's clock (injectable for the negative-TTL tests).
func (c *DockerCollector) clockNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

const freshnessCacheTTL = 30 * time.Minute

// freshnessFailedTTL is how long a failed registry check is cached (FR-051).
const freshnessFailedTTL = 5 * time.Minute

// Registry comparison results cached per image reference.
const (
	resultNewer  = "newer"  // the registry has a different (newer) image than the local tag
	resultSame   = "same"   // the registry digest matches the local tag
	resultFailed = "failed" // the registry check failed
)

// perCallTimeout is the per-call timeout applied to every individual Docker
// API call and subprocess invocation (FR-019).
const perCallTimeout = 10 * time.Second

// Worker pool size limits (FR-002, FR-006, FR-010).
const (
	statsWorkerLimit       = 10
	freshnessWorkerLimit   = 5
	networkModeWorkerLimit = 10
)

// imageFreshness caches the registry comparison for one image reference
// (AD-016). LocalImageID is the image ID the tag resolved to when the check ran:
// the entry is only reused while the tag still resolves to it. Fields are
// exported so encoding/json can serialize them for disk persistence (AD-004).
type imageFreshness struct {
	LocalImageID string    `json:"local_image_id"`
	Result       string    `json:"result"`
	CheckedAt    time.Time `json:"checked_at"`
}

// ttl is how long the entry stays valid: 30m, or 5m for a failed check.
func (e *imageFreshness) ttl() time.Duration {
	if e.Result == resultFailed {
		return freshnessFailedTTL
	}
	return freshnessCacheTTL
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
		c.checkImageUpdates(ctx, sdkFreshness{cli}, info.Containers)
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
		ImageID:  c.ImageID,
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
				containers[idx].UsesGPU = usesGPU(inspect.HostConfig)
			}
			if inspect.State != nil && inspect.State.Health != nil {
				containers[idx].HealthStatus = inspect.State.Health.Status
			}
		}(i)
	}

	wg.Wait()
}

// usesGPU reports whether a container's HostConfig requests GPU access
// (PATCH-GATE FR-028, AD-012): a DeviceRequest with the nvidia driver or a
// "gpu" capability (covers --gpus all and compose device_requests), or a
// host device mapping under /dev/nvidia*, /dev/dri, or /dev/kfd (AMD/ROCm
// and legacy NVIDIA mappings). Pure — reads fields the collector already
// fetches, zero extra Docker API calls.
func usesGPU(hc *container.HostConfig) bool {
	if hc == nil {
		return false
	}
	for _, req := range hc.DeviceRequests {
		if req.Driver == "nvidia" {
			return true
		}
		for _, capSet := range req.Capabilities {
			for _, capability := range capSet {
				if capability == "gpu" {
					return true
				}
			}
		}
	}
	for _, dev := range hc.Devices {
		if strings.HasPrefix(dev.PathOnHost, "/dev/nvidia") ||
			strings.HasPrefix(dev.PathOnHost, "/dev/dri") ||
			strings.HasPrefix(dev.PathOnHost, "/dev/kfd") {
			return true
		}
	}
	return false
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
				containers[idx].UsesGPU = usesGPU(inspect.HostConfig)
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

// InvalidateFreshness drops the cache entries for the given image references
// only, in memory and on disk, so the next telemetry cycle re-checks exactly the
// images an update touched while every other verdict stays cached (FR-052).
func (c *DockerCollector) InvalidateFreshness(refs ...string) {
	c.cacheOnce.Do(func() { c.loadFreshnessCache() })
	c.cacheMu.Lock()
	for _, ref := range refs {
		delete(c.digestCache, ref)
	}
	c.cacheMu.Unlock()
	c.saveFreshnessCache()
}

// freshnessAPI is the part of the Docker client the freshness check uses. Tests
// supply a fake registry and local image store.
type freshnessAPI interface {
	DistributionInspect(ctx context.Context, imageRef, encodedRegistryAuth string) (registry.DistributionInspect, error)
	ImageInspect(ctx context.Context, imageRef string) (image.InspectResponse, error)
}

// sdkFreshness adapts *client.Client to freshnessAPI (the SDK's ImageInspect is
// variadic).
type sdkFreshness struct{ *client.Client }

func (s sdkFreshness) ImageInspect(ctx context.Context, imageRef string) (image.InspectResponse, error) {
	return s.Client.ImageInspect(ctx, imageRef)
}

// isPinnedRef reports whether an image reference is immutable: a bare sha256 ID
// or a reference containing @sha256: (FR-050).
func isPinnedRef(ref string) bool {
	return strings.HasPrefix(ref, "sha256:") || strings.Contains(ref, "@sha256:")
}

// refVerdict is what one image reference resolved to this cycle: the image ID
// its tag currently has locally and the cached or fresh registry comparison.
type refVerdict struct {
	localID     string
	result      string // resultNewer, resultSame or resultFailed
	localFailed bool
}

// checkImageUpdates computes a freshness verdict per running container (AD-016).
// Registry lookups are cached per image reference (30m, or 5m when the check
// failed), so containers on the same tag cost one lookup but can still get
// different verdicts: the verdict also compares the container's own image ID
// with the image the tag now resolves to locally. Uses a bounded worker pool
// (FR-005, FR-006). Each individual API call has a per-call timeout (FR-007,
// FR-008, FR-019). A failure for one image does not affect others (FR-018).
func (c *DockerCollector) checkImageUpdates(ctx context.Context, api freshnessAPI, containers []models.ContainerInfo) {
	// Load the on-disk cache exactly once across all Collect() calls (AD-006).
	c.cacheOnce.Do(func() { c.loadFreshnessCache() })

	byRef := map[string][]int{}
	var refs []string
	for i := range containers {
		if containers[i].State != "running" || containers[i].Image == "" {
			continue
		}
		ref := containers[i].Image
		if isPinnedRef(ref) {
			setPinned(&containers[i])
			continue
		}
		if _, ok := byRef[ref]; !ok {
			refs = append(refs, ref)
		}
		byRef[ref] = append(byRef[ref], i)
	}

	verdicts := make(map[string]refVerdict, len(refs))
	var vmu sync.Mutex
	sem := make(chan struct{}, freshnessWorkerLimit)
	var wg sync.WaitGroup
	for _, ref := range refs {
		wg.Add(1)
		go func(ref string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			v := c.verdictForRef(ctx, api, ref)
			vmu.Lock()
			verdicts[ref] = v
			vmu.Unlock()
		}(ref)
	}
	wg.Wait()

	for ref, idxs := range byRef {
		for _, i := range idxs {
			applyVerdict(&containers[i], verdicts[ref])
		}
	}

	// Persist the updated cache to disk once after the entire phase completes
	// (AD-003, section 7.6).
	c.saveFreshnessCache()
}

func setPinned(ci *models.ContainerInfo) {
	f := false
	ci.UpdateStatus, ci.UpdateReason, ci.UpdateAvailable = models.UpdateStatusPinned, "", &f
}

// applyVerdict sets UpdateStatus, UpdateReason and the legacy UpdateAvailable
// consistently: true only for update_available, false for up_to_date, nil for
// check_failed.
func applyVerdict(ci *models.ContainerInfo, v refVerdict) {
	yes, no := true, false
	ci.UpdateReason = ""
	switch {
	case v.localFailed:
		ci.UpdateStatus, ci.UpdateAvailable = models.UpdateStatusFailed, nil
	case ci.ImageID != "" && ci.ImageID != v.localID:
		ci.UpdateStatus, ci.UpdateReason, ci.UpdateAvailable = models.UpdateStatusAvailable, models.UpdateReasonPulledNotRecreated, &yes
	case v.result == resultNewer:
		ci.UpdateStatus, ci.UpdateReason, ci.UpdateAvailable = models.UpdateStatusAvailable, models.UpdateReasonRegistryNewer, &yes
	case v.result == resultSame:
		ci.UpdateStatus, ci.UpdateAvailable = models.UpdateStatusUpToDate, &no
	default:
		ci.UpdateStatus, ci.UpdateAvailable = models.UpdateStatusFailed, nil
	}
}

// verdictForRef inspects the tag's local image once and returns the registry
// comparison, from the cache when it is still valid for this local image.
func (c *DockerCollector) verdictForRef(ctx context.Context, api freshnessAPI, ref string) refVerdict {
	localCtx, cancel := context.WithTimeout(ctx, perCallTimeout)
	local, err := api.ImageInspect(localCtx, ref)
	cancel()
	if err != nil {
		slog.Debug("image freshness: local inspect failed", "image", ref, "error", err)
		return refVerdict{localFailed: true}
	}

	now := c.clockNow()
	c.cacheMu.RLock()
	cached, ok := c.digestCache[ref]
	c.cacheMu.RUnlock()
	if ok && cached.LocalImageID == local.ID && now.Sub(cached.CheckedAt) < cached.ttl() {
		return refVerdict{localID: local.ID, result: cached.Result}
	}

	regCtx, cancel := context.WithTimeout(ctx, perCallTimeout)
	result := c.registryResult(regCtx, api, ref, local)
	cancel()

	c.cacheMu.Lock()
	if c.digestCache == nil {
		c.digestCache = make(map[string]*imageFreshness)
	}
	c.digestCache[ref] = &imageFreshness{LocalImageID: local.ID, Result: result, CheckedAt: now}
	c.cacheMu.Unlock()
	return refVerdict{localID: local.ID, result: result}
}

// registryResult compares the registry digest for ref with the local image's
// digests. A failed lookup is resultFailed, never "same" (FR-051).
func (c *DockerCollector) registryResult(ctx context.Context, api freshnessAPI, ref string, local image.InspectResponse) string {
	// Query registry for remote digest (empty auth works for public registries)
	distInfo, err := api.DistributionInspect(ctx, ref, "")
	if err != nil {
		slog.Debug("image freshness: registry check failed", "image", ref, "error", err)
		return resultFailed
	}
	remoteDigest := distInfo.Descriptor.Digest.String()

	// Strategy 1: Direct comparison — RepoDigests contains "repo@sha256:..." strings.
	// This works when the registry manifest digest matches the pulled image digest
	// (single-platform images built without provenance attestations).
	for _, rd := range local.RepoDigests {
		if atIdx := strings.LastIndex(rd, "@"); atIdx != -1 {
			if rd[atIdx+1:] == remoteDigest {
				return resultSame
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
		return resultNewer
	}

	// Strategy 2: Manifest list fallback — images built with buildx (provenance: true)
	// wrap the image in an OCI index whose digest differs from the per-platform
	// image manifest digest stored in RepoDigests.
	if resolved := c.checkViaManifestInspect(ctx, ref, local.RepoDigests); resolved != "" {
		return resolved
	}

	// Manifest list but couldn't resolve sub-manifests — trust the mismatch.
	return resultNewer
}

// checkViaManifestInspect runs `docker manifest inspect` to get the full
// manifest list contents, then checks if any per-platform sub-manifest digest
// matches a local RepoDigest entry. It returns "" if the command fails or the
// output can't be parsed (the caller falls back).
func (c *DockerCollector) checkViaManifestInspect(ctx context.Context, imageRef string, repoDigests []string) string {
	inspect := c.manifestInspect
	if inspect == nil {
		inspect = func(ctx context.Context, ref string) ([]byte, error) {
			return exec.CommandContext(ctx, "docker", "manifest", "inspect", ref).Output()
		}
	}
	out, err := inspect(ctx, imageRef)
	if err != nil {
		return ""
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
		return ""
	}
	if len(idx.Manifests) == 0 {
		return ""
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
			return resultSame // a platform manifest matches local — up to date
		}
	}
	return resultNewer // manifest list exists but no sub-manifest matches — genuine update
}
