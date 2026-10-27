package expect_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-007 tracker tests (ADD section 8, "H7"). They use the EXPECT-001/002
// harness: a real Tracker, commands.Service and events.Generator over the
// testutil mocks and a fake clock; no sleeps, no real time. The agent is
// simulated by the harness helpers, which mirror the handlers' order. Labels
// are asserted on the activity DTO the web renders.

const (
	ex7AttemptA = "9f1c0123456789abcdef0123456789ab"
	ex7AttemptB = "1111222233334444aaaabbbbccccdddd"
)

// waitingBroadcast reports whether any activity update broadcast so far for the
// device had the waiting flag set (true) or cleared (false) as its latest.
func latestBroadcastWaiting(h *harness, device string) (waiting, seen bool) {
	for _, r := range h.bc.Activities() {
		if r.DeviceID == device && r.Activity != nil {
			waiting, seen = r.Activity.WaitingDpkgLock, true
		}
	}
	return waiting, seen
}

func TestEX7_AC001_WaitingLabelWhileOnline(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionReboot)
	h.at("10:00:04")
	h.step(c, models.StepWaitDpkgLock)
	h.at("10:00:05")

	a := h.activity("vermont")
	require.NotNil(t, a, "[AC-001] the fleet shows an activity")
	assert.True(t, a.WaitingDpkgLock, "[AC-001] Rebooting (waiting) within 5 s of the step report")
	assert.Equal(t, models.ActionReboot, a.Action)
	assert.Equal(t, "by ryan", a.ActorPhrase, "[AC-001] names the actor")
	assert.True(t, a.StartedAt.Equal(baseTime), "[AC-001] and the start time 10:00")
	assert.True(t, a.DeviceOnline, "the label is for an online device")
	waiting, seen := latestBroadcastWaiting(h, "vermont")
	assert.True(t, seen && waiting, "[AC-001] the dashboards are told within the tick, with no reload")
}

func TestEX7_AC002_StepChangeClearsSuffix(t *testing.T) {
	t.Run("the leaving step", func(t *testing.T) {
		h := newHarness(t)
		h.at("10:00:00")
		c := h.dispatch("vermont", models.ActionReboot)
		h.at("10:00:04")
		h.step(c, models.StepWaitDpkgLock)
		h.at("10:04:00")
		require.True(t, h.activity("vermont").WaitingDpkgLock)

		h.step(c, models.StepPowerAction)
		h.at("10:04:02")

		a := h.activity("vermont")
		require.NotNil(t, a, "[AC-002] still a reboot activity")
		assert.False(t, a.WaitingDpkgLock, "[AC-002] the suffix is gone within 5 s of the step change")
		waiting, seen := latestBroadcastWaiting(h, "vermont")
		assert.True(t, seen && !waiting, "[AC-002] and the dashboards were told")
	})
	t.Run("the result", func(t *testing.T) {
		h := newHarness(t)
		h.at("10:00:00")
		c := h.dispatch("vermont", models.ActionReboot)
		h.step(c, models.StepWaitDpkgLock)
		h.at("10:04:00")
		h.succeed(c)
		h.at("10:04:02")
		if a := h.activity("vermont"); a != nil {
			assert.False(t, a.WaitingDpkgLock, "[AC-002] the suffix is gone within 5 s of the result")
		}
	})
}

func TestEX7_AC003_TimeoutClearsSuffixNoNotification(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionReboot)
	h.step(c, models.StepWaitDpkgLock)
	h.at("10:10:05")
	require.True(t, h.activity("vermont").WaitingDpkgLock)

	h.fail(c) // the wait timed out: the command fails in the wait step
	h.at("10:10:07")

	if a := h.activity("vermont"); a != nil {
		assert.False(t, a.WaitingDpkgLock, "[AC-003] the waiting label clears within 5 s of the failure")
	}
	assert.Equal(t, "failed", h.command(c.ID).Status)
	requireNoAlerts(t, h, "[AC-003] with the device online no notification is sent (EXPECT-001 AC-017)")
	assert.Empty(t, h.eventsOf(models.EventDeviceOffline, ""), "no offline event")
}

