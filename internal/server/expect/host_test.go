package expect_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEX_AC011_RebootAfterDpkgWaitNoAlert(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot) // 10:00:00
	h.at("10:00:30")
	h.step(c, models.StepWaitDpkgLock)
	h.at("10:09:40")
	h.succeed(c)

	h.at("10:10:20")
	off := h.deviceOffline("vermont")
	h.at("10:12:00")
	note := h.register("vermont")

	assert.True(t, off.Covered, "[AC-011] the offline transition after a 10-minute dpkg wait is covered")
	evts := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, evts, 1, "[AC-011] one suppressed offline event")
	assert.Equal(t, models.SeverityInfo, evts[0].Severity)
	assert.Equal(t, c.ID, evts[0].CommandID, "[AC-011] linked to the reboot")
	assert.Contains(t, evts[0].Message, "Reboot", "[AC-011] names the action")
	assert.Contains(t, evts[0].Message, "by ryan", "[AC-011] names the actor")
	require.NotNil(t, note, "[AC-011] the online transition is explained")
	online := h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected)
	require.Len(t, online, 1)
	assert.Equal(t, "Device vermont came online after Reboot by ryan (offline 1m40s)", online[0].Message, "[AC-011]/[FR-044]")
	requireNoAlerts(t, h, "[AC-011] nothing is notified")

	h.at("10:15:01")
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the window closes 3 min after the device returned")
	assert.Empty(t, h.escalations(), "a device that came back raises no escalation")
}

func TestEX_AC012_TwelveDeathsDuringReboot(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionReboot)

	for i := 0; i < 12; i++ {
		r := h.containerDied("hawaii", fmt.Sprintf("svc-%02d", i))
		require.True(t, r.Suppressed, "[AC-012] container %d is covered by the host window", i)
	}

	evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	assert.Len(t, evts, 12, "[AC-012] 12 info events")
	for _, e := range evts {
		assert.Equal(t, c.ID, e.CommandID, "[AC-012] each linked to the reboot")
	}
	requireNoAlerts(t, h, "[AC-012] no notification")
}

func TestEX_AC013_PatchWithRebootNoAlert(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionOSUpdate, withOrigin(models.OriginAutoPatch, 0), byActor(""))
	h.at("10:28:00")
	h.succeed(c)

	h.at("10:29:00")
	off := h.deviceOffline("vermont")
	h.at("10:31:00")
	h.register("vermont")

	assert.True(t, off.Covered, "[AC-013] the reboot after a patch is covered")
	evts := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Equal(t, c.ID, evts[0].CommandID, "[AC-013] linked to the os_update")
	assert.Contains(t, evts[0].Message, "by the auto-patch schedule", "[AC-013]/[FR-018]")
	requireNoAlerts(t, h, "[AC-013] nothing is notified")
}

func TestEX_AC014_DockerRestartDuringPatchNoEscalation(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionOSUpdate)
	h.at("10:05:00")
	names := []string{"plex", "sonarr", "radarr"}
	for _, n := range names {
		require.True(t, h.containerDied("hawaii", n).Suppressed, "[AC-014] %s is covered by the os_update", n)
	}
	h.at("10:06:00")
	for _, n := range names {
		h.containerStarted("hawaii", n, h.clock.Now())
	}
	h.at("10:30:00")
	h.succeed(c)

	h.at("10:33:01")

	assert.Len(t, h.eventsOf(models.EventContainerDied, models.CommandAttrExpected), 3, "[AC-014] every death is an info event linked to the os_update")
	assert.Empty(t, h.escalations(), "[AC-014] every container came back, so no escalation fires")
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the window closed")
	requireNoAlerts(t, h, "[AC-014] no notification")
}

func TestEX_AC015_NotBackByBoundEscalatesOnce(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	h.succeed(c) // finished 10:00:00
	h.at("10:01:00")
	h.deviceOffline("vermont")

	h.at("10:14:59")
	assert.Empty(t, h.escalations(), "[AC-015] nothing before the return bound")
	h.at("10:15:30")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-015] exactly one escalation is raised by 10:16")
	assert.Equal(t, fmt.Sprintf("vermont still offline 15m after Reboot by ryan (command %s)", models.ShortCommandID(c.ID)), esc[0].Message,
		"[AC-015] says how long and names the action, the actor and the command id")
	assert.Equal(t, models.SeverityCrit, esc[0].Severity, "[AC-015] through the device_offline rule")
	assert.Equal(t, c.ID, esc[0].CommandID, "[FR-026] linked to the command")
	require.Len(t, h.esc.Pushes(), 1, "[AC-015] the rule notifies once")

	h.advance(30 * time.Minute)
	h.advance(30 * time.Minute)
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-015] no second escalation for the command")
	assert.Len(t, h.esc.Pushes(), 1)
}

