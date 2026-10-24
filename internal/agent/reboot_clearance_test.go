package agent

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// REBOOT-DPKG-001 AD-002 / AD-003 / AD-005 / AD-007: the shared reboot gate, its
// three call paths and the single-use permit. Every test runs on the fake clock
// with a scripted dpkg lock probe: no real sleep, no real /proc, no real lock.

// freeDpkgLockProbe is the harness default: the dpkg lock reads as free, so a
// reboot path clears with no sleep and no read of the real /proc.
func freeDpkgLockProbe() dpkgLockStatus { return dpkgLockStatus{State: dpkgLockFree, UID: -1} }

// unattendedUpgrHolder is the AC-006 example holder.
var unattendedUpgrHolder = dpkgLockStatus{State: dpkgLockHeld, PID: 1234, Comm: "unattended-upgr", UID: 0}

const unattendedUpgrText = "held by unattended-upgr (pid 1234, uid 0)"

type rebootPath string

const (
	pathStandalone   rebootPath = "standalone"
	pathUnauthorized rebootPath = "unauthorized"
	pathAuthorized   rebootPath = "authorized"
)

var allRebootPaths = []rebootPath{pathStandalone, pathUnauthorized, pathAuthorized}

// gateRig drives the three reboot paths through the real command lifecycle with
// a scripted lock probe and a pause seam that advances the fake clock.
type gateRig struct {
	*authHarness

	mu                 sync.Mutex
	script             func(elapsed time.Duration, call int) dpkgLockStatus
	probeCalls         int
	firstProbeAt       time.Time
	pauses             []time.Duration
	pausesAtFirstProbe int
	rebootTimes        []time.Time
}

func newGateRig(t *testing.T) *gateRig {
	t.Helper()
	h := newAuthHarness(t, true, true)
	h.a.telemetryNow = make(chan struct{}, 8) // room to count triggers (the real channel coalesces to 1)
	r := &gateRig{authHarness: h}
	r.script = func(time.Duration, int) dpkgLockStatus { return freeDpkgLockProbe() }
	h.a.dpkgLockProbe = r.probe
	h.a.pause = r.pause
	inner := h.a.runner
	h.a.runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && len(args) > 0 && args[0] == "systemctl" {
			r.mu.Lock()
			r.rebootTimes = append(r.rebootTimes, h.clk.Now())
			r.mu.Unlock()
		}
		return inner(ctx, name, args...)
	}
	return r
}

// probe is the scripted dpkgLockProbe. elapsed counts from the first probe.
func (r *gateRig) probe() dpkgLockStatus {
	r.mu.Lock()
	r.probeCalls++
	calls := r.probeCalls
	now := r.clk.Now()
	if calls == 1 {
		r.firstProbeAt, r.pausesAtFirstProbe = now, len(r.pauses)
	}
	elapsed := now.Sub(r.firstProbeAt)
	script := r.script
	r.mu.Unlock()
	return script(elapsed, calls)
}

func (r *gateRig) pause(ctx context.Context, d time.Duration) bool {
	r.mu.Lock()
	r.pauses = append(r.pauses, d)
	r.mu.Unlock()
	r.clk.Advance(d)
	return ctx.Err() == nil
}

// gatePauses is the number of pauses taken after the first probe, that is, by the
// gate itself. The paths' own delivery pauses come before it.
func (r *gateRig) gatePauses() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.probeCalls == 0 {
		return 0
	}
	return len(r.pauses) - r.pausesAtFirstProbe
}

func (r *gateRig) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.probeCalls
}

// sinceFirstProbe is the fake time the attempt has used so far.
func (r *gateRig) sinceFirstProbe() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.clk.Now().Sub(r.firstProbeAt)
}

func heldUntil(release time.Duration, held dpkgLockStatus) func(time.Duration, int) dpkgLockStatus {
	return func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed < release {
			return held
		}
		return freeDpkgLockProbe()
	}
}

func alwaysHeld(held dpkgLockStatus) func(time.Duration, int) dpkgLockStatus {
	return func(time.Duration, int) dpkgLockStatus { return held }
}

// drive runs one reboot attempt on path to the end, including the post-result
// goroutines of the two post-patch paths.
func (r *gateRig) drive(path rebootPath) {
	r.t.Helper()
	switch path {
	case pathStandalone:
		r.submit(idA, models.ActionReboot, nil)
		r.wait()
	case pathUnauthorized:
		run := successRun("ok")
		run.RebootClassApplied = []string{"linux-image-amd64"}
		r.runOSUpdate(false, run)
		r.awaitReservationReleased()
	case pathAuthorized:
		r.runOSUpdate(true, successRun("ok"))
		r.awaitReservationReleased()
	}
}

// awaitReservationReleased waits for the post-patch goroutine to end: its last
// act is to release the host reservation it holds for the pending reboot.
func (r *gateRig) awaitReservationReleased() {
	r.t.Helper()
	require.Eventually(r.t, func() bool {
		_, held := r.a.locks.Holder(models.TargetKeyHost)
		return !held
	}, 5*time.Second, time.Millisecond, "the post-patch goroutine must end and release the host reservation")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.a.notify.Flush(ctx)
}

