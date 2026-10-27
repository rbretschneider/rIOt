package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-007 AW: the dpkg-lock wait step of a standalone reboot or shutdown
// (ADD section 8). Every test runs on the fake clock with a scripted dpkg-lock
// probe and a recording runner. Nothing here starts a process, sleeps in real
// time or reads the real /proc.

// ex7Log is a goroutine-safe ordered log shared by a test's fakes.
type ex7Log struct {
	mu      sync.Mutex
	entries []string
}

func (l *ex7Log) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, fmt.Sprintf(format, args...))
}

func (l *ex7Log) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// ex7ProgressMode is what the progress channel of ex7Transport does.
type ex7ProgressMode int

const (
	progressWorks ex7ProgressMode = iota
	progressReject
	progressServer
	progressDropped
	progressBlocks
)

// ex7Transport wraps fakeTransport. Progress reports always go over HTTP (the
// WebSocket reads as down), are recorded in order, and are answered according
// to mode.
type ex7Transport struct {
	*fakeTransport
	log     *ex7Log
	mode    ex7ProgressMode
	release chan struct{} // closed to free blocked sends

	mu    sync.Mutex
	steps []string
}

func newEX7Transport(t *testing.T, base *fakeTransport, mode ex7ProgressMode, log *ex7Log) *ex7Transport {
	t.Helper()
	tt := &ex7Transport{fakeTransport: base, log: log, mode: mode, release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-tt.release:
		default:
			close(tt.release)
		}
	})
	return tt
}

func (e *ex7Transport) WSConnected() bool { return false }

func (e *ex7Transport) PostReport(ctx context.Context, id string, rep models.CommandReport) (*ReportReply, error) {
	if rep.Kind == models.ReportKindProgress && rep.Step != "" {
		e.mu.Lock()
		e.steps = append(e.steps, rep.Step)
		e.mu.Unlock()
		if e.log != nil {
			e.log.add("send:%s", rep.Step)
		}
		switch e.mode {
		case progressReject:
			return replyStatus(400, "invalid_request"), nil
		case progressServer:
			return replyStatus(500, ""), nil
		case progressDropped:
			return nil, errors.New("connection dropped")
		case progressBlocks:
			select {
			case <-e.release:
			case <-ctx.Done():
			}
			return nil, errors.New("blocked send released")
		}
	}
	return replyOK(id), nil
}

func (e *ex7Transport) sentSteps() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.steps...)
}

func countOf(steps []string, want string) int {
	n := 0
	for _, s := range steps {
		if s == want {
			n++
		}
	}
	return n
}

// ex7Rig is a reboot gateRig whose progress goes through an ex7Transport.
func ex7Rig(t *testing.T, mode ex7ProgressMode, log *ex7Log) (*gateRig, *ex7Transport) {
	t.Helper()
	r := newGateRig(t)
	tt := newEX7Transport(t, r.tr, mode, log)
	r.a.tr = tt
	return r, tt
}

// powerTrace is everything AC-007 requires to be identical across progress
// channels.
type powerTrace struct {
	probes      int
	pauses      []time.Duration
	rebootTimes []time.Time
	message     string
	status      string
	endedAt     time.Time
}

func traceOf(r *gateRig) powerTrace {
	res := r.resultFor(idA)
	r.mu.Lock()
	defer r.mu.Unlock()
	return powerTrace{
		probes:      r.probeCalls,
		pauses:      append([]time.Duration(nil), r.pauses...),
		rebootTimes: append([]time.Time(nil), r.rebootTimes...),
		message:     res.Message,
		status:      res.Status,
		endedAt:     r.clk.Now(),
	}
}

func stepNames(res models.CommandResult) []string {
	var out []string
	for _, s := range res.Steps {
		out = append(out, s.Name)
	}
	return out
}

func TestEX7_AC001_RebootWaitReportsStepOnce(t *testing.T) {
	r, tt := ex7Rig(t, progressWorks, nil)
	r.script = heldUntil(4*time.Minute, unattendedUpgrHolder)

	r.submit(idA, models.ActionReboot, nil)
	r.wait()

	steps := tt.sentSteps()
	assert.Equal(t, 1, countOf(steps, models.StepWaitDpkgLock),
		"[AC-001] exactly one wait_dpkg_lock progress report must be sent, got %v", steps)
	require.GreaterOrEqual(t, len(steps), 1)
	assert.Equal(t, models.StepWaitDpkgLock, steps[0], "[AC-001] the wait step is the first step reported")
	res := r.resultFor(idA)
	assert.Equal(t, "reboot initiated after waiting 4m for the dpkg lock", res.Message, "[AC-001] result text unchanged")
	assert.EqualValues(t, 1, r.reboots.Load(), "[AC-001] one reboot")
}

