package agent

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SHUTDOWN-DPKG-001: the shutdown command through the shared power gate and the
// truthful poweroff result. Every test runs on the fake clock with a scripted dpkg
// lock probe and a runner seam: no real sleep, no real /proc, no real lock, and no
// real poweroff. Every test that submits a shutdown uses shutdownRig, which always
// installs a runner. (C-1 in powerguard.go backs this up if a test forgets.)

const sdIDC = "33333333-3333-4333-8333-333333333333"

// shutdownRig is a gateRig whose runner answers `sudo systemctl poweroff`.
// poweroffResult is what the runner returns for it; poweroffBlock, when set, holds
// the runner until it is closed (a poweroff child that does not return).
type shutdownRig struct {
	*gateRig

	poweroffs atomic.Int32

	smu            sync.Mutex
	poweroffArgv   [][]string
	poweroffTimes  []time.Time
	poweroffBlock  chan struct{}
	poweroffResult func() ([]byte, error)
}

func newShutdownRig(t *testing.T) *shutdownRig {
	t.Helper()
	g := newGateRig(t)
	g.a.config.Commands.AllowShutdown = true
	r := &shutdownRig{gateRig: g, poweroffResult: func() ([]byte, error) { return nil, nil }}
	inner := g.a.runner
	g.a.runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && len(args) == 2 && args[0] == "systemctl" && args[1] == "poweroff" {
			r.poweroffs.Add(1)
			r.smu.Lock()
			r.poweroffArgv = append(r.poweroffArgv, append([]string{name}, args...))
			r.poweroffTimes = append(r.poweroffTimes, g.clk.Now())
			block, result := r.poweroffBlock, r.poweroffResult
			r.smu.Unlock()
			if block != nil {
				<-block
			}
			return result()
		}
		return inner(ctx, name, args...)
	}
	require.NotNil(t, g.a.runner, "[C-1] a shutdown test must never run without a runner")
	return r
}

// runShutdown submits a shutdown through the real lifecycle and returns its result.
func (r *shutdownRig) runShutdown() models.CommandResult {
	r.t.Helper()
	r.submit(idA, models.ActionShutdown, nil)
	r.wait()
	return r.resultFor(idA)
}

// setPoweroff sets what the poweroff child returns.
func (r *shutdownRig) setPoweroff(out string, err error) {
	r.smu.Lock()
	defer r.smu.Unlock()
	r.poweroffResult = func() ([]byte, error) { return []byte(out), err }
}

// launchedAndArmed reports that the poweroff child was launched and the
// start-check timer is armed on the fake clock, so a test can Advance past it.
func (r *shutdownRig) launchedAndArmed() bool {
	return r.poweroffs.Load() >= 1 && slices.Contains(r.clk.timerDurations(), models.ShutdownStartCheckBound)
}

// blockingPause makes the gate's sleep wait until release is closed, then advance
// the fake clock. entered is signalled when the gate first sleeps.
func (r *shutdownRig) blockingPause() (entered <-chan struct{}, release func()) {
	in := make(chan struct{}, 1)
	gate := make(chan struct{})
	r.a.pause = func(ctx context.Context, d time.Duration) bool {
		select {
		case in <- struct{}{}:
		default:
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return false
		}
		r.clk.Advance(d)
		return ctx.Err() == nil
	}
	var once sync.Once
	return in, func() { once.Do(func() { close(gate) }) }
}

// ---------------------------------------------------------------------------
// AC-001: a free lock never delays
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC001_FreeLockPowersOffWithNoWait(t *testing.T) {
	r := newShutdownRig(t)

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-001] status")
	assert.Equal(t, "shutdown initiated", res.Message, "[AC-001] the success text")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-001] exactly one poweroff")
	assert.Equal(t, 2, r.calls(), "[AC-001] a free lock is checked twice: before the guards and as the final check")
	assert.Zero(t, r.gatePauses(), "[AC-001] a free lock must add no wait")
}

func TestShutdownClearance_SD_AC001_FreeRunsGuardsThenFinalProbe(t *testing.T) {
	r := newShutdownRig(t)
	var seq []string
	r.script = func(time.Duration, int) dpkgLockStatus {
		seq = append(seq, "probe")
		return freeDpkgLockProbe()
	}

	c := r.a.awaitPowerClearance(context.Background(), shutdownGate(), "test", idA, func(context.Context) string {
		seq = append(seq, "guards")
		return ""
	})
	require.True(t, c.Cleared, "[AC-001] a free lock with passing guards clears")
	seq = append(seq, "execShutdown")
	require.NoError(t, r.a.execShutdown(c.Permit))

	assert.Equal(t, []string{"probe", "guards", "probe", "execShutdown"}, seq,
		"[AC-001] the last check before the poweroff is a lock check, with nothing in between (FR-011)")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-001] one poweroff")
}

// ---------------------------------------------------------------------------
// AC-002 / AC-003: a held lock delays the shutdown
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC002_HeldLockNeverPowersOffWithinBound(t *testing.T) {
	r := newShutdownRig(t)
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		assert.Zero(t, r.poweroffs.Load(), "[AC-002] no poweroff may start while the lock is held (elapsed %s)", elapsed)
		return unattendedUpgrHolder
	}

	res := r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-002] a lock held for the whole bound must never be powered off over")
	assert.Positive(t, r.gatePauses(), "[AC-002] the gate must wait while the lock is held")
	assert.Contains(t, res.Message, "dpkg lock held past the timeout", "[AC-002] the skip is reported")
}

func TestShutdown_SD_AC003_ReleaseAfter3mPowersOffWithin10s(t *testing.T) {
	t.Run("the poweroff starts within 10s of the release", func(t *testing.T) {
		r := newShutdownRig(t)
		release := 3*time.Minute + time.Second
		r.script = heldUntil(release, unattendedUpgrHolder)

		r.runShutdown()

		require.EqualValues(t, 1, r.poweroffs.Load(), "[AC-003] the poweroff must start after the release")
		r.mu.Lock()
		releasedAt := r.firstProbeAt.Add(release)
		r.mu.Unlock()
		r.smu.Lock()
		startedAt := r.poweroffTimes[0]
		r.smu.Unlock()
		assert.LessOrEqual(t, startedAt.Sub(releasedAt), 10*time.Second,
			"[AC-003] the poweroff must start within 10s of the release, started %s after it", startedAt.Sub(releasedAt))
	})
	t.Run("the result says it waited", func(t *testing.T) {
		r := newShutdownRig(t)
		r.script = heldUntil(3*time.Minute, unattendedUpgrHolder)

		res := r.runShutdown()

		assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-003] status")
		assert.Equal(t, "shutdown initiated after waiting 3m for the dpkg lock", res.Message, "[AC-003] the wait is reported")
	})
}