// report is the text the operator sees: the result message of the standalone
// reboot, or the single warning event of a post-patch path.
func (r *gateRig) report(path rebootPath) string {
	r.t.Helper()
	if path == pathStandalone {
		return r.resultFor(idA).Message
	}
	events := r.agentEventMessages()
	require.Len(r.t, events, 1, "a skipped post-patch reboot emits exactly one warning event")
	return events[0]
}

func (r *gateRig) clearanceFor(guards rebootGuards) rebootClearance {
	return r.a.awaitRebootClearance(context.Background(), "test", idA, guards)
}

func noGuards(context.Context) string { return "" }

// maintenanceFlip returns a maintenance client that reports no work until running
// is set, then an active run of another command.
func maintenanceFlip(running *atomic.Bool) *maintenanceClient {
	m, rec := maintenanceFixture()
	rec.CommandID, rec.Outcome, rec.FinishedAt = idB, "success", &rec.UpdatedAt
	m.current = func() (maintenance.Record, error) { return rec, nil }
	m.active = func(context.Context) (bool, error) { return running.Load(), nil }
	return m
}

// linesWith counts the log lines that contain every one of substrs.
func linesWith(out string, substrs ...string) int {
	n := 0
lines:
	for _, line := range strings.Split(out, "\n") {
		for _, s := range substrs {
			if !strings.Contains(line, s) {
				continue lines
			}
		}
		n++
	}
	return n
}

// ---------------------------------------------------------------------------
// AC-001: a free lock never delays
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC001_FreeLockRebootsWithNoSleepOnAllThreePaths(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)

			r.drive(path)

			assert.EqualValues(t, 1, r.reboots.Load(), "[AC-001] the runner must be called exactly once")
			assert.Zero(t, r.gatePauses(), "[AC-001] a free lock must add no wait")
			assert.Equal(t, 2, r.calls(), "[AC-001] a free lock is checked twice: before the guards and as the final check")
			if path == pathStandalone {
				res := r.resultFor(idA)
				assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-001] status")
				assert.Equal(t, "reboot initiated", res.Message, "[AC-001] the unchanged success text")
			}
		})
	}
}

func TestRebootClearance_RD_AC001_FreeRunsGuardsThenFinalProbe(t *testing.T) {
	r := newGateRig(t)
	var seq []string
	r.script = func(time.Duration, int) dpkgLockStatus {
		seq = append(seq, "probe")
		return freeDpkgLockProbe()
	}

	c := r.clearanceFor(func(context.Context) string {
		seq = append(seq, "guards")
		return ""
	})
	require.True(t, c.Cleared, "[AC-001] a free lock with passing guards clears")
	seq = append(seq, "execReboot")
	require.NoError(t, r.a.execReboot(c.Permit))

	assert.Equal(t, []string{"probe", "guards", "probe", "execReboot"}, seq,
		"[AC-001] the last check before the reboot is a lock check, with nothing in between (FR-012)")
	assert.EqualValues(t, 1, r.reboots.Load(), "[AC-001] one reboot")
}

// ---------------------------------------------------------------------------
// AC-002 / AC-003: a held lock delays every path
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC002_HeldLockNeverRebootsWithinBoundOnAllThreePaths(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
				assert.Zero(t, r.reboots.Load(), "[AC-002] no reboot may start while the lock is held (elapsed %s)", elapsed)
				return unattendedUpgrHolder
			}

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[AC-002] a lock held for the whole bound must never be rebooted over")
			assert.Positive(t, r.gatePauses(), "[AC-002] the gate must wait while the lock is held")
			assert.Contains(t, r.report(path), "dpkg lock held past the timeout", "[AC-002] the skip is reported")
		})
	}
}

func TestRebootClearance_RD_AC003_LegacyHeldWaitsLikeFrontend(t *testing.T) {
	// End to end through the real probe: only /var/lib/dpkg/lock is held, for 3m.
	host := newFakeDpkgHost()
	r := newGateRig(t)
	var first time.Time
	r.a.dpkgLockProbe = func() dpkgLockStatus {
		r.mu.Lock()
		r.probeCalls++
		if first.IsZero() {
			first, r.firstProbeAt, r.pausesAtFirstProbe = r.clk.Now(), r.clk.Now(), len(r.pauses)
		}
		r.mu.Unlock()
		if r.clk.Now().Sub(first) < 3*time.Minute {
			host.setLocks("2: POSIX  ADVISORY  WRITE 55 103:02:262339 0 EOF")
			host.setHolder(55, "dpkg", 0)
		} else {
			host.setLocks()
		}
		return probeDpkgLock(host.env())
	}

	r.drive(pathStandalone)

	assert.EqualValues(t, 1, r.reboots.Load(), "[AC-003] the reboot follows the release")
	assert.Positive(t, r.gatePauses(), "[AC-003] a lock on /var/lib/dpkg/lock alone must make the gate wait")
	assert.Equal(t, "reboot initiated after waiting 3m for the dpkg lock", r.resultFor(idA).Message, "[AC-003] waited exactly as for lock-frontend")
}

