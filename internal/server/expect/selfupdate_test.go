package expect_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-002 tracker tests (ADD section 8, "H2"). They use the EXPECT-001
// harness: a real Tracker, commands.Service and events.Generator over the
// testutil mocks and a fake clock; no sleeps, no real time. Agent behaviour
// (start events, registrations, completions) is simulated by the harness
// helpers, which mirror the handlers' order.

const (
	selfLabel = "Agent update 2.78.0 → 2.80.0"
	selfBy    = "by agent auto-update"
)

// suppressedOffline returns the suppressed offline events (info, expected).
func suppressedOffline(h *harness) []models.Event {
	return h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
}

func TestEX2_AC001_ReleaseTimelineNoNotification(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	link := h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	require.Equal(t, rec.ID, link.CommandID, "the start links to the new record")
	assert.Equal(t, "running", rec.Status)

	h.at("10:01:00")
	off := h.deviceOffline("hawaii")
	require.True(t, off.Covered, "[AC-001] the offline inside the window is covered")
	assert.Equal(t, models.OfflineKindWindow, off.Kind)

	h.at("10:02:30")
	note := h.registerOn("hawaii", "2.80.0")
	require.NotNil(t, note, "the tracker names the update in the online event")
	h.at("10:02:35")
	h.agentCompleted("hawaii", "signed installation completion verified against running executable")
	h.tick()

	got := h.command(rec.ID)
	assert.Equal(t, "succeeded", got.Status, "[AC-001] the record ends succeeded")
	assert.Equal(t, models.SelfUpdateStepConfirmed, got.Step)
	require.NotNil(t, got.VerifyingAt)
	assert.True(t, got.VerifyingAt.Equal(baseTime.Add(150*time.Second)), "verifying_at is the registration time")
	assert.NotNil(t, got.WindowClosedAt, "the window is processed")

	offline := suppressedOffline(h)
	require.Len(t, offline, 1, "[AC-001] one suppressed offline event")
	assert.Equal(t, models.SeverityInfo, offline[0].Severity)
	assert.Equal(t, rec.ID, offline[0].CommandID, "linked to the record")
	assert.Equal(t, fmt.Sprintf("Device hawaii went offline during %s %s (expected, back by %s)", selfLabel, selfBy, h.local(baseTime.Add(10*time.Minute))),
		offline[0].Message, "[FR-016] the exact text")
	online := h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected)
	require.Len(t, online, 1, "[FR-017] one online event that names the update")
	assert.Equal(t, fmt.Sprintf("Device hawaii came online after %s %s (offline 1m30s)", selfLabel, selfBy), online[0].Message)
	assert.Equal(t, rec.ID, online[0].CommandID)

	started := h.eventsLinked(models.EventAgentUpdateStarted, rec.ID)
	completed := h.eventsLinked(models.EventAgentUpdateCompleted, rec.ID)
	require.Len(t, started, 1, "[FR-005] the start event is stored and linked")
	require.Len(t, completed, 1, "the completed event is stored and linked")
	assert.Equal(t, startMessage("2.78.0", "2.80.0"), started[0].Message, "stored as the agent sent it")
	assert.Equal(t, models.SeverityInfo, started[0].Severity)
	assert.Equal(t, models.CommandAttrInferred, started[0].CommandAttr)
	assert.Equal(t, "signed installation completion verified against running executable", completed[0].Message)

	requireNoAlerts(t, h, "[AC-001] no notification on any channel")
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdateFailed), "no outcome event for a success")
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdateUnconfirmed))
	assert.Empty(t, h.escalations(), "no escalation")
	assert.Nil(t, h.activity("hawaii"), "[FR-024] the label is gone")
}

func TestEX2_AC001_LabelFromStartUntilRegistration(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")

	a := h.activity("hawaii")
	require.NotNil(t, a, "[FR-024] the label shows from the start")
	assert.Equal(t, models.ActivityAgent, a.Kind)
	assert.Equal(t, models.PhaseInProgress, a.Phase)
	assert.Equal(t, models.ActionAgentUpdate, a.Action)
	assert.Equal(t, rec.ID, a.CommandID)
	assert.Equal(t, selfBy, a.ActorPhrase)
	assert.Equal(t, "2.78.0", a.AgentFromVersion)
	assert.Equal(t, "2.80.0", a.AgentToVersion)
	assert.True(t, a.StartedAt.Equal(baseTime), "started_at is the start time")
	assert.True(t, a.CoversOffline && a.DeviceOnline)
	assert.Nil(t, a.BackBy, "[AD-005] the DTO carries no back_by")
	assert.Nil(t, a.Escalation)

	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	a = h.activity("hawaii")
	require.NotNil(t, a, "[FR-024] the label shows whether the device is online or offline")
	assert.False(t, a.DeviceOnline)
	assert.Equal(t, "2.80.0", a.AgentToVersion)

	h.at("10:02:30")
	h.registerOn("hawaii", "2.80.0")
	assert.Nil(t, h.activity("hawaii"), "[FR-024] the label clears at the registration")
	assert.Equal(t, "verifying", h.command(rec.ID).Status, "the record waits for the outcome")
}

