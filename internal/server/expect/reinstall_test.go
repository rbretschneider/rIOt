package expect_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-008 tracker tests (ADD section 8, "H8"). They use the EXPECT-001
// harness: a real Tracker, commands.Service and events.Generator over the
// testutil mocks and a fake clock; no sleeps, no real time. The installer's
// notice, the agent's registration and the offline detector are simulated by the
// harness helpers, which mirror the handlers' order.

const (
	reLabel = "Agent reinstall"
	reBy    = "by installer"
)

// withPiVPN adds the PiVPN device of the 2026-10-27 evidence to the harness.
func withPiVPN() hOpt {
	return withDevices(func(h *harness) {
		hb := baseTime
		h.devices.Devices["pivpn"] = &models.Device{ID: "pivpn", Hostname: "PiVPN", Status: models.DeviceStatusOnline, LastHeartbeat: &hb, AgentVersion: "2.82.0"}
	})
}

func TestEX8_AC008_TimelineNoNotification(t *testing.T) {
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	require.Equal(t, models.ReinstallCreated, h.reinstallNotice("pivpn"))
	rec := h.reinstallRecord("pivpn")
	assert.Equal(t, "running", rec.Status)
	assert.True(t, rec.DispatchedAt.Equal(baseTime), "the start is the notice's receive time")

	h.at("10:00:50")
	off := h.deviceOffline("pivpn")
	require.True(t, off.Covered, "[AC-008] the offline inside the window is covered")
	assert.Equal(t, models.OfflineKindWindow, off.Kind)

	h.at("10:01:10")
	note := h.registerOn("pivpn", "2.83.0")
	require.NotNil(t, note, "the tracker names the reinstall in the online event")
	h.tick()

	got := h.command(rec.ID)
	assert.Equal(t, "succeeded", got.Status, "[AC-008] the record ends succeeded")
	assert.Equal(t, models.ReinstallStepConfirmed, got.Step)
	require.NotNil(t, got.FinishedAt)
	assert.True(t, got.FinishedAt.Equal(baseTime.Add(70*time.Second)), "finished_at is the registration time")
	assert.NotNil(t, got.WindowClosedAt, "the window is processed")

	offline := suppressedOffline(h)
	require.Len(t, offline, 1, "[AC-008] one suppressed offline event")
	assert.Equal(t, models.SeverityInfo, offline[0].Severity)
	assert.Equal(t, rec.ID, offline[0].CommandID, "linked to the record")
	assert.Equal(t, fmt.Sprintf("Device PiVPN went offline during %s %s (expected, back by %s)", reLabel, reBy, h.local(baseTime.Add(10*time.Minute))),
		offline[0].Message, "[FR-017] the exact text")
	online := h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected)
	require.Len(t, online, 1, "[FR-018] one online event that names the reinstall")
	assert.Equal(t, fmt.Sprintf("Device PiVPN came online after %s %s (offline 20s)", reLabel, reBy), online[0].Message)
	assert.Equal(t, rec.ID, online[0].CommandID)

	requireNoAlerts(t, h, "[AC-008] no notification on any channel")
	assert.Empty(t, h.warningEvents(models.EventAgentReinstallUnconfirmed), "no outcome event for a success")
	assert.Empty(t, h.warningEvents(models.EventAgentReinstallPaused))
	assert.Empty(t, h.escalations(), "no escalation")
	assert.Nil(t, h.activity("pivpn"), "[FR-024] the label is gone")
	assert.Empty(t, h.eventsOf(models.EventAgentReinstallStarted, ""), "the notice itself is never stored as an event")
}