func TestEX7_AC004_ShutdownWaitingLabel(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("picups", models.ActionShutdown)
	h.at("10:00:05")
	h.step(c, models.StepWaitDpkgLock)
	h.at("10:01:00")

	a := h.activity("picups")
	require.NotNil(t, a)
	assert.True(t, a.WaitingDpkgLock, "[AC-004] Shutting down (waiting) while it waits")
	assert.Equal(t, models.ActionShutdown, a.Action)
	assert.Equal(t, "by ryan", a.ActorPhrase)

	h.step(c, models.StepPowerAction)
	h.at("10:02:02")
	a = h.activity("picups")
	require.NotNil(t, a)
	assert.False(t, a.WaitingDpkgLock, "[AC-004] the suffix clears when the shutdown is initiated")
}

func TestEX7_AC005_NoStepPlainRebooting(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionReboot)
	h.at("10:00:30")
	a := h.activity("vermont")
	require.NotNil(t, a)
	assert.False(t, a.WaitingDpkgLock, "[AC-005] no wait step reported: plain Rebooting")
	assert.Equal(t, models.ActionReboot, a.Action)

	h.succeed(c)
	h.at("10:00:35")
	if a := h.activity("vermont"); a != nil {
		assert.False(t, a.WaitingDpkgLock, "[AC-005] Rebooting until the result")
	}
}

func TestEX7_AC008_OfflineHidesSuffix(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionReboot)
	h.at("10:00:04")
	h.step(c, models.StepWaitDpkgLock)
	h.at("10:00:20")
	require.True(t, h.activity("vermont").WaitingDpkgLock)

	h.at("10:00:50")
	off := h.deviceOffline("vermont")
	h.at("10:00:52")

	require.True(t, off.Covered, "[AC-008] the offline is expected")
	a := h.activity("vermont")
	require.NotNil(t, a)
	assert.False(t, a.WaitingDpkgLock, "[AC-008] the suffix is hidden within 5 s of the device going offline (BR-006)")
	assert.False(t, a.DeviceOnline)
	assert.True(t, a.CoversOffline, "[AC-008] the device is counted under Expected offline")
	assert.Equal(t, models.StepWaitDpkgLock, h.command(c.ID).Step, "the command's step itself is untouched: the server hides, it does not rewrite")
	waiting, seen := latestBroadcastWaiting(h, "vermont")
	assert.True(t, seen && !waiting, "[AC-008] the dashboards were told")

	// It comes back when the device is online again and the step is still current.
	h.at("10:02:00")
	h.register("vermont")
	if a := h.activity("vermont"); a != nil {
		assert.True(t, a.DeviceOnline)
	}
}

func TestEX7_AC010_OldAgentPlainRebooting(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionReboot)
	// An old agent waits 4 minutes for the lock and reports no step.
	for _, at := range []string{"10:00:30", "10:02:00", "10:03:59"} {
		h.at(at)
		a := h.activity("vermont")
		require.NotNil(t, a, "at %s", at)
		assert.False(t, a.WaitingDpkgLock, "[AC-010] the label shows Rebooting throughout the wait (%s)", at)
		assert.Equal(t, models.ActionReboot, a.Action)
	}
	assert.Equal(t, "", h.command(c.ID).Step, "the command's handling is identical to today: no step")
}

func TestEX7_AC011_SelfUpdateWithAttemptNoNotification(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	link := h.selfStartRef("hawaii", ex7AttemptA)
	rec := h.selfRecord("hawaii")
	require.Equal(t, rec.ID, link.CommandID, "[AC-011] the start links to the new record")
	assert.Equal(t, models.CommandAttrAgent, link.CommandAttr, "[AC-011] by a validated reference")
	assert.Equal(t, ex7AttemptA, models.SelfUpdateAttemptID(rec), "[AC-011] the record carries the attempt ID")

	h.at("10:01:00")
	off := h.deviceOffline("hawaii")
	require.True(t, off.Covered, "[AC-011] the offline inside the window is covered")
	h.at("10:02:30")
	require.NotNil(t, h.registerOn("hawaii", suTo), "the tracker names the update in the online event")
	h.at("10:02:35")
	done := h.agentCompletedRef("hawaii", "signed installation completion verified against running executable", ex7AttemptA)
	h.tick()

	assert.Equal(t, rec.ID, done.CommandID, "[AC-011] the completion is attributed by the attempt ID")
	got := h.command(rec.ID)
	assert.Equal(t, "succeeded", got.Status, "[AC-011] its outcome is succeeded")
	assert.Len(t, h.selfRecords("hawaii"), 1, "[AC-011] one record")
	assert.Len(t, h.eventsLinked(models.EventAgentUpdateStarted, rec.ID), 1, "[AC-011] the start is linked")
	assert.Len(t, h.eventsLinked(models.EventAgentUpdateCompleted, rec.ID), 1, "[AC-011] the completion is linked")
	offline := suppressedOffline(h)
	require.Len(t, offline, 1, "[AC-011] the offline transition is an info event")
	assert.Equal(t, models.SeverityInfo, offline[0].Severity)
	requireNoAlerts(t, h, "[AC-011] no notification")
}