func TestEX2_AC002_FleetReleaseFourRecordsZeroPushes(t *testing.T) {
	h := newHarness(t, withDevices(func(h *harness) {
		hb := baseTime
		h.devices.Devices["pivpn"] = &models.Device{ID: "pivpn", Hostname: "PiVPN", Status: models.DeviceStatusOnline, LastHeartbeat: &hb, AgentVersion: "2.78.0"}
	}))
	devices := []string{"hawaii", "vermont", "picups", "pivpn"}
	for i, d := range devices {
		base := baseTime.Add(time.Duration(i) * 3 * time.Minute)
		h.clock.Set(base)
		h.settle()
		h.selfStart(d)
		h.clock.Set(base.Add(time.Minute))
		h.settle()
		require.True(t, h.deviceOffline(d).Covered, "%s: the offline is covered", d)
		h.clock.Set(base.Add(105 * time.Second))
		h.settle()
		h.registerOn(d, "2.80.0")
		h.clock.Set(base.Add(110 * time.Second))
		h.agentCompleted(d, "signed installation completion verified against running executable")
		h.settle()
	}
	for _, d := range devices {
		rec := h.selfRecord(d)
		assert.Equal(t, "succeeded", rec.Status, "[AC-002] %s: one record, succeeded", d)
	}
	assert.Len(t, suppressedOffline(h), 4, "[AC-002] four info offline events")
	requireNoAlerts(t, h, "[AC-002] zero notifications for a release across the fleet")
}

func TestEX2_AC003_ContainerDeathDuringSelfUpdateAlerts(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:00:10")
	res := h.containerDied("hawaii", "plex")
	assert.False(t, res.Suppressed, "[AC-003, FR-010] a self-update window never covers a container")
	deaths := h.alertsOf(models.EventContainerDied)
	require.Len(t, deaths, 1, "the container_died alert fires")
	assert.True(t, deaths[0].Notified, "and notifies exactly as with no window")
}

func TestEX2_AC004_OfflineAfterCompletionAlerts(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:02:30")
	h.registerOn("hawaii", "2.80.0")
	h.at("10:02:35")
	h.agentCompleted("hawaii", "signed installation completion verified against running executable")
	h.tick()
	require.Equal(t, "succeeded", h.selfRecord("hawaii").Status)

	h.at("10:08:00") // still before the 10-minute bound, but after the registration
	res := h.deviceOffline("hawaii")
	assert.False(t, res.Covered, "[AC-004] coverage ended at the registration, not at the bound")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "the device_offline alert fires")
	assert.True(t, offs[0].Notified, "and notifies normally")
}

func TestEX2_AC005_FastRestartClosesOnRegistration(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:00:20") // the heartbeat gap was 20 s: never marked offline
	require.Nil(t, h.registerOn("hawaii", "2.80.0"), "the device was live: no online event")
	assert.Nil(t, h.activity("hawaii"), "[AC-005] the window closes at the registration")
	h.at("10:00:25")
	h.agentCompleted("hawaii", "signed installation completion verified against running executable")
	h.tick()

	assert.Empty(t, h.eventsOf(models.EventDeviceOffline, ""), "no device_offline event")
	assert.Empty(t, suppressedOffline(h), "and nothing was suppressed")
	assert.Equal(t, "succeeded", h.command(rec.ID).Status, "[AC-005] the outcome is succeeded")
	assert.NotNil(t, h.command(rec.ID).WindowClosedAt)
	requireNoAlerts(t, h, "no notification")
}

func TestEX2_AC006_FleetUpdateShowsCommandLabel(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("hawaii", models.ActionAgentUpdate)
	require.Equal(t, "dispatched", c.Status)
	h.at("10:00:02")
	link := h.selfStart("hawaii")
	assert.Equal(t, c.ID, link.CommandID, "[AC-006] the start is linked to the command")
	assert.Empty(t, h.selfRecords("hawaii"), "no record")
	a := h.activity("hawaii")
	require.NotNil(t, a)
	assert.Equal(t, "by ryan", a.ActorPhrase, "[AC-006] the EXPECT-001 command label")
	assert.Empty(t, a.AgentToVersion, "a command window carries no target version")
	assert.Equal(t, c.ID, a.CommandID)

	// After the command succeeded, up to 10 minutes later.
	h.at("10:01:00")
	h.succeed(c)
	h.at("10:11:00") // exactly 10 minutes after the result
	assert.Equal(t, c.ID, h.selfStart("hawaii").CommandID, "[AC-006] 10 minutes after the result: still the command's")
	h.at("10:11:01")
	link = h.selfStart("hawaii")
	assert.NotEqual(t, c.ID, link.CommandID, "[AC-007] 10m01s after the result: a record")
	assert.Len(t, h.selfRecords("hawaii"), 1)
}

func TestEX2_AC008_NotBackEscalatesOnceNamingVersions(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)

	h.at("10:09:59")
	assert.Empty(t, h.escalations(), "nothing before the bound")
	assert.Equal(t, "running", h.command(rec.ID).Status)
	h.at("10:10:00")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-008] exactly one escalation, within 60 s of the bound")
	assert.Equal(t, fmt.Sprintf("hawaii still offline 10m after %s %s (record %s)", selfLabel, selfBy, models.ShortCommandID(rec.ID)),
		esc[0].Message, "[FR-020] the exact text")
	assert.Equal(t, rec.ID, esc[0].CommandID)
	assert.Len(t, h.esc.Pushes(), 1, "and it notifies, through the device_offline rule")
	got := h.command(rec.ID)
	assert.Equal(t, "timed_out", got.Status, "the outcome is not confirmed")
	assert.Equal(t, models.SelfUpdateFailureUnconfirmedOffline, got.FailureStep)
	assert.Equal(t, "not confirmed: hawaii was offline 10m after the start", got.ResultMsg)
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdateUnconfirmed), "an offline device escalates; no extra event")
	outcome, _ := h.repo.WindowOutcome(rec.ID)
	assert.Equal(t, models.WindowOutcomeEscalated, outcome)

	h.advance(time.Hour)
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-008] never a second escalation")
	assert.Len(t, h.esc.Pushes(), 1)
}

