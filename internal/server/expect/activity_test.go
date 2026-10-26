package expect_test

import (
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Activity tests (ADD section 8, "A"): FR-034 to FR-043.

func TestEX_AC024_AgentActivityVersions(t *testing.T) {
	h := newHarness(t)
	for _, d := range allDevices {
		h.dispatch(d.id, models.ActionAgentUpdate)
	}

	resp := h.tr.Activities()

	assert.Equal(t, "2.78.0", resp.NewestAgentVersion, "[AC-024] the newest version the server knows")
	require.Len(t, resp.Activities, 3, "[AC-024] each device shows its own update")
	for _, a := range resp.Activities {
		assert.Equal(t, models.ActivityAgent, a.Kind)
		assert.Equal(t, models.PhaseInProgress, a.Phase)
		assert.Equal(t, models.ActionAgentUpdate, a.Action)
		assert.Equal(t, "2.77.0", a.AgentFromVersion, "[AC-024] the version when the command was created")
		assert.Equal(t, "by ryan", a.ActorPhrase, "[AC-024] the actor")
		assert.True(t, a.StartedAt.Equal(baseTime), "[AC-024] the start time")
		assert.True(t, a.CoversOffline)
		assert.True(t, a.DeviceOnline)
		assert.NotEmpty(t, a.CommandID)
	}
	ids := []string{resp.Activities[0].DeviceID, resp.Activities[1].DeviceID, resp.Activities[2].DeviceID}
	assert.Equal(t, []string{"hawaii", "picups", "vermont"}, ids, "sorted by device id")

	// A second browser, a reload, a new tracker after a restart: the same answer.
	h.restart(time.Time{})
	again := h.tr.Activities()
	require.Len(t, again.Activities, 3, "[AC-024]/[FR-040] the labels come from the server's state, not from the browser that clicked")
}

func TestEX_AC025_RestartingAgentBackBy(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c)
	h.at("10:00:50")
	h.deviceOffline("vermont")

	a := h.activity("vermont")

	require.NotNil(t, a, "[AC-025] the device shows what is happening")
	assert.Equal(t, models.PhaseReturn, a.Phase)
	assert.Equal(t, models.ActivityAgent, a.Kind)
	require.NotNil(t, a.BackBy)
	assert.True(t, a.BackBy.Equal(baseTime.Add(10*time.Minute)), "[AC-025] back by 10:10, got %v", a.BackBy)
	assert.False(t, a.DeviceOnline, "[FR-043] the device is visibly offline")
	assert.True(t, a.CoversOffline, "so the fleet counts it as expected offline")
}

func TestEX_AC026_EscalatedActivity(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")
	h.at("10:10:30")

	a := h.activity("vermont")

	require.NotNil(t, a)
	assert.Equal(t, models.ActivityEscalated, a.Kind, "[AC-026] the fleet shows the failure state")
	assert.Equal(t, models.PhaseEscalated, a.Phase)
	assert.Equal(t, models.ActionAgentUpdate, a.Action)
	assert.Equal(t, c.ID, a.CommandID)
	assert.Equal(t, "by ryan", a.ActorPhrase)
	require.NotNil(t, a.Escalation)
	assert.Equal(t, models.EscalationReasonNotBack, a.Escalation.Reason)
	assert.Equal(t, "10m", a.Escalation.Bound)
	assert.False(t, a.CoversOffline, "[FR-038] an escalated device counts as offline")
}

func TestEX_AC028_PoweredOffActivity(t *testing.T) {
	h := newHarness(t)
	c := shutdownPoweredOff(t, h)

	a := h.activity("picups")

	require.NotNil(t, a)
	assert.Equal(t, models.ActivityPoweredOff, a.Kind)
	assert.Equal(t, models.PhasePoweredOff, a.Phase)
	assert.Equal(t, c.ID, a.CommandID)
	assert.Equal(t, "by ryan", a.ActorPhrase)
	require.NotNil(t, a.PoweredOffAt)
	assert.True(t, a.PoweredOffAt.Equal(baseTime.Add(2*time.Minute)), "[AC-028] powered off at 10:02")
	assert.False(t, a.CoversOffline, "the web counts powered-off separately from covers_offline")
	assert.False(t, a.DeviceOnline)
}

func TestEX_AC034_RebootQueuedActivity(t *testing.T) {
	h := newHarness(t, withAgentsOffline())
	c := h.dispatch("vermont", models.ActionReboot)

	a := h.activity("vermont")

	require.NotNil(t, a)
	assert.Equal(t, models.PhaseQueued, a.Phase, "[AC-034] a queued host command is a label only")
	assert.Equal(t, models.ActivityHost, a.Kind)
	assert.Equal(t, c.ID, a.CommandID)
	assert.False(t, a.CoversOffline, "a queued command suppresses nothing")
	assert.True(t, a.StartedAt.Equal(c.CreatedAt))
}