func TestEX7_AC015_LabelUsesStructuredVersions(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.agentEventRef("hawaii", models.EventAgentUpdateStarted, models.SeverityInfo, startMessage(suFrom, "2.99.0"),
		evRef{Attempt: ex7AttemptA, From: suFrom, To: suTo})
	rec := h.selfRecord("hawaii")
	a := h.activity("hawaii")
	require.NotNil(t, a)
	assert.Equal(t, suFrom, a.AgentFromVersion, "[AC-015] Updating agent 2.78.0 → ...")
	assert.Equal(t, suTo, a.AgentToVersion, "[AC-015] ... → 2.80.0: the structured versions, not the message's 2.99.0")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "agent update start versions differ from the message"), "[AC-015] at most one debug line")

	h.at("10:01:00")
	h.deviceOffline("hawaii")
	h.at("10:02:30")
	h.registerOn("hawaii", suTo)
	assert.Equal(t, "verifying", h.command(rec.ID).Status, "[AC-015] a registration on 2.80.0 closes the window as succeeded-pending")
	assert.Nil(t, h.activity("hawaii"), "the label clears at the registration")
	assert.Zero(t, h.logLevelLines("WARN")+h.logLevelLines("ERROR"), "[AC-015] nothing above debug")
	requireNoAlerts(t, h, "[AC-015] no notification")
	assert.Empty(t, h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID), "no extra event")
}

func TestEX7_AC017_BothInvalidOfflineAlerts(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	link, stored := h.agentEventRef("hawaii", models.EventAgentUpdateStarted, models.SeverityInfo, startMessage("dev", suTo),
		evRef{Attempt: ex7AttemptA, From: "latest", To: suTo})
	assert.True(t, stored, "[AC-017] the event is stored as today")
	assert.Empty(t, link.CommandID, "[AC-017] no link")
	assert.Empty(t, h.selfRecords("hawaii"), "[AC-017] no record")

	h.at("10:01:00")
	off := h.deviceOffline("hawaii")
	assert.False(t, off.Covered, "[AC-017] nothing covers the offline")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "[AC-017] the device_offline alert fires")
	assert.True(t, offs[0].Notified, "[AC-017] and notifies")
}

func TestEX7_AC018_ReplayedAttemptOfflineAlerts(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStartRef("hawaii", ex7AttemptA)
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	h.agentFailedRef("hawaii", "Agent update failed: x", ex7AttemptA)
	h.tick()
	require.Equal(t, "failed", h.command(rec.ID).Status)

	h.at("10:20:00")
	before := h.observerCalls()
	link := h.selfStartRef("hawaii", ex7AttemptA)
	assert.Equal(t, rec.ID, link.CommandID, "[AC-018] linked to the existing record")
	assert.Equal(t, before, h.observerCalls(), "[C-3 a] the replay makes zero observer calls")
	assert.Len(t, h.selfRecords("hawaii"), 1, "[AC-018] no record is created: the 6-per-24h count is unchanged")

	h.at("10:21:00")
	off := h.deviceOffline("hawaii")
	assert.False(t, off.Covered, "[AC-018] no window opened: the offline is not covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "[AC-018] the offline transition alerts")
	assert.True(t, offs[0].Notified, "[AC-018] and notifies")
}

func TestEX7_AC020_FleetUpdateTiedToCommand(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	require.Equal(t, "dispatched", c.Status)

	h.at("10:00:02")
	link, stored := h.cmdEventRef("vermont", models.EventAgentUpdateStarted, startMessage(suFrom, suTo), c.ID)
	require.True(t, stored)
	assert.Equal(t, c.ID, link.CommandID, "[AC-020] the start is linked to C")
	assert.Equal(t, models.CommandAttrAgent, link.CommandAttr, "[AC-020] by the command reference")
	assert.Empty(t, h.selfRecords("vermont"), "[AC-020] no self-update record exists")
	a := h.activity("vermont")
	require.NotNil(t, a)
	assert.Equal(t, models.ActionAgentUpdate, a.Action, "[AC-020] Updating agent ...")
	assert.Equal(t, "by ryan", a.ActorPhrase, "[AC-020] with the command's actor phrase, not 'by agent auto-update'")
	assert.Equal(t, c.ID, a.CommandID)

	h.at("10:00:50")
	off := h.deviceOffline("vermont")
	require.True(t, off.Covered, "[AC-020] the offline is covered by C's window")
	h.at("10:01:40")
	note := h.register("vermont")
	require.NotNil(t, note, "[AC-020] the online transition is explained")
	h.at("10:01:41")
	h.succeed(c) // the restarted process completes C with a command result

	offline := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, offline, 1, "[AC-020] the offline transition is an info event")
	assert.Equal(t, models.SeverityInfo, offline[0].Severity)
	assert.Equal(t, c.ID, offline[0].CommandID, "[AC-020] linked to C")
	assert.Contains(t, offline[0].Message, "Agent update by ryan")
	online := h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected)
	require.Len(t, online, 1)
	assert.Contains(t, online[0].Message, "came online after Agent update by ryan", "[AC-020] the online event names 'Agent update by ryan'")
	assert.Equal(t, c.ID, online[0].CommandID)
	assert.Len(t, h.eventsLinked(models.EventAgentUpdateStarted, c.ID), 1, "the start event is linked to C")
	assert.Empty(t, h.selfRecords("vermont"), "[AC-020] still no self-update record")
	requireNoAlerts(t, h, "[AC-020] no notification")
}