// ---------------------------------------------------------------------------
// AC-004 / AC-005 / AC-006: timeout skips, never forces
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC004_TimeoutSkipsAndNeverForces(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)

	r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-004] a lock held past the bound must skip the shutdown, never force it")
	assert.Equal(t, models.ShutdownDpkgLockWaitBound, r.sinceFirstProbe(), "[AC-004] the wait ends at exactly the 10 minute bound")
}

func TestShutdown_SD_AC004_NoSecondWaitAfterSkip(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	r.runShutdown()
	require.Zero(t, r.poweroffs.Load(), "[AC-004] precondition: the attempt was skipped")
	callsAtSkip := r.calls()

	r.script = func(time.Duration, int) dpkgLockStatus { return freeDpkgLockProbe() }
	r.clk.Advance(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.a.notify.Flush(ctx)

	assert.Equal(t, callsAtSkip, r.calls(), "[AC-004] a skipped attempt must not check the lock again on its own")
	assert.Zero(t, r.poweroffs.Load(), "[AC-004] a skipped shutdown is never retried (BR-002)")
}

func TestShutdown_SD_AC005_TimeoutResultFailedAndNamesHolder(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-005] a skipped shutdown is a failed result")
	assert.Equal(t, "shutdown skipped: dpkg lock held past the timeout (10m), held by unattended-upgr (pid 1234, uid 0)", res.Message,
		"[AC-005] the result names the timeout, the holder, its pid and its uid")
	assert.NotContains(t, res.Message, "initiated", "[AC-005] the result must not say a shutdown started")
	assert.NotEqual(t, models.ReasonTimeout, res.Reason, "[AC-005] FR-016: a dpkg skip is not a budget timeout")
}

func TestShutdown_SD_AC006_TimeoutReportSaysHolderNotVisible(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(dpkgLockStatus{State: dpkgLockHeld, PID: -1, UID: -1})

	res := r.runShutdown()

	assert.Equal(t, "shutdown skipped: dpkg lock held past the timeout (10m), holder not visible", res.Message,
		"[AC-006] a holder that cannot be identified is reported as such")
}

// ---------------------------------------------------------------------------
// AC-007: unknown state fails closed
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC007_UnknownSkipsImmediatelyWithoutWait(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(dpkgLockStatus{State: dpkgLockUnknown, UID: -1, Cause: "cannot read /proc/locks: boom"})

	res := r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-007] an unknown lock state must not power off")
	assert.Zero(t, r.gatePauses(), "[AC-007] an unknown state must not wait")
	assert.Equal(t, 1, r.calls(), "[AC-007] one check, then the skip")
	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-007] a failed result")
	assert.Equal(t, "shutdown skipped: dpkg lock state unknown: cannot read /proc/locks: boom", res.Message, "[AC-007] the state and the cause")
}

func TestShutdown_SD_AC007_HeldThenAbsentNeverPowersOff(t *testing.T) {
	r := newShutdownRig(t)
	r.script = func(_ time.Duration, call int) dpkgLockStatus {
		if call <= 2 {
			return unattendedUpgrHolder
		}
		return dpkgLockStatus{State: dpkgLockAbsent, UID: -1}
	}

	res := r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-007] a lock that was held and then vanished must never clear the shutdown")
	assert.Equal(t, "shutdown skipped: dpkg lock state unknown: dpkg lock files disappeared during the wait", res.Message, "[AC-007] the skip says why")
}

func TestShutdown_SD_AC007_ZeroStatusSkipsAsUnknown(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(dpkgLockStatus{})

	res := r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-007] a zero lock status must not power off")
	assert.Equal(t, "shutdown skipped: dpkg lock state unknown: lock state not recognized", res.Message, "[AC-007] fail closed")
}

// ---------------------------------------------------------------------------
// AC-008: no lock file means no wait
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC008_AbsentPowersOffAsToday(t *testing.T) {
	logs := captureLogs(t)
	r := newShutdownRig(t)
	r.script = alwaysHeld(dpkgLockStatus{State: dpkgLockAbsent, UID: -1})

	res := r.runShutdown()

	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-008] a host without dpkg powers off as today")
	assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-008] status")
	assert.Equal(t, "shutdown initiated", res.Message, "[AC-008] exactly the unchanged text, nothing about dpkg")
	assert.Zero(t, r.gatePauses(), "[AC-008] no wait")
	assert.NotContains(t, logs.String(), "dpkg lock", "[AC-008] no lock-related log line")
}

func TestShutdown_SD_AC008_RunnerGetsUnchangedArgv(t *testing.T) {
	r := newShutdownRig(t)

	r.runShutdown()

	r.smu.Lock()
	defer r.smu.Unlock()
	assert.Equal(t, [][]string{{"sudo", "systemctl", "poweroff"}}, r.poweroffArgv, "[AC-008] the runner sees the previous release's argv")
}

// ---------------------------------------------------------------------------
// AC-009: disabled is refused without a lock check
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC009_AllowShutdownOffRefusedWithoutLockCheck(t *testing.T) {
	r := newShutdownRig(t)
	r.a.config.Commands.AllowShutdown = false
	r.script = alwaysHeld(unattendedUpgrHolder)

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-009] refused")
	assert.Equal(t, "shutdown not allowed by agent config (set commands.allow_shutdown: true)", res.Message, "[AC-009] the unchanged message")
	assert.Zero(t, r.calls(), "[AC-009] no lock check")
	assert.Zero(t, r.gatePauses(), "[AC-009] no wait")
	assert.Zero(t, r.poweroffs.Load(), "[AC-009] no poweroff")
}

