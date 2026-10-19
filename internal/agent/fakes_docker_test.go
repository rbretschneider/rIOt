package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// fakeContainer is one container of the fake daemon.
type fakeContainer struct {
	ID          string
	Name        string
	Image       string // image reference (Config.Image)
	ImageID     string
	Running     bool
	Restarting  bool
	Labels      map[string]string
	NetworkMode string
	Config      *container.Config
}

// fakeDocker is an in-memory Docker daemon implementing dockerAPI. Every call is
// recorded as "<op>:<identifier as given>[:<detail>]", so tests can assert both
// what was done and by which identifier (name or immutable ID).
type fakeDocker struct {
	mu         sync.Mutex
	containers []*fakeContainer
	calls      []string
	nextID     int

	// images maps an image reference to the image ID it currently resolves to.
	images map[string]string
	// pullStream is the body ImagePull returns; empty means a clean stream.
	pullStream string
	pullErr    error
	// pulledID is the image ID a successful pull makes the reference resolve to.
	pulledID string

	// failIf returns an error to inject for an operation, keyed by the recorded
	// call string. onCall observes every call before it executes.
	failIf func(call string) error
	onCall func(call string)
	// afterStart lets a test change a container right after it started.
	afterStart func(c *fakeContainer)
	// ctxs records, per call, the context error and remaining deadline.
	ctxInfo map[string]ctxSnapshot

	log *eventLog
}

type ctxSnapshot struct {
	Err         error
	HasDeadline bool
	Remaining   time.Duration
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{images: map[string]string{}, pulledID: "sha256:new", ctxInfo: map[string]ctxSnapshot{}}
}

// add registers a container and returns it.
func (f *fakeDocker) add(c fakeContainer) *fakeContainer {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == "" {
		f.nextID++
		c.ID = fmt.Sprintf("%064x", f.nextID)
	}
	if c.ImageID == "" {
		c.ImageID = "sha256:old"
	}
	if c.Labels == nil {
		c.Labels = map[string]string{}
	}
	cc := c
	f.containers = append(f.containers, &cc)
	return &cc
}