func TestEX7_AC002_PowerRunsRightAfterFinalProbe(t *testing.T) {
	// log holds only what the gate's own goroutine does. The notifier runs on
	// another goroutine and may send at any real moment, so transport calls are
	// not part of this ordering; what is checked is that the gate and handler
	// goroutine go from the final probe straight to the power runner.
	log := &ex7Log{}
	r, tt := ex7Rig(t, progressBlocks, nil)
	release := 4 * time.Minute
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed < release {
			log.add("probe:held")
			return unattendedUpgrHolder
		}
		log.add("probe:free")
		return freeDpkgLockProbe()
	}
	var (
		markerStep string
		runnerAt   time.Time
		mu         sync.Mutex
	)
	inner := r.a.runner
	r.a.runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && len(args) > 0 && args[0] == "systemctl" {
			m, _ := r.a.inflight.Get(idA)
			mu.Lock()
			markerStep, runnerAt = m.Step, r.clk.Now()
			mu.Unlock()
			log.add("runner:reboot")
		}
		return inner(ctx, name, args...)
	}

	r.submit(idA, models.ActionReboot, nil)
	r.a.cmdWG.Wait() // the blocked transport never lets the notifier flush

	entries := log.snapshot()
	runnerIdx := slices.Index(entries, "runner:reboot")
	require.GreaterOrEqual(t, runnerIdx, 2, "[AC-002] the power runner must be invoked; log: %v", entries)
	assert.Equal(t, "probe:free", entries[runnerIdx-1],
		"[AC-002] the entry before the power runner call must be the gate's final lock probe; log: %v", entries)
	assert.Equal(t, "probe:free", entries[runnerIdx-2], "[AC-002] the guards' probe precedes the fresh final probe")
	assert.Equal(t, 1, countOf(entries, "runner:reboot"), "[AC-002] one power runner call")
	mu.Lock()
	assert.NotEqual(t, models.StepPowerAction, markerStep,
		"[AC-002] at the runner call the leaving step has not been written: power_action follows the power action")
	atRunner := runnerAt
	mu.Unlock()
	assert.Equal(t, atRunner, r.rebootTimes[0], "[AC-002] runner time is the recorded reboot time")
	assert.Equal(t, atRunner, r.clk.Now(),
		"[AC-002] no fake time passes between the power runner call and the result: the leaving step adds no wait")

	res := r.resultFor(idA)
	assert.Equal(t, "reboot initiated after waiting 4m for the dpkg lock", res.Message, "[AC-002] result text")
	assert.Equal(t, []string{models.StepWaitDpkgLock, models.StepPowerAction}, stepNames(res),
		"[AC-002] the result lists the wait step, then the power step")
	require.Eventually(t, func() bool { return len(tt.sentSteps()) == 1 }, 2*time.Second, time.Millisecond,
		"[AC-002] the blocked transport holds the first report")
	assert.Equal(t, []string{models.StepWaitDpkgLock}, tt.sentSteps(),
		"[AC-002] only the first report reached the blocked transport; power_action stays queued behind it")
}

func TestEX7_AC003_TimedOutWaitFailsInWaitStep(t *testing.T) {
	r, tt := ex7Rig(t, progressWorks, nil)
	holder := dpkgLockStatus{State: dpkgLockHeld, PID: 1234, Comm: "apt-get", UID: 0}
	r.script = alwaysHeld(holder)

	r.submit(idA, models.ActionReboot, nil)
	r.wait()

	res := r.resultFor(idA)
	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-003] the command fails")
	assert.Equal(t, "reboot skipped: dpkg lock held past the timeout (10m), "+holder.holderText(), res.Message,
		"[AC-003] message exactly as today")
	assert.Equal(t, models.StepWaitDpkgLock, res.Step, "[AC-003] the result names the dpkg wait as the failed step")
	require.Len(t, res.Steps, 1, "[AC-003] only the wait step exists")
	assert.Equal(t, "failed", res.Steps[0].Outcome, "[AC-003] the history shows the wait failed")
	assert.Equal(t, []string{models.StepWaitDpkgLock}, tt.sentSteps(), "[AC-003] one step report, no power step")
	assert.Zero(t, r.reboots.Load(), "[AC-003] no reboot")
}

func TestEX7_AC004_ShutdownWaitReportsStepAndInitiates(t *testing.T) {
	s := newShutdownRig(t)
	tt := newEX7Transport(t, s.tr, progressWorks, nil)
	s.a.tr = tt
	s.script = heldUntil(2*time.Minute, unattendedUpgrHolder)

	res := s.runShutdown()

	assert.Equal(t, "shutdown initiated after waiting 2m for the dpkg lock", res.Message, "[AC-004] result text")
	assert.EqualValues(t, 1, s.poweroffs.Load(), "[AC-004] the fake poweroff runner is invoked once")
	assert.Equal(t, 1, countOf(tt.sentSteps(), models.StepWaitDpkgLock), "[AC-004] one wait report, got %v", tt.sentSteps())
	assert.Equal(t, []string{models.StepWaitDpkgLock, models.StepPowerAction}, stepNames(res), "[AC-004] wait then power step")
}