// ---------------------------------------------------------------------------
// AC-010 / AC-011 / AC-012: guards after the wait
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC010_AllowShutdownOffDuringWaitSkipsNamingGuard(t *testing.T) {
	// Unit-test-only state: a running agent reads its config once, at start-up.
	r := newShutdownRig(t)
	r.script = func(elapsed time.Duration, call int) dpkgLockStatus {
		if call == 3 {
			r.a.config.Commands.AllowShutdown = false
		}
		if elapsed < 10*time.Second {
			return unattendedUpgrHolder
		}
		return freeDpkgLockProbe()
	}

	res := r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-010] allow_shutdown turned off during the wait must stop the poweroff")
	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-010] failed result")
	assert.Equal(t, "shutdown skipped: guard failed: commands.allow_shutdown is off", res.Message, "[AC-010] the result names the failed guard")
}

func TestShutdownClearance_SD_AC010_GuardOrderIsShutdownThenAllowShutdownThenMaintenance(t *testing.T) {
	r := newShutdownRig(t)
	var running atomic.Bool
	r.a.maintenanceClient = maintenanceFlip(&running)
	guards := r.a.commonShutdownGuards()

	assert.Equal(t, "", guards(context.Background()), "[AC-010] all guards pass")
	running.Store(true)
	assert.Equal(t, "host busy: "+maintenanceRunning, guards(context.Background()), "[AC-010] maintenance guard")
	r.a.config.Commands.AllowShutdown = false
	assert.Equal(t, "commands.allow_shutdown is off", guards(context.Background()), "[AC-010] allow_shutdown is checked before maintenance")
	r.a.shuttingDown.Store(true)
	assert.Equal(t, "agent shutting down", guards(context.Background()), "[AC-010] shutdown is checked first")
}

func TestShutdown_SD_AC011_MaintenanceStartsDuringWaitSkipsNamingGuard(t *testing.T) {
	r := newShutdownRig(t)
	var running atomic.Bool
	r.a.maintenanceClient = maintenanceFlip(&running)
	r.script = func(elapsed time.Duration, call int) dpkgLockStatus {
		if call == 3 {
			running.Store(true) // a rIOt maintenance run starts mid-wait
		}
		if elapsed < 10*time.Second {
			return unattendedUpgrHolder
		}
		return freeDpkgLockProbe()
	}

	res := r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-011] a maintenance run that starts during the wait must stop the shutdown")
	assert.Equal(t, "shutdown skipped: guard failed: host busy: "+maintenanceRunning, res.Message, "[AC-011] the result names the failed guard")
	assert.Equal(t, models.ReasonConflict, res.Reason, "[AC-011] a busy host is a conflict")
}

func TestShutdown_SD_AC012_FinalCheckRetakenKeepsWaitingWithinBound(t *testing.T) {
	logs := captureLogs(t)
	r := newShutdownRig(t)
	freedOnce := false
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed < 4*time.Minute {
			return unattendedUpgrHolder
		}
		if !freedOnce { // free at minute 4 for the first check only, then taken again
			freedOnce = true
			return freeDpkgLockProbe()
		}
		return unattendedUpgrHolder
	}

	res := r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-012] a lock re-taken before the final check must not power off")
	assert.Contains(t, res.Message, "dpkg lock held past the timeout (10m)", "[AC-012] the wait ends in the timeout")
	assert.Equal(t, models.ShutdownDpkgLockWaitBound, r.sinceFirstProbe(), "[AC-012] one deadline, firstHeld + 10m: the total wait never exceeds the bound")
	assert.Equal(t, 1, strings.Count(logs.String(), `msg="dpkg lock held, waiting before shutdown"`), "[AC-012] one wait-started line")
}

func TestShutdownClearance_SD_AC012_GuardsRunAgainAfterAFurtherWait(t *testing.T) {
	r := newShutdownRig(t)
	guardRuns := 0
	freedOnce := false
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		switch {
		case elapsed < 4*time.Minute:
			return unattendedUpgrHolder
		case !freedOnce:
			freedOnce = true
			return freeDpkgLockProbe()
		case elapsed < 6*time.Minute:
			return unattendedUpgrHolder
		}
		return freeDpkgLockProbe()
	}

	c := r.a.awaitPowerClearance(context.Background(), shutdownGate(), "test", idA, func(context.Context) string { guardRuns++; return "" })

	assert.True(t, c.Cleared, "[AC-012] a lock that is finally free clears")
	assert.Equal(t, 2, guardRuns, "[AC-012] FR-011 applies again after any further wait")
	assert.Equal(t, 6*time.Minute, c.Waited, "[AC-012] waited from the first held check to the clearance")
}

// ---------------------------------------------------------------------------
// AC-013: agent stop cancels the wait
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC013_AgentStopEndsWaitWithin5s(t *testing.T) {
	r := newShutdownRig(t)
	var stoppedAt time.Time
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed >= time.Minute && stoppedAt.IsZero() {
			stoppedAt = r.clk.Now()
			r.a.shuttingDown.Store(true) // SIGTERM
		}
		return unattendedUpgrHolder
	}

	r.runShutdown()

	assert.Zero(t, r.poweroffs.Load(), "[AC-013] no poweroff after the agent was told to stop")
	assert.LessOrEqual(t, r.clk.Now().Sub(stoppedAt), 5*time.Second, "[AC-013] the wait must end within 5 seconds of the stop")
}

func TestShutdown_SD_AC013_ShutdownResultReasonShutdown(t *testing.T) {
	r := newShutdownRig(t)
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed >= 10*time.Second {
			r.a.shuttingDown.Store(true)
		}
		return unattendedUpgrHolder
	}

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-013] failed result")
	assert.Equal(t, models.ReasonShutdown, res.Reason, "[AC-013] reason shutdown")
	assert.Equal(t, "shutdown skipped: agent shutting down", res.Message, "[AC-013] exact text")
	assert.Zero(t, r.poweroffs.Load(), "[AC-013] no poweroff")
}

// ---------------------------------------------------------------------------
// AC-014: no poweroff after a not-started outcome or an expired budget
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC014_NoPoweroffAfterSkippedResult(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	r.runShutdown()
	require.Equal(t, models.ResultStatusError, r.resultFor(idA).Status, "[AC-014] precondition: the shutdown was reported as skipped")
	callsAtSkip := r.calls()

	r.script = func(time.Duration, int) dpkgLockStatus { return freeDpkgLockProbe() } // the lock is released afterwards
	r.clk.Advance(time.Hour)
	r.submit(idA, models.ActionShutdown, nil) // the server redelivers the same command
	r.wait()

	assert.Zero(t, r.poweroffs.Load(), "[AC-014] the agent must not power off for a command it reported as not started")
	assert.Equal(t, callsAtSkip, r.calls(), "[AC-014] the redelivery is deduplicated and checks nothing")
}