// ---------------------------------------------------------------------------
// AC-004: release during the wait
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC004_ReleaseAfter3mRebootsWithin10s(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			release := 3*time.Minute + time.Second
			r.script = heldUntil(release, unattendedUpgrHolder)

			r.drive(path)

			require.EqualValues(t, 1, r.reboots.Load(), "[AC-004] the reboot must start after the release")
			r.mu.Lock()
			releasedAt := r.firstProbeAt.Add(release)
			startedAt := r.rebootTimes[0]
			r.mu.Unlock()
			assert.LessOrEqual(t, startedAt.Sub(releasedAt), 10*time.Second,
				"[AC-004] the reboot must start within 10s of the release, started %s after it", startedAt.Sub(releasedAt))
		})
	}
}

func TestRebootClearance_RD_AC004_PollIntervalAndProbeTimeoutFitBound(t *testing.T) {
	const guardsCap = 5 * time.Second // maintenanceBlockerFor runs one systemctl show, capped at 5s
	worst := dpkgLockPoll + guardsCap + 2*dpkgProbeTimeout

	assert.LessOrEqual(t, worst, 10*time.Second,
		"[AC-004] poll %s + guards %s + two probes %s must fit FR-004's 10 seconds", dpkgLockPoll, guardsCap, 2*dpkgProbeTimeout)
}

// ---------------------------------------------------------------------------
// AC-005: timeout skips, never forces
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC005_TimeoutSkipsAndNeverForces(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = alwaysHeld(unattendedUpgrHolder)

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[AC-005] a lock held past the bound must skip the reboot, never force it")
			assert.Equal(t, models.DpkgLockWaitBound, r.sinceFirstProbe(), "[AC-005] the wait ends at exactly the 10 minute bound")
		})
	}
}

func TestRebootClearance_RD_AC005_NoSecondWaitAfterSkip(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	r.drive(pathUnauthorized)
	require.Zero(t, r.reboots.Load(), "[AC-005] precondition: the attempt was skipped")
	callsAtSkip := r.calls()

	r.script = func(time.Duration, int) dpkgLockStatus { return freeDpkgLockProbe() }
	r.clk.Advance(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.a.notify.Flush(ctx)

	assert.Equal(t, callsAtSkip, r.calls(), "[AC-005] a skipped attempt must not check the lock again on its own")
	assert.Zero(t, r.reboots.Load(), "[AC-005] a skipped reboot is never retried (BR-002)")
}

func TestRebootClearance_RD_AC005_ProbeAtExactBoundHeldSkips(t *testing.T) {
	r := newGateRig(t)
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed <= models.DpkgLockWaitBound {
			return unattendedUpgrHolder
		}
		return freeDpkgLockProbe()
	}

	c := r.clearanceFor(noGuards)

	assert.False(t, c.Cleared, "[AC-005] a lock still held at exactly the bound is a timeout")
	assert.Equal(t, skipLockTimeout, c.Skip, "[AC-005] skip kind")
	assert.Equal(t, models.DpkgLockWaitBound, c.Waited, "[AC-005] waited exactly the bound")
}

func TestRebootClearance_RD_AC005_ProbeAtExactBoundFreeProceeds(t *testing.T) {
	r := newGateRig(t)
	r.script = heldUntil(models.DpkgLockWaitBound, unattendedUpgrHolder)

	c := r.clearanceFor(noGuards)

	assert.True(t, c.Cleared, "[AC-005] a lock that is free at exactly the bound has been released in time")
	assert.Equal(t, models.DpkgLockWaitBound, c.Waited, "[AC-005] waited exactly the bound")
}

// ---------------------------------------------------------------------------
// AC-006 / AC-007: the standalone result
// ---------------------------------------------------------------------------

func TestHandleReboot_RD_AC006_TimeoutResultFailedAndNamesHolder(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)

	r.drive(pathStandalone)

	res := r.resultFor(idA)
	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-006] a skipped reboot is a failed result")
	assert.Equal(t, "reboot skipped: dpkg lock held past the timeout (10m), held by unattended-upgr (pid 1234, uid 0)", res.Message,
		"[AC-006] the result names the timeout, the holder, its pid and its uid")
	assert.NotContains(t, res.Message, "reboot initiated", "[AC-006] the result must not say a reboot started")
	assert.NotEqual(t, models.ReasonTimeout, res.Reason, "[AC-006] FR-016: a dpkg skip is not a budget timeout")
	assert.Zero(t, r.reboots.Load(), "[AC-006] no reboot")
}

