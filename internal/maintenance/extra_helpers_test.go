package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SC-001: nothing in these tests starts a process. The step runner, the lock
// probe, the busy scan, the unit probe and the trust walk are fakes, and the
// fixture dietpi-update file is a tripwire with mode 0644.
const tripwire = "#!/bin/sh\necho DIETPI TRIPWIRE >&2\nexit 97\n"

// fakeClock is the injected clock. Sleep advances it, so lock waits and budget
// checks are deterministic and instant.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// validatingStore is an in-memory store that applies the real Record.Validate
// to everything the worker saves, as the root Store does.
type validatingStore struct {
	records   []Record
	failPhase string
	seen      map[string]bool
}

func (s *validatingStore) Current() (Record, error) {
	if len(s.records) == 0 {
		return Record{}, os.ErrNotExist
	}
	return s.records[len(s.records)-1], nil
}
func (s *validatingStore) Begin(r Record) error {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[r.CommandID] {
		return errors.New("maintenance command already recorded")
	}
	s.seen[r.CommandID] = true
	return s.Save(r)
}
func (s *validatingStore) Save(r Record) error {
	if r.Phase == s.failPhase {
		return errors.New("disk full")
	}
	if err := r.Validate(); err != nil {
		return errors.New("store refused an invalid record: " + err.Error())
	}
	// The real store serialises on Save, so later in-place edits of the
	// worker's slices never reach a saved record. Copy to match.
	r.ExtraUpdaters = append([]UpdaterResult(nil), r.ExtraUpdaters...)
	s.records = append(s.records, r)
	return nil
}
func (s *validatingStore) Finish(r Record) error { return s.Save(r) }

// stepCall is what the fake step runner saw.
type stepCall struct {
	id       string
	spec     extraupd.ApplySpec
	deadline time.Time
	hasDL    bool
}

type fakeStep struct {
	calls  []stepCall
	out    StepOutcome
	block  bool
	onCall func()
}

func (f *fakeStep) RunStep(ctx context.Context, id string, spec extraupd.ApplySpec) StepOutcome {
	dl, ok := ctx.Deadline()
	f.calls = append(f.calls, stepCall{id: id, spec: spec, deadline: dl, hasDL: ok})
	if f.onCall != nil {
		f.onCall()
	}
	if f.block {
		<-ctx.Done()
		return StepOutcome{ExitCode: -1, Err: ctx.Err()}
	}
	return f.out
}

// orderRunner records every apt command and every other event in one log.
type orderRunner struct {
	log    *[]string
	failAt map[string]string
	after  map[string]func()
	calls  []invocation
}

func (r *orderRunner) Run(_ context.Context, p string, a ...string) (CommandResult, error) {
	key := p + " " + strings.Join(a, " ")
	r.calls = append(r.calls, invocation{p, append([]string(nil), a...)})
	*r.log = append(*r.log, "apt:"+key)
	if f := r.after[key]; f != nil {
		f()
	}
	if msg := r.failAt[key]; msg != "" {
		return CommandResult{Output: msg, ExitCode: 100}, errors.New(msg)
	}
	return CommandResult{}, nil
}

const (
	upgradeKey = "/usr/bin/apt-get -y --no-remove -o DPkg::Lock::Timeout=120 -o Acquire::Retries=0 -o Dpkg::Options::=--force-confold -o Dpkg::Options::=--force-confdef dist-upgrade"
	refreshKey = "/usr/bin/apt-get -o Acquire::Retries=0 update"
)

// extraRig is a worker wired for the extra-updater step against a fixture host.
type extraRig struct {
	t      *testing.T
	root   string
	w      *Worker
	store  *validatingStore
	runner *orderRunner
	step   *fakeStep
	holds  *loggingHolds
	clock  *fakeClock
	log    []string

	policy    []string
	policyErr error
	lock      func() LockState
	busy      func() (bool, string)
	unit      UnitState
	unitErr   error
	trustErr  error
	last      time.Time
	hasLast   bool
	lastErr   error
	trustSeen int
	sleeps    int
}

type loggingHolds struct {
	log        *[]string
	restoreErr error
}