func TestShutdown_SD_AC014_CommandBudgetExhaustedIsATruthfulSkip(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	r.a.pause = func(context.Context, time.Duration) bool { return false } // the command context deadline fired

	c := r.a.awaitShutdownClearance(context.Background(), idA)
	res := shutdownSkippedResult(c)

	assert.False(t, c.Cleared, "[AC-014] an exhausted command budget never powers off")
	assert.Equal(t, "shutdown skipped: guard failed: agent command time budget exhausted", res.Message, "[AC-014] the outcome is the truthful skip")
	assert.NotEqual(t, models.ReasonTimeout, res.Reason, "[AC-014] FR-016: not recorded as a budget timeout")
	assert.Zero(t, r.poweroffs.Load(), "[AC-014] no poweroff")
}

// ---------------------------------------------------------------------------
// AC-015: launch failure
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC015_LaunchErrorIsFailedResult(t *testing.T) {
	r := newShutdownRig(t)
	r.setPoweroff("", errors.New(`exec: "sudo": executable file not found in $PATH`))

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-015] a launch error is a failed result")
	assert.Equal(t, `shutdown: exec: "sudo": executable file not found in $PATH`, res.Message, "[AC-015] the form is shutdown: <err>")
	assert.NotContains(t, res.Message, "initiated", "[AC-015] the result must not say a shutdown was initiated")
}

// ---------------------------------------------------------------------------
// AC-016: a full wait is not recorded as timed out
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC016_NineMinutesFiftyWaitReportsStarted(t *testing.T) {
	r := newShutdownRig(t)
	r.script = heldUntil(9*time.Minute+50*time.Second, unattendedUpgrHolder)

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-016] the outcome is an initiated shutdown")
	assert.Equal(t, "shutdown initiated after waiting 9m50s for the dpkg lock", res.Message, "[AC-016] the result states the wait")
	assert.NotEqual(t, models.ReasonTimeout, res.Reason, "[AC-016] not recorded as timed out by the agent")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-016] the poweroff started once")
}

// ---------------------------------------------------------------------------
// AC-018 / AC-019: independent specs
// ---------------------------------------------------------------------------

func TestPowerClearance_SD_AC018_EachGateSkipsAtItsOwnBound(t *testing.T) {
	for _, bound := range []time.Duration{3 * time.Minute, 7 * time.Minute} {
		t.Run(bound.String(), func(t *testing.T) {
			r := newShutdownRig(t)
			r.script = alwaysHeld(unattendedUpgrHolder)
			g := shutdownGate()
			g.waitBound = bound

			c := r.a.awaitPowerClearance(context.Background(), g, "test", idA, nil)

			assert.False(t, c.Cleared, "[AC-018] a lock held past the gate's own bound skips")
			assert.Equal(t, skipLockTimeout, c.Skip, "[AC-018] skip kind")
			assert.Equal(t, bound, c.Waited, "[AC-018] the wait ends at the gate's own bound, not at another action's")
			assert.Equal(t, bound, c.Bound, "[AC-018] the clearance carries its own bound")
			assert.Contains(t, c.reason(), "("+models.FormatDurationShort(bound)+")", "[AC-018] the text names the gate's own bound")
		})
	}
}

func TestShutdownClearance_SD_AC018_ShutdownGateUsesShutdownBound(t *testing.T) {
	g := shutdownGate()
	assert.Equal(t, models.ShutdownDpkgLockWaitBound, g.waitBound, "[AC-018] the shutdown gate uses the shutdown bound")
	assert.Equal(t, powerActionShutdown, g.action, "[AC-018] the shutdown gate mints shutdown permits")
	assert.Equal(t, "dpkg lock held, waiting before shutdown", g.waitMsg, "[AC-018] wait message")
	assert.Equal(t, "commands.allow_shutdown is off", g.allowOffText, "[AC-018] allow-off text")

	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	c := r.a.awaitShutdownClearance(context.Background(), idA)
	assert.Equal(t, models.ShutdownDpkgLockWaitBound, c.Bound, "[AC-018] the entry point's clearance carries the shutdown bound")
	assert.Equal(t, models.ShutdownDpkgLockWaitBound, c.Waited, "[AC-018] it skipped at the shutdown bound")
}

func TestRebootGate_SD_AC019_RebootSpecUnchanged(t *testing.T) {
	g := rebootGate()
	assert.Equal(t, models.DpkgLockWaitBound, g.waitBound, "[AC-019] the reboot wait bound")
	assert.Equal(t, 10*time.Minute, g.waitBound, "[AC-019] still 10 minutes")
	assert.Equal(t, powerActionReboot, g.action, "[AC-019] the reboot gate mints reboot permits")
	assert.Equal(t, "dpkg lock held, waiting before reboot", g.waitMsg, "[AC-019] the unchanged reboot wait message")
	assert.Equal(t, "commands.allow_reboot is off", g.allowOffText, "[AC-019] the unchanged allow-off text")
	assert.Equal(t, 12*time.Minute, models.AgentHardMax(models.ActionReboot, 1), "[AC-019] reboot agent budget")
	assert.Equal(t, 20*time.Minute, models.ExecDeadline(models.ActionReboot, 1), "[AC-019] reboot server deadline")
}