func TestEX7_AC005_FreeLockNoStep(t *testing.T) {
	r, tt := ex7Rig(t, progressWorks, nil)

	r.submit(idA, models.ActionReboot, nil)
	r.wait()

	assert.Zero(t, countOf(tt.sentSteps(), models.StepWaitDpkgLock), "[AC-005] no wait step when the lock is free at the first check")
	assert.Zero(t, countOf(tt.sentSteps(), models.StepPowerAction), "[AC-005] and no power step: behaviour is as before")
	res := r.resultFor(idA)
	assert.Equal(t, "reboot initiated", res.Message, "[AC-005] result text unchanged")
	assert.Empty(t, stepNames(res), "[AC-005] the command records no step at all")
}

func TestEX7_AC006_RetakenLockReportsOnceDeadlineFromFirstHeld(t *testing.T) {
	r, tt := ex7Rig(t, progressWorks, nil)
	// Probe calls: 1 held, 2 free (the guards run), 3 held again (the fresh final
	// check), 4 free (the guards run again), 5 free (final check): cleared.
	r.script = func(_ time.Duration, call int) dpkgLockStatus {
		switch call {
		case 1, 3:
			return unattendedUpgrHolder
		default:
			return freeDpkgLockProbe()
		}
	}

	r.submit(idA, models.ActionReboot, nil)
	r.wait()

	assert.Equal(t, 1, countOf(tt.sentSteps(), models.StepWaitDpkgLock),
		"[AC-006] a lock taken again after the guards passed must not report the wait again, got %v", tt.sentSteps())
	assert.Equal(t, 5, r.calls(), "[AC-006] the script ran as designed: five probes")
	res := r.resultFor(idA)
	assert.Equal(t, "reboot initiated after waiting 4s for the dpkg lock", res.Message,
		"[AC-006] the wait is counted from the first held observation (two 2 s polls), not from the second")
	assert.EqualValues(t, 1, r.reboots.Load(), "[AC-006] one reboot")
}

func TestEX7_AC007_StepReportFailureChangesNothing(t *testing.T) {
	run := func(t *testing.T, mode ex7ProgressMode) powerTrace {
		r, _ := ex7Rig(t, mode, nil)
		r.script = heldUntil(4*time.Minute, unattendedUpgrHolder)
		r.submit(idA, models.ActionReboot, nil)
		if mode == progressBlocks {
			r.a.cmdWG.Wait()
		} else {
			r.wait()
		}
		return traceOf(r)
	}
	baseline := run(t, progressWorks)
	require.Equal(t, models.ResultStatusSuccess, baseline.status, "[AC-007] baseline run succeeds")
	for name, mode := range map[string]ex7ProgressMode{
		"HTTP 400 rejected": progressReject,
		"HTTP 500":          progressServer,
		"dropped":           progressDropped,
		"blocked":           progressBlocks,
	} {
		t.Run(name, func(t *testing.T) {
			got := run(t, mode)
			assert.Equal(t, baseline.probes, got.probes, "[AC-007] the gate probed the same number of times")
			assert.Equal(t, baseline.pauses, got.pauses, "[AC-007] the gate paused identically")
			assert.Equal(t, baseline.rebootTimes, got.rebootTimes, "[AC-007] the power runner ran at the same fake time")
			assert.Equal(t, baseline.message, got.message, "[AC-007] same result text")
			assert.Equal(t, baseline.status, got.status, "[AC-007] same outcome")
			assert.Equal(t, baseline.endedAt, got.endedAt, "[AC-007] same fake-clock end time")
		})
	}
}

func TestEX7_AC009_PostPatchRebootReportsNoWaitStep(t *testing.T) {
	r, tt := ex7Rig(t, progressWorks, nil)
	r.script = heldUntil(3*time.Minute, unattendedUpgrHolder)

	r.drive(pathAuthorized)

	require.Eventually(t, func() bool { return r.reboots.Load() == 1 }, 2*time.Second, time.Millisecond,
		"[AC-009] the post-patch reboot still happens after its dpkg wait")
	assert.Zero(t, countOf(tt.sentSteps(), models.StepWaitDpkgLock),
		"[AC-009] the post-patch reboot must not report the waiting step, got %v", tt.sentSteps())
	assert.Zero(t, countOf(tt.sentSteps(), models.StepPowerAction), "[AC-009] nor the power step")
}

