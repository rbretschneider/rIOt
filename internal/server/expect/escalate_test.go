package expect_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Escalation tests (ADD section 8, "E").

func bulkWithDeadContainers(t *testing.T, h *harness, n int) models.Command {
	t.Helper()
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("docker:container:svc-%02d", i)
	}
	c := h.dispatch("hawaii", models.ActionDockerBulkUpdate, withBulk(n), withKeys(keys...))
	for i := 0; i < n; i++ {
		require.True(t, h.containerDied("hawaii", fmt.Sprintf("svc-%02d", i)).Suppressed, "container %d is covered", i)
	}
	h.succeed(c)
	return c
}

func TestEX_AC021_TwentyPlusSummary(t *testing.T) {
	h := newHarness(t)
	c := bulkWithDeadContainers(t, h, 30)

	h.advance(2*time.Minute + time.Second)

	esc := h.escalationsOf(models.EventContainerDied)
	require.Len(t, esc, 21, "[AC-021] 20 container escalations and one summary")
	containers, summary := 0, 0
	for _, e := range esc {
		if e.ContainerName == "" {
			summary++
			assert.Equal(t, fmt.Sprintf("10 more containers still not running 2m after Docker bulk update by ryan (succeeded, command %s)", models.ShortCommandID(c.ID)),
				e.Message, "[AC-021] the summary names the 10 further containers and the action")
			continue
		}
		containers++
	}
	assert.Equal(t, 20, containers, "[FR-025] at most 20 container escalations per command")
	assert.Equal(t, 1, summary, "[FR-025] one summary")
	assert.Len(t, h.repo.EscalationMarkers(c.ID), 21, "[NFR-003] 21 markers")

	h.advance(10 * time.Minute)
	assert.Len(t, h.escalationsOf(models.EventContainerDied), 21, "[AC-021] and no more")
}

func TestEX_AC021_CapHoldsAcrossRestart(t *testing.T) {
	h := newHarness(t)
	c := bulkWithDeadContainers(t, h, 30)
	h.repo.FailOn("CloseWindow", errors.New("crash before the close"))
	h.advance(2*time.Minute + time.Second)
	require.Len(t, h.escalationsOf(models.EventContainerDied), 21, "the escalations were recorded before the close failed")
	require.Nil(t, h.command(c.ID).WindowClosedAt, "the window is still pending")

	h.repo.FailOn("CloseWindow", nil)
	h.restart(h.clock.Now())
	h.advance(time.Minute)
	assert.Nil(t, h.command(c.ID).WindowClosedAt, "after a restart the check waits 3 min for the containers to start (AD-011)")
	h.advance(3 * time.Minute)
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the pending window finally closed")
	h.restart(h.clock.Now())
	h.advance(5 * time.Minute)

	assert.Len(t, h.escalationsOf(models.EventContainerDied), 21, "[AC-021] the cap holds across restarts: still 20 + 1")
	assert.Len(t, h.repo.EscalationMarkers(c.ID), 21)
}

func TestEX_AC040_DispatchAndCloseBroadcastWithinOneTick(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey), withOrigin(models.OriginAutoUpdate, 0))
	h.tick()

	recs := h.bc.Activities()
	require.Len(t, recs, 1, "[AC-040] a dispatched docker_update is broadcast within one tick")
	require.NotNil(t, recs[0].Activity)
	assert.Equal(t, "hawaii", recs[0].DeviceID)
	assert.Equal(t, models.ActivityDocker, recs[0].Activity.Kind)
	assert.Equal(t, "plex", recs[0].Activity.Target)

	h.tick()
	assert.Len(t, h.bc.Activities(), 1, "[AC-040] an unchanged activity is not broadcast again")

	h.succeed(c)
	h.tick()
	h.at("10:02:01") // the window closes
	recs = h.bc.Activities()
	require.GreaterOrEqual(t, len(recs), 2)
	last := recs[len(recs)-1]
	assert.Equal(t, "hawaii", last.DeviceID)
	assert.Nil(t, last.Activity, "[AC-040] the cleared activity is broadcast within one tick of the window closing")
}

func TestEX_Escalation_PushIsEnqueuedOnlyWhenInserted(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")
	h.at("10:15:30")
	require.Len(t, h.esc.Pushes(), 1)

	h.cmds.Commands[c.ID].WindowClosedAt = nil // reprocessed after a crash
	h.restart(h.clock.Now())
	h.advance(5 * time.Minute)

	assert.Len(t, h.esc.Pushes(), 1, "[FR-024] an escalation whose marker already exists is not pushed again")
}