// ---------------------------------------------------------------------------
// AC-021 / AC-022: host reservation and restart
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC021_SecondShutdownOrRebootDuringWaitIsHostBusy(t *testing.T) {
	r := newShutdownRig(t)
	r.script = heldUntil(4*time.Second, unattendedUpgrHolder)
	entered, release := r.blockingPause()
	defer release()

	r.submit(idA, models.ActionShutdown, nil)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("[AC-021] the first shutdown never started waiting")
	}
	callsMidWait := r.calls()

	r.submit(idB, models.ActionShutdown, nil)
	r.submit(sdIDC, models.ActionReboot, nil)
	hasResult := func(id string) bool {
		for _, e := range r.a.out.Snapshot() {
			if e.CommandID == id {
				return true
			}
		}
		return false
	}
	require.Eventually(t, func() bool { return hasResult(idB) && hasResult(sdIDC) }, 5*time.Second, time.Millisecond, "[AC-021] both extra commands must be answered while the first waits")

	for _, id := range []string{idB, sdIDC} {
		res := r.resultFor(id)
		assert.Equal(t, models.ResultStatusError, res.Status, "[AC-021] %s is refused", id)
		assert.Equal(t, models.ReasonConflict, res.Reason, "[AC-021] %s is refused as a conflict (host busy)", id)
		assert.Contains(t, res.Message, "(command "+idA+")", "[AC-021] %s names the command holding the host", id)
	}
	assert.Equal(t, callsMidWait, r.calls(), "[AC-021] no second wait starts: no further lock check")

	release()
	r.wait()
	assert.Equal(t, "shutdown initiated after waiting 4s for the dpkg lock", r.resultFor(idA).Message, "[AC-021] the first shutdown still completes")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-021] exactly one poweroff")
	assert.Zero(t, r.reboots.Load(), "[AC-021] the refused reboot never started")
}

func TestShutdown_SD_AC022_RestartDoesNotRerunShutdown(t *testing.T) {
	r := newShutdownRig(t)
	r.script = heldUntil(4*time.Second, unattendedUpgrHolder)
	entered, release := r.blockingPause()
	defer release()
	r.submit(idA, models.ActionShutdown, nil)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("[AC-022] the shutdown never started waiting")
	}
	_, marked := r.a.inflight.Get(idA)
	require.True(t, marked, "[AC-022] precondition: the waiting shutdown has an in-flight marker")

	// A new process over the same state files, with its own counters.
	restarted := newLifecycleHarnessAt(t, filepath.Dir(r.seenPath))
	var probes, runs atomic.Int32
	restarted.a.config.Commands.AllowShutdown = true
	restarted.a.dpkgLockProbe = func() dpkgLockStatus { probes.Add(1); return freeDpkgLockProbe() }
	restarted.a.runner = func(context.Context, string, ...string) ([]byte, error) { runs.Add(1); return nil, nil }
	restarted.a.recoverInterruptedCommands(context.Background())
	restarted.submit(idA, models.ActionShutdown, nil) // the server redelivers it
	restarted.wait()

	assert.Zero(t, runs.Load(), "[AC-022] the restarted agent must not run anything for the old shutdown")
	assert.Zero(t, probes.Load(), "[AC-022] nor check the lock for it")
	assert.Zero(t, restarted.a.out.Len(), "[AC-022] and report nothing for it")
	_, stillMarked := restarted.a.inflight.Get(idA)
	assert.False(t, stillMarked, "[AC-022] the marker is cleared without a result")

	r.a.shuttingDown.Store(true) // let the first agent's gate end
	release()
	r.wait()
}

// ---------------------------------------------------------------------------
// AC-023: bounded growth
// ---------------------------------------------------------------------------

func TestShutdownClearance_SD_AC023_FullWaitBoundedOutput(t *testing.T) {
	logs := captureLogs(t)
	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	dir := filepath.Dir(r.seenPath)
	before := listStateDir(t, dir)

	c := r.a.awaitShutdownClearance(context.Background(), idA)

	require.False(t, c.Cleared, "[AC-023] precondition: the full wait ended in a skip")
	assert.LessOrEqual(t, r.calls(), 303, "[AC-023] at most 10m/2s + 3 lock checks per attempt")
	assert.EqualValues(t, 0, r.a.powerPermitSeq.Load(), "[AC-023] a skipped attempt mints no permit")
	assert.Equal(t, before, listStateDir(t, dir), "[AC-023] the gate must not create any file")
	out := logs.String()
	assert.Equal(t, 2, linesWith(out, `msg="dpkg lock `), "[AC-023] exactly the wait-started and wait-ended lines, none per check")
	assert.Equal(t, 2, linesWith(out, "level=INFO", `msg="dpkg lock `), "[AC-023] both gate lines are Info")
	assert.Zero(t, linesWith(out, "level=DEBUG", "dpkg"), "[AC-023] nothing is logged at debug level, so nothing grows per check")
}

func TestShutdown_SD_AC023_FullWaitOneResultOnlyTheFailedCommandEvent(t *testing.T) {
	r := newShutdownRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)

	res := r.runShutdown()

	require.Equal(t, models.ResultStatusError, res.Status, "[AC-023] precondition: the full wait ended in a skip")
	assert.Equal(t, 1, r.a.out.Len(), "[AC-023] exactly one command result")
	events := r.agentEventMessages()
	require.Len(t, events, 1, "[AC-023] exactly one agent event")
	assert.Equal(t, "Command shutdown failed: "+res.Message, events[0], "[AC-023] it is the existing failed-command warning")
	r.tr.mu.Lock()
	assert.Equal(t, models.EventCommandCompleted, r.tr.agentEvents[0].Type, "[AC-023] no new event type")
	assert.Equal(t, models.SeverityWarning, r.tr.agentEvents[0].Severity, "[AC-023] a warning")
	r.tr.mu.Unlock()
	assert.Zero(t, r.telemetryPending(), "[AC-023] no telemetry refresh")
}

// ---------------------------------------------------------------------------
// AC-024: no new privileges, same argv
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC024_RunsOnlyTheExistingPoweroffArgv(t *testing.T) {
	r := newShutdownRig(t)
	var ran []string
	inner := r.a.runner
	r.a.runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return inner(ctx, name, args...)
	}
	r.script = heldUntil(10*time.Second, unattendedUpgrHolder)

	c := r.a.awaitShutdownClearance(context.Background(), idA)
	require.True(t, c.Cleared, "[AC-024] precondition: cleared")
	require.NoError(t, r.a.execShutdown(c.Permit))

	assert.Equal(t, []string{"sudo systemctl poweroff"}, ran,
		"[AC-024] the lock probe and the gate run no process (the guards' systemctl show needs the maintenance helper); the poweroff is the only one and its argv is unchanged")
}

// ---------------------------------------------------------------------------
// AC-027 / AC-028 / AC-029 / AC-030 / AC-032: the truthful result
// ---------------------------------------------------------------------------