func TestEX2_AC008_EscalationIgnoresUnrelatedCooldown(t *testing.T) {
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
	h.selfStart("hawaii")
	h.at("10:01:30")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:10:30")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-008] the escalation is raised although the rule's cooldown is running")
	assert.Len(t, h.esc.Pushes(), 1, "and it notifies (X-8)")
}

func TestEX2_AC008_NoSecondEscalationAfterRestart(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:05:00")
	h.restart(time.Time{})
	h.at("10:10:00")
	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1)
	rec := h.selfRecord("hawaii")
	assert.Equal(t, []string{models.EscalationTargetDevice}, targetsOf(h.repo.EscalationMarkers(rec.ID)), "one marker")

	h.restart(time.Time{})
	h.at("10:20:00")
	h.restart(time.Time{})
	h.at("12:00:00")
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-008] across server restarts, never twice")
	assert.Len(t, h.esc.Pushes(), 1)
}

func targetsOf(ms []models.Escalation) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Target)
	}
	return out
}

func TestEX2_AC008_EscalatedLabelAndOfflineCount(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:10:00")

	a := h.activity("hawaii")
	require.NotNil(t, a)
	assert.Equal(t, models.ActivityEscalated, a.Kind, "[FR-023] the escalated state")
	assert.Equal(t, models.PhaseEscalated, a.Phase)
	require.NotNil(t, a.Escalation)
	assert.Equal(t, models.EscalationReasonNotBack, a.Escalation.Reason)
	assert.Equal(t, "10m", a.Escalation.Bound, "the label reads 'Not back 10m after Agent update ...'")
	assert.Equal(t, "2.78.0", a.AgentFromVersion)
	assert.Equal(t, "2.80.0", a.AgentToVersion)
	assert.Equal(t, rec.ID, a.CommandID)
	assert.Equal(t, selfBy, a.ActorPhrase)
	assert.False(t, a.DeviceOnline, "an escalated device counts as offline")
	assert.False(t, a.CoversOffline, "and no longer covers anything")

	// The label survives a restart, with the versions (rebuilt from the row).
	h.restart(time.Time{})
	h.at("10:11:00")
	a = h.activity("hawaii")
	require.NotNil(t, a, "the escalated label is rebuilt after a restart")
	assert.Equal(t, models.ActivityEscalated, a.Kind)
	assert.Equal(t, "2.80.0", a.AgentToVersion, "[AD-009] the versions are rebuilt from the record's params")
	assert.Equal(t, models.EscalationReasonNotBack, a.Escalation.Reason, "timed_out maps to not_back for a self-update record")
	assert.Equal(t, "10m", a.Escalation.Bound)

	h.at("10:30:00")
	h.registerOn("hawaii", "2.80.0")
	assert.Nil(t, h.activity("hawaii"), "the failure label clears when the device is back")
}

func TestEX2_AC009_FailedWhileOnlineWarningNoPush(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:00:40")
	link := h.agentFailed("hawaii", "Agent update failed: download: connection reset")
	require.Equal(t, rec.ID, link.CommandID, "the agent's event is stored and linked")
	h.tick()

	got := h.command(rec.ID)
	assert.Equal(t, "failed", got.Status, "[AC-009] the outcome is failed")
	assert.NotNil(t, got.WindowClosedAt, "the window is closed")
	out := h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID)
	require.Len(t, out, 1, "[AC-009] exactly one warning event")
	assert.Equal(t, models.SeverityWarning, out[0].Severity)
	assert.Equal(t, models.CommandAttrOutcome, out[0].CommandAttr)
	assert.Equal(t, "Agent update 2.78.0 → 2.80.0 by agent auto-update failed on hawaii: Agent update failed: download: connection reset", out[0].Message)
	requireNoAlerts(t, h, "[AC-009] no notification")
	var broadcast bool
	for _, e := range h.bc.events {
		broadcast = broadcast || (e.Type == models.EventAgentSelfUpdateFailed && e.CommandID == rec.ID)
	}
	assert.True(t, broadcast, "the event reaches the dashboards")

	h.at("10:02:00")
	res := h.deviceOffline("hawaii")
	assert.False(t, res.Covered, "[AC-009] when hawaii goes offline later, nothing is covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified, "the device_offline alert fires normally")
}

func TestEX2_AC010_FailureAfterReturnSetsFailed(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:02:00")
	h.registerOn("hawaii", "2.80.0")
	require.Equal(t, "verifying", h.command(rec.ID).Status, "closed, outcome pending")
	h.at("10:02:05")
	h.agentFailed("hawaii", "installed digest does not match the running executable; completion remains unconfirmed")
	h.tick()

	assert.Equal(t, "failed", h.command(rec.ID).Status, "[AC-010] verifying -> failed")
	out := h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID)
	require.Len(t, out, 1, "exactly one warning event")
	assert.Contains(t, out[0].Message, "2.78.0 → 2.80.0 by agent auto-update failed on hawaii: installed digest does not match")
	requireNoAlerts(t, h, "no notification")
}

func TestEX2_AC011_ReturnOnNewVersionWithoutCompletionSucceedsAtBound(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:01:40")
	note := h.registerOn("hawaii", "v2.80.0")
	require.NotNil(t, note, "[AC-011] the online event names the update")
	assert.Nil(t, h.activity("hawaii"), "the window closes at the registration")
	assert.Equal(t, "verifying", h.command(rec.ID).Status)
	requireNoAlerts(t, h, "no notification")

	h.at("10:09:59")
	assert.Equal(t, "verifying", h.command(rec.ID).Status, "the outcome waits for the bound")
	h.at("10:10:00")
	got := h.command(rec.ID)
	assert.Equal(t, "succeeded", got.Status, "[AC-011] succeeded at the bound")
	assert.Equal(t, "confirmed: the agent registered on 2.80.0", got.ResultMsg)
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdateFailed), "no outcome event")
	requireNoAlerts(t, h, "no notification, no further event")
}