func TestHandleReboot_RD_AC007_TimeoutReportSaysHolderNotVisible(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(dpkgLockStatus{State: dpkgLockHeld, PID: -1, UID: -1})

	r.drive(pathStandalone)

	assert.Equal(t, "reboot skipped: dpkg lock held past the timeout (10m), holder not visible", r.resultFor(idA).Message,
		"[AC-007] a holder that cannot be identified is reported as such")
}

// ---------------------------------------------------------------------------
// AC-008 / AC-009: post-patch skips
// ---------------------------------------------------------------------------

func TestPostPatchReboot_RD_AC008_AuthorizedTimeoutOneEventOneTelemetry(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)

	r.drive(pathAuthorized)

	events := r.agentEventMessages()
	require.Len(t, events, 1, "[AC-008] exactly one warning event")
	assert.Equal(t, "authorized reboot for command "+idA+" was not started: dpkg lock held past the timeout (10m), "+unattendedUpgrText, events[0],
		"[AC-008] the event names the command, the timeout reason and the holder")
	assert.Equal(t, 1, r.telemetryPending(), "[AC-008] exactly one updates telemetry refresh")
	assert.Zero(t, r.reboots.Load(), "[AC-008] no reboot")
}

func TestPostPatchReboot_RD_AC008_UnauthorizedTimeoutOneEventOneTelemetry(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)

	r.drive(pathUnauthorized)

	events := r.agentEventMessages()
	require.Len(t, events, 1, "[AC-008] exactly one warning event")
	assert.Equal(t, "post-patch reboot for command "+idA+" was not started: dpkg lock held past the timeout (10m), "+unattendedUpgrText, events[0],
		"[AC-008] the event names the command, the timeout reason and the holder")
	assert.Equal(t, 1, r.telemetryPending(), "[AC-008] exactly one updates telemetry refresh")
	assert.Zero(t, r.reboots.Load(), "[AC-008] no reboot")
	r.tr.mu.Lock()
	defer r.tr.mu.Unlock()
	assert.Equal(t, models.SeverityWarning, r.tr.agentEvents[0].Severity, "[AC-008] the event is a warning")
	assert.Equal(t, models.EventCommandCompleted, r.tr.agentEvents[0].Type, "[AC-008] the event type is unchanged")
}

func TestPostPatchReboot_RD_AC009_SkipReleasesHostReservation(t *testing.T) {
	for _, path := range []rebootPath{pathUnauthorized, pathAuthorized} {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = alwaysHeld(unattendedUpgrHolder)

			r.drive(path)

			_, held := r.a.locks.Holder(models.TargetKeyHost)
			assert.False(t, held, "[AC-009] a skipped post-patch reboot must release its host reservation")
			_, _, ok := r.a.locks.TryLockAll([]string{models.TargetKeyHost}, idB, models.ActionOSUpdate)
			assert.True(t, ok, "[AC-009] another command for the host must be accepted afterwards")
		})
	}
}

// ---------------------------------------------------------------------------
// AC-010 / AC-011 / AC-012: unknown, oversized and absent
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC010_UnknownSkipsImmediatelyWithoutSleepOnAllThreePaths(t *testing.T) {
	unknown := dpkgLockStatus{State: dpkgLockUnknown, UID: -1, Cause: "cannot read /proc/locks: boom"}
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = alwaysHeld(unknown)

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[AC-010] an unknown lock state must not reboot")
			assert.Zero(t, r.gatePauses(), "[AC-010] an unknown state must not wait")
			assert.Equal(t, 1, r.calls(), "[AC-010] one check, then the skip")
			report := r.report(path)
			assert.Contains(t, report, "dpkg lock state unknown: cannot read /proc/locks: boom", "[AC-010] the report states the state and the cause")
			if path == pathStandalone {
				assert.Equal(t, "reboot skipped: dpkg lock state unknown: cannot read /proc/locks: boom", report, "[AC-010] exact standalone text")
				assert.Equal(t, models.ResultStatusError, r.resultFor(idA).Status, "[AC-010] a failed result")
			}
		})
	}
}

func TestRebootClearance_RD_AC010_UnknownMidWaitSkipsAtOnce(t *testing.T) {
	r := newGateRig(t)
	r.script = func(_ time.Duration, call int) dpkgLockStatus {
		if call <= 2 {
			return unattendedUpgrHolder
		}
		return dpkgLockStatus{State: dpkgLockUnknown, UID: -1, Cause: "lock information exceeds 1 MiB"}
	}

	c := r.clearanceFor(noGuards)

	assert.False(t, c.Cleared, "[AC-010] an unknown state during the wait must not clear")
	assert.Equal(t, skipLockUnknown, c.Skip, "[AC-010] skip kind")
	assert.Equal(t, 3, r.calls(), "[AC-010] the wait ends at the first unknown check")
	assert.Equal(t, 2*dpkgLockPoll, c.Waited, "[AC-010] it did not wait out the bound")
}