func TestEX8_AC008_LabelFromNoticeUntilRegistration(t *testing.T) {
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	h.reinstallNotice("pivpn")
	rec := h.reinstallRecord("pivpn")

	a := h.activity("pivpn")
	require.NotNil(t, a, "[FR-024] the label shows from the notice")
	assert.Equal(t, models.ActivityAgent, a.Kind)
	assert.Equal(t, models.PhaseInProgress, a.Phase)
	assert.Equal(t, models.ActionAgentUpdate, a.Action)
	assert.Equal(t, models.OriginInstaller, a.Origin, "[AD-010] the DTO names the origin")
	assert.Equal(t, rec.ID, a.CommandID)
	assert.Equal(t, reBy, a.ActorPhrase)
	assert.Empty(t, a.AgentFromVersion, "[DEC-3] no versions")
	assert.Empty(t, a.AgentToVersion)
	assert.True(t, a.StartedAt.Equal(baseTime), "started_at is the notice time")
	assert.True(t, a.CoversOffline && a.DeviceOnline)
	assert.Nil(t, a.BackBy, "the DTO carries no back_by")
	assert.Nil(t, a.Escalation)

	h.at("10:00:50")
	require.True(t, h.deviceOffline("pivpn").Covered)
	a = h.activity("pivpn")
	require.NotNil(t, a, "[FR-024] the label shows whether the device is online or offline")
	assert.False(t, a.DeviceOnline)
	assert.True(t, a.CoversOffline, "[FR-025] an offline device inside the window counts as expected offline")

	h.at("10:01:10")
	h.registerOn("pivpn", "2.83.0")
	assert.Nil(t, h.activity("pivpn"), "[FR-024] the label clears at the registration")
	assert.Equal(t, "succeeded", h.command(rec.ID).Status)
}

func TestEX8_AC009_TwoDevicesBackToBackZeroPushes(t *testing.T) {
	h := newHarness(t, withPiVPN())
	for i, d := range []string{"picups", "pivpn"} {
		base := baseTime.Add(time.Duration(i) * 30 * time.Second)
		h.clock.Set(base)
		h.settle()
		require.Equal(t, models.ReinstallCreated, h.reinstallNotice(d), "%s: the notice opens a record", d)
		h.clock.Set(base.Add(50 * time.Second))
		h.settle()
		require.True(t, h.deviceOffline(d).Covered, "%s: the offline is covered", d)
		h.clock.Set(base.Add(90 * time.Second))
		h.settle()
		h.registerOn(d, "2.83.0")
	}
	h.advance(time.Minute)
	for _, d := range []string{"picups", "pivpn"} {
		recs := h.reinstallRecords(d)
		require.Len(t, recs, 1, "[AC-009] one record for %s", d)
		assert.Equal(t, "succeeded", recs[0].Status)
	}
	assert.Len(t, suppressedOffline(h), 2, "[AC-009] two info offline events")
	requireNoAlerts(t, h, "[AC-009, BR-001] zero notifications")
}

func TestEX8_AC010_ContainerDeathDuringReinstallAlerts(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("picups")
	h.at("10:00:10")
	res := h.containerDied("picups", "plex")
	assert.False(t, res.Suppressed, "[AC-010, FR-014] a reinstall window never covers a container")
	deaths := h.alertsOf(models.EventContainerDied)
	require.Len(t, deaths, 1, "the container_died alert fires")
	assert.True(t, deaths[0].Notified, "and notifies exactly as with no window")
}

func TestEX8_AC011_OfflineAfterCloseAlerts(t *testing.T) {
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	h.reinstallNotice("pivpn")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("pivpn").Covered)
	h.at("10:01:10")
	h.registerOn("pivpn", "2.83.0")
	h.tick()
	require.Equal(t, "succeeded", h.reinstallRecord("pivpn").Status)

	h.at("10:05:00") // still before the 10-minute bound, but after the registration
	res := h.deviceOffline("pivpn")
	assert.False(t, res.Covered, "[AC-011, FR-016] coverage ended at the registration, not at the bound")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "the device_offline alert fires")
	assert.True(t, offs[0].Notified, "and notifies normally")
}