func (h *loggingHolds) Snapshot(context.Context) (HoldSnapshot, error) {
	return HoldSnapshot{Packages: []string{"linux-image-generic"}, Versions: map[string]string{"linux-image-generic": "1"}}, nil
}
func (h *loggingHolds) Release(context.Context, HoldSnapshot) error { return nil }
func (h *loggingHolds) Restore(context.Context, HoldSnapshot) error {
	*h.log = append(*h.log, "holds:restore")
	return h.restoreErr
}
func (h *loggingHolds) Changes(context.Context, HoldSnapshot) ([]VersionChange, error) {
	return []VersionChange{{"linux-image-generic", "1", "2"}}, nil
}

func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func versionFixture(core, sub, rc string) string {
	return "G_DIETPI_VERSION_CORE=" + core + "\nG_DIETPI_VERSION_SUB=" + sub + "\nG_DIETPI_VERSION_RC=" + rc + "\nG_GITBRANCH='master'\nG_GITOWNER='MichaIng'\n"
}

// newExtraRig builds a DietPi 9.20.1 host with an update to 10.8.2 pending, an
// opted-in policy, a free lock, a healthy unit and a clean trust walk.
func newExtraRig(t *testing.T) *extraRig {
	t.Helper()
	g := &extraRig{t: t, root: t.TempDir(), clock: newFakeClock(), store: &validatingStore{}, step: &fakeStep{}}
	writeFixture(t, g.root, "boot/dietpi/.version", versionFixture("9", "20", "1"))
	writeFixture(t, g.root, "boot/dietpi/dietpi-update", tripwire)
	writeFixture(t, g.root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\n")
	writeFixture(t, g.root, "run/dietpi/.update_available", "10.8.2\n")
	writeFixture(t, g.root, "proc/sys/kernel/osrelease", "6.12.47+rpt-rpi-v8\n")
	if err := os.MkdirAll(filepath.Join(g.root, "lib", "modules", "6.12.47+rpt-rpi-v8"), 0o755); err != nil {
		t.Fatal(err)
	}
	g.policy = []string{"dietpi"}
	g.lock = func() LockState { return LockState{Kind: LockFree} }
	g.busy = func() (bool, string) { return false, "" }
	g.unit = UnitState{RuntimeMax: ExtraServiceBackstop, PrivateTmp: "no", MountFlags: ""}
	g.step.out = StepOutcome{Output: "dietpi-update ok\n"}
	g.holds = &loggingHolds{log: &g.log}
	g.runner = &orderRunner{log: &g.log, failAt: map[string]string{}, after: map[string]func(){}}
	g.w = &Worker{Store: g.store, Runner: g.runner, Holds: g.holds, Now: g.clock.Now}
	g.w.Extras = &ExtraEnv{
		FS:     extraupd.DirFS(g.root),
		Policy: func() ([]string, error) { return g.policy, g.policyErr },
		Lock:   func() LockState { return g.lock() },
		Busy:   func() (bool, string) { return g.busy() },
		Unit:   func() (UnitState, error) { return g.unit, g.unitErr },
		Trust: func(extraupd.ApplySpec, models.ExtraUpdater) error {
			g.trustSeen++
			return g.trustErr
		},
		LastStart: func(string) (time.Time, bool, error) { return g.last, g.hasLast, g.lastErr },
		Step:      &loggingStep{inner: g.step, log: &g.log},
		Sleep:     func(_ context.Context, d time.Duration) { g.sleeps++; g.clock.Advance(d) },
	}
	return g
}

type loggingStep struct {
	inner *fakeStep
	log   *[]string
}

func (l *loggingStep) RunStep(ctx context.Context, id string, spec extraupd.ApplySpec) StepOutcome {
	*l.log = append(*l.log, "step:"+id)
	return l.inner.RunStep(ctx, id, spec)
}

func v2Request(mode string) Request {
	return Request{Version: 2, CommandID: testRequest().CommandID, Mode: mode, ExtraUpdaters: []string{"dietpi"}}
}

func (g *extraRig) run(req Request) (Record, error) {
	g.t.Helper()
	return g.w.Execute(context.Background(), req)
}

func (g *extraRig) result(rec Record) UpdaterResult {
	g.t.Helper()
	for _, r := range rec.ExtraUpdaters {
		if r.ID == "dietpi" {
			return r
		}
	}
	g.t.Fatalf("no dietpi result in %+v", rec.ExtraUpdaters)
	return UpdaterResult{}
}

func (g *extraRig) stepCalls() int { return len(g.step.calls) }