func TestShutdown_SD_AC027_ExitZeroReportsInitiated(t *testing.T) {
	cases := []struct {
		name string
		hold time.Duration
		want string
	}{
		{"no wait", 0, "shutdown initiated"},
		{"3 minute wait", 3 * time.Minute, "shutdown initiated after waiting 3m for the dpkg lock"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newShutdownRig(t)
			r.script = heldUntil(tc.hold, unattendedUpgrHolder)
			r.setPoweroff("", nil)

			res := r.runShutdown()

			assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-027] success")
			assert.Equal(t, tc.want, res.Message, "[AC-027] message")
		})
	}
}

func TestShutdown_SD_AC028_NonZeroExitReportsFailed(t *testing.T) {
	r := newShutdownRig(t)
	r.setPoweroff(sudoNoTTY, fakeExit{code: 1})

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-028] a refused poweroff is a failed result")
	assert.Equal(t, "shutdown: exit status 1: "+strings.TrimSpace(sudoNoTTY), res.Message, "[AC-028] the form is shutdown: <err>")
	assert.NotContains(t, res.Message, "initiated", "[AC-028] the result does not say a shutdown was initiated")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-028] the command was launched once")
}

func TestShutdown_SD_AC029_LongErrorIsBoundedInTheResult(t *testing.T) {
	r := newShutdownRig(t)
	r.setPoweroff("\x1b[31m"+strings.Repeat("sudo: denied\r\n\t", 400)+"\x00", fakeExit{code: 1})

	res := r.runShutdown()

	require.Equal(t, models.ResultStatusError, res.Status, "[AC-029] failed")
	require.True(t, strings.HasPrefix(res.Message, "shutdown: "), "[AC-029] the form is shutdown: <err>")
	text := strings.TrimPrefix(res.Message, "shutdown: ")
	assert.LessOrEqual(t, len(text), 200, "[AC-029] the <err> text is at most 200 bytes, got %d", len(text))
	for _, ch := range text {
		assert.True(t, unicode.IsPrint(ch), "[AC-029] rune %U in the <err> text is not printable", ch)
	}
}

func TestShutdown_SD_AC030_StillRunningAtBoundIsInitiatedAndReaped(t *testing.T) {
	r := newShutdownRig(t)
	r.poweroffBlock = make(chan struct{})
	defer func() {
		select {
		case <-r.poweroffBlock:
		default:
			close(r.poweroffBlock)
		}
	}()

	r.submit(idA, models.ActionShutdown, nil)
	require.Eventually(t, r.launchedAndArmed,
		5*time.Second, time.Millisecond, "[AC-030] the start-check timer must be armed once the poweroff is launched")
	r.smu.Lock()
	launchedAt := r.poweroffTimes[0]
	r.smu.Unlock()
	r.clk.Advance(models.ShutdownStartCheckBound)
	r.wait()

	res := r.resultFor(idA)
	assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-030] still running at the bound counts as initiated")
	assert.Equal(t, "shutdown initiated", res.Message, "[AC-030] message")
	assert.Equal(t, models.ShutdownStartCheckBound, r.clk.Now().Sub(launchedAt), "[AC-030] the result is ready no later than the bound after launch")
	assert.True(t, r.a.poweroffRunning.Load(), "[AC-030] the child is still being waited for")

	close(r.poweroffBlock) // the child exits
	require.Eventually(t, func() bool { return !r.a.poweroffRunning.Load() }, 5*time.Second, time.Millisecond,
		"[AC-030] the exit is collected and nothing stays running")
}

func TestShutdown_SD_AC030_SecondLaunchWhileChildRunsIsRefused(t *testing.T) {
	r := newShutdownRig(t)
	r.poweroffBlock = make(chan struct{})
	defer func() {
		select {
		case <-r.poweroffBlock:
		default:
			close(r.poweroffBlock)
		}
	}()
	r.submit(idA, models.ActionShutdown, nil)
	require.Eventually(t, r.launchedAndArmed,
		5*time.Second, time.Millisecond, "[AC-030] precondition: the first poweroff is launched")
	r.clk.Advance(models.ShutdownStartCheckBound)
	r.wait()
	require.True(t, r.a.poweroffRunning.Load(), "[AC-030] precondition: the child is still alive")

	r.submit(idB, models.ActionShutdown, nil)
	r.wait()

	res := r.resultFor(idB)
	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-030] a second launch is a failed result")
	assert.Equal(t, "shutdown: a previous poweroff command is still running", res.Message, "[AC-030] it says why")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-030] at most one poweroff child runs per agent")
}

// Post-QA regression: the one-poweroff flag is cleared before the exit is handed
// over, so the next shutdown can start the moment the previous one has returned.
// Under the old ordering (a deferred clear after the hand-off) a launch in that
// window was refused as "a previous poweroff command is still running". The window
// cannot be forced without a hook, so this test repeats back-to-back launches; with
// the flag cleared first, the clear happens-before the receive and every iteration
// passes deterministically.
func TestExecShutdown_SD_AC030_FlagClearedBeforeHandOffAllowsBackToBackLaunches(t *testing.T) {
	r := newShutdownRig(t)
	const rounds = 2000

	for i := 0; i < rounds; i++ {
		require.NoError(t, r.a.execShutdown(r.a.mintPowerPermit(powerActionShutdown)),
			"[AC-030] launch %d must not be refused: the previous child had already exited", i)
		require.False(t, r.a.poweroffRunning.Load(),
			"[AC-030] once the exit is handed over the one-child flag must already be clear (launch %d)", i)
	}

	assert.EqualValues(t, rounds, r.poweroffs.Load(), "[AC-030] every launch ran its poweroff once")
}