func TestEX8_AC012_NotBackEscalatesOnce(t *testing.T) {
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	h.reinstallNotice("pivpn")
	rec := h.reinstallRecord("pivpn")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("pivpn").Covered)

	h.at("10:09:59")
	assert.Empty(t, h.escalations(), "nothing before the bound")
	assert.Equal(t, "running", h.command(rec.ID).Status)
	h.at("10:10:00")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-012] exactly one escalation, within 60 s of the bound")
	assert.Equal(t, fmt.Sprintf("PiVPN still offline 10m after %s %s (record %s)", reLabel, reBy, models.ShortCommandID(rec.ID)),
		esc[0].Message, "[FR-021] the exact text")
	assert.Equal(t, rec.ID, esc[0].CommandID)
	assert.Len(t, h.esc.Pushes(), 1, "and it notifies, through the device_offline rule")
	got := h.command(rec.ID)
	assert.Equal(t, "timed_out", got.Status, "the outcome is not confirmed")
	assert.Equal(t, models.ReinstallFailureUnconfirmedOffline, got.FailureStep)
	assert.Equal(t, "not confirmed: PiVPN was offline 10m after the start", got.ResultMsg)
	assert.Empty(t, h.warningEvents(models.EventAgentReinstallUnconfirmed), "an offline device escalates; no extra event")
	outcome, _ := h.repo.WindowOutcome(rec.ID)
	assert.Equal(t, models.WindowOutcomeEscalated, outcome)

	h.advance(time.Hour)
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-012] never a second escalation")
	assert.Len(t, h.esc.Pushes(), 1)
}

func TestEX8_AC012_EscalationIgnoresUnrelatedCooldown(t *testing.T) {
	h := newHarness(t)
	for i := range h.rules.Rules {
		if h.rules.Rules[i].Metric == "device_offline" {
			h.rules.Rules[i].CooldownSeconds = 3600
		}
	}
	h.gen.InvalidateRulesCache()
	// An unrelated alert starts the rule's cooldown for hawaii at 10:00:00.
	require.False(t, h.deviceOffline("hawaii").Covered)
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1)
	h.at("10:00:20")
	h.registerOn("hawaii", "2.77.0")
	h.at("10:00:30")
	require.Equal(t, models.ReinstallCreated, h.reinstallNotice("hawaii"))
	h.at("10:01:30")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:10:30")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-012] the escalation is raised although the rule's cooldown is running")
	assert.Len(t, h.esc.Pushes(), 1, "and it notifies")
}

func TestEX8_AC012_NoSecondEscalationAfterRestart(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("hawaii")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:05:00")
	h.restart(time.Time{})
	h.at("10:10:00")
	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1)
	rec := h.reinstallRecord("hawaii")
	assert.Equal(t, []string{models.EscalationTargetDevice}, targetsOf(h.repo.EscalationMarkers(rec.ID)), "one marker")

	h.restart(time.Time{})
	h.at("10:20:00")
	h.restart(time.Time{})
	h.at("12:00:00")
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-012] across server restarts, never twice")
	assert.Len(t, h.esc.Pushes(), 1)
}

func TestEX8_AC012_EscalatedLabelAndOfflineCount(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("hawaii")
	rec := h.reinstallRecord("hawaii")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:10:00")

	a := h.activity("hawaii")
	require.NotNil(t, a)
	assert.Equal(t, models.ActivityEscalated, a.Kind, "the escalated state")
	assert.Equal(t, models.PhaseEscalated, a.Phase)
	assert.Equal(t, models.OriginInstaller, a.Origin, "[AD-010] the escalated DTO names the origin")
	require.NotNil(t, a.Escalation)
	assert.Equal(t, models.EscalationReasonNotBack, a.Escalation.Reason)
	assert.Equal(t, "10m", a.Escalation.Bound, "the label reads 'Not back 10m after Agent reinstall'")
	assert.Equal(t, rec.ID, a.CommandID)
	assert.Equal(t, reBy, a.ActorPhrase)
	assert.False(t, a.DeviceOnline, "an escalated device counts as offline")
	assert.False(t, a.CoversOffline, "and no longer covers anything")

	// The label survives a restart (rebuilt from the row).
	h.restart(time.Time{})
	h.at("10:11:00")
	a = h.activity("hawaii")
	require.NotNil(t, a, "the escalated label is rebuilt after a restart")
	assert.Equal(t, models.ActivityEscalated, a.Kind)
	assert.Equal(t, models.OriginInstaller, a.Origin, "[AD-010] the origin is rebuilt")
	assert.Equal(t, models.EscalationReasonNotBack, a.Escalation.Reason, "timed_out maps to not_back for a reinstall record")
	assert.Equal(t, "10m", a.Escalation.Bound)

	h.at("10:30:00")
	h.registerOn("hawaii", "2.83.0")
	assert.Nil(t, h.activity("hawaii"), "the failure label clears when the device is back")
}