func TestRebootClearance_RD_AC010_HeldThenAbsentNeverRebootsOnAllThreePaths(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = func(_ time.Duration, call int) dpkgLockStatus {
				if call <= 2 {
					return unattendedUpgrHolder
				}
				return dpkgLockStatus{State: dpkgLockAbsent, UID: -1}
			}

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[C-1] a lock that was held and then vanished must never clear the reboot")
			assert.Contains(t, r.report(path), "dpkg lock files disappeared during the wait", "[C-1] the skip says why")
		})
	}
}

func TestHandleReboot_RD_AC011_OversizedLockInfoSkipsReboot(t *testing.T) {
	host := newFakeDpkgHost()
	host.files["/proc/locks"] = locksOfSize(t, dpkgProbeMaxBytes+1)
	r := newGateRig(t)
	r.a.dpkgLockProbe = func() dpkgLockStatus {
		r.mu.Lock()
		r.probeCalls++
		r.mu.Unlock()
		return probeDpkgLock(host.env())
	}

	r.drive(pathStandalone)

	assert.Zero(t, r.reboots.Load(), "[AC-011] oversized lock information must skip the reboot")
	assert.Equal(t, "reboot skipped: dpkg lock state unknown: lock information exceeds 1 MiB", r.resultFor(idA).Message,
		"[AC-011] the lock state is unknown")
	assert.Equal(t, 1, r.calls(), "[AC-011] no wait")
}

func TestRebootClearance_RD_AC012_AbsentRebootsAsToday(t *testing.T) {
	logs := captureLogs(t)
	r := newGateRig(t)
	r.script = alwaysHeld(dpkgLockStatus{State: dpkgLockAbsent, UID: -1})

	r.drive(pathStandalone)

	assert.EqualValues(t, 1, r.reboots.Load(), "[AC-012] a host without dpkg reboots as today")
	assert.Equal(t, "reboot initiated", r.resultFor(idA).Message, "[AC-012] the unchanged success text")
	assert.Zero(t, r.gatePauses(), "[AC-012] no wait")
	assert.NotContains(t, logs.String(), "dpkg lock", "[AC-012] no lock-related log line")
}

// ---------------------------------------------------------------------------
// AC-013: guards are re-checked after the wait
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC013_AllowRebootOffDuringWaitSkipsNamingGuard(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = func(elapsed time.Duration, call int) dpkgLockStatus {
				if call == 3 {
					r.a.config.Commands.AllowReboot = false // the operator turns reboots off mid-wait
				}
				if elapsed < 10*time.Second {
					return unattendedUpgrHolder
				}
				return freeDpkgLockProbe()
			}

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[AC-013] allow_reboot turned off during the wait must stop the reboot")
			report := r.report(path)
			assert.Contains(t, report, "commands.allow_reboot is off", "[AC-013] the report names the failed guard")
			if path == pathStandalone {
				assert.Equal(t, "reboot skipped: guard failed: commands.allow_reboot is off", report, "[AC-013] exact standalone text")
			}
		})
	}
}

func TestRebootClearance_RD_AC013_MaintenanceStartsDuringWaitSkipsNamingGuard(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
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

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[AC-013] a maintenance run that starts during the wait must stop the reboot")
			assert.Contains(t, r.report(path), "host busy: "+maintenanceRunning, "[AC-013] the report names the failed guard")
			if path == pathStandalone {
				assert.Equal(t, models.ReasonConflict, r.resultFor(idA).Reason, "[AC-013] a busy host is a conflict")
			}
		})
	}
}

func TestRebootClearance_RD_AC013_GuardOrderIsShutdownThenAllowRebootThenMaintenance(t *testing.T) {
	r := newGateRig(t)
	var running atomic.Bool
	r.a.maintenanceClient = maintenanceFlip(&running)
	guards := r.a.commonRebootGuards("")

	assert.Equal(t, "", guards(context.Background()), "[AC-013] all guards pass")
	running.Store(true)
	assert.Equal(t, "host busy: "+maintenanceRunning, guards(context.Background()), "[AC-013] maintenance guard")
	r.a.config.Commands.AllowReboot = false
	assert.Equal(t, "commands.allow_reboot is off", guards(context.Background()), "[AC-013] allow_reboot is checked before maintenance")
	r.a.shuttingDown.Store(true)
	assert.Equal(t, "agent shutting down", guards(context.Background()), "[AC-013] shutdown is checked first")
}