func TestShutdown_SD_AC030_LateNonZeroExitLogsOneWarn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		exit     error
		wantWarn int
	}{
		{"a late refusal", fakeExit{code: 1}, 1},
		{"a late exit 0", nil, 0},
		{"a late signal", fakeExit{code: -1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			r := newShutdownRig(t)
			r.poweroffBlock = make(chan struct{})
			r.setPoweroff("denied", tc.exit)
			r.submit(idA, models.ActionShutdown, nil)
			require.Eventually(t, r.launchedAndArmed,
				5*time.Second, time.Millisecond, "[AC-030] precondition: launched")
			r.clk.Advance(models.ShutdownStartCheckBound)
			r.wait()
			require.Equal(t, "shutdown initiated", r.resultFor(idA).Message, "[AC-030] precondition: reported initiated at the bound")

			close(r.poweroffBlock)
			require.Eventually(t, func() bool { return !r.a.poweroffRunning.Load() }, 5*time.Second, time.Millisecond, "[AC-030] reaped")

			// The flag is cleared before the late exit is logged, so the WARN can land
			// just after the reap is observed: wait for it rather than read once.
			warns := func() int {
				return linesWith(logs.String(), "level=WARN", "poweroff command failed after the start check")
			}
			if tc.wantWarn > 0 {
				require.Eventually(t, func() bool { return warns() >= tc.wantWarn }, 5*time.Second, time.Millisecond,
					"[AC-030] warn line for %s", tc.name)
			}
			assert.Equal(t, tc.wantWarn, warns(), "[AC-030] warn lines for %s", tc.name)
		})
	}
}

func TestShutdown_SD_AC032_SignalTerminationIsInitiated(t *testing.T) {
	r := newShutdownRig(t)
	r.setPoweroff("", fakeExit{code: -1})

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-032] a child killed by a signal counts as initiated (R-10)")
	assert.Equal(t, "shutdown initiated", res.Message, "[AC-032] message")
}

// ---------------------------------------------------------------------------
// AST checks on the exec functions (never the real branch)
// ---------------------------------------------------------------------------

// parsedFunc parses commands.go and returns the FuncDecl named name.
func parsedFunc(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()
	src, err := os.ReadFile("commands.go")
	require.NoError(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), "commands.go", src, 0)
	require.NoError(t, err)
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("%s not found in commands.go", name)
	return nil
}

// execCommandArgs returns the string-literal arguments of every exec.Command call
// in fn, one slice per call.
func execCommandArgs(fn *ast.FuncDecl) [][]string {
	var out [][]string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "exec" || sel.Sel.Name != "Command" {
			return true
		}
		var args []string
		for _, a := range call.Args {
			if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				s, _ := strconv.Unquote(lit.Value)
				args = append(args, s)
			}
		}
		out = append(out, args)
		return true
	})
	return out
}

func TestExecShutdown_SD_AC008_WindowsArgvUnchanged(t *testing.T) {
	got := execCommandArgs(parsedFunc(t, "execShutdown"))

	assert.ElementsMatch(t, [][]string{
		{"shutdown", "/s", "/t", "5"},
		{"sudo", "systemctl", "poweroff"},
	}, got, "[AC-008] execShutdown launches exactly the previous release's two argvs")
}

func TestExecShutdown_SD_AC030_ProductionBranchStartsThenWaits(t *testing.T) {
	fn := parsedFunc(t, "execShutdown")
	var starts, waits []token.Pos
	waitDelaySet, commandContext := false, false
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if recv, ok := sel.X.(*ast.Ident); ok && recv.Name == "cmd" {
					switch sel.Sel.Name {
					case "Start":
						starts = append(starts, x.Pos())
					case "Wait":
						waits = append(waits, x.Pos())
					}
				}
				if sel.Sel.Name == "CommandContext" {
					commandContext = true
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "WaitDelay" {
					if recv, ok := sel.X.(*ast.Ident); ok && recv.Name == "cmd" {
						waitDelaySet = true
					}
				}
			}
		}
		return true
	})

	require.Len(t, starts, 1, "[AC-030] the real branch starts the one *exec.Cmd once")
	require.Len(t, waits, 1, "[AC-030] and waits for the same *exec.Cmd once, so the child is reaped")
	assert.Less(t, starts[0], waits[0], "[AC-030] Start comes before Wait")
	assert.True(t, waitDelaySet, "[AC-030] WaitDelay is set, so a grandchild holding the pipe cannot hold up Wait")
	assert.False(t, commandContext, "[AC-030] exec.CommandContext is never used: a cancelled command must not kill a poweroff")
}

func TestExecReboot_SD_AC032_RebootLaunchUnchanged(t *testing.T) {
	fn := parsedFunc(t, "execReboot")
	var starts, waits, timers int
	ast.Inspect(fn, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Start":
					starts++
				case "Wait":
					waits++
				case "NewTimer":
					timers++
				}
			}
		}
		return true
	})

	assert.Equal(t, 1, starts, "[AC-032] the reboot is started with Start()")
	assert.Zero(t, waits, "[AC-032] and not waited for (FR-031)")
	assert.Zero(t, timers, "[AC-032] with no start-check timer")
	assert.ElementsMatch(t, [][]string{
		{"shutdown", "/r", "/t", "5"},
		{"sudo", "systemctl", "reboot"},
	}, execCommandArgs(fn), "[AC-032] the reboot argv is unchanged")
}

// ---------------------------------------------------------------------------
// C-3: permits are bound to their action and to the allow flag
// ---------------------------------------------------------------------------

func TestExecShutdown_SD_C3_NonClearedPermitStartsNoPoweroff(t *testing.T) {
	t.Run("zero permit", func(t *testing.T) {
		r := newShutdownRig(t)

		err := r.a.execShutdown(powerPermit{})

		assert.ErrorIs(t, err, errShutdownNotCleared, "[C-3] a zero permit must be refused")
		assert.Zero(t, r.poweroffs.Load(), "[C-3] a refused permit starts no poweroff")
	})
	t.Run("the permit of a skipped clearance", func(t *testing.T) {
		r := newShutdownRig(t)
		skipped := powerClearance{Skip: skipLockTimeout}

		err := r.a.execShutdown(skipped.Permit)

		assert.ErrorIs(t, err, errShutdownNotCleared, "[C-3] a skipped clearance carries no valid permit")
		assert.Zero(t, r.poweroffs.Load(), "[C-3] a refused permit starts no poweroff")
	})
	t.Run("a permit used twice", func(t *testing.T) {
		r := newShutdownRig(t)
		p := r.a.mintPowerPermit(powerActionShutdown)
		require.NoError(t, r.a.execShutdown(p), "[C-3] first use")

		err := r.a.execShutdown(p)

		assert.ErrorIs(t, err, errShutdownNotCleared, "[C-3] a permit is single-use")
		assert.EqualValues(t, 1, r.poweroffs.Load(), "[C-3] only the first use started a poweroff")
	})
	t.Run("an older permit after a newer one was minted", func(t *testing.T) {
		r := newShutdownRig(t)
		older := r.a.mintPowerPermit(powerActionShutdown)
		newer := r.a.mintPowerPermit(powerActionShutdown)

		assert.ErrorIs(t, r.a.execShutdown(older), errShutdownNotCleared, "[C-3] a stale permit must be refused")
		assert.Zero(t, r.poweroffs.Load(), "[C-3] a stale permit starts no poweroff")
		assert.NoError(t, r.a.execShutdown(newer), "[C-3] the newest permit still works")
		assert.EqualValues(t, 1, r.poweroffs.Load(), "[C-3] only the newest permit started a poweroff")
	})
}