func TestEX8_AC013_NotConfirmedWhileOnlineWarningNoPush(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("picups")
	rec := h.reinstallRecord("picups")
	h.at("10:05:00")
	h.heartbeat("picups")
	h.at("10:09:50")
	h.heartbeat("picups")
	h.at("10:10:00")

	got := h.command(rec.ID)
	assert.Equal(t, "timed_out", got.Status, "[AC-013] not confirmed")
	assert.Equal(t, models.ReinstallFailureUnconfirmedOnline, got.FailureStep)
	assert.NotNil(t, got.WindowClosedAt, "the window is closed")
	out := h.eventsLinked(models.EventAgentReinstallUnconfirmed, rec.ID)
	require.Len(t, out, 1, "exactly one warning event")
	assert.Equal(t, models.SeverityWarning, out[0].Severity)
	assert.Equal(t, "Agent reinstall by installer on PiCUPS was not confirmed within 10m (agent did not restart)", out[0].Message)
	requireNoAlerts(t, h, "no notification")
	h.advance(time.Hour)
	assert.Len(t, h.eventsLinked(models.EventAgentReinstallUnconfirmed, rec.ID), 1, "still exactly one")

	// A later offline transition alerts normally.
	h.at("11:15:00")
	assert.False(t, h.deviceOffline("picups").Covered, "[AC-013] the window is closed")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified)
}

func TestEX8_AC014_OfflineAfterRejectedNoticeAlerts(t *testing.T) {
	// A rejected notice (no key, wrong key, revoked key, another device's key)
	// never reaches the service: the handler tests pin that. The tracker side is
	// that a device with no record alerts as today.
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	h.reinstallNotice("picups") // picups's own valid notice
	h.at("10:01:00")
	res := h.deviceOffline("pivpn")
	assert.False(t, res.Covered, "[AC-014] pivpn has no record: its offline is not covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.Equal(t, "pivpn", offs[0].Device)
	assert.True(t, offs[0].Notified, "and notifies")
	assert.Empty(t, h.reinstallRecords("pivpn"))
	assert.Nil(t, h.activity("pivpn"), "no label either")
}

func TestEX8_AC016_CappedNoticeOfflineAlertsNoLogAboveDebug(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < models.ReinstallRateCap; i++ {
		h.clock.Set(baseTime.Add(time.Duration(i) * time.Hour))
		h.settle()
		h.heartbeat("hawaii")
		require.Equal(t, models.ReinstallCreated, h.reinstallNotice("hawaii"), "notice %d creates a record", i+1)
		h.advance(90 * time.Second)
		h.registerOn("hawaii", "2.83.0")
		h.tick()
	}
	h.clock.Set(baseTime.Add(6*time.Hour + 30*time.Minute))
	h.settle()
	h.heartbeat("hawaii")
	assert.Equal(t, models.ReinstallCapped, h.reinstallNotice("hawaii"), "[AC-016] the 7th notice is capped")
	assert.Len(t, h.reinstallRecords("hawaii"), 6)
	paused := h.warningEvents(models.EventAgentReinstallPaused)
	require.Len(t, paused, 1, "exactly one rate-cap warning event")
	assert.Equal(t, models.ReinstallRateCapMessage("hawaii"), paused[0].Message)
	assert.Empty(t, h.esc.Pushes(), "no notification for the rate-cap event")
	assert.Empty(t, h.disp.Alerts, "and nothing dispatched")

	h.advance(time.Minute)
	assert.Equal(t, models.ReinstallCapped, h.reinstallNotice("hawaii")) // a repeat: no second cap event
	assert.Len(t, h.warningEvents(models.EventAgentReinstallPaused), 1)
	h.advance(time.Minute)
	res := h.deviceOffline("hawaii")
	assert.False(t, res.Covered, "[AC-016] offline transitions after a capped notice alert normally")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified)
	for _, lvl := range []string{"WARN", "ERROR", "INFO"} {
		assert.Zero(t, h.logLevelLines(lvl), "[NFR-005] no %s log line for the repeats and the refused notices", lvl)
	}
}