func TestEX_AC016_TimedOutRebootOfflineEscalates(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot, byActor("ryan"))
	h.at("10:05:00")
	require.True(t, h.deviceOffline("vermont").Covered, "the offline during the running reboot is covered")

	h.clock.Set(time.Date(2026, 10, 26, 10, 21, 0, 0, time.UTC))
	h.sweep()
	require.Equal(t, "timed_out", h.command(c.ID).Status, "the sweeper timed the reboot out")
	h.tick()

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-016] one escalation within one tick of the timeout")
	assert.Equal(t, fmt.Sprintf("vermont is offline: Reboot by ryan timed out (command %s)", models.ShortCommandID(c.ID)), esc[0].Message,
		"[AC-016] names the reboot, the actor and the outcome")
	h.advance(10 * time.Minute)
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-016] still one after the window ends")
}

func TestEX_AC017_OfflineAfterFailedRebootAlerts(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	h.fail(c) // 10:00:00, device still online

	h.at("10:01:00")
	off := h.deviceOffline("vermont")

	assert.False(t, off.Covered, "[AC-017] an offline after a failed reboot is not covered")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-017] the device_offline alert fires normally")
	assert.True(t, h.alerts[0].Notified)
	h.advance(30 * time.Minute)
	assert.Empty(t, h.escalations(), "[AC-017] the alert is not followed by an escalation")
}

func TestEX_AC018_ContainerNotBackAfterRebootEscalates(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionReboot)
	h.at("10:02:00")
	h.containerDied("hawaii", "plex")
	h.containerDied("hawaii", "sonarr")
	h.at("10:03:00")
	h.succeed(c)
	h.at("10:03:30")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:06:00")
	h.register("hawaii")
	h.at("10:06:30")
	h.containerStarted("hawaii", "plex", h.clock.Now())

	h.at("10:09:01")

	esc := h.escalationsOf(models.EventContainerDied)
	require.Len(t, esc, 1, "[AC-018] one escalation for the container that did not come back")
	assert.Equal(t, "sonarr", esc[0].ContainerName, "[AC-018] it is sonarr, not plex")
	assert.Equal(t, fmt.Sprintf("Container sonarr still not running 3m after Reboot by ryan (succeeded, command %s)", models.ShortCommandID(c.ID)),
		esc[0].Message, "[AC-018] it names the reboot and the actor")
	assert.Equal(t, c.ID, esc[0].CommandID)
}

func TestEX_AC019_StackRedeployContainerDownEscalates(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerStackRedeploy, withKeys(mediaKey))
	h.succeed(c) // 10:00:00
	h.at("10:00:10")
	require.True(t, h.containerDied("hawaii", "sonarr", "docker:container:sonarr", mediaKey).Suppressed)

	h.at("10:02:01")

	esc := h.escalationsOf(models.EventContainerDied)
	require.Len(t, esc, 1, "[AC-019] one container_died escalation when the window closes")
	assert.Equal(t, fmt.Sprintf("Container sonarr still not running 2m after Docker stack redeploy by ryan (succeeded, command %s)", models.ShortCommandID(c.ID)),
		esc[0].Message, "[AC-019] names the action, the actor, the outcome and the command id")
	assert.Equal(t, models.CommandAttrEscalation, esc[0].CommandAttr, "[FR-026]")
	assert.Len(t, h.esc.Pushes(), 1, "the container_died rule notifies")
}

func TestEX_AC020_IntentionalDownNoEscalation(t *testing.T) {
	h := newHarness(t)
	stop := h.dispatch("hawaii", models.ActionDockerStop, withKeys(plexKey))
	down := h.dispatch("hawaii", models.ActionDockerStackDown, withKeys(mediaKey))
	require.True(t, h.containerDied("hawaii", "plex").Suppressed)
	require.True(t, h.containerDied("hawaii", "sonarr", "docker:container:sonarr", mediaKey).Suppressed)
	h.succeed(stop)
	h.succeed(down)

	h.at("10:02:01")

	assert.Empty(t, h.escalations(), "[AC-020] a container that is meant to be down never escalates")
	assert.NotNil(t, h.command(stop.ID).WindowClosedAt, "the docker_stop window closed")
	assert.NotNil(t, h.command(down.ID).WindowClosedAt, "the docker_stack_down window closed")
	requireNoAlerts(t, h, "[AC-020] no notification")
}

func TestEX_AC022_EscalationWithoutRuleIsWarning(t *testing.T) {
	h := newHarness(t)
	kept := h.rules.Rules[:0]
	for _, r := range h.rules.Rules {
		if r.Metric != "device_offline" {
			kept = append(kept, r)
		}
	}
	h.rules.Rules = kept
	c := h.dispatch("vermont", models.ActionReboot)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")

	h.at("10:15:30")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[AC-022] the escalation is stored")
	assert.Equal(t, models.SeverityWarning, esc[0].Severity, "[AC-022] as a warning when no rule matches")
	assert.Equal(t, c.ID, esc[0].CommandID, "[FR-026] linked to the command")
	assert.Empty(t, h.esc.Pushes(), "[AC-022] and nothing is notified")
}