// ---------------------------------------------------------------------------
// AC-014: a re-taken lock keeps waiting within the same bound
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC014_FinalCheckRetakenKeepsWaitingWithinBound(t *testing.T) {
	t.Run("lock re-taken and held to the bound skips at exactly 10m", func(t *testing.T) {
		logs := captureLogs(t)
		r := newGateRig(t)
		guardRuns := 0
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

		c := r.clearanceFor(func(context.Context) string { guardRuns++; return "" })

		assert.False(t, c.Cleared, "[AC-014] a lock re-taken before the final check must not clear")
		assert.Equal(t, skipLockTimeout, c.Skip, "[AC-014] the wait ends in the timeout")
		assert.Equal(t, models.DpkgLockWaitBound, c.Waited, "[AC-014] the total wait never exceeds 10 minutes")
		assert.Equal(t, models.DpkgLockWaitBound, r.sinceFirstProbe(), "[AC-014] one deadline, firstHeld + 10m")
		assert.Equal(t, 1, guardRuns, "[AC-014] the guards ran once, when the lock first looked free")
		assert.Equal(t, 1, strings.Count(logs.String(), `msg="dpkg lock held, waiting before reboot"`), "[AC-014] one wait-started line")
	})
	t.Run("lock re-taken then released re-runs the guards before clearing", func(t *testing.T) {
		r := newGateRig(t)
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

		c := r.clearanceFor(func(context.Context) string { guardRuns++; return "" })

		assert.True(t, c.Cleared, "[AC-014] a lock that is finally free clears")
		assert.Equal(t, 2, guardRuns, "[AC-014] FR-011 applies again after any further wait")
		assert.Equal(t, 6*time.Minute, c.Waited, "[AC-014] waited from the first held check to the clearance")
	})
}

// ---------------------------------------------------------------------------
// AC-015: shutdown ends the wait
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC015_ShutdownEndsWaitWithin5s(t *testing.T) {
	r := newGateRig(t)
	var stoppedAt time.Time
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed >= time.Minute && stoppedAt.IsZero() {
			stoppedAt = r.clk.Now()
			r.a.shuttingDown.Store(true) // SIGTERM
		}
		return unattendedUpgrHolder
	}

	c := r.clearanceFor(noGuards)

	assert.False(t, c.Cleared, "[AC-015] no reboot after the agent was told to stop")
	assert.Equal(t, skipShutdown, c.Skip, "[AC-015] skip kind")
	assert.LessOrEqual(t, r.clk.Now().Sub(stoppedAt), 5*time.Second, "[AC-015] the wait must end within 5 seconds of the stop")
}

func TestRebootClearance_RD_AC015_CancelledContextWithShutdownIsAShutdownSkip(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	r.a.pause = func(context.Context, time.Duration) bool {
		r.a.shuttingDown.Store(true)
		return false // the command context ended
	}

	c := r.clearanceFor(noGuards)

	assert.Equal(t, skipShutdown, c.Skip, "[AC-015] a cancelled wait during shutdown is a shutdown skip, not a budget failure")
}

func TestHandleReboot_RD_AC015_ShutdownResultReasonShutdown(t *testing.T) {
	r := newGateRig(t)
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed >= 10*time.Second {
			r.a.shuttingDown.Store(true)
		}
		return unattendedUpgrHolder
	}

	r.drive(pathStandalone)

	res := r.resultFor(idA)
	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-015] failed result")
	assert.Equal(t, models.ReasonShutdown, res.Reason, "[AC-015] reason shutdown")
	assert.Equal(t, "reboot skipped: agent shutting down", res.Message, "[AC-015] exact text")
	assert.Zero(t, r.reboots.Load(), "[AC-015] no reboot")
}

func TestPostPatchReboot_RD_AC015_ShutdownSkipLogsWarn(t *testing.T) {
	cases := map[rebootPath]string{
		pathUnauthorized: `level=WARN msg="os_update: post-patch reboot not started"`,
		pathAuthorized:   `level=WARN msg="os_update: authorized reboot not started"`,
	}
	for path, want := range cases {
		t.Run(string(path), func(t *testing.T) {
			logs := captureLogs(t)
			r := newGateRig(t)
			r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
				if elapsed >= 10*time.Second {
					r.a.shuttingDown.Store(true)
				}
				return unattendedUpgrHolder
			}

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[AC-015] no reboot")
			assert.Contains(t, logs.String(), want, "[AC-015] the skip is always logged at Warn, even when the event cannot be sent")
			assert.Contains(t, logs.String(), `reason="agent shutting down"`, "[AC-015] the log names the reason")
		})
	}
}

// ---------------------------------------------------------------------------
// AC-016: no reboot after a not-started result
// ---------------------------------------------------------------------------

func TestHandleReboot_RD_AC016_NoRebootAfterSkippedResult(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	r.drive(pathStandalone)
	require.Equal(t, models.ResultStatusError, r.resultFor(idA).Status, "[AC-016] precondition: the reboot was reported as skipped")
	callsAtSkip := r.calls()

	r.script = func(time.Duration, int) dpkgLockStatus { return freeDpkgLockProbe() } // the lock is released afterwards
	r.clk.Advance(time.Hour)
	r.submit(idA, models.ActionReboot, nil) // the server redelivers the same command
	r.wait()

	assert.Zero(t, r.reboots.Load(), "[AC-016] the agent must not start a reboot for a command it reported as not started")
	assert.Equal(t, callsAtSkip, r.calls(), "[AC-016] the redelivery is deduplicated and checks nothing")
}

