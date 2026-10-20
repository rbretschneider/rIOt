package agent

import (
	"context"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDUX_BufferDeletesEachSuccessfulSnapshot(t *testing.T) {
	buffer, err := NewBuffer(filepath.Join(t.TempDir(), "buffer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer buffer.Close()
	for _, id := range []string{"first", "second", "third"} {
		if err := buffer.Store(&models.TelemetrySnapshot{DeviceID: id}); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 2 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
		}
	}))
	defer srv.Close()
	a := &Agent{buffer: buffer, client: NewHTTPClient(srv.URL, "")}
	a.flushBuffer(context.Background())
	items, err := buffer.Items()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Snapshot.DeviceID != "second" {
		t.Fatalf("already accepted snapshot was retained: %+v", items)
	}
	if until, blocked := a.client.telemetryBlocked(); !blocked || time.Until(until) < 110*time.Second {
		t.Fatal("429 retry-after was ignored")
	}
}
func TestDUX_BufferRejectsOversize(t *testing.T) {
	b, err := NewBuffer(filepath.Join(t.TempDir(), "buffer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	s := &models.TelemetrySnapshot{DeviceID: strings.Repeat("x", AgentBufferMaxBytes)}
	if b.Store(s) == nil {
		t.Fatal("oversized snapshot should not be stored")
	}
}
func TestDUX_LiveUnknownKeepDoesNotGrowState(t *testing.T) {
	c := &agentWSClient{}
	ids := make([]string, 8)
	for i := range ids {
		ids[i] = strings.Repeat("a", 32)
	}
	data, _ := json.Marshal(logRequest{StreamIDs: ids})
	c.handleLogs(context.Background(), AgentWSMessage{Type: "logs_keep", Data: data})
	if len(c.logs.streams) != 0 {
		t.Fatal("keep must not create streams")
	}
}

func TestDUX_ExclusiveDockerLocksAreAtomic(t *testing.T) {
	locks := newTargetLocks()
	if _, _, ok := locks.TryLockAll([]string{models.TargetKeyContainer("web")}, "one", "docker_restart"); !ok {
		t.Fatal("first container lock refused")
	}
	if _, _, ok := locks.TryLockAll([]string{"docker:host"}, "two", "docker_remove_items"); ok {
		t.Fatal("prune must conflict with active container command")
	}
	locks.Unlock([]string{models.TargetKeyContainer("web")}, "one")
	if _, _, ok := locks.TryLockAll([]string{"docker:host"}, "two", "docker_remove_items"); !ok {
		t.Fatal("prune lock refused")
	}
	if _, _, ok := locks.TryLockAll([]string{models.TargetKeyContainer("db")}, "three", "docker_stop"); ok {
		t.Fatal("container command must conflict with prune")
	}
	if _, _, ok := locks.TryLockAll([]string{models.TargetKeyHost}, "four", models.ActionAgentUpdate); ok {
		t.Fatal("agent restart must conflict with prune")
	}
}
func TestDUX_EventBusSubscriberBoundsAndUnsubscribe(t *testing.T) {
	bus := &dockerEventBus{}
	var unsubscribers []func()
	for i := 0; i < 10; i++ {
		_, unsubscribe, err := bus.Subscribe()
		if err != nil {
			t.Fatal(err)
		}
		unsubscribers = append(unsubscribers, unsubscribe)
	}
	if _, _, err := bus.Subscribe(); err == nil {
		t.Fatal("subscriber cap not enforced")
	}
	for _, unsubscribe := range unsubscribers {
		unsubscribe()
		unsubscribe()
	}
	if len(bus.subscribers) != 0 {
		t.Fatal("subscriber leaked")
	}
}
func TestDUX_SEC024_SocketManagerCannotWriteStack(t *testing.T) {
	x := newTrustFx(t)
	x.writer("manager", bindRW(x.dir, "/stacks"), bindRW(x.sock, "/var/run/docker.sock"))
	if err := x.ct.strictWritable(x.snap, x.dir); err == nil {
		t.Fatal("socket-mounted manager must not bypass writable path refusal")
	}
	x.ct.trustedRoots = []string{x.dir}
	if err := x.ct.strictWritable(x.snap, x.dir); err != nil {
		t.Fatal("explicit trusted root should authorize manager stack", err)
	}
}
func TestDUX_SEC024_BindBackedVolumeCannotWriteStack(t *testing.T) {
	x := newTrustFx(t)
	x.writer("writer", container.MountPoint{Type: mount.TypeVolume, Name: "stack-volume", Source: filepath.Join(x.snap.DataRoot, "volumes", "v", "_data"), RW: true})
	x.snap.volumeDevices["stack-volume"] = x.dir
	if err := x.ct.strictWritable(x.snap, x.dir); err == nil {
		t.Fatal("bind-backed volume must count as writable")
	}
}

func TestDUX_StackRegistrySurvivesDownAndPinsPaths(t *testing.T) {
	x := newTrustFx(t)
	x.snap.Containers[0].Labels["com.docker.compose.version"] = "2.40.0"
	x.snap.Containers[0].Labels["com.docker.compose.project.config_files"] = filepath.Join(x.dir, "compose.yaml")
	config := DefaultConfig()
	a := &Agent{config: config, trustFS: x.fs, trustPlatform: "linux"}
	a.stacks.path = filepath.Join(t.TempDir(), "stacks.json")
	_, td, err := a.resolveStackPaths(x.snap, x.project, x.dir, []string{filepath.Join(x.dir, "compose.yaml")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if td.Path() != x.dir {
		t.Fatal("wrong trusted stack")
	}
	down := x.snap
	down.Containers = nil
	b := &Agent{config: config, trustFS: x.fs, trustPlatform: "linux"}
	b.stacks.path = a.stacks.path
	if _, td, err := b.resolveStackPaths(down, x.project, x.dir, nil, false); err != nil || td.Path() != x.dir {
		t.Fatal("down stack lost agreed paths", err)
	}
	other := x.p("srv", "other")
	x.fs.chain(other)
	x.fs.file(filepath.Join(other, "compose.yaml"), 0, 0, 0644, "services: {}\n")
	x.snap.Containers[0].Labels[models.LabelComposeWorkingDir] = other
	x.snap.Containers[0].Labels["com.docker.compose.project.config_files"] = filepath.Join(other, "compose.yaml")
	if _, _, err := b.resolveStackPaths(x.snap, x.project, other, nil, false); err == nil {
		t.Fatal("changed labels silently replaced agreed paths")
	}
	if _, _, err := b.resolveStackPaths(x.snap, x.project, other, []string{filepath.Join(other, "compose.yaml")}, true); err != nil {
		t.Fatal("operator acknowledgement failed", err)
	}
}

func TestDUX_StackRegistryAgesOutWithoutLiveProjects(t *testing.T) {
	a := &Agent{config: DefaultConfig()}
	a.stacks.path = filepath.Join(t.TempDir(), "stacks.json")
	a.stacks.loaded = true
	a.stacks.entries = map[string]stackPaths{"old": {WorkingDir: "/srv/stacks/old", LastSeen: time.Now().Add(-91 * 24 * time.Hour)}}
	if err := a.saveStackRegistry(); err != nil {
		t.Fatal(err)
	}
	a.observeStackRegistry(context.Background(), &models.DockerInfo{Available: true})
	if len(a.stacks.entries) != 0 {
		t.Fatal("down stack registry entries never expired")
	}
	var doc struct {
		Version int                    `json:"version"`
		Stacks  map[string]interface{} `json:"stacks"`
	}
	raw, err := os.ReadFile(a.stacks.path)
	if err != nil || json.Unmarshal(raw, &doc) != nil || doc.Version != 2 || len(doc.Stacks) != 0 {
		t.Fatal("bounded v2 registry was not persisted", err)
	}
}
func TestDUX_StackDownNeverRemovesVolumes(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	res := h.run(idA, "docker_stack_down", projectParams("stack", dir))
	if res.Status != "success" {
		t.Fatal(res.Message)
	}
	calls := h.procs.invocations()
	if len(calls) != 1 {
		t.Fatalf("expected one compose invocation, got %v", calls)
	}
	expected := []string{"docker", "compose", "--project-directory=" + dir, "--project-name=stack", "-f", filepath.Join(dir, "compose.yaml"), "down"}
	if !reflect.DeepEqual(calls[0], expected) {
		t.Fatalf("down arguments changed: %v", calls[0])
	}
}

func TestDUX_SEC026_LiveKeepsBudgetDuringBackfill(t *testing.T) {
	c := &agentWSClient{}
	c.logs.backfills = 2
	now := time.Now()
	for i := 0; i < 64; i++ {
		if !c.allowLogBytesAt(16<<10, true, now) {
			t.Fatal("backfill budget too small")
		}
	}
	if c.allowLogBytesAt(1, true, now) {
		t.Fatal("backfill consumed reserved live allowance")
	}
	for i := 0; i < 64; i++ {
		if !c.allowLogBytesAt(16<<10, false, now) {
			t.Fatal("live budget stolen by backfill")
		}
	}
	if c.allowLogBytesAt(1, false, now) {
		t.Fatal("agent byte limit exceeded")
	}
	if !c.allowLogBytesAt(512<<10, false, now.Add(500*time.Millisecond)) {
		t.Fatal("live tokens did not refill continuously")
	}
	if c.allowLogBytesAt(1, false, now.Add(500*time.Millisecond)) {
		t.Fatal("fixed-window burst reset reintroduced")
	}
}

func TestDUX_LiveLinePreservesFullTextWithinFrameBound(t *testing.T) {
	e := models.ContainerLogEntry{Line: strings.Repeat("a", 16<<10), Timestamp: time.Now(), Stream: "stdout"}
	line := prepareLiveLine(e)
	if line.Line != e.Line || line.Truncated {
		t.Fatal("ordinary 16 KiB line truncated unnecessarily")
	}
	e.Line = strings.Repeat("<", 16<<10)
	line = prepareLiveLine(e)
	raw, _ := marshalLogJSON(line)
	if len(raw) > 60<<10 || line.Truncated || line.Line != e.Line {
		t.Fatal("escaped JSON line exceeded frame budget", len(raw))
	}
}

func TestDUX_ScopedCheckUpdatesKeepsOtherImagesCached(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	web := h.composeService(dir, "web", true, "")
	h.docker.add(fakeContainer{Name: "other", Image: "other:latest", Running: true})
	res := h.run(idA, "docker_check_updates", map[string]interface{}{"compose_project": "stack", "container_ids": []string{web.ID}})
	if res.Status != "success" {
		t.Fatal(res.Message)
	}
	h.fresh.mu.Lock()
	defer h.fresh.mu.Unlock()
	if h.fresh.cleared || len(h.fresh.invalidated) != 1 || h.fresh.invalidated[0] != "web:latest" {
		t.Fatalf("scoped check cleared unrelated freshness: %+v", h.fresh)
	}
}
func TestDUX_CheckUpdatesWithoutSelectionStillClearsHostCache(t *testing.T) {
	h := newDockerHarness(t)
	res := h.run(idA, "docker_check_updates", nil)
	if res.Status != "success" {
		t.Fatal(res.Message)
	}
	h.fresh.mu.Lock()
	defer h.fresh.mu.Unlock()
	if !h.fresh.cleared {
		t.Fatal("legacy host check no longer clears cache")
	}
}

func TestDUX_ConfiguredStoredLiveLineIdentity(t *testing.T) {
	e := models.ContainerLogEntry{Line: "password=supersecret " + strings.Repeat("€", 5000)}
	got := prepareLiveLineWithLimit(e, 4096)
	if got.Line != collectors.CanonicalLogLine(e.Line, 4096) || len(got.Line) > 4096 || strings.Contains(got.Line, "supersecret") || !strings.HasSuffix(got.Line, " [truncated]") {
		t.Fatal("configured live/stored line identity differs")
	}
}