func TestEX_AC038_SelectionPriorityHostOverDocker(t *testing.T) {
	t.Run("one docker action shows its target", func(t *testing.T) {
		h := newHarness(t)
		h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
		a := h.activity("hawaii")
		require.NotNil(t, a)
		assert.Equal(t, models.ActivityDocker, a.Kind)
		assert.Equal(t, "plex", a.Target, "[AC-038] Docker: updating plex")
		assert.Equal(t, 1, a.DockerActions)
	})
	t.Run("a stack names the stack", func(t *testing.T) {
		h := newHarness(t)
		h.dispatch("hawaii", models.ActionDockerStackRedeploy, withKeys(mediaKey))
		a := h.activity("hawaii")
		require.NotNil(t, a)
		assert.Equal(t, "media", a.Target, "[AC-038] Docker: recreating stack media")
	})
	t.Run("two docker actions count", func(t *testing.T) {
		h := newHarness(t)
		h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
		h.dispatch("hawaii", models.ActionDockerRestart, withKeys("docker:container:sonarr"))
		a := h.activity("hawaii")
		require.NotNil(t, a)
		assert.Equal(t, 2, a.DockerActions, "[AC-038] Docker: 2 actions in progress")
		assert.Empty(t, a.Target)
	})
	t.Run("a host window wins over docker windows", func(t *testing.T) {
		h := newHarness(t)
		h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
		reboot := h.dispatch("hawaii", models.ActionReboot)
		a := h.activity("hawaii")
		require.NotNil(t, a)
		assert.Equal(t, models.ActivityHost, a.Kind, "[AC-038] the host label wins")
		assert.Equal(t, reboot.ID, a.CommandID)
	})
	t.Run("a dpkg-lock wait is reported", func(t *testing.T) {
		h := newHarness(t)
		c := h.dispatch("hawaii", models.ActionReboot)
		h.step(c, models.StepWaitDpkgLock)
		a := h.activity("hawaii")
		require.NotNil(t, a)
		assert.True(t, a.WaitingDpkgLock, "[AC-038] Rebooting (waiting for dpkg lock)")
	})
	t.Run("patch progress", func(t *testing.T) {
		h := newHarness(t)
		c := h.dispatch("hawaii", models.ActionOSUpdate)
		pct := 40
		h.report("hawaii", models.CommandReport{Kind: models.ReportKindProgress, CommandID: c.ID, State: "running", Step: "apply", Progress: &pct})
		a := h.activity("hawaii")
		require.NotNil(t, a)
		require.NotNil(t, a.Progress)
		assert.Equal(t, 40, *a.Progress, "[AC-038] Patching (40%)")
	})
	t.Run("an escalation wins over docker, a powered-off record over docker", func(t *testing.T) {
		h := newHarness(t)
		c := h.dispatch("vermont", models.ActionAgentUpdate)
		h.succeed(c)
		h.at("10:01:00")
		h.deviceOffline("vermont")
		h.at("10:10:30")
		h.dispatch("vermont", models.ActionDockerUpdate, withKeys(plexKey))
		a := h.activity("vermont")
		require.NotNil(t, a)
		assert.Equal(t, models.ActivityEscalated, a.Kind, "the failure state outranks a Docker action")

		shutdownPoweredOffOn(t, h, "picups")
		h.dispatch("picups", models.ActionDockerUpdate, withKeys(plexKey))
		p := h.activity("picups")
		require.NotNil(t, p)
		assert.Equal(t, models.ActivityPoweredOff, p.Kind, "the powered-off state outranks a Docker action")
	})
	t.Run("a queued host command outranks docker", func(t *testing.T) {
		h := newHarness(t, withAgentsOffline())
		h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
		q := h.dispatch("hawaii", models.ActionReboot)
		a := h.activity("hawaii")
		require.NotNil(t, a)
		assert.Equal(t, models.PhaseQueued, a.Phase)
		assert.Equal(t, q.ID, a.CommandID)
	})
}

// shutdownPoweredOffOn is shutdownPoweredOff for another device, starting at
// the current clock.
func shutdownPoweredOffOn(t *testing.T, h *harness, device string) {
	t.Helper()
	c := h.dispatch(device, models.ActionShutdown)
	h.advance(time.Minute)
	h.succeed(c)
	h.advance(time.Minute)
	require.Equal(t, models.OfflineKindPoweredOff, h.deviceOffline(device).Kind)
}

func TestEX_AC041_EscalatedClearsAfter24h(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")
	h.at("10:10:30")
	require.NotNil(t, h.activity("vermont"))

	h.advance(23 * time.Hour)
	assert.NotNil(t, h.activity("vermont"), "[AC-041] still shown inside the 24 h display window")
	h.advance(61 * time.Minute)

	assert.Nil(t, h.activity("vermont"), "[AC-041] after 24 h the failure label is no longer shown (the device shows plain offline)")
}

func TestEX_AC041_EscalatedClearsOnOnline(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")
	h.at("10:10:30")
	require.NotNil(t, h.activity("vermont"))

	h.advance(time.Hour)
	h.register("vermont")

	assert.Nil(t, h.activity("vermont"), "[AC-041] the failure label clears when the device reports online")
	open, err := h.repo.ListOpenDeviceEscalations(h.ctx(), baseTime)
	require.NoError(t, err)
	assert.Empty(t, open, "[FR-037] the escalation is cleared durably, so a restart does not bring it back")
	h.restart(h.clock.Now())
	assert.Nil(t, h.activity("vermont"))
}

func TestEX_Activity_EscalationSurvivesRestart(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionAgentUpdate)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")
	h.at("10:10:30")

	h.restart(h.clock.Now().Add(2 * time.Minute))

	a := h.activity("vermont")
	require.NotNil(t, a, "[FR-037] the failure label is rebuilt from the durable marker")
	assert.Equal(t, models.ActivityEscalated, a.Kind)
	assert.Equal(t, models.ActionAgentUpdate, a.Action, "the command is read back to label it")
	assert.Equal(t, "by ryan", a.ActorPhrase)
	assert.Equal(t, models.EscalationReasonNotBack, a.Escalation.Reason)
}