func TestRebootClearance_RD_AC016_CommandBudgetExhaustedIsATruthfulSkip(t *testing.T) {
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	r.a.pause = func(context.Context, time.Duration) bool { return false } // the command context deadline fired

	c := r.clearanceFor(noGuards)
	res := rebootSkippedResult(c)

	assert.False(t, c.Cleared, "[AC-016] an exhausted command budget never reboots")
	assert.Equal(t, "reboot skipped: guard failed: agent command time budget exhausted", res.Message, "[AC-016] the outcome is the truthful skip")
	assert.NotEqual(t, models.ReasonTimeout, res.Reason, "[AC-016] FR-016: not recorded as a budget timeout")
}

// ---------------------------------------------------------------------------
// AC-017: a 9 minute wait is a success
// ---------------------------------------------------------------------------

func TestHandleReboot_RD_AC017_NineMinuteWaitReportsStartedAndWaited(t *testing.T) {
	r := newGateRig(t)
	r.script = heldUntil(9*time.Minute, unattendedUpgrHolder)

	r.drive(pathStandalone)

	res := r.resultFor(idA)
	assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-017] the outcome is a started reboot")
	assert.Equal(t, "reboot initiated after waiting 9m for the dpkg lock", res.Message, "[AC-017] the result states the wait")
	assert.NotEqual(t, models.ReasonTimeout, res.Reason, "[AC-017] not recorded as timed out by the agent")
	assert.EqualValues(t, 1, r.reboots.Load(), "[AC-017] the reboot started once")
}

// ---------------------------------------------------------------------------
// AC-019: bounded growth during a full wait
// ---------------------------------------------------------------------------

func listStateDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestRebootClearance_RD_AC019_FullWaitBoundedOutput(t *testing.T) {
	logs := captureLogs(t)
	r := newGateRig(t)
	r.script = alwaysHeld(unattendedUpgrHolder)
	dir := filepath.Dir(r.seenPath)
	before := listStateDir(t, dir)

	c := r.clearanceFor(noGuards)

	require.False(t, c.Cleared, "[AC-019] precondition: the full wait ended in a skip")
	assert.LessOrEqual(t, r.calls(), 303, "[AC-019] at most 10m/2s + 3 lock checks per attempt")
	assert.EqualValues(t, 0, r.a.rebootPermitSeq.Load(), "[AC-019] a skipped attempt mints no permit")
	assert.Equal(t, before, listStateDir(t, dir), "[AC-019] the gate must not create any file")
	out := logs.String()
	assert.Equal(t, 2, linesWith(out, `msg="dpkg lock `), "[AC-019] exactly the wait-started and wait-ended lines, none per check")
	assert.Equal(t, 2, linesWith(out, "level=INFO", `msg="dpkg lock `), "[AC-019] both gate lines are Info")
	assert.Zero(t, linesWith(out, "level=DEBUG", "dpkg"), "[AC-019] the gate logs nothing at debug level, so nothing grows per check")
}

func TestRebootClearance_RD_AC019_PostPatchSkipProducesOneEventOneTelemetry(t *testing.T) {
	for _, path := range []rebootPath{pathUnauthorized, pathAuthorized} {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = alwaysHeld(unattendedUpgrHolder)

			r.drive(path)

			assert.Len(t, r.agentEventMessages(), 1, "[AC-019] exactly one warning event")
			assert.Equal(t, 1, r.telemetryPending(), "[AC-019] exactly one telemetry refresh")
			assert.LessOrEqual(t, r.calls(), 303, "[AC-019] bounded lock checks")
		})
	}
}

func TestRebootClearance_RD_AC019_WaitLogLinesCarryContext(t *testing.T) {
	logs := captureLogs(t)
	r := newGateRig(t)
	r.script = heldUntil(5*time.Second, unattendedUpgrHolder)

	c := r.clearanceFor(noGuards)

	require.True(t, c.Cleared, "[AC-019] precondition: cleared after the release")
	out := logs.String()
	assert.Contains(t, out, `msg="dpkg lock held, waiting before reboot" path=test command_id=`+idA, "[AC-019] wait started")
	assert.Contains(t, out, `msg="dpkg lock wait ended" path=test command_id=`+idA+` outcome=cleared waited=6s`, "[AC-019] wait ended: outcome and waited")
	assert.Contains(t, out, `holder="held by unattended-upgr (pid 1234, uid 0)"`, "[AC-019] the holder is logged")
}

// ---------------------------------------------------------------------------
// AC-021: no process is run
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC021_GateRunsNoProcessExceptTheReboot(t *testing.T) {
	r := newGateRig(t)
	var ran []string
	r.a.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	r.script = heldUntil(10*time.Second, unattendedUpgrHolder)

	c := r.clearanceFor(r.a.commonRebootGuards(""))

	require.True(t, c.Cleared, "[AC-021] precondition: cleared")
	assert.Empty(t, ran, "[AC-021] the lock probe and the gate run no exec and no sudo (the guard's systemctl show runs only on a host with the maintenance helper)")
	require.NoError(t, r.a.execReboot(c.Permit))
	assert.Equal(t, []string{"sudo systemctl reboot"}, ran, "[AC-021] the reboot is the only process")
}