func TestEX8_AC018_StoreFailureFailsOpen(t *testing.T) {
	h := newHarness(t)
	h.cmds.FailOn("CreateReinstall", fmt.Errorf("database is down"))
	h.at("10:00:00")
	assert.Equal(t, models.ReinstallUnavailable, h.reinstallNotice("hawaii"))
	assert.Empty(t, h.reinstallRecords("hawaii"))
	h.at("10:01:00")
	res := h.deviceOffline("hawaii")
	assert.False(t, res.Covered, "[AC-018] without a record the offline is not covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "and alerts")
	assert.True(t, offs[0].Notified, "and notifies")
}

func TestEX8_AC019_RestartMidWindowSucceeds(t *testing.T) {
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	h.reinstallNotice("pivpn")
	rec := h.reinstallRecord("pivpn")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("pivpn").Covered)
	require.Len(t, suppressedOffline(h), 1, "the offline was stored as info")

	h.clock.Set(baseTime.Add(90 * time.Second))
	h.restart(baseTime.Add(3 * time.Minute)) // 10:01:30, grace to 10:03:00
	h.at("10:02:00")
	note := h.registerOn("pivpn", "2.83.0")
	require.NotNil(t, note, "[AC-019] the online event still names the reinstall after the restart")
	assert.Contains(t, note.ActionLabel, reLabel)
	h.tick()

	assert.Equal(t, "succeeded", h.command(rec.ID).Status, "the outcome is succeeded")
	assert.Empty(t, h.alertsOf(models.EventDeviceOffline), "no device_offline notification exists")
	requireNoAlerts(t, h, "no notification")
}

func TestEX8_AC019_BoundDuringDowntimeEscalatesOnce(t *testing.T) {
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	h.reinstallNotice("pivpn")
	rec := h.reinstallRecord("pivpn")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("pivpn").Covered)

	// The server is down from 10:05:00 to 10:20:00 and starts with a 2 minute grace.
	h.clock.Set(baseTime.Add(20 * time.Minute))
	h.restart(baseTime.Add(22 * time.Minute))
	h.at("10:20:30")
	assert.Empty(t, h.escalations(), "nothing is decided during the restart grace")
	h.at("10:22:00")
	h.detectorPass()
	h.tick()

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-019] exactly one escalation, right after the grace")
	assert.Contains(t, esc[0].Message, "still offline 10m after "+reLabel)
	assert.Equal(t, "timed_out", h.command(rec.ID).Status)
	assert.Equal(t, models.ReinstallFailureUnconfirmedOffline, h.command(rec.ID).FailureStep)

	h.restart(time.Time{})
	h.at("10:30:00")
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-019] and no second one after another restart")
	assert.Len(t, h.esc.Pushes(), 1)
}

func TestEX8_AC019_DeadDuringDowntimeEscalatesNotUnconfirmed(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("hawaii")
	rec := h.reinstallRecord("hawaii")
	h.at("10:04:40")
	h.heartbeat("hawaii")

	h.clock.Set(baseTime.Add(20 * time.Minute))
	h.restart(baseTime.Add(22 * time.Minute))
	h.at("10:22:00")
	assert.Equal(t, "running", h.command(rec.ID).Status, "the bound waits for the detector's first pass")
	assert.Empty(t, h.warningEvents(models.EventAgentReinstallUnconfirmed), "no 'not confirmed while online' for a dead device")

	h.detectorPass()
	h.tick()
	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-019] the device escalates")
	got := h.command(rec.ID)
	assert.Equal(t, models.ReinstallFailureUnconfirmedOffline, got.FailureStep, "unconfirmed while OFFLINE, not online")
	assert.Empty(t, h.warningEvents(models.EventAgentReinstallUnconfirmed))
}