func TestEX2_AC012_ReturnOnOldVersionFails(t *testing.T) {
	h := newHarness(t, withDevices(func(h *harness) { h.devices.Devices["hawaii"].Hostname = "PiVPN" }))
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:01:50")
	h.registerOn("hawaii", "2.78.0")
	h.tick()

	got := h.command(rec.ID)
	assert.Equal(t, "failed", got.Status, "[AC-012] the outcome is failed")
	assert.Equal(t, models.SelfUpdateFailureWrongVersion, got.FailureStep)
	out := h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID)
	require.Len(t, out, 1, "exactly one warning event")
	assert.Equal(t, "PiVPN came back on agent 2.78.0 after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)", out[0].Message)
	assert.Nil(t, h.activity("hawaii"), "the window closed")
	requireNoAlerts(t, h, "no notification")

	h.at("10:02:00")
	link := h.agentCompleted("hawaii", "signed installation completion verified against running executable")
	h.tick()
	assert.Equal(t, rec.ID, link.CommandID, "a later completed event is stored and linked")
	assert.Equal(t, "failed", h.command(rec.ID).Status, "[FR-022] the outcome does not change")
	assert.Len(t, h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID), 1, "and no second event is created")

	h.at("10:05:00")
	assert.False(t, h.deviceOffline("hawaii").Covered, "[AC-012] a later offline transition alerts normally")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified)
}

func TestEX2_AC013_NotConfirmedWhileOnline(t *testing.T) {
	h := newHarness(t, withDevices(func(h *harness) { h.devices.Devices["hawaii"].AgentVersion = "2.78.0" }))
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:05:00")
	h.heartbeat("hawaii")
	h.at("10:09:50")
	h.heartbeat("hawaii")
	h.at("10:10:00")

	got := h.command(rec.ID)
	assert.Equal(t, "timed_out", got.Status, "[AC-013] not confirmed")
	assert.Equal(t, models.SelfUpdateFailureUnconfirmedOnline, got.FailureStep)
	assert.NotNil(t, got.WindowClosedAt, "the window is closed")
	out := h.eventsLinked(models.EventAgentSelfUpdateUnconfirmed, rec.ID)
	require.Len(t, out, 1, "exactly one warning event")
	assert.Equal(t, models.SeverityWarning, out[0].Severity)
	assert.Equal(t, "Agent update 2.78.0 → 2.80.0 by agent auto-update on hawaii was not confirmed within 10m (agent reports 2.78.0)", out[0].Message)
	requireNoAlerts(t, h, "no notification")
	h.advance(time.Hour)
	assert.Len(t, h.eventsLinked(models.EventAgentSelfUpdateUnconfirmed, rec.ID), 1, "still exactly one")
}

func TestEX2_AC014_RestartMidWindow(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	require.Len(t, suppressedOffline(h), 1, "the offline was stored as info")

	h.clock.Set(baseTime.Add(2 * time.Minute))
	h.restart(baseTime.Add(4 * time.Minute)) // 10:02:00, serving again at 10:02:30, grace to 10:04:00
	h.at("10:02:30")
	h.at("10:03:00")
	note := h.registerOn("hawaii", "2.80.0")
	require.NotNil(t, note, "[AC-014] the online event still names the update after the restart")
	assert.Contains(t, note.ActionLabel, "Agent update 2.78.0 → 2.80.0")
	h.at("10:03:05")
	link := h.agentCompleted("hawaii", "signed installation completion verified against running executable")
	h.tick()

	assert.Equal(t, rec.ID, link.CommandID, "[NFR-006 d] the completed event is still attributed")
	assert.Equal(t, "succeeded", h.command(rec.ID).Status)
	assert.Empty(t, h.alertsOf(models.EventDeviceOffline), "no device_offline notification exists")
	requireNoAlerts(t, h, "no notification")
}

func TestEX2_AC015_BoundDuringDowntimeEscalatesOnce(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)

	// The server is down from 10:05:00 to 10:20:00 and starts with a 2 minute grace.
	h.clock.Set(baseTime.Add(20 * time.Minute))
	h.restart(baseTime.Add(22 * time.Minute))
	h.at("10:20:30")
	assert.Empty(t, h.escalations(), "nothing is decided during the restart grace")
	h.at("10:22:00")
	h.detectorPass()
	h.tick()

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-015] exactly one escalation, right after the grace")
	assert.Contains(t, esc[0].Message, "still offline 10m after "+selfLabel)
	assert.Equal(t, "timed_out", h.command(rec.ID).Status)
	assert.Equal(t, models.SelfUpdateFailureUnconfirmedOffline, h.command(rec.ID).FailureStep)

	h.restart(time.Time{})
	h.at("10:30:00")
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-015] and no second one after another restart")
	assert.Len(t, h.esc.Pushes(), 1)
}