// ---------------------------------------------------------------------------
// C-3: execReboot accepts only the gate's single-use permit
// ---------------------------------------------------------------------------

func TestExecReboot_RD_C3_NonClearedPermitStartsNoReboot(t *testing.T) {
	t.Run("zero permit", func(t *testing.T) {
		r := newGateRig(t)

		err := r.a.execReboot(rebootPermit{})

		assert.ErrorIs(t, err, errRebootNotCleared, "[C-3] a zero permit must be refused")
		assert.Zero(t, r.reboots.Load(), "[C-3] a refused permit starts no reboot")
	})
	t.Run("the permit of a skipped clearance", func(t *testing.T) {
		r := newGateRig(t)
		skipped := rebootClearance{Skip: skipLockTimeout}

		err := r.a.execReboot(skipped.Permit)

		assert.ErrorIs(t, err, errRebootNotCleared, "[C-3] a skipped clearance carries no valid permit")
		assert.Zero(t, r.reboots.Load(), "[C-3] a refused permit starts no reboot")
	})
	t.Run("a permit used twice", func(t *testing.T) {
		r := newGateRig(t)
		p := r.a.mintRebootPermit()
		require.NoError(t, r.a.execReboot(p), "[C-3] first use")

		err := r.a.execReboot(p)

		assert.ErrorIs(t, err, errRebootNotCleared, "[C-3] a permit is single-use")
		assert.EqualValues(t, 1, r.reboots.Load(), "[C-3] only the first use started a reboot")
	})
	t.Run("an older permit after a newer one was minted", func(t *testing.T) {
		r := newGateRig(t)
		older := r.a.mintRebootPermit()
		newer := r.a.mintRebootPermit()

		assert.ErrorIs(t, r.a.execReboot(older), errRebootNotCleared, "[C-3] a stale permit must be refused")
		assert.Zero(t, r.reboots.Load(), "[C-3] a stale permit starts no reboot")
		assert.NoError(t, r.a.execReboot(newer), "[C-3] the newest permit still works")
		assert.EqualValues(t, 1, r.reboots.Load(), "[C-3] only the newest permit started a reboot")
	})
}

func TestExecReboot_RD_C3_ClearedPermitStartsExactlyOnce(t *testing.T) {
	r := newGateRig(t)

	c := r.clearanceFor(noGuards)
	require.True(t, c.Cleared, "[C-3] precondition: the gate cleared")

	require.NoError(t, r.a.execReboot(c.Permit), "[C-3] the cleared permit starts the reboot")
	assert.EqualValues(t, 1, r.reboots.Load(), "[C-3] exactly one reboot")
	assert.ErrorIs(t, r.a.execReboot(c.Permit), errRebootNotCleared, "[C-3] the same permit cannot start a second reboot")
	assert.EqualValues(t, 1, r.reboots.Load(), "[C-3] still exactly one reboot")
	assert.EqualValues(t, 1, r.a.rebootPermitSeq.Load(), "[C-3] one permit minted for one attempt")
}

func TestExecReboot_RD_C3_RefusalRunsNothing(t *testing.T) {
	a := &Agent{}
	called := false
	a.runner = func(context.Context, string, ...string) ([]byte, error) { called = true; return nil, nil }

	err := a.execReboot(rebootPermit{})

	assert.ErrorIs(t, err, errRebootNotCleared, "[C-3] refused")
	assert.False(t, called, "[C-3] a refused permit must not reach the runner or exec")
}

// ---------------------------------------------------------------------------
// Result and reason texts
// ---------------------------------------------------------------------------

func TestRebootClearance_RD_AC006_ReasonTexts(t *testing.T) {
	cases := []struct {
		name string
		c    rebootClearance
		want string
	}{
		{"timeout", rebootClearance{Skip: skipLockTimeout, Detail: unattendedUpgrText}, "dpkg lock held past the timeout (10m), " + unattendedUpgrText},
		{"unknown", rebootClearance{Skip: skipLockUnknown, Detail: "cannot stat /x: boom"}, "dpkg lock state unknown: cannot stat /x: boom"},
		{"shutdown", rebootClearance{Skip: skipShutdown, Detail: "ignored"}, "agent shutting down"},
		{"guard", rebootClearance{Skip: skipGuard, Detail: "commands.allow_reboot is off"}, "commands.allow_reboot is off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.c.reason(), "[AC-006] reason text")
		})
	}
}

func TestHandleReboot_RD_AC020_PreGateAllowRebootOffIsUnchanged(t *testing.T) {
	r := newGateRig(t)
	r.a.config.Commands.AllowReboot = false

	r.drive(pathStandalone)

	res := r.resultFor(idA)
	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-020] failed result")
	assert.Equal(t, "reboot not allowed by agent config (set commands.allow_reboot: true)", res.Message, "[AC-020] the pre-gate text is unchanged")
	assert.Zero(t, r.calls(), "[AC-020] the fail-fast check runs before any lock check")
}
