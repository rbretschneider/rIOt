package expect_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEX_AC025_AgentRestartOfflineInfo(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c) // 10:00:00

	h.at("10:00:50")
	off := h.deviceOffline("vermont")
	h.at("10:02:00")
	h.register("vermont")

	assert.True(t, off.Covered, "[AC-025] the agent restart offline is covered")
	evts := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Equal(t, c.ID, evts[0].CommandID, "[AC-025] linked to the agent_update")
	assert.Contains(t, evts[0].Message, "Agent update by ryan")
	online := h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected)
	require.Len(t, online, 1)
	assert.Equal(t, "Device vermont came online after Agent update by ryan (offline 1m10s)", online[0].Message)
	requireNoAlerts(t, h, "[AC-025] nothing is notified")

	h.at("10:03:01")
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the window closes 1 min after the agent is back")
	assert.Empty(t, h.escalations())
}

func TestEX_AC026_AgentNotBackEscalates(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c)
	h.at("10:01:00")
	require.True(t, h.deviceOffline("vermont").Covered)

	h.at("10:09:59")
	assert.Empty(t, h.escalations(), "[AC-026] nothing before the 10 min bound")
	h.at("10:10:30")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-026] one escalation")
	assert.Equal(t, fmt.Sprintf("vermont still offline 10m after Agent update by ryan (command %s)", models.ShortCommandID(c.ID)), esc[0].Message,
		"[AC-026] names the action, the actor and the command id")
	a := h.activity("vermont")
	require.NotNil(t, a)
	assert.Equal(t, models.ActivityEscalated, a.Kind, "[AC-026] the fleet shows the failure state")
	require.NotNil(t, a.Escalation)
	assert.Equal(t, models.EscalationReasonNotBack, a.Escalation.Reason)
}

func TestEX_AC027_AgentUpdateDoesNotHideDeaths(t *testing.T) {
	h := newHarness(t)
	h.dispatch("hawaii", models.ActionAgentUpdate)

	r := h.containerDied("hawaii", "plex")

	assert.False(t, r.Suppressed, "[AC-027] an agent_update window covers offline only")
	require.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-027] the container_died alert fires normally")
	assert.True(t, h.alerts[0].Notified)
}

// shutdownPoweredOff runs a shutdown by ryan that succeeds at 10:02 and the
// device going offline at 10:03.
func shutdownPoweredOff(t *testing.T, h *harness) models.Command {
	t.Helper()
	c := h.dispatch("picups", models.ActionShutdown)
	h.at("10:02:00")
	h.succeed(c)
	h.at("10:03:00")
	off := h.deviceOffline("picups")
	require.True(t, off.Covered, "the offline after a successful shutdown is covered")
	require.Equal(t, models.OfflineKindPoweredOff, off.Kind, "[FR-028] it enters the powered-off state")
	return c
}

func TestEX_AC028_ShutdownPoweredOffNoAlertsThreeDays(t *testing.T) {
	h := newHarness(t)
	c := shutdownPoweredOff(t, h)

	evts := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Equal(t, fmt.Sprintf("Device PiCUPS powered off by ryan at %s", h.local(time.Date(2026, 10, 26, 10, 2, 0, 0, time.UTC))), evts[0].Message,
		"[AC-028] the info event names the actor and the shutdown time")
	assert.Equal(t, c.ID, evts[0].CommandID)
	row, ok := h.repo.PoweredOffRow("picups")
	require.True(t, ok, "[FR-032] one durable row")
	assert.Equal(t, c.ID, row.CommandID)
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the shutdown window is finished with outcome powered_off")
	outcome, _ := h.repo.WindowOutcome(c.ID)
	assert.Equal(t, models.WindowOutcomePoweredOff, outcome)

	h.advance(72 * time.Hour)

	requireNoAlerts(t, h, "[AC-028] no device_offline alert after 3 days")
	assert.Empty(t, h.escalations(), "[AC-028]/[FR-029] and no escalation")
	a := h.activity("picups")
	require.NotNil(t, a, "[AC-028] the fleet shows the powered-off state")
	assert.Equal(t, models.ActivityPoweredOff, a.Kind)
	assert.Equal(t, "by ryan", a.ActorPhrase)
	require.NotNil(t, a.PoweredOffAt)
	assert.True(t, a.PoweredOffAt.Equal(time.Date(2026, 10, 26, 10, 2, 0, 0, time.UTC)), "[AC-028] powered off at the shutdown time")
	assert.False(t, a.DeviceOnline)
}

func TestEX_AC029_PoweredOffEndsOnOnline(t *testing.T) {
	h := newHarness(t)
	shutdownPoweredOff(t, h)
	h.advance(72 * time.Hour)

	note := h.register("picups")

	require.NotNil(t, note, "[AC-029] the online transition is explained")
	assert.True(t, note.PoweredOff)
	online := h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected)
	require.Len(t, online, 1)
	assert.Equal(t, "Device PiCUPS came online after Shutdown by ryan (powered off 72h)", online[0].Message, "[AC-029]/[FR-030]")
	_, ok := h.repo.PoweredOffRow("picups")
	assert.False(t, ok, "[AC-029] the powered-off row is gone")
	assert.Nil(t, h.activity("picups"), "[AC-029] the label clears")

	h.advance(10 * time.Minute)
	off := h.deviceOffline("picups")
	assert.False(t, off.Covered, "[AC-029] a later offline transition is not covered")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-029] it alerts normally")
	assert.True(t, h.alerts[0].Notified)
}

func TestEX_AC030_FailedShutdownNoPoweredOff(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("picups", models.ActionShutdown)
	h.fail(c) // poweroff refused, the device stays online

	h.at("10:03:00")
	off := h.deviceOffline("picups")

	assert.False(t, off.Covered, "[AC-030] a failed shutdown does not cover a later offline")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-030] the device_offline alert fires normally")
	_, ok := h.repo.PoweredOffRow("picups")
	assert.False(t, ok, "[FR-031] no powered-off state")
	assert.Equal(t, 0, len(h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)))
}