func TestEX7_AC025_PR46ReturnKept(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.at("10:00:50")
	require.True(t, h.deviceOffline("vermont").Covered)
	h.at("10:01:30")
	h.register("vermont") // before the command's result arrives
	h.succeed(c)          // the result follows at the same instant

	evts := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, evts, 1, "[AC-025] the offline transition is an info event")
	assert.Equal(t, c.ID, evts[0].CommandID, "[AC-025] linked to C")

	h.at("10:02:29")
	assert.Nil(t, h.command(c.ID).WindowClosedAt, "[AC-025] the window is still open a second before 10:02:30")
	h.at("10:02:31")
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "[AC-025] the window closes 1 min after the agent is back")

	h.at("10:03:00")
	off := h.deviceOffline("vermont")
	assert.False(t, off.Covered, "[AC-025] an offline transition at 10:03:00 is not covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "[AC-025] it alerts normally")
	assert.True(t, offs[0].Notified)
}

// TestEX7_AC026_OldAgentTimelinesUnchanged runs the EXPECT-002 AC-001, AC-006,
// AC-009 and AC-012 timelines through the new input with empty references and
// versions (an old agent), and requires the EXPECT-002 results.
func TestEX7_AC026_OldAgentTimelinesUnchanged(t *testing.T) {
	old := evRef{}
	start := func(h *harness, device string) {
		h.agentEventRef(device, models.EventAgentUpdateStarted, models.SeverityInfo, startMessage(suFrom, suTo), old)
	}
	t.Run("AC-001 release timeline, no notification", func(t *testing.T) {
		h := newHarness(t)
		h.at("10:00:00")
		start(h, "hawaii")
		rec := h.selfRecord("hawaii")
		assert.Empty(t, models.SelfUpdateAttemptID(rec), "no attempt ID without a reference")
		h.at("10:01:00")
		require.True(t, h.deviceOffline("hawaii").Covered)
		h.at("10:02:30")
		require.NotNil(t, h.registerOn("hawaii", suTo))
		h.at("10:02:35")
		link, _ := h.agentEventRef("hawaii", models.EventAgentUpdateCompleted, models.SeverityInfo, "signed installation completion verified against running executable", old)
		h.tick()
		assert.Equal(t, rec.ID, link.CommandID)
		assert.Equal(t, models.CommandAttrInferred, link.CommandAttr, "the timing link of EXPECT-002")
		assert.Equal(t, "succeeded", h.command(rec.ID).Status)
		assert.Len(t, suppressedOffline(h), 1)
		requireNoAlerts(t, h, "no notification")
	})
	t.Run("AC-006 fleet update shows the command label", func(t *testing.T) {
		h := newHarness(t)
		h.at("10:00:00")
		c := h.dispatch("hawaii", models.ActionAgentUpdate)
		h.at("10:00:02")
		link, _ := h.agentEventRef("hawaii", models.EventAgentUpdateStarted, models.SeverityInfo, startMessage(suFrom, suTo), old)
		assert.Equal(t, c.ID, link.CommandID, "the start is captured by the command by timing")
		assert.Equal(t, models.CommandAttrInferred, link.CommandAttr)
		assert.Empty(t, h.selfRecords("hawaii"))
		a := h.activity("hawaii")
		require.NotNil(t, a)
		assert.Equal(t, "by ryan", a.ActorPhrase)
	})
	t.Run("AC-009 failed while online: a warning, no push", func(t *testing.T) {
		h := newHarness(t)
		h.at("10:00:00")
		start(h, "hawaii")
		rec := h.selfRecord("hawaii")
		h.at("10:00:40")
		link, _ := h.agentEventRef("hawaii", models.EventAgentUpdateFailed, models.SeverityWarning, "Agent update failed: download: connection reset", old)
		h.tick()
		assert.Equal(t, rec.ID, link.CommandID)
		assert.Equal(t, "failed", h.command(rec.ID).Status)
		out := h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID)
		require.Len(t, out, 1)
		assert.Equal(t, models.SeverityWarning, out[0].Severity)
		requireNoAlerts(t, h, "no notification")
	})
	t.Run("AC-012 return on the old version fails", func(t *testing.T) {
		h := newHarness(t)
		h.at("10:00:00")
		start(h, "hawaii")
		rec := h.selfRecord("hawaii")
		h.at("10:01:00")
		require.True(t, h.deviceOffline("hawaii").Covered)
		h.at("10:01:50")
		h.registerOn("hawaii", suFrom)
		h.tick()
		assert.Equal(t, "failed", h.command(rec.ID).Status)
		assert.Equal(t, models.SelfUpdateFailureWrongVersion, h.command(rec.ID).FailureStep)
		requireNoAlerts(t, h, "no notification")
	})
}