func TestEX2_AC015_DeadDuringDowntimeEscalatesNotUnconfirmed(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:04:40")
	h.heartbeat("hawaii") // the last sign of life; the server goes down just after

	// The DB still says online when the server comes back: the device died during the downtime.
	h.clock.Set(baseTime.Add(20 * time.Minute))
	h.restart(baseTime.Add(22 * time.Minute))
	require.Equal(t, models.DeviceStatusOnline, h.devices.Devices["hawaii"].Status)
	h.at("10:22:00")
	assert.Equal(t, "running", h.command(rec.ID).Status, "[INFO-4] the bound waits for the detector's first pass")
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdateUnconfirmed), "no 'not confirmed while online' for a dead device")

	h.detectorPass() // the one-shot at graceEnd marks hawaii offline, dated last heartbeat + 45 s
	h.tick()
	require.Len(t, suppressedOffline(h), 1, "the stale transition is inside the window: stored as info, not pushed twice")
	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-015] the device escalates")
	got := h.command(rec.ID)
	assert.Equal(t, "timed_out", got.Status)
	assert.Equal(t, models.SelfUpdateFailureUnconfirmedOffline, got.FailureStep, "[INFO-4] unconfirmed while OFFLINE, not online")
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdateUnconfirmed), "no FR-021 event")
	assert.Len(t, h.esc.Pushes(), 1, "one push")
}

func TestEX2_AC015_GateOpensAfterTheCapWithoutADetectorPass(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.clock.Set(baseTime.Add(20 * time.Minute))
	h.restart(baseTime.Add(22 * time.Minute))
	h.at("10:22:29")
	assert.Equal(t, "running", h.command(rec.ID).Status, "[Section 9] within graceEnd + 30 s the gate holds")
	h.at("10:22:30")
	assert.NotEqual(t, "running", h.command(rec.ID).Status, "at graceEnd + 30 s the gate opens even if the detector never reported")
}

func TestEX2_AC016_MalformedStartAlertsAsToday(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	for _, msg := range []string{
		"Agent update started: 2.78.0 -> 2.80.0",
		"Agent update started: dev → 2.80.0",
		"Agent update started: 2.80.0 → 2.80.0",
		"Agent update started: 2.78.0 → 2.80.0\u0000x",
		"agent update started: 2.78.0 → 2.80.0",
		"Agent update started: 2.78.0 → " + strings.Repeat("9", 300),
		strings.Repeat("A", 10<<10),
	} {
		link := h.agentEvent("hawaii", models.EventAgentUpdateStarted, models.SeverityInfo, msg)
		assert.Empty(t, link.CommandID, "[AC-016] %.30q is stored without a link", msg)
	}
	assert.Empty(t, h.selfRecords("hawaii"), "[AC-016] no record")
	assert.Len(t, h.eventsOf(models.EventAgentUpdateStarted, ""), 7, "every event is stored as today")
	h.at("10:01:00")
	res := h.deviceOffline("hawaii")
	assert.False(t, res.Covered, "nothing is covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified, "the device_offline alert fires and notifies")
}

func TestEX2_AC017_CappedStartOfflineAlertsNoDebugAbove(t *testing.T) {
	h := newHarness(t)
	base := baseTime
	for i := 0; i < models.SelfUpdateRateCap; i++ {
		h.clock.Set(base.Add(time.Duration(i) * time.Hour))
		h.settle()
		h.heartbeat("hawaii")
		require.NotEmpty(t, h.selfStart("hawaii").CommandID, "start %d creates a record", i+1)
		h.advance(90 * time.Second)
		h.agentCompleted("hawaii", "signed installation completion verified against running executable")
		h.tick()
	}
	h.clock.Set(base.Add(6*time.Hour + 30*time.Minute))
	h.settle()
	h.heartbeat("hawaii")
	link := h.selfStart("hawaii")
	assert.Empty(t, link.CommandID, "[AC-017] the 7th start is capped")
	assert.Len(t, h.selfRecords("hawaii"), 6)
	paused := h.warningEvents(models.EventAgentSelfUpdatePaused)
	require.Len(t, paused, 1, "exactly one rate-cap warning event")
	assert.Empty(t, h.esc.Pushes(), "no notification for the rate-cap event")

	h.advance(time.Minute)
	h.selfStart("hawaii") // a repeat: no second cap event
	assert.Len(t, h.warningEvents(models.EventAgentSelfUpdatePaused), 1)
	h.advance(time.Minute)
	res := h.deviceOffline("hawaii")
	assert.False(t, res.Covered, "[AC-017] offline transitions after a capped start alert normally")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified)
	for _, lvl := range []string{"WARN", "ERROR", "INFO"} {
		assert.Zero(t, h.logLevelLines(lvl), "[NFR-005] no %s log line for the repeats and the refused starts", lvl)
	}
}

func TestEX2_AC018_OtherDeviceUnaffected(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:01:00")
	res := h.deviceOffline("vermont")
	assert.False(t, res.Covered, "[AC-018] vermont's offline is not covered by hawaii's window")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.Equal(t, "vermont", offs[0].Device)
	assert.True(t, offs[0].Notified, "vermont's alert fires normally")
	assert.Nil(t, h.activity("vermont"), "and vermont shows no update label")
	assert.NotNil(t, h.activity("hawaii"))
}

func TestEX2_AC018_OtherDeviceRegisterCompleteFailLeavesRecord(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	beforeRow := h.command(rec.ID)
	beforeAct := h.activity("hawaii")

	h.at("10:02:00")
	h.registerOn("vermont", "2.80.0")
	h.registerOn("vermont", "2.70.0")
	assert.Empty(t, h.agentCompleted("vermont", "signed installation completion verified against running executable").CommandID,
		"[INFO-1] vermont's completed event links to nothing")
	assert.Empty(t, h.agentFailed("vermont", "boom").CommandID, "nor does its failed event")
	h.tick()

	assert.Equal(t, beforeRow, h.command(rec.ID), "[AC-018] hawaii's record is unchanged")
	afterAct := h.activity("hawaii")
	require.NotNil(t, afterAct)
	assert.Equal(t, beforeAct.AgentToVersion, afterAct.AgentToVersion)
	assert.Equal(t, beforeAct.Phase, afterAct.Phase, "hawaii's label is unchanged")
	assert.Empty(t, h.selfRecords("vermont"), "no record for vermont")
}