func TestExecShutdown_SD_C3_RebootPermitIsRefusedAndNotConsumed(t *testing.T) {
	r := newShutdownRig(t)
	p := r.a.mintPowerPermit(powerActionReboot)

	err := r.a.execShutdown(p)

	assert.ErrorIs(t, err, errShutdownNotCleared, "[C-3] a reboot permit must not start a poweroff")
	assert.Zero(t, r.poweroffs.Load(), "[C-3] no poweroff")
	assert.Equal(t, p.permitID, r.a.powerPermitLive.Load(), "[C-3] the refusal leaves the live permit intact")
	assert.NoError(t, r.a.execReboot(p), "[C-3] the rightful reboot still works")
	assert.EqualValues(t, 1, r.reboots.Load(), "[C-3] and starts exactly one reboot")
}

func TestExecReboot_SD_C3_ShutdownPermitIsRefusedAndNotConsumed(t *testing.T) {
	r := newShutdownRig(t)
	p := r.a.mintPowerPermit(powerActionShutdown)

	err := r.a.execReboot(p)

	assert.ErrorIs(t, err, errRebootNotCleared, "[C-3] a shutdown permit must not start a reboot")
	assert.Zero(t, r.reboots.Load(), "[C-3] no reboot")
	assert.Equal(t, p.permitID, r.a.powerPermitLive.Load(), "[C-3] the refusal leaves the live permit intact")
	assert.NoError(t, r.a.execShutdown(p), "[C-3] the rightful shutdown still works")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[C-3] and starts exactly one poweroff")
}

func TestExecShutdown_SD_C3_RefusalRunsNothing(t *testing.T) {
	a := &Agent{}
	called := false
	a.runner = func(context.Context, string, ...string) ([]byte, error) { called = true; return nil, nil }

	err := a.execShutdown(powerPermit{})

	assert.ErrorIs(t, err, errShutdownNotCleared, "[C-3] refused")
	assert.False(t, called, "[C-3] a refused permit must not reach the runner or exec")
	assert.False(t, a.poweroffRunning.Load(), "[C-3] a refused permit starts no child")
}

func TestExecShutdown_SD_C3_ClearedPermitStartsExactlyOnce(t *testing.T) {
	r := newShutdownRig(t)

	c := r.a.awaitShutdownClearance(context.Background(), idA)
	require.True(t, c.Cleared, "[C-3] precondition: the gate cleared")

	require.NoError(t, r.a.execShutdown(c.Permit), "[C-3] the cleared permit starts the poweroff")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[C-3] exactly one poweroff")
	assert.ErrorIs(t, r.a.execShutdown(c.Permit), errShutdownNotCleared, "[C-3] the same permit cannot start a second poweroff")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[C-3] still exactly one poweroff")
	assert.EqualValues(t, 1, r.a.powerPermitSeq.Load(), "[C-3] one permit minted for one attempt")
}

// TestPowerClearance_SD_C3_AllowFlagGatesTheMintWhateverTheGuards is the runtime
// counterpart of the AST rules: with the action's own flag off and the other
// action's flag on, no entry point mints a permit, whatever guards it is given.
func TestPowerClearance_SD_C3_AllowFlagGatesTheMintWhateverTheGuards(t *testing.T) {
	type entry struct {
		name     string
		gate     func() powerGate
		offFlag  func(a *Agent)
		onFlag   func(a *Agent)
		wantText string
	}
	entries := []entry{
		{"shutdown", shutdownGate,
			func(a *Agent) { a.config.Commands.AllowShutdown = false },
			func(a *Agent) { a.config.Commands.AllowReboot = true },
			guardAllowShutdownOff},
		{"reboot", rebootGate,
			func(a *Agent) { a.config.Commands.AllowReboot = false },
			func(a *Agent) { a.config.Commands.AllowShutdown = true },
			guardAllowRebootOff},
	}
	for _, e := range entries {
		guardsFor := map[string]func(a *Agent) powerGuards{
			"nil guards":            func(*Agent) powerGuards { return nil },
			"no-op guards":          func(*Agent) powerGuards { return noGuards },
			"the reboot guards":     func(a *Agent) powerGuards { return a.commonRebootGuards("") },
			"the shutdown guards":   func(a *Agent) powerGuards { return a.commonShutdownGuards() },
			"always-pass the other": func(*Agent) powerGuards { return func(context.Context) string { return "" } },
		}
		for gname, mk := range guardsFor {
			t.Run(e.name+"/"+gname, func(t *testing.T) {
				r := newShutdownRig(t)
				e.offFlag(r.a)
				e.onFlag(r.a)

				c := r.a.awaitPowerClearance(context.Background(), e.gate(), "test", idA, mk(r.a))

				assert.False(t, c.Cleared, "[C-3] an action whose own flag is off must not clear")
				assert.Equal(t, skipGuard, c.Skip, "[C-3] skip kind")
				assert.Equal(t, e.wantText, c.Detail, "[C-3] the skip names the action's own flag")
				assert.EqualValues(t, 0, r.a.powerPermitSeq.Load(), "[C-3] no permit was minted")
			})
		}
	}

	t.Run("a nil config is not allowed", func(t *testing.T) {
		a := &Agent{}
		a.dpkgLockProbe = freeDpkgLockProbe

		c := a.awaitPowerClearance(context.Background(), shutdownGate(), "test", idA, nil)

		assert.False(t, c.Cleared, "[C-3] a gate with no config must not clear")
		assert.EqualValues(t, 0, a.powerPermitSeq.Load(), "[C-3] no permit was minted")
	})
}
