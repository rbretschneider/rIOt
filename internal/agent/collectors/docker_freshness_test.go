package collectors

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// fakeFreshness is a registry plus local image store for the freshness check.
type fakeFreshness struct {
	mu       sync.Mutex
	local    map[string]image.InspectResponse
	localErr map[string]error
	dist     map[string]registry.DistributionInspect
	distErr  map[string]error

	registryCalls atomic.Int32
	localCalls    atomic.Int32
}

func newFakeFreshness() *fakeFreshness {
	return &fakeFreshness{
		local:    map[string]image.InspectResponse{},
		localErr: map[string]error{},
		dist:     map[string]registry.DistributionInspect{},
		distErr:  map[string]error{},
	}
}

func (f *fakeFreshness) DistributionInspect(_ context.Context, ref, _ string) (registry.DistributionInspect, error) {
	f.registryCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.distErr[ref]; err != nil {
		return registry.DistributionInspect{}, err
	}
	return f.dist[ref], nil
}

func (f *fakeFreshness) ImageInspect(_ context.Context, ref string) (image.InspectResponse, error) {
	f.localCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.localErr[ref]; err != nil {
		return image.InspectResponse{}, err
	}
	return f.local[ref], nil
}

// setRef configures a tag: its local image ID and digest, and the digest the
// registry advertises for it.
func (f *fakeFreshness) setRef(ref, localID, localDigest, registryDigest string) {
	f.local[ref] = image.InspectResponse{ID: localID, RepoDigests: []string{"repo@" + localDigest}}
	d := registry.DistributionInspect{}
	setDigest(&d.Descriptor.Digest, registryDigest)
	d.Descriptor.MediaType = "application/vnd.docker.distribution.manifest.v2+json"
	f.dist[ref] = d
}

// setDigest assigns a string to a digest-typed field without importing the
// digest package.
func setDigest[T ~string](dst *T, s string) { *dst = T(s) }

func containerSummaryFor(imageID string) container.Summary {
	return container.Summary{ID: "abc", Names: []string{"/web"}, Image: "nginx:latest", ImageID: imageID, State: "running"}
}

func running(image, imageID string) models.ContainerInfo {
	return models.ContainerInfo{ID: "c-" + imageID, State: "running", Image: image, ImageID: imageID}
}

func newTestCollector(clk *time.Time) *DockerCollector {
	c := &DockerCollector{}
	c.cacheOnce.Do(func() {})
	if clk != nil {
		c.now = func() time.Time { return *clk }
	}
	return c
}

// AC-054 (FR-047, FR-048): a running image X with the tag now resolving to Y,
// and a registry digest equal to Y's, is update_available/pulled_not_recreated.
func TestCL_AC054_PulledNotRecreated(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dY")
	cs := []models.ContainerInfo{running("nginx:latest", "sha256:X")}

	newTestCollector(nil).checkImageUpdates(context.Background(), api, cs)

	if cs[0].UpdateStatus != models.UpdateStatusAvailable || cs[0].UpdateReason != models.UpdateReasonPulledNotRecreated {
		t.Fatalf("[AC-054] want update_available/pulled_not_recreated, got %q/%q", cs[0].UpdateStatus, cs[0].UpdateReason)
	}
	if cs[0].UpdateAvailable == nil || !*cs[0].UpdateAvailable {
		t.Errorf("[AC-054] the legacy UpdateAvailable flag must be true")
	}
}

// AC-055 (FR-047, FR-048): the registry digest differs from the local tag.
func TestCL_AC055_RegistryNewer(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dNEW")
	cs := []models.ContainerInfo{running("nginx:latest", "sha256:Y")}

	newTestCollector(nil).checkImageUpdates(context.Background(), api, cs)

	if cs[0].UpdateStatus != models.UpdateStatusAvailable || cs[0].UpdateReason != models.UpdateReasonRegistryNewer {
		t.Fatalf("[AC-055] want update_available/registry_newer, got %q/%q", cs[0].UpdateStatus, cs[0].UpdateReason)
	}
}

// AC-056 (FR-047): everything matches.
func TestCL_AC056_AllMatchUpToDate(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dY")
	cs := []models.ContainerInfo{running("nginx:latest", "sha256:Y")}

	newTestCollector(nil).checkImageUpdates(context.Background(), api, cs)

	if cs[0].UpdateStatus != models.UpdateStatusUpToDate || cs[0].UpdateReason != "" {
		t.Fatalf("[AC-056] want up_to_date with no reason, got %q/%q", cs[0].UpdateStatus, cs[0].UpdateReason)
	}
	if cs[0].UpdateAvailable == nil || *cs[0].UpdateAvailable {
		t.Errorf("[AC-056] the legacy UpdateAvailable flag must be false")
	}
}