func TestEX8_AC020_ResolverUnsetFailsSafe(t *testing.T) {
	h := newHarness(t, withoutReinstallResolver())
	h.at("10:00:00")
	h.reinstallNotice("hawaii")
	rec := h.reinstallRecord("hawaii")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("hawaii").Covered)

	h.at("10:10:00")
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "without a resolver the escalation still fires: it runs before resolution")
	assert.Len(t, h.esc.Pushes(), 1)
	assert.Equal(t, "running", h.command(rec.ID).Status, "the record cannot be resolved")
	h.at("10:10:30")
	assert.False(t, h.deviceOffline("hawaii", baseTime.Add(10*time.Minute+30*time.Second)).Covered,
		"coverage ended at the bound: a later offline would alert")

	for _, hms := range []string{"10:20:00", "10:40:00", "11:09:00"} {
		h.at(hms)
	}
	assert.Empty(t, h.warningEvents(models.EventAgentReinstallPaused), "no stuck event before bound + 1h")

	dispatchedBefore := len(h.disp.Alerts)
	h.at("11:10:00")
	paused := h.eventsLinked(models.EventAgentReinstallPaused, rec.ID)
	require.Len(t, paused, 1, "exactly one agent_reinstall_paused event at bound + 1h")
	assert.Equal(t, models.SeverityWarning, paused[0].Severity)
	assert.Equal(t, models.ReinstallStuckMessage("hawaii", models.ShortCommandID(rec.ID)), paused[0].Message)
	assert.Len(t, h.esc.Pushes(), 1, "no push for it")
	assert.Len(t, h.disp.Alerts, dispatchedBefore, "nothing was dispatched for it")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "reinstall record could not be completed one hour after its bound"), "and one Warn")

	h.at("11:11:30")
	assert.Len(t, h.eventsLinked(models.EventAgentReinstallPaused, rec.ID), 1, "still exactly one")
	h.restart(time.Time{})
	h.at("11:13:00")
	assert.Len(t, h.eventsLinked(models.EventAgentReinstallPaused, rec.ID), 1, "also across a restart (the marker is durable)")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "reinstall record could not be completed one hour after its bound"), "and no second Warn")

	// The next accepted notice (the device is online again) replaces the stuck row.
	h.at("11:14:00")
	h.registerOn("hawaii", "2.83.0")
	h.at("11:15:00")
	assert.Equal(t, models.ReinstallCreated, h.reinstallNotice("hawaii"), "the next notice is not blocked by the stuck row")
	assert.Equal(t, "timed_out", h.command(rec.ID).Status)
	assert.Equal(t, models.ReinstallFailureUnresolved, h.command(rec.ID).FailureStep)
}

func TestEX8_AC021_BoundedEventsLogsMemory(t *testing.T) {
	h := newHarness(t, withPiVPN())
	// AC-008 on pivpn, AC-012 on hawaii, AC-013 on picups, interleaved in time.
	h.at("10:00:00")
	h.reinstallNotice("pivpn")
	h.reinstallNotice("hawaii")
	h.reinstallNotice("picups")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("pivpn").Covered)
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:01:10")
	h.registerOn("pivpn", "2.83.0")
	h.at("10:05:00")
	h.heartbeat("picups")
	h.at("10:09:50")
	h.heartbeat("picups")
	h.at("10:10:00")

	count := func(device string) int {
		n := 0
		for _, e := range h.allEvents() {
			if e.DeviceID == device {
				n++
			}
		}
		return n
	}
	assert.Equal(t, 2, count("pivpn"), "[AC-021] AC-008: one info offline and one online (the notice stores no event)")
	assert.Equal(t, 2, count("hawaii"), "AC-012: one info offline and one escalation")
	assert.Equal(t, 1, count("picups"), "AC-013: one warning")
	assert.Len(t, h.escalations(), 1, "one escalation in all")
	for _, d := range []string{"pivpn", "hawaii", "picups"} {
		assert.Len(t, h.reinstallRecords(d), 1, "one record for %s", d)
	}
	for _, lvl := range []string{"WARN", "ERROR"} {
		assert.Zero(t, h.logLevelLines(lvl), "[NFR-005] no %s line", lvl)
	}
	assert.Equal(t, 1, h.logLevelLines("INFO"), "[NFR-005] one info line: the AC-012 escalation")

	h.advance(6 * time.Minute)
	st := h.tr.Stats()
	assert.Zero(t, st.Windows, "[NFR-005] no window is cached")
	assert.Zero(t, st.ClosedRecent, "and the closed ids are released")
}