func TestEX2_AC019_FailedEventMessageSanitized(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:00:30")
	raw := "bad\x00reason ‮evil​  line\nbreak \xff\xfe" + strings.Repeat("z", 2048)
	h.agentFailed("hawaii", raw)
	h.tick()

	stored := h.eventsLinked(models.EventAgentUpdateFailed, rec.ID)
	require.Len(t, stored, 1, "the agent's event is stored as today")
	out := h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID)
	require.Len(t, out, 1)
	assert.True(t, utf8.ValidString(out[0].Message), "[AC-019] valid UTF-8")
	for _, r := range out[0].Message {
		assert.False(t, unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r),
			"no control or format rune (%U)", r)
	}
	prefix := "Agent update 2.78.0 → 2.80.0 by agent auto-update failed on hawaii: "
	require.True(t, strings.HasPrefix(out[0].Message, prefix), "got %q", out[0].Message)
	assert.LessOrEqual(t, len(out[0].Message)-len(prefix), 200, "[AC-019] the quoted reason is at most 200 bytes")
}

func TestEX2_AC020_StoreFailureFailsOpen(t *testing.T) {
	h := newHarness(t)
	h.cmds.FailOn("CreateSelfUpdate", errors.New("the record store is down"))
	h.at("10:00:00")
	link := h.selfStart("hawaii")
	assert.Empty(t, link.CommandID, "[AC-020] no link")
	assert.Len(t, h.eventsOf(models.EventAgentUpdateStarted, ""), 1, "the event is stored as today")
	assert.Empty(t, h.selfRecords("hawaii"))
	h.at("10:01:00")
	res := h.deviceOffline("hawaii")
	assert.False(t, res.Covered, "nothing is covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified, "[AC-020] the device_offline alert fires and notifies")
}

func TestEX2_AC022_BoundedEventsLogsMemory(t *testing.T) {
	h := newHarness(t)
	// AC-001 on hawaii, AC-008 on vermont, AC-009 on picups, interleaved in time.
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.selfStart("vermont")
	h.selfStart("picups")
	h.at("10:00:40")
	h.agentFailed("picups", "Agent update failed: download: connection reset")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	require.True(t, h.deviceOffline("vermont").Covered)
	h.at("10:02:30")
	h.registerOn("hawaii", "2.80.0")
	h.at("10:02:35")
	h.agentCompleted("hawaii", "signed installation completion verified against running executable")
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
	assert.Equal(t, 4, count("hawaii"), "[AC-022] AC-001: the started and completed events, one info offline, one online")
	assert.Equal(t, 3, count("vermont"), "AC-008: the started event, one info offline, one escalation")
	assert.Equal(t, 3, count("picups"), "AC-009: the started and failed events, one warning")
	assert.Len(t, h.escalations(), 1, "one escalation in all")
	assert.Len(t, h.selfRecords("hawaii"), 1)
	for _, lvl := range []string{"WARN", "ERROR"} {
		assert.Zero(t, h.logLevelLines(lvl), "[NFR-005] no %s line", lvl)
	}
	assert.Equal(t, 1, h.logLevelLines("INFO"), "[NFR-005] one info line: the AC-008 escalation")

	// Within 5 minutes of the outcomes, nothing is held for any record.
	h.advance(6 * time.Minute)
	st := h.tr.Stats()
	assert.Zero(t, st.Windows, "[NFR-004] no window is cached")
	assert.Zero(t, st.ClosedRecent, "and the closed ids are released")
}

func TestEX2_AC023_ActivityCarriesVersionsAndActor(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("vermont")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("vermont").Covered)
	resp := h.tr.Activities()
	require.Len(t, resp.Activities, 1)
	raw, err := json.Marshal(resp.Activities[0])
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "agent", m["kind"])
	assert.Equal(t, "in_progress", m["phase"])
	assert.Equal(t, "agent_update", m["action"])
	assert.Equal(t, "2.78.0", m["agent_from_version"])
	assert.Equal(t, "2.80.0", m["agent_to_version"], "[AC-023] the new field")
	assert.Equal(t, selfBy, m["actor_phrase"])
	assert.Equal(t, true, m["covers_offline"])
	assert.Equal(t, false, m["device_online"], "the device is offline inside the window")
	assert.Nil(t, m["back_by"], "[AD-005] back_by is null")
	assert.Nil(t, m["escalation"])
}

func TestEX2_AC025_CommandAndRecordOneLabel(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:00:30")
	c := h.dispatch("hawaii", models.ActionAgentUpdate) // no key on the record: no conflict (FR-028)
	h.at("10:00:31")
	a := h.activity("hawaii")
	require.NotNil(t, a, "one label")
	assert.Equal(t, c.ID, a.CommandID, "[AC-025] the latest dispatch takes the label")
	assert.Equal(t, "by ryan", a.ActorPhrase)
	assert.Empty(t, a.AgentToVersion)

	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	off := suppressedOffline(h)
	require.Len(t, off, 1, "the offline is suppressed")
	assert.Equal(t, c.ID, off[0].CommandID, "[X-12] linked to the latest dispatch")
	assert.Contains(t, off[0].Message, "during Agent update by ryan")
	requireNoAlerts(t, h, "no notification")
}