// AC-057 (FR-049): two containers on one tag get different verdicts from one
// registry lookup.
func TestCL_AC057_SameTagDifferentVerdictsOneLookup(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dY")
	cs := []models.ContainerInfo{
		running("nginx:latest", "sha256:Y"), // recreated after the pull
		running("nginx:latest", "sha256:X"), // still on the old image
	}
	cs[1].ID = "c-other"

	newTestCollector(nil).checkImageUpdates(context.Background(), api, cs)

	if cs[0].UpdateStatus != models.UpdateStatusUpToDate {
		t.Errorf("[AC-057] the recreated container must be up_to_date, got %q", cs[0].UpdateStatus)
	}
	if cs[1].UpdateStatus != models.UpdateStatusAvailable || cs[1].UpdateReason != models.UpdateReasonPulledNotRecreated {
		t.Errorf("[AC-057] the stale container must be pulled_not_recreated, got %q/%q", cs[1].UpdateStatus, cs[1].UpdateReason)
	}
	if n := api.registryCalls.Load(); n != 1 {
		t.Errorf("[AC-057] exactly one registry lookup is expected for one tag, got %d", n)
	}
}

// AC-058 (FR-050): digest-pinned references make no registry call and are
// reported as pinned, never as update-available.
func TestCL_AC058_DigestRefsPinnedNoRegistryCall(t *testing.T) {
	api := newFakeFreshness()
	cs := []models.ContainerInfo{
		running("sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", "sha256:abcdef"),
		running("nginx@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "sha256:012345"),
	}
	cs[1].ID = "c-two"

	newTestCollector(nil).checkImageUpdates(context.Background(), api, cs)

	for i := range cs {
		if cs[i].UpdateStatus != models.UpdateStatusPinned {
			t.Errorf("[AC-058] container %d: want pinned, got %q", i, cs[i].UpdateStatus)
		}
		if cs[i].UpdateAvailable == nil || *cs[i].UpdateAvailable {
			t.Errorf("[AC-058] container %d: pinned must not be flagged update-available", i)
		}
	}
	if api.registryCalls.Load() != 0 || api.localCalls.Load() != 0 {
		t.Errorf("[AC-058] no registry or image call may be made for pinned references (registry %d, local %d)",
			api.registryCalls.Load(), api.localCalls.Load())
	}
}

// AC-059 (FR-051): a registry error is check_failed, never up_to_date.
func TestCL_AC059_RegistryErrorIsCheckFailed(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dY")
	api.distErr["nginx:latest"] = errors.New("toomanyrequests: rate limit")
	cs := []models.ContainerInfo{running("nginx:latest", "sha256:Y")}

	newTestCollector(nil).checkImageUpdates(context.Background(), api, cs)

	if cs[0].UpdateStatus != models.UpdateStatusFailed {
		t.Fatalf("[AC-059] want check_failed, got %q", cs[0].UpdateStatus)
	}
	if cs[0].UpdateAvailable != nil {
		t.Errorf("[AC-059] a failed check must leave UpdateAvailable nil, got %v", *cs[0].UpdateAvailable)
	}
}

// AC-060 (FR-051): a failed check is cached for 5 minutes, not 30.
func TestCL_AC060_FailedCheckCachedFiveMinutes(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dY")
	api.distErr["nginx:latest"] = errors.New("registry unreachable")
	clk := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	c := newTestCollector(&clk)
	check := func() {
		cs := []models.ContainerInfo{running("nginx:latest", "sha256:Y")}
		c.checkImageUpdates(context.Background(), api, cs)
	}

	check() // T: the failure is recorded
	if n := api.registryCalls.Load(); n != 1 {
		t.Fatalf("[AC-060] first check: want 1 registry call, got %d", n)
	}
	clk = clk.Add(4 * time.Minute)
	check()
	if n := api.registryCalls.Load(); n != 1 {
		t.Errorf("[AC-060] T+4m must use the cached failure, got %d registry calls", n)
	}
	clk = clk.Add(2 * time.Minute)
	check()
	if n := api.registryCalls.Load(); n != 2 {
		t.Errorf("[AC-060] T+6m must call the registry again, got %d registry calls", n)
	}
}

// FR-051: a successful check keeps the 30-minute cache.
func TestCL_AC060_SuccessCachedThirtyMinutes(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dY")
	clk := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	c := newTestCollector(&clk)
	check := func() {
		c.checkImageUpdates(context.Background(), api, []models.ContainerInfo{running("nginx:latest", "sha256:Y")})
	}

	check()
	clk = clk.Add(29 * time.Minute)
	check()
	if n := api.registryCalls.Load(); n != 1 {
		t.Errorf("[AC-060] a success is cached for 30 minutes, got %d registry calls at +29m", n)
	}
	clk = clk.Add(2 * time.Minute)
	check()
	if n := api.registryCalls.Load(); n != 2 {
		t.Errorf("[AC-060] a success older than 30 minutes is re-checked, got %d registry calls at +31m", n)
	}
}

// FR-047(b): a manual pull inside the cache window is noticed, because the
// local tag is inspected every cycle and the cached verdict only applies to the
// local image it was computed for.
func TestCL_AC054_ManualPullInsideCacheWindowDetected(t *testing.T) {
	api := newFakeFreshness()
	api.setRef("nginx:latest", "sha256:X", "sha256:dX", "sha256:dX")
	clk := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	c := newTestCollector(&clk)
	cs := []models.ContainerInfo{running("nginx:latest", "sha256:X")}
	c.checkImageUpdates(context.Background(), api, cs)
	if cs[0].UpdateStatus != models.UpdateStatusUpToDate {
		t.Fatalf("[AC-054] precondition: up_to_date, got %q", cs[0].UpdateStatus)
	}

	// Someone runs `docker pull`: the tag now resolves to a new image.
	api.setRef("nginx:latest", "sha256:Y", "sha256:dY", "sha256:dY")
	clk = clk.Add(time.Minute)
	cs = []models.ContainerInfo{running("nginx:latest", "sha256:X")}
	c.checkImageUpdates(context.Background(), api, cs)

	if cs[0].UpdateReason != models.UpdateReasonPulledNotRecreated {
		t.Errorf("[AC-054] the pull must be detected within the cache window, got %q/%q", cs[0].UpdateStatus, cs[0].UpdateReason)
	}
}

// AC-061 (FR-052): invalidating one reference leaves the others, in memory and
// on disk.
func TestCL_AC061_InvalidateOnlyInvolvedRefs(t *testing.T) {
	path := makeTempCachePath(t)
	c := &DockerCollector{CachePath: path}
	c.cacheOnce.Do(func() {})
	now := time.Now()
	c.cacheMu.Lock()
	c.digestCache = map[string]*imageFreshness{
		"nginx:latest": {LocalImageID: "sha256:a", Result: resultSame, CheckedAt: now},
		"redis:7":      {LocalImageID: "sha256:b", Result: resultNewer, CheckedAt: now},
	}
	c.cacheMu.Unlock()
	c.saveFreshnessCache()

	c.InvalidateFreshness("nginx:latest")

	c.cacheMu.RLock()
	_, nginxInMemory := c.digestCache["nginx:latest"]
	_, redisInMemory := c.digestCache["redis:7"]
	c.cacheMu.RUnlock()
	if nginxInMemory || !redisInMemory {
		t.Errorf("[AC-061] in memory: nginx present=%v (want false), redis present=%v (want true)", nginxInMemory, redisInMemory)
	}

	onDisk := &DockerCollector{CachePath: path}
	onDisk.loadFreshnessCache()
	if _, ok := onDisk.digestCache["nginx:latest"]; ok {
		t.Errorf("[AC-061] the nginx entry must be gone from disk")
	}
	if _, ok := onDisk.digestCache["redis:7"]; !ok {
		t.Errorf("[AC-061] the redis entry must remain on disk")
	}
}

// An unreadable local tag is check_failed, not a guess.
func TestCL_AC059_LocalInspectFailureIsCheckFailed(t *testing.T) {
	api := newFakeFreshness()
	api.localErr["nginx:latest"] = errors.New("No such image")
	cs := []models.ContainerInfo{running("nginx:latest", "sha256:X")}

	newTestCollector(nil).checkImageUpdates(context.Background(), api, cs)

	if cs[0].UpdateStatus != models.UpdateStatusFailed || cs[0].UpdateAvailable != nil {
		t.Errorf("[AC-059] want check_failed with nil UpdateAvailable, got %q", cs[0].UpdateStatus)
	}
	if api.registryCalls.Load() != 0 {
		t.Errorf("[AC-059] no registry call is useful without a local image")
	}
}

// containerFromAPI carries the container's running image ID (AD-016).
func TestCL_AC054_ContainerInfoCarriesImageID(t *testing.T) {
	ci := containerFromAPI(containerSummaryFor("sha256:abc"))
	if ci.ImageID != "sha256:abc" {
		t.Errorf("[AC-054] ContainerInfo.ImageID = %q, want sha256:abc", ci.ImageID)
	}
}