func TestEX7_AC028_FailOpenOfflineAlerts(t *testing.T) {
	h := newHarness(t)
	h.cmds.FailOn("GetForDevice", errors.New("the command store is down"))
	h.cmds.FailOn("CreateSelfUpdate", errors.New("the record store is down"))
	h.cmds.FailOn("SelfUpdateByAttempt", errors.New("the record store is down"))
	h.at("10:00:00")

	withCmd, stored1 := h.cmdEventRef("hawaii", models.EventAgentUpdateStarted, startMessage(suFrom, suTo), "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c4d")
	withAttempt, stored2 := h.agentEventRef("hawaii", models.EventAgentUpdateStarted, models.SeverityInfo, startMessage(suFrom, suTo),
		evRef{Attempt: ex7AttemptA, From: suFrom, To: suTo})

	assert.True(t, stored1 && stored2, "[AC-028] both events are stored")
	assert.Empty(t, withCmd.CommandID, "[AC-028] no link")
	assert.Empty(t, withAttempt.CommandID, "[AC-028] no link")
	assert.Empty(t, h.selfRecords("hawaii"))

	h.at("10:01:00")
	off := h.deviceOffline("hawaii")
	assert.False(t, off.Covered, "[AC-028] nothing covers the offline")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "[AC-028] the offline alert behaves exactly as under EXPECT-002 AC-020")
	assert.True(t, offs[0].Notified)
}

func TestEX7_AC035_OneOfflineOneOnlinePerAttempt(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	deliver := func(n int, f func()) {
		for i := 0; i < n; i++ {
			f()
		}
	}
	deliver(2, func() { h.selfStartRef("hawaii", ex7AttemptA) })
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	h.deviceOffline("hawaii")
	h.at("10:02:30")
	h.registerOn("hawaii", suTo)
	h.at("10:02:35")
	deliver(2, func() {
		h.agentCompletedRef("hawaii", "signed installation completion verified against running executable", ex7AttemptA)
	})
	h.tick()

	assert.Len(t, h.eventsLinked(models.EventAgentUpdateStarted, rec.ID), 1, "[AC-035] exactly one started event")
	assert.Len(t, h.eventsLinked(models.EventAgentUpdateCompleted, rec.ID), 1, "[AC-035] exactly one completed event")
	assert.Len(t, suppressedOffline(h), 1, "[AC-035] one info offline event")
	assert.Len(t, h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected), 1, "[AC-035] one online event")
	attempt := models.SelfUpdateAttemptID(h.command(rec.ID))
	assert.Equal(t, ex7AttemptA, attempt, "the record carries the attempt ID")
	assert.LessOrEqual(t, len(attempt), 64, "[NFR-002] at most 64 bytes")
	requireNoAlerts(t, h, "no notification")
	_ = time.Second
}