func TestEX2_AC026_OlderAgentNoCompletionSucceeds(t *testing.T) {
	h := newHarness(t, withDevices(func(h *harness) { h.devices.Devices["vermont"].AgentVersion = "2.40.0" }))
	h.at("10:00:00")
	h.selfStartVersions("vermont", "2.40.0", "2.80.0")
	rec := h.selfRecord("vermont")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("vermont").Covered, "[AC-026] the offline is suppressed")
	h.at("10:02:00")
	h.registerOn("vermont", "2.80.0")
	h.at("10:10:00")
	assert.Equal(t, "succeeded", h.command(rec.ID).Status, "[AC-026] the record ends succeeded at the bound without any completed event")
	requireNoAlerts(t, h, "no notification")
}

func TestEX2_AC026_CompletionBeforeRestartStillCovered(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:00:20")
	h.agentCompleted("hawaii", "Agent updated to 2.80.0") // the older path completes BEFORE restart()
	assert.Equal(t, "succeeded", h.command(rec.ID).Status)
	a := h.activity("hawaii")
	require.NotNil(t, a, "[X-1] coverage continues after an early completion")
	assert.Equal(t, models.PhaseInProgress, a.Phase)

	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered, "[AC-026, X-17] the restart that follows is still covered")
	h.at("10:01:40")
	note := h.registerOn("hawaii", "2.80.0")
	require.NotNil(t, note, "the online event names the update")
	assert.Contains(t, note.ActionLabel, "Agent update 2.78.0")
	h.tick()
	assert.Nil(t, h.activity("hawaii"), "the return ends coverage")
	assert.NotNil(t, h.command(rec.ID).WindowClosedAt, "and the window closes at the return")
	requireNoAlerts(t, h, "no notification")

	h.at("10:05:00")
	assert.False(t, h.deviceOffline("hawaii").Covered, "an offline after the return alerts")
}

func TestEX2_AC026_OtherWordingAsToday(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	link := h.agentEvent("hawaii", models.EventAgentUpdateStarted, models.SeverityInfo, "Updating the agent to 2.80.0 now")
	assert.Empty(t, link.CommandID)
	h.at("10:01:00")
	assert.False(t, h.deviceOffline("hawaii").Covered, "[AC-026] a start in any other wording behaves exactly as today")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1)
}

func TestEX2_AC028_CommandAgentUpdateUnchanged(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.step(c, "download")
	h.at("10:00:30")
	a := h.activity("vermont")
	require.NotNil(t, a)
	assert.Equal(t, models.ActivityAgent, a.Kind)
	assert.Equal(t, models.PhaseInProgress, a.Phase)
	assert.Equal(t, "by ryan", a.ActorPhrase)
	assert.Equal(t, "2.77.0", a.AgentFromVersion, "[AC-028] the command label still reads the device's version")
	assert.Empty(t, a.AgentToVersion)

	h.at("10:01:00")
	h.succeed(c)
	a = h.activity("vermont")
	require.NotNil(t, a)
	assert.Equal(t, models.PhaseReturn, a.Phase, "the EXPECT-001 return phase")
	require.NotNil(t, a.BackBy)
	assert.True(t, a.BackBy.Equal(baseTime.Add(11*time.Minute)), "back by = result + 10 minutes")
	h.at("10:01:30")
	require.True(t, h.deviceOffline("vermont").Covered)
	off := suppressedOffline(h)
	require.Len(t, off, 1)
	assert.Equal(t, fmt.Sprintf("Device vermont went offline during Agent update by ryan (expected, back by %s)", h.local(baseTime.Add(11*time.Minute))), off[0].Message,
		"[AC-028] the EXPECT-001 text is unchanged")
}

func TestEX2_X1_CompletionBeforeReturnCoversRestart(t *testing.T) {
	// The full path is TestEX2_AC026_CompletionBeforeRestartStillCovered; this one
	// pins the coverage bound: the completion does not move the end of coverage.
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:00:20")
	h.agentCompleted("hawaii", "Agent updated to 2.80.0")
	h.at("10:09:59")
	require.True(t, h.deviceOffline("hawaii", baseTime.Add(9*time.Minute+58*time.Second)).Covered,
		"[X-1] an offline dated just before the bound is still covered")
}

func TestEX2_X1_NoReturnByBoundEscalates(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:00:20")
	h.agentCompleted("hawaii", "Agent updated to 2.80.0")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:10:00")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[X-1] a completion with no return by the bound escalates")
	assert.Contains(t, esc[0].Message, "still offline 10m after "+selfLabel)
	assert.Equal(t, "succeeded", h.command(rec.ID).Status, "the agent's completion is not overridden")
	assert.Len(t, h.esc.Pushes(), 1)
}

func TestEX2_X3_HeartbeatVersionConfirmsAtBound(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:05:00")
	h.heartbeatOn("hawaii", "2.80.0") // the registration was lost; the heartbeat carries the version
	h.at("10:10:00")
	got := h.command(rec.ID)
	assert.Equal(t, "succeeded", got.Status, "[X-3] an online device whose stored version matches <to> is confirmed")
	assert.Equal(t, "confirmed: the agent reports 2.80.0", got.ResultMsg)
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdateUnconfirmed), "no outcome event")
	requireNoAlerts(t, h, "no notification")
}

func TestEX2_X7_FailedWithCoveredOfflineEscalatesOnce(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:02:00")
	h.agentFailed("hawaii", "signed agent update error: helper stopped")
	h.tick()

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[X-7] a failure that ends the window with the covered offline still ongoing escalates")
	assert.Equal(t, fmt.Sprintf("hawaii is offline: %s %s failed (record %s)", selfLabel, selfBy, models.ShortCommandID(rec.ID)), esc[0].Message)
	out := h.eventsLinked(models.EventAgentSelfUpdateFailed, rec.ID)
	require.Len(t, out, 1, "and the outcome event is written too")
	assert.Len(t, h.esc.Pushes(), 1, "one push in all: the outcome event never notifies")
	a := h.activity("hawaii")
	require.NotNil(t, a)
	assert.Equal(t, models.EscalationReasonFailed, a.Escalation.Reason, "the label is the failed variant")
	h.advance(time.Hour)
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "exactly once")
}

