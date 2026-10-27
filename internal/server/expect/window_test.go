package expect

import (
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Window evaluation tests (ADD section 8, "W"): the pure function of AD-003.

var t0 = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

type winSpec struct {
	action   string
	status   models.CommandState
	dispatch *time.Time
	exec     time.Duration
	finished *time.Time
	keys     []string
}

func mkWindow(t *testing.T, s winSpec) *window {
	t.Helper()
	pol, ok := models.ExpectPolicy(s.action)
	require.True(t, ok, "%s must open a window", s.action)
	c := models.Command{ID: "cmd-1", DeviceID: "dev", Action: s.action, Status: string(s.status), Origin: models.OriginUser,
		CreatedAt: t0, UpdatedAt: t0, DispatchedAt: s.dispatch, FinishedAt: s.finished, TargetKeys: s.keys}
	if s.dispatch != nil {
		dl := s.dispatch.Add(s.exec)
		c.DeadlineAt = &dl
		c.ExecDeadlineS = int(s.exec / time.Second)
	}
	c.Normalize()
	return &window{cmd: c, policy: pol}
}

func at(d time.Duration) *time.Time { v := t0.Add(d); return &v }

var online = deviceView{online: true, onlineSince: t0.Add(-time.Hour)}

func TestEX_AC004_BulkWindowCoversThreeHours(t *testing.T) {
	exec := models.ExecDeadline(models.ActionDockerBulkUpdate, 8)
	require.Equal(t, 4*time.Hour+45*time.Minute, exec, "the 8-target bulk update has the 4h45m deadline")
	w := mkWindow(t, winSpec{action: models.ActionDockerBulkUpdate, status: models.CommandRunning, dispatch: at(0), exec: exec})

	tl := evaluate(w, online, t0.Add(3*time.Hour), time.Time{})

	assert.Equal(t, models.PhaseInProgress, tl.phase)
	assert.True(t, tl.coversAt(tl.containerEnd, t0.Add(3*time.Hour)), "[AC-004] a death 3 h after dispatch is covered")
	assert.True(t, tl.coversAt(tl.containerEnd, t0.Add(exec+time.Minute)), "covered past the execution deadline until the settle bound")
	assert.False(t, tl.coversAt(tl.containerEnd, t0.Add(exec+2*time.Minute)), "[FR-005] never at the hard bound: deadline + 2 min")
}

func TestEX_AC007_ContainerSettleBoundary(t *testing.T) {
	w := mkWindow(t, winSpec{action: models.ActionDockerUpdate, status: models.CommandSucceeded, dispatch: at(-10 * time.Minute),
		exec: 35 * time.Minute, finished: at(0)})
	tl := evaluate(w, online, t0.Add(time.Minute), time.Time{})

	assert.Equal(t, models.PhaseSettling, tl.phase)
	assert.True(t, tl.coversAt(tl.containerEnd, t0.Add(110*time.Second)), "[AC-007] 10:01:50 is inside the settle period")
	assert.True(t, tl.coversAt(tl.containerEnd, t0.Add(2*time.Minute-time.Nanosecond)), "[AC-007] one instant before the boundary is covered")
	assert.False(t, tl.coversAt(tl.containerEnd, t0.Add(2*time.Minute)), "[AC-007] the boundary itself is not covered")
	assert.False(t, tl.coversAt(tl.containerEnd, t0.Add(130*time.Second)), "[AC-007] 10:02:10 is after the settle period")
	assert.True(t, evaluate(w, online, t0.Add(2*time.Minute), time.Time{}).due, "the window is due at the boundary")
	assert.False(t, evaluate(w, online, t0.Add(2*time.Minute-time.Second), time.Time{}).due)
	assert.Equal(t, models.EndSettled, tl.reason)
}

func TestEX_AC033_EveryWindowHasHardBound(t *testing.T) {
	for _, action := range models.ExpectActions() {
		pol, _ := models.ExpectPolicy(action)
		exec := models.ExecDeadline(action, 50)
		hard := t0.Add(exec + pol.PostTerminal)

		active := mkWindow(t, winSpec{action: action, status: models.CommandRunning, dispatch: at(0), exec: exec})
		tl := evaluate(active, online, t0.Add(time.Second), time.Time{})
		assert.Equal(t, hard, tl.hard, "[AC-033] %s: hard bound is dispatch + deadline + post-terminal period", action)
		assert.Equal(t, hard, tl.closeAt, "[AC-033] %s: an active window ends at its hard bound", action)
		assert.True(t, evaluate(active, online, hard, time.Time{}).due, "[AC-033] %s is due at the bound", action)
		assert.Equal(t, models.EndHardBound, tl.reason, "[AC-033] %s", action)

		// A result that arrives late (after the deadline) must not stretch the window.
		late := mkWindow(t, winSpec{action: action, status: models.CommandSucceeded, dispatch: at(0), exec: exec, finished: at(exec + 10*time.Minute)})
		for _, dv := range []deviceView{online, {online: false}} {
			tl := evaluate(late, dv, t0.Add(exec+11*time.Minute), time.Time{})
			assert.False(t, tl.closeAt.After(hard), "[AC-033] %s: closeAt %v must not pass the hard bound %v", action, tl.closeAt, hard)
			assert.False(t, tl.coversAt(tl.offlineEnd, hard) || tl.coversAt(tl.containerEnd, hard), "[AC-033] %s covers nothing at the bound", action)
		}
	}
}

func TestEX_Window_QueuedAndNeverOpened(t *testing.T) {
	queued := mkWindow(t, winSpec{action: models.ActionReboot, status: models.CommandQueued})
	tl := evaluate(queued, online, t0, time.Time{})
	assert.Equal(t, models.PhaseQueued, tl.phase, "[FR-002] a queued command opens no window")
	assert.False(t, tl.opened)
	assert.False(t, tl.coversAt(tl.offlineEnd, t0))
	assert.False(t, tl.due)

	expired := mkWindow(t, winSpec{action: models.ActionReboot, status: models.CommandExpired, finished: at(time.Minute)})
	tl = evaluate(expired, online, t0.Add(time.Hour), time.Time{})
	assert.True(t, tl.due, "an expired queued command is finished at once")
	assert.Equal(t, models.EndNotOpened, tl.reason)
}

func TestEX_Window_HostReturnEarlyClose(t *testing.T) {
	f := at(0)
	reboot := mkWindow(t, winSpec{action: models.ActionReboot, status: models.CommandSucceeded, dispatch: at(-time.Minute), exec: 20 * time.Minute, finished: f})

	notBack := evaluate(reboot, deviceView{online: false}, t0.Add(time.Minute), time.Time{})
	assert.Equal(t, t0.Add(15*time.Minute), notBack.closeAt, "[FR-003] offline: the 15 min return bound")
	assert.Equal(t, models.EndReturnBound, notBack.reason)
	require.NotNil(t, notBack.backBy)
	assert.Equal(t, t0.Add(15*time.Minute), *notBack.backBy)

	noObserved := evaluate(reboot, deviceView{online: true, onlineSince: t0.Add(-time.Hour)}, t0.Add(time.Minute), time.Time{})
	assert.Equal(t, t0.Add(15*time.Minute), noObserved.closeAt, "[AD-003] a reboot needs an observed return after F before it may close early")

	back := evaluate(reboot, deviceView{online: true, onlineSince: t0.Add(2 * time.Minute), returnedAt: t0.Add(2 * time.Minute)}, t0.Add(3*time.Minute), time.Time{})
	assert.Equal(t, t0.Add(5*time.Minute), back.closeAt, "[FR-003] closes 3 min after the device was back online")
	assert.Equal(t, models.EndSettled, back.reason)

	graced := evaluate(reboot, deviceView{online: true, onlineSince: t0.Add(2 * time.Minute), returnedAt: t0.Add(2 * time.Minute)}, t0.Add(3*time.Minute), t0.Add(10*time.Minute))
	assert.Equal(t, t0.Add(13*time.Minute), graced.closeAt, "[AD-008] never earlier than the restart grace plus the settle period")

	patch := mkWindow(t, winSpec{action: models.ActionOSUpdate, status: models.CommandSucceeded, dispatch: at(-time.Minute), exec: 40 * time.Minute, finished: f})
	noReboot := evaluate(patch, deviceView{online: true, onlineSince: t0.Add(-time.Hour)}, t0.Add(time.Minute), time.Time{})
	assert.Equal(t, t0.Add(3*time.Minute), noReboot.closeAt, "[A-005] an os_update on a device that stays online closes 3 min after success")
}

func TestEX_Window_AgentReturn(t *testing.T) {
	w := mkWindow(t, winSpec{action: models.ActionAgentUpdate, status: models.CommandSucceeded, dispatch: at(-time.Minute), exec: 15 * time.Minute, finished: at(0)})
	off := evaluate(w, deviceView{online: false}, t0.Add(time.Minute), time.Time{})
	assert.Equal(t, t0.Add(10*time.Minute), off.closeAt, "the 10 min agent return bound")
	assert.True(t, off.coversAt(off.offlineEnd, t0.Add(time.Minute)), "covers offline")
	assert.True(t, off.containerEnd.IsZero(), "[FR-010] covers no container")
	back := evaluate(w, deviceView{online: true, onlineSince: t0.Add(time.Minute), returnedAt: t0.Add(time.Minute)}, t0.Add(2*time.Minute), time.Time{})
	assert.Equal(t, t0.Add(2*time.Minute), back.closeAt, "closes 1 min after the agent is back")
}

// A signed agent update is completed by the restarted agent: it re-registers a
// moment before F. That return must count, or every agent update sits in
// "Agent restarted, settling" until the 10 min bound (seen on hawaii 2026-10-27).
func TestEX_Window_AgentReturnBeforeCompletion(t *testing.T) {
	w := mkWindow(t, winSpec{action: models.ActionAgentUpdate, status: models.CommandSucceeded, dispatch: at(-time.Minute), exec: 15 * time.Minute, finished: at(0)})
	back := evaluate(w, deviceView{online: true, onlineSince: t0.Add(-time.Second), returnedAt: t0.Add(-time.Second)}, t0.Add(30*time.Second), time.Time{})
	assert.Equal(t, t0.Add(time.Minute), back.closeAt, "closes 1 min after F when the agent re-registered between dispatch and F")
	assert.Equal(t, models.EndSettled, back.reason)

	stale := evaluate(w, deviceView{online: true, onlineSince: t0.Add(-time.Hour), returnedAt: t0.Add(-time.Hour)}, t0.Add(30*time.Second), time.Time{})
	assert.Equal(t, t0.Add(10*time.Minute), stale.closeAt, "a return from before the dispatch is not a restart: wait for the bound")

	reboot := mkWindow(t, winSpec{action: models.ActionReboot, status: models.CommandSucceeded, dispatch: at(-time.Minute), exec: 20 * time.Minute, finished: at(0)})
	rb := evaluate(reboot, deviceView{online: true, onlineSince: t0.Add(-time.Second), returnedAt: t0.Add(-time.Second)}, t0.Add(30*time.Second), time.Time{})
	assert.Equal(t, t0.Add(15*time.Minute), rb.closeAt, "[AD-003] a reboot still needs a return after F")
}

func TestEX_Window_NonSuccessEndsOfflineCoverageAtOnce(t *testing.T) {
	for _, st := range []models.CommandState{models.CommandFailed, models.CommandTimedOut} {
		reboot := mkWindow(t, winSpec{action: models.ActionReboot, status: st, dispatch: at(-10 * time.Minute), exec: 20 * time.Minute, finished: at(0)})
		tl := evaluate(reboot, online, t0.Add(time.Second), time.Time{})
		assert.True(t, tl.coversAt(tl.offlineEnd, t0.Add(-time.Second)), "[FR-004] %s: an offline before F is covered", st)
		assert.False(t, tl.coversAt(tl.offlineEnd, t0), "[FR-004] %s: not from F on", st)
		assert.True(t, tl.coversAt(tl.containerEnd, t0.Add(90*time.Second)), "[FR-004] %s: a container death is covered for 2 min", st)
		assert.False(t, tl.coversAt(tl.containerEnd, t0.Add(2*time.Minute)), "%s: but not at F + 2 min", st)
		assert.Equal(t, models.EndNonSuccessSettle, tl.reason)
		assert.Equal(t, t0.Add(2*time.Minute), tl.closeAt)

		agent := mkWindow(t, winSpec{action: models.ActionAgentUpdate, status: st, dispatch: at(-10 * time.Minute), exec: 15 * time.Minute, finished: at(0)})
		atl := evaluate(agent, online, t0, time.Time{})
		assert.Equal(t, t0, atl.closeAt, "%s: an agent window ends at F", st)
	}
}

func TestEX_Window_ShutdownReturnPhase(t *testing.T) {
	w := mkWindow(t, winSpec{action: models.ActionShutdown, status: models.CommandSucceeded, dispatch: at(-time.Minute), exec: 20 * time.Minute, finished: at(0)})
	tl := evaluate(w, online, t0.Add(time.Minute), time.Time{})
	assert.Equal(t, models.PhaseReturn, tl.phase)
	assert.Equal(t, t0.Add(15*time.Minute), tl.closeAt, "[Table 4.1] 15 min after success when the device never goes offline")
	assert.True(t, tl.coversAt(tl.offlineEnd, t0.Add(time.Minute)))
	assert.True(t, tl.coversAt(tl.containerEnd, t0.Add(time.Minute)), "a host-class window covers every container")
}

func TestEX_Window_LapsedAndDeferred(t *testing.T) {
	w := mkWindow(t, winSpec{action: models.ActionReboot, status: models.CommandSucceeded, dispatch: at(-time.Minute), exec: 20 * time.Minute, finished: at(0)})
	tl := evaluate(w, online, t0.Add(15*time.Minute+models.ExpectDeferredRecheckBound), time.Time{})
	assert.True(t, tl.due)
	assert.Equal(t, models.EndLapsed, tl.reason, "[AD-009] an unprocessed window 24 h past its close lapses")

	w.deferred = true
	d := evaluate(w, online, t0.Add(time.Hour), time.Time{})
	assert.Equal(t, models.PhaseAwaitingReturn, d.phase)
	assert.False(t, d.coversAt(d.containerEnd, t0.Add(time.Minute)), "[AD-011] a deferred window covers nothing")
	assert.False(t, d.coversAt(d.offlineEnd, t0.Add(time.Minute)))
}

func TestEX_Window_ExecBudgetFallbacks(t *testing.T) {
	d := t0
	c := models.Command{Action: models.ActionReboot, DispatchedAt: &d}
	assert.Equal(t, models.RebootExecDeadline, execBudget(&c), "no deadline and no seconds: the policy default")
	c.ExecDeadlineS = 600
	assert.Equal(t, 10*time.Minute, execBudget(&c), "exec_deadline_s is the fallback")
	dl := d.Add(7 * time.Minute)
	c.DeadlineAt = &dl
	assert.Equal(t, 7*time.Minute, execBudget(&c), "deadline_at wins")
}

// EXTRA-UPD-001 AC-034: an os_update stamped with the extra-updater budget keeps
// its window open for the longer 95-minute deadline, not the plain patch one.
func TestEX_Window_ExecBudgetExtraUpdaterDeadline(t *testing.T) {
	d := t0
	c := models.Command{Action: models.ActionOSUpdate, DispatchedAt: &d,
		ExecDeadlineS: int(models.OSUpdateExtraExecDeadline / time.Second)}
	assert.Equal(t, models.OSUpdateExtraExecDeadline, execBudget(&c), "exec_deadline_s carries the extra-updater deadline")
	dl := d.Add(models.OSUpdateExtraExecDeadline)
	c.DeadlineAt = &dl
	assert.Equal(t, models.OSUpdateExtraExecDeadline, execBudget(&c), "deadline_at derived from it agrees")
	assert.Greater(t, execBudget(&c), models.ExecDeadline(models.ActionOSUpdate, 0), "longer than the plain patch deadline")
}
