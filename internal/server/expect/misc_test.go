package expect_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEX_FR007_ExactTargetBeatsHostWindow(t *testing.T) {
	h := newHarness(t)
	reboot := h.dispatch("hawaii", models.ActionReboot)
	update := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))

	h.containerDied("hawaii", "plex")
	h.containerDied("hawaii", "sonarr")

	evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	require.Len(t, evts, 2)
	byName := map[string]string{}
	for _, e := range evts {
		byName[e.ContainerName] = e.CommandID
	}
	assert.Equal(t, update.ID, byName["plex"], "[FR-007] a window that holds the event's exact target is preferred over a host window")
	assert.Equal(t, reboot.ID, byName["sonarr"], "[FR-007] an untargeted container is covered by the host window")
}

func TestEX_FR007_LatestDispatchWinsAmongEquals(t *testing.T) {
	h := newHarness(t)
	h.dispatch("hawaii", models.ActionDockerRestart, withKeys(plexKey))
	h.advance(time.Minute)
	second := h.dispatch("hawaii", models.ActionDockerStackRedeploy, withKeys(mediaKey))

	h.containerDied("hawaii", "plex", plexKey, mediaKey)

	evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Equal(t, second.ID, evts[0].CommandID, "[FR-007] ties go to the latest dispatch")
}

func TestEX_SEC003_TickBudgetCarriesOver(t *testing.T) {
	h := newHarness(t)
	var cmds []models.Command
	for i := 0; i < 3; i++ {
		c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(fmt.Sprintf("docker:container:svc-%d", i)))
		h.succeed(c)
		cmds = append(cmds, c)
	}
	closed := func() int {
		n := 0
		for _, c := range cmds {
			if h.command(c.ID).WindowClosedAt != nil {
				n++
			}
		}
		return n
	}
	// Every close takes 4 s of (fake) time: past the 3 s tick budget.
	h.repo.Before = func(method string) {
		if method == "CloseWindow" {
			h.clock.Advance(4 * time.Second)
		}
	}

	h.at("10:02:01")
	assert.Equal(t, 1, closed(), "[SEC-003] no new database call starts after 3 s of tick time: one window finished")
	h.tick()
	assert.Equal(t, 2, closed(), "[SEC-003] the rest carries over to the next tick")
	h.tick()
	assert.Equal(t, 3, closed(), "[SEC-003] and is finished in the one after")
	assert.NoError(t, h.tr.Ready(), "[SEC-003] every tick completed")
}

func TestEX_Escalation_ExcludedContainerIsSkipped(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerStackRedeploy, withKeys(mediaKey))
	h.containerDied("hawaii", "plex", mediaKey)
	h.containerDied("hawaii", "sonarr", mediaKey)
	h.succeed(c)
	h.rules.Rules[0].ExcludeContainers = "plex" // excluded after the deaths were recorded

	h.at("10:02:01")

	esc := h.escalationsOf(models.EventContainerDied)
	require.Len(t, esc, 1, "[FR-015] a container excluded from the rule is not escalated")
	assert.Equal(t, "sonarr", esc[0].ContainerName)
	assert.Len(t, h.repo.EscalationMarkers(c.ID), 1, "an excluded container takes no marker and no cap slot")
}

func TestEX_Observe_HeartbeatReturnClearsEscalationLabel(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")
	h.at("10:10:30")
	require.NotNil(t, h.activity("vermont"))

	h.heartbeat("vermont") // a silent return: no registration, no online event
	h.observe()

	assert.Nil(t, h.activity("vermont"), "[FR-037] the failure label clears when the device is live again")
	open, err := h.repo.ListOpenDeviceEscalations(h.ctx(), baseTime)
	require.NoError(t, err)
	assert.Empty(t, open, "and the marker is cleared durably")
}

func TestEX_Observe_WarningDevicesStayLive(t *testing.T) {
	h := newHarness(t)
	h.devices.Devices["hawaii"].Status = models.DeviceStatusWarning
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	h.observe()
	h.tick()

	a := h.activity("hawaii")

	require.NotNil(t, a)
	assert.True(t, a.DeviceOnline, "[SEC-004] a warning device is live: ObserveDevices does not flip it offline")
	assert.Equal(t, c.ID, a.CommandID)
}

func TestEX_Reconcile_KeepsProcessingStateAndPolicyTargets(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	h.at("10:00:30")
	require.True(t, h.containerDied("hawaii", "plex").Suppressed)
	h.succeed(c)
	h.at("10:01:30")
	h.deviceOffline("hawaii")
	h.at("10:02:30")
	require.Nil(t, h.command(c.ID).WindowClosedAt, "the check is deferred (device offline)")

	h.advance(time.Minute) // reconciles: the deferred state must survive
	h.advance(time.Minute)

	assert.Equal(t, 1, h.tr.Stats().Windows, "[AD-011] a reconcile does not drop or reopen a deferred window")
	assert.Nil(t, h.activity("hawaii"), "a deferred window produces no label")
	assert.False(t, h.containerDied("hawaii", "plex").Suppressed, "and covers nothing")
}