func TestEX2_NoteOnlineOnlyForCoveredOffline(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:02:00")
	note := h.registerOn("hawaii", "2.80.0")
	require.NotNil(t, note, "the covered offline names the update")

	// A later offline, after the registration, is not covered; its online note is plain.
	h.at("10:03:00")
	assert.False(t, h.deviceOffline("hawaii").Covered)
	h.at("10:07:30")
	note = h.registerOn("hawaii", "2.80.0")
	assert.Nil(t, note, "[AD-008] an offline that began outside the coverage gets no update note")
	var plain bool
	for _, e := range h.eventsOf(models.EventDeviceOnline, "") {
		plain = plain || e.Message == "Device hawaii came online"
	}
	assert.True(t, plain, "the online event is the plain one")
}

func TestEX2_OutcomeEventsNeverDispatchedNorCeilinged(t *testing.T) {
	h := newHarness(t)
	// Five failed records in an hour: five outcome events. If they used the push
	// ceiling (5 per device per hour), the escalation after them would be held.
	for i := 0; i < 5; i++ {
		h.clock.Set(baseTime.Add(time.Duration(i) * 10 * time.Minute))
		h.settle()
		h.heartbeat("hawaii")
		require.NotEmpty(t, h.selfStart("hawaii").CommandID)
		h.advance(30 * time.Second)
		h.agentFailed("hawaii", fmt.Sprintf("Agent update failed: attempt %d", i))
		h.tick()
	}
	require.Len(t, h.warningEvents(models.EventAgentSelfUpdateFailed), 5, "five outcome events")
	assert.Empty(t, h.disp.Alerts, "[C-6] an outcome event is never dispatched to a channel")
	assert.Empty(t, h.esc.Pushes(), "and never queued as a push")

	// The sixth record escalates: it must still push.
	h.clock.Set(baseTime.Add(50 * time.Minute))
	h.settle()
	h.heartbeat("hawaii")
	require.NotEmpty(t, h.selfStart("hawaii").CommandID)
	h.advance(time.Minute)
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.clock.Set(baseTime.Add(60*time.Minute + 5*time.Second))
	h.settle()
	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1)
	assert.Len(t, h.esc.Pushes(), 1, "[C-6] the escalation pushes: the outcome events consumed none of the 5-per-hour ceiling")
	assert.Len(t, h.disp.Alerts, 1, "the only dispatch is the escalation")
	assert.Empty(t, h.warningEvents(models.EventExpectNotificationsHeld), "nothing was held")
}

func TestEX2_ResolverUnsetFailsSafe(t *testing.T) {
	h := newHarness(t, withoutSelfUpdateResolver())
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)

	h.at("10:10:00")
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[C-4] without a resolver the escalation still fires: it runs before resolution")
	assert.Len(t, h.esc.Pushes(), 1)
	assert.Equal(t, "running", h.command(rec.ID).Status, "the record cannot be resolved")
	h.at("10:10:30")
	assert.False(t, h.deviceOffline("hawaii", baseTime.Add(10*time.Minute+30*time.Second)).Covered,
		"coverage ended at the bound: a later offline would alert")

	// Retried (and evicted and reloaded) for the next hour; nothing visible yet.
	for _, hms := range []string{"10:20:00", "10:40:00", "11:09:00"} {
		h.at(hms)
	}
	assert.Empty(t, h.warningEvents(models.EventAgentSelfUpdatePaused), "no stuck event before bound + 1h")

	dispatchedBefore := len(h.disp.Alerts)
	h.at("11:10:00")
	paused := h.eventsLinked(models.EventAgentSelfUpdatePaused, rec.ID)
	require.Len(t, paused, 1, "[C-4] exactly one agent_self_update_paused event at bound + 1h")
	assert.Equal(t, models.SeverityWarning, paused[0].Severity)
	assert.Equal(t, fmt.Sprintf("Agent self-update tracking paused on hawaii: record %s (%s) could not be completed 1h after its bound; the next agent update replaces it",
		models.ShortCommandID(rec.ID), selfLabel), paused[0].Message)
	assert.Len(t, h.esc.Pushes(), 1, "[C-4] no push for it")
	assert.Len(t, h.disp.Alerts, dispatchedBefore, "[C-4] nothing was dispatched for it")
	paused30 := strings.Count(h.logs.String(), "could not be completed one hour after its bound")
	assert.Equal(t, 1, paused30, "[C-4] and one Warn")

	h.at("11:10:30")
	h.at("11:11:30")
	assert.Len(t, h.eventsLinked(models.EventAgentSelfUpdatePaused, rec.ID), 1, "still exactly one")
	h.restart(time.Time{})
	h.at("11:13:00")
	assert.Len(t, h.eventsLinked(models.EventAgentSelfUpdatePaused, rec.ID), 1, "[C-4] also across a restart (the marker is durable)")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "could not be completed one hour after its bound"), "and no second Warn")

	// The next valid start replaces the stuck row.
	h.at("11:15:00")
	link := h.selfStart("hawaii")
	assert.NotEqual(t, rec.ID, link.CommandID, "[C-4] the next start is not blocked by the stuck row")
	assert.Equal(t, "timed_out", h.command(rec.ID).Status)
	assert.Equal(t, models.SelfUpdateFailureUnresolved, h.command(rec.ID).FailureStep)
}