func TestEX8_AC023_ReinstallDuringSelfUpdate(t *testing.T) {
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	su := h.selfStart("pivpn")
	require.NotEmpty(t, su.CommandID)
	selfID := su.CommandID
	h.at("10:02:00")
	require.Equal(t, models.ReinstallCreated, h.reinstallNotice("pivpn"), "[AC-023] a reinstall record is created next to the self-update record")
	reID := h.reinstallRecord("pivpn").ID
	require.NotEqual(t, selfID, reID)

	a := h.activity("pivpn")
	require.NotNil(t, a)
	assert.Equal(t, reID, a.CommandID, "[FR-019, DEC-8] the device shows the label with the latest start")
	assert.Equal(t, models.OriginInstaller, a.Origin)
	assert.Equal(t, reBy, a.ActorPhrase)

	h.at("10:02:50")
	require.True(t, h.deviceOffline("pivpn").Covered)
	off := suppressedOffline(h)
	require.Len(t, off, 1, "the offline transition is suppressed once")
	assert.Equal(t, reID, off[0].CommandID, "and linked to the later record")

	// Not back: each record escalates by its own bound; the earlier bound is the
	// self-update's (10:10:00), then the reinstall's (10:12:00).
	h.at("10:10:00")
	require.Equal(t, "timed_out", h.command(selfID).Status, "the self-update record reaches its own outcome by its own bound")
	assert.Equal(t, "running", h.command(reID).Status, "the reinstall record is still open")
	h.at("10:12:00")
	assert.Equal(t, "timed_out", h.command(reID).Status, "[AC-023] the reinstall record reaches its own outcome by its own bound")
	assert.NotEmpty(t, h.escalationsOf(models.EventDeviceOffline), "the outage escalated")
	assert.LessOrEqual(t, len(h.esc.Pushes()), 2, "at most one push per record, under the ceiling")
}

func TestEX8_AC024_ReinstallWithoutNoticeAlertsAsToday(t *testing.T) {
	// An old installer never sends a notice: no record, and the offline alerts.
	h := newHarness(t, withPiVPN())
	h.at("10:00:00")
	h.at("10:00:50")
	res := h.deviceOffline("pivpn")
	assert.False(t, res.Covered, "[AC-024] no notice, no coverage")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified, "the offline pages as today")
	h.at("10:01:10")
	note := h.registerOn("pivpn", "2.83.0")
	assert.Nil(t, note, "and the online event is the plain one")
	assert.Empty(t, h.reinstallRecords("pivpn"))
}

func TestEX8_AC025_SelfUpdateAndCommandWindowsUnchanged(t *testing.T) {
	h := newHarness(t)
	// A self-update still reads as before next to the reinstall code.
	h.at("10:00:00")
	h.selfStart("hawaii")
	a := h.activity("hawaii")
	require.NotNil(t, a)
	assert.Equal(t, "2.78.0", a.AgentFromVersion)
	assert.Equal(t, "2.80.0", a.AgentToVersion)
	assert.Equal(t, selfBy, a.ActorPhrase)
	assert.Equal(t, models.OriginAgentSelfUpdate, a.Origin)
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	off := suppressedOffline(h)
	require.Len(t, off, 1)
	assert.Contains(t, off[0].Message, "during "+selfLabel+" "+selfBy, "[FR-027] the EXPECT-002 text is unchanged")
	h.at("10:02:30")
	h.registerOn("hawaii", "2.80.0")
	h.agentCompleted("hawaii", "signed installation completion verified against running executable")
	h.tick()
	assert.Equal(t, "succeeded", h.selfRecord("hawaii").Status, "a registration still moves only the self-update record")
	assert.Empty(t, h.reinstallRecords("hawaii"))

	// A user agent_update command keeps its label and policy.
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.at("10:03:00")
	b := h.activity("vermont")
	require.NotNil(t, b)
	assert.Equal(t, c.ID, b.CommandID)
	assert.Equal(t, "by ryan", b.ActorPhrase)
	assert.Equal(t, models.OriginUser, b.Origin)
}