func (f *fakeDocker) get(name string) *fakeContainer {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.containers {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (f *fakeDocker) byID(id string) *fakeContainer {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.containers {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// snapshot returns a copy of the container, or false if it does not exist.
func (f *fakeDocker) snapshot(name string) (fakeContainer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.containers {
		if c.Name == name {
			return *c, true
		}
	}
	return fakeContainer{}, false
}

func (f *fakeDocker) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// callsWithPrefix returns the recorded calls that start with prefix.
func (f *fakeDocker) callsWithPrefix(prefix string) []string {
	var out []string
	for _, c := range f.callList() {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeDocker) has(call string) bool {
	for _, c := range f.callList() {
		if c == call {
			return true
		}
	}
	return false
}

func (f *fakeDocker) indexOf(call string) int {
	for i, c := range f.callList() {
		if c == call {
			return i
		}
	}
	return -1
}

// begin records a call and returns an injected error, if any. mu is not held.
func (f *fakeDocker) begin(ctx context.Context, call string) error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	snap := ctxSnapshot{Err: ctx.Err()}
	if dl, ok := ctx.Deadline(); ok {
		snap.HasDeadline, snap.Remaining = true, time.Until(dl)
	}
	f.ctxInfo[call] = snap
	hook, fail := f.onCall, f.failIf
	f.mu.Unlock()
	f.log.add("docker:" + call)
	if hook != nil {
		hook(call)
	}
	if fail != nil {
		return fail(call)
	}
	return nil
}

// find resolves an identifier (name or full ID) to a container; mu must be held.
func (f *fakeDocker) findLocked(raw string) *fakeContainer {
	raw = strings.TrimPrefix(raw, "/")
	for _, c := range f.containers {
		if c.ID == raw || c.Name == raw {
			return c
		}
	}
	return nil
}

func (f *fakeDocker) summary(c *fakeContainer) container.Summary {
	state := "exited"
	switch {
	case c.Restarting:
		state = "restarting"
	case c.Running:
		state = "running"
	}
	s := container.Summary{
		ID: c.ID, Names: []string{"/" + c.Name}, Image: c.Image, ImageID: c.ImageID,
		State: state, Labels: c.Labels,
	}
	s.HostConfig.NetworkMode = c.NetworkMode
	return s
}

func (f *fakeDocker) ContainerList(ctx context.Context, _ container.ListOptions) ([]container.Summary, error) {
	if err := f.begin(ctx, "list"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]container.Summary, 0, len(f.containers))
	for _, c := range f.containers {
		out = append(out, f.summary(c))
	}
	return out, nil
}

func (f *fakeDocker) ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error) {
	if err := f.begin(ctx, "inspect:"+id); err != nil {
		return container.InspectResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.findLocked(id)
	if c == nil {
		return container.InspectResponse{}, fmt.Errorf("Error response from daemon: No such container: %s", id)
	}
	cfg := c.Config
	if cfg == nil {
		cfg = &container.Config{}
	}
	cfg.Image = c.Image
	cfg.Labels = c.Labels
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID: c.ID, Name: "/" + c.Name, Image: c.ImageID,
			State:      &container.State{Running: c.Running, Restarting: c.Restarting},
			HostConfig: &container.HostConfig{NetworkMode: container.NetworkMode(c.NetworkMode)},
		},
		Config: cfg,
	}, nil
}

func (f *fakeDocker) ContainerStop(ctx context.Context, id string, _ container.StopOptions) error {
	if err := f.begin(ctx, "stop:"+id); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.findLocked(id)
	if c == nil {
		return fmt.Errorf("No such container: %s", id)
	}
	c.Running = false
	return nil
}

func (f *fakeDocker) ContainerStart(ctx context.Context, id string, _ container.StartOptions) error {
	if err := f.begin(ctx, "start:"+id); err != nil {
		return err
	}
	f.mu.Lock()
	c := f.findLocked(id)
	if c == nil {
		f.mu.Unlock()
		return fmt.Errorf("No such container: %s", id)
	}
	// A container joining another's network namespace needs that container to
	// exist, as the real daemon enforces at start.
	if ref := containerRef(c.NetworkMode); ref != "" && f.findLocked(ref) == nil {
		f.mu.Unlock()
		return fmt.Errorf("cannot join network of a non running container: %s", ref)
	}
	c.Running = true
	hook := f.afterStart
	f.mu.Unlock()
	if hook != nil {
		hook(c)
	}
	return nil
}

func (f *fakeDocker) ContainerRestart(ctx context.Context, id string, _ container.StopOptions) error {
	if err := f.begin(ctx, "restart:"+id); err != nil {
		return err
	}
	return nil
}

func (f *fakeDocker) ContainerRename(ctx context.Context, id, newName string) error {
	if err := f.begin(ctx, "rename:"+id+":"+newName); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.findLocked(id)
	if c == nil {
		return fmt.Errorf("No such container: %s", id)
	}
	for _, o := range f.containers {
		if o.Name == newName {
			return fmt.Errorf("Conflict. The container name %q is already in use", newName)
		}
	}
	c.Name = newName
	return nil
}

func (f *fakeDocker) ContainerRemove(ctx context.Context, id string, opts container.RemoveOptions) error {
	call := "remove:" + id
	if opts.Force {
		call += ":force"
	}
	if err := f.begin(ctx, call); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.containers {
		if c.ID == id || c.Name == strings.TrimPrefix(id, "/") {
			f.containers = append(f.containers[:i], f.containers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("No such container: %s", id)
}

func (f *fakeDocker) ContainerCreate(ctx context.Context, cfg *container.Config, host *container.HostConfig, _ *network.NetworkingConfig, name string) (container.CreateResponse, error) {
	if err := f.begin(ctx, "create:"+name); err != nil {
		return container.CreateResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.containers {
		if o.Name == name {
			return container.CreateResponse{}, fmt.Errorf("Conflict. The container name %q is already in use", name)
		}
	}
	f.nextID++
	id := fmt.Sprintf("%064x", f.nextID)
	imgID := f.images[cfg.Image]
	if imgID == "" {
		imgID = f.pulledID
	}
	nm := ""
	if host != nil {
		nm = string(host.NetworkMode)
	}
	f.containers = append(f.containers, &fakeContainer{
		ID: id, Name: name, Image: cfg.Image, ImageID: imgID, Labels: cfg.Labels, NetworkMode: nm, Config: cfg,
	})
	return container.CreateResponse{ID: id}, nil
}

func (f *fakeDocker) ImagePull(ctx context.Context, ref string, _ image.PullOptions) (io.ReadCloser, error) {
	if err := f.begin(ctx, "pull:"+ref); err != nil {
		return nil, err
	}
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	stream := f.pullStream
	if stream == "" {
		stream = "{\"status\":\"Pulling\"}\n{\"status\":\"Downloaded newer image\"}\n"
	}
	if !strings.Contains(stream, "error") {
		f.mu.Lock()
		f.images[ref] = f.pulledID
		f.mu.Unlock()
	}
	return io.NopCloser(strings.NewReader(stream)), nil
}

func (f *fakeDocker) ImageInspect(ctx context.Context, ref string) (image.InspectResponse, error) {
	if err := f.begin(ctx, "imageinspect:"+ref); err != nil {
		return image.InspectResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.images[ref]
	if !ok {
		return image.InspectResponse{}, errors.New("No such image: " + ref)
	}
	return image.InspectResponse{ID: id}, nil
}

func (f *fakeDocker) Close() error { return nil }

// ---------------------------------------------------------------------------
// Process factory
// ---------------------------------------------------------------------------

// fakeProcs hands out fakeProcess values for `docker compose ...` invocations
// and records their argv.
type fakeProcs struct {
	mu    sync.Mutex
	argv  [][]string
	procs []*fakeProcess
	// configure shapes each new process by its argv; nil means "succeeds at once".
	configure func(args []string, p *fakeProcess)
	created   chan *fakeProcess
	log       *eventLog
}

func newFakeProcs(log *eventLog) *fakeProcs {
	return &fakeProcs{created: make(chan *fakeProcess, 16), log: log}
}

func (fp *fakeProcs) factory(name string, args ...string) limitedProcess {
	p := newFakeProcess()
	p.log = fp.log
	p.autoExit = true
	full := append([]string{name}, args...)
	fp.mu.Lock()
	fp.argv = append(fp.argv, full)
	fp.procs = append(fp.procs, p)
	cfg := fp.configure
	fp.mu.Unlock()
	if cfg != nil {
		cfg(args, p)
	}
	fp.created <- p
	return p
}

func (fp *fakeProcs) invocations() [][]string {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([][]string(nil), fp.argv...)
}

// subcommands lists the compose sub-command of each invocation ("pull", "up").
func (fp *fakeProcs) subcommands() []string {
	var out []string
	for _, a := range fp.invocations() {
		for _, tok := range a[1:] {
			if tok == "pull" || tok == "up" {
				out = append(out, tok)
				break
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// fakeFreshness records which references an update invalidated.
type fakeFreshness struct {
	mu          sync.Mutex
	invalidated []string
	cleared     bool
}

func (f *fakeFreshness) InvalidateFreshness(refs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidated = append(f.invalidated, refs...)
}

func (f *fakeFreshness) ClearFreshnessCache() {
	f.mu.Lock()
	f.cleared = true
	f.mu.Unlock()
}

// dockerHarness is a lifecycle harness whose Docker daemon, compose processes,
// verification wait and freshness cache are fakes.
type dockerHarness struct {
	*lifecycleHarness
	docker    *fakeDocker
	procs     *fakeProcs
	fresh     *fakeFreshness
	pauseHook func(d time.Duration)
}

func newDockerHarness(t *testing.T) *dockerHarness {
	t.Helper()
	lh := newLifecycleHarness(t)
	h := &dockerHarness{
		lifecycleHarness: lh,
		docker:           newFakeDocker(),
		procs:            newFakeProcs(lh.log),
		fresh:            &fakeFreshness{},
	}
	h.docker.log = lh.log
	h.a.dockerFactory = func() (dockerAPI, error) { return h.docker, nil }
	h.a.procFactory = h.procs.factory
	h.a.freshness = h.fresh
	h.a.pause = func(_ context.Context, d time.Duration) bool {
		lh.log.add("pause")
		if h.pauseHook != nil {
			h.pauseHook(d)
		}
		return true
	}
	return h
}

// run submits one command through the real intake and waits for the result.
func (h *dockerHarness) run(id, action string, params map[string]interface{}) resultView {
	h.t.Helper()
	h.submit(id, action, params)
	h.wait()
	return resultView{h.resultFor(id)}
}

type resultView struct{ models.CommandResult }

// composeDir creates a project directory with a compose file and returns it.
func composeDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "stack")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	yml := "version: \"3\"\nservices:\n  web:\n    image: nginx\n    deploy:\n      resources: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// composeLabels builds the compose labels of a service in dir.
func composeLabels(dir, service string) map[string]string {
	return map[string]string{
		models.LabelComposeProject:    "stack",
		models.LabelComposeService:    service,
		models.LabelComposeWorkingDir: dir,
	}
}