func TestEX7_AC035_AtMostTwoStepChanges(t *testing.T) {
	r, tt := ex7Rig(t, progressWorks, nil)
	r.script = heldUntil(9*time.Minute, unattendedUpgrHolder)

	r.submit(idA, models.ActionReboot, nil)
	r.wait()

	res := r.resultFor(idA)
	assert.LessOrEqual(t, len(res.Steps), 2, "[AC-035] at most two step changes for the wait, got %v", stepNames(res))
	assert.LessOrEqual(t, len(tt.sentSteps()), 2, "[AC-035] at most two step reports, got %v", tt.sentSteps())
	assert.Equal(t, []string{models.StepWaitDpkgLock, models.StepPowerAction}, stepNames(res), "[AC-035] enter and leave")
}

// slowStateFS blocks the next atomic write when armed, as a slow disk would.
type slowStateFS struct {
	stateFS
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *slowStateFS) Rename(oldpath, newpath string) error {
	if s.armed.Load() {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	return s.stateFS.Rename(oldpath, newpath)
}

func TestEX7_Q2_EnterStepDoesNotDelayGate(t *testing.T) {
	t.Run("a slow durable step write does not delay the power action", func(t *testing.T) {
		r, _ := ex7Rig(t, progressWorks, nil)
		slow := &slowStateFS{stateFS: r.fs, entered: make(chan struct{}), release: make(chan struct{})}
		t.Cleanup(func() {
			select {
			case <-slow.release:
			default:
				close(slow.release)
			}
		})
		r.a.inflight = newInflightStore(r.inflightPath, slow)
		runnerSawWriteBlocked := make(chan bool, 1)
		inner := r.a.runner
		r.a.runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "sudo" && len(args) > 0 && args[0] == "systemctl" {
				select {
				case <-slow.entered:
					runnerSawWriteBlocked <- true
				default:
					runnerSawWriteBlocked <- false
				}
			}
			return inner(ctx, name, args...)
		}
		r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
			if elapsed < 10*time.Second {
				slow.armed.Store(true) // the next durable step write is slow
				return unattendedUpgrHolder
			}
			// Make sure the step write is really stuck on the slow disk before the
			// gate goes on; the gate must then clear without waiting for it.
			select {
			case <-slow.entered:
			case <-time.After(5 * time.Second):
			}
			return freeDpkgLockProbe()
		}

		r.submit(idA, models.ActionReboot, nil)
		select {
		case saw := <-runnerSawWriteBlocked:
			assert.True(t, saw, "[Q-2] the power runner must be invoked while the wait-step write is still blocked")
		case <-time.After(5 * time.Second):
			t.Fatal("[Q-2] the power runner was not invoked: the gate was delayed by the step write")
		}
		assert.Equal(t, 5, r.gatePauses(), "[Q-2] the gate paused 5 times (10 s at 2 s a poll): probe times are unchanged")
		close(slow.release)
		r.wait()
		assert.Equal(t, []string{models.StepWaitDpkgLock, models.StepPowerAction}, stepNames(r.resultFor(idA)),
			"[Q-2] both steps are recorded once the disk answers")
	})

	t.Run("a full notifier queue does not delay the gate", func(t *testing.T) {
		r, _ := ex7Rig(t, progressWorks, nil)
		block := make(chan struct{})
		// Hold the notifier on one job and fill its queue, so enqueue must wait.
		started := make(chan struct{})
		r.a.notify.jobs <- func(context.Context) { close(started); <-block }
		<-started
		for i := 0; i < models.NotifierQueueBound; i++ {
			r.a.notify.jobs <- func(context.Context) {}
		}
		t.Cleanup(func() { close(block) })
		runnerAt := make(chan time.Time, 1)
		inner := r.a.runner
		r.a.runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "sudo" && len(args) > 0 && args[0] == "systemctl" {
				runnerAt <- r.clk.Now()
			}
			return inner(ctx, name, args...)
		}
		r.script = heldUntil(10*time.Second, unattendedUpgrHolder)

		r.submit(idA, models.ActionReboot, nil)
		// The wait step may sit in enqueue's 1 s fake timer; free it on the fake
		// clock until the command ends.
		done := make(chan struct{})
		go func() { r.a.cmdWG.Wait(); close(done) }()
		var at time.Time
		select {
		case at = <-runnerAt:
		case <-time.After(5 * time.Second):
			t.Fatal("[Q-2] the power runner was not invoked: a full notifier queue delayed the gate")
		}
		for finished := false; !finished; {
			select {
			case <-done:
				finished = true
			case <-time.After(time.Millisecond):
				r.clk.Advance(time.Second)
			}
		}
		first := r.firstProbeAt
		assert.Equal(t, 10*time.Second, at.Sub(first),
			"[Q-2] the power action started exactly when the lock cleared on the fake clock: the full queue added no delay")
	})
}