func TestEX8_OutcomeEventsNeverDispatchedNorCeilinged(t *testing.T) {
	h := newHarness(t)
	// Five unconfirmed-online records in an hour: five outcome events. If they
	// used the push ceiling (5 per device per hour), the escalation after them
	// would be held.
	for i := 0; i < 5; i++ {
		h.clock.Set(baseTime.Add(time.Duration(i) * 11 * time.Minute))
		h.settle()
		h.heartbeat("hawaii")
		require.Equal(t, models.ReinstallCreated, h.reinstallNotice("hawaii"))
		h.advance(10*time.Minute + time.Second)
		h.heartbeat("hawaii")
		h.tick()
	}
	require.Len(t, h.warningEvents(models.EventAgentReinstallUnconfirmed), 5, "five outcome events")
	assert.Empty(t, h.disp.Alerts, "an outcome event is never dispatched to a channel")
	assert.Empty(t, h.esc.Pushes(), "and never queued as a push")

	// The sixth record escalates: it must still push.
	h.clock.Set(baseTime.Add(56 * time.Minute))
	h.settle()
	h.heartbeat("hawaii")
	require.Equal(t, models.ReinstallCreated, h.reinstallNotice("hawaii"))
	h.advance(time.Minute)
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.clock.Set(baseTime.Add(66*time.Minute + 5*time.Second))
	h.settle()
	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1)
	assert.Len(t, h.esc.Pushes(), 1, "the escalation pushes: the outcome events consumed none of the 5-per-hour ceiling")
	assert.Len(t, h.disp.Alerts, 1, "the only dispatch is the escalation")
	assert.Empty(t, h.warningEvents(models.EventExpectNotificationsHeld), "nothing was held")
}

func TestEX8_NoteOnlineOnlyForCoveredOffline(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("hawaii")
	h.at("10:00:50")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:01:10")
	note := h.registerOn("hawaii", "2.83.0")
	require.NotNil(t, note, "the covered offline names the reinstall")

	// A later offline, after the registration, is not covered; its online note is plain.
	h.at("10:03:00")
	assert.False(t, h.deviceOffline("hawaii").Covered)
	h.at("10:07:30")
	note = h.registerOn("hawaii", "2.83.0")
	assert.Nil(t, note, "an offline that began outside the coverage gets no reinstall note")
	var plain bool
	for _, e := range h.eventsOf(models.EventDeviceOnline, "") {
		plain = plain || e.Message == "Device hawaii came online"
	}
	assert.True(t, plain, "the online event is the plain one")
}

func TestEX8_OtherDeviceUnaffected(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("hawaii")
	h.at("10:01:00")
	res := h.deviceOffline("vermont")
	assert.False(t, res.Covered, "[NFR-007] vermont's offline is not covered by hawaii's window")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.Equal(t, "vermont", offs[0].Device)
	assert.True(t, offs[0].Notified)
	assert.Nil(t, h.activity("vermont"))
	assert.NotNil(t, h.activity("hawaii"))
	// Vermont registering does not close hawaii's record.
	h.at("10:02:00")
	h.registerOn("vermont", "2.83.0")
	assert.Equal(t, "running", h.reinstallRecord("hawaii").Status)
}

func TestEX8_AC016_RepeatedNoticeDoesNotExtendWindow(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.reinstallNotice("hawaii")
	for i := 0; i < 49; i++ {
		h.advance(10 * time.Second)
		assert.Equal(t, models.ReinstallOpenRecord, h.reinstallNotice("hawaii"))
	}
	rec := h.reinstallRecord("hawaii")
	assert.True(t, rec.DeadlineAt.Equal(baseTime.Add(10*time.Minute)), "[AC-016] the window still ends 10 minutes after the first notice")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:10:00")
	assert.Equal(t, "timed_out", h.command(rec.ID).Status, "the bound is the first notice's")
}
