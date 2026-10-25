package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer (REBOOT-DPKG-001). Each test closes a gap that a mutation
// of the gate survived, or covers an edge the ADD calls out that no test pinned.

// Covers AC-014 / FR-003 / FR-012 (mutant: the last sleep not capped to the time
// left in the bound). Every other test times the guards at zero cost, so the
// elapsed time is always a multiple of the 2s poll and the cap never matters. Here
// one guard evaluation takes 500ms, which puts the checks at x.5s: without the cap
// the last sleep overshoots the 10 minute bound by 500ms.
func TestRebootClearance_QA_AC014_TotalWaitNeverExceedsBoundWhenGuardsTakeOddTime(t *testing.T) {
	r := newGateRig(t)
	freedOnce := false
	r.script = func(elapsed time.Duration, _ int) dpkgLockStatus {
		if elapsed >= 2*time.Second && !freedOnce {
			freedOnce = true // free for one check, then the lock is taken again
			return freeDpkgLockProbe()
		}
		return unattendedUpgrHolder
	}

	c := r.clearanceFor(func(context.Context) string {
		r.clk.Advance(500 * time.Millisecond) // a slow guard (systemctl show)
		return ""
	})

	assert.False(t, c.Cleared, "[AC-014] a lock re-taken and held to the bound must not clear")
	assert.Equal(t, skipLockTimeout, c.Skip, "[AC-014] the wait ends in the timeout")
	assert.Equal(t, models.DpkgLockWaitBound, c.Waited, "[AC-014] the last sleep is capped, so the wait ends at exactly 10 minutes, not 10m0.5s")
	assert.Equal(t, models.DpkgLockWaitBound, r.sinceFirstProbe(), "[AC-014] one deadline, firstHeld + 10m")
}

// Covers AC-010 / C-1 (Held -> Free -> Absent within one attempt). The developer
// test goes Held -> Absent directly. The sawHeld memory must survive an
// intermediate Free observation (guards passed, final probe pending), because the
// final probe is exactly where vanished lock files would otherwise read as a clear.
func TestRebootClearance_QA_C1_HeldThenFreeThenAbsentNeverClears(t *testing.T) {
	r := newGateRig(t)
	r.script = func(_ time.Duration, call int) dpkgLockStatus {
		switch {
		case call <= 2:
			return unattendedUpgrHolder
		case call == 3:
			return freeDpkgLockProbe() // guards run
		}
		return dpkgLockStatus{State: dpkgLockAbsent, UID: -1} // the final probe: lock files gone
	}
	guardRuns := 0

	c := r.clearanceFor(func(context.Context) string { guardRuns++; return "" })

	assert.False(t, c.Cleared, "[C-1] lock files that vanish between the guards and the final probe must not clear the reboot")
	assert.Equal(t, skipLockUnknown, c.Skip, "[C-1] skip kind")
	assert.Equal(t, causeLockFilesVanished, c.Detail, "[C-1] the skip says the lock files disappeared")
	assert.Equal(t, 1, guardRuns, "[C-1] the guards ran once")
	assert.EqualValues(t, 0, r.a.powerPermitSeq.Load(), "[C-1] no permit was minted")
}

// Covers AC-015 / FR-013 with the REAL timer. All other AC-015 tests use the fake
// clock and a pause seam that advances it, so nothing proves that the production
// sleep path (a timer, with the command context cancelled only after the 20s
// shutdown grace) ends within the bound. The gate polls shuttingDown every 2s, so
// this takes about 2 real seconds. FR-013 allows 5s.
func TestRebootClearance_QA_AC015_RealTimerShutdownEndsWaitWithin5s(t *testing.T) {
	logs := captureLogs(t)
	a := &Agent{}
	var probes atomic.Int32
	a.dpkgLockProbe = func() dpkgLockStatus {
		probes.Add(1)
		return unattendedUpgrHolder
	}
	reboots := 0
	a.runner = func(context.Context, string, ...string) ([]byte, error) { reboots++; return nil, nil }

	go func() {
		time.Sleep(50 * time.Millisecond)
		a.shuttingDown.Store(true) // SIGTERM; the command context stays live during the grace period
	}()
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second) // a regression fails instead of hanging
	defer cancel()
	c := a.awaitRebootClearance(ctx, "test", "cmd-qa", nil)

	elapsed := time.Since(start)
	assert.False(t, c.Cleared, "[AC-015] no reboot after the agent was told to stop")
	assert.Equal(t, skipShutdown, c.Skip, "[AC-015] skip kind")
	assert.Less(t, elapsed, 5*time.Second, "[AC-015] the wait must end within 5 seconds of the stop, took %s", elapsed)
	assert.Zero(t, reboots, "[AC-015] no reboot was started")
	assert.GreaterOrEqual(t, int(probes.Load()), 1, "[AC-015] precondition: the lock was held")
	assert.Equal(t, 2, linesWith(logs.String(), `msg="dpkg lock `), "[AC-015] exactly the wait-started and wait-ended lines")
}

// Covers AC-017 / FR-021: the reboot budgets through the real command policy.
// (SHUTDOWN-DPKG-001 AD-005 gave shutdown its own budgets, pinned by
// TestCommandPolicy_SD_AC017_ShutdownBudgetCoversWait.)
func TestCommandPolicy_QA_AC020_RebootAndShutdownBudgetsAreIndependent(t *testing.T) {
	assert.Equal(t, 12*time.Minute, models.AgentHardMax(models.ActionReboot, 1), "[AC-017] reboot agent budget covers the 10m wait")
	assert.Equal(t, 20*time.Minute, models.ExecDeadline(models.ActionReboot, 1), "[AC-017] reboot server deadline")
	assert.EqualValues(t, 1200, models.ExecDeadlineSeconds(models.ActionReboot, 1), "[AC-017] reboot exec_deadline_s")
}

// Covers AC-010 (ADD section 8 lists this test, the implementation lacks it). A
// state value outside the four defined ones, such as a future constant added
// without a gate case, must fail closed on every path: no reboot, no wait, and
// not a silent re-probe loop. The zero value is already covered; this is a value
// that is neither zero nor defined.
func TestRebootClearance_RD_AC010_UnrecognizedStateFailsClosed(t *testing.T) {
	for _, path := range allRebootPaths {
		t.Run(string(path), func(t *testing.T) {
			r := newGateRig(t)
			r.script = func(time.Duration, int) dpkgLockStatus { return dpkgLockStatus{State: dpkgLockState(99), UID: -1} }

			r.drive(path)

			assert.Zero(t, r.reboots.Load(), "[AC-010] an unrecognized lock state must never reboot")
			assert.Zero(t, r.gatePauses(), "[AC-010] an unrecognized state does not wait")
			assert.Equal(t, 1, r.calls(), "[AC-010] one check, then the skip (no re-probe loop)")
			assert.Contains(t, r.report(path), "dpkg lock state unknown: lock state not recognized", "[AC-010] the skip says the state was not recognized")
		})
	}
}
