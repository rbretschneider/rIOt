package expect_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Publisher tests (ADD section 8, "P"): SEC-003 and SEC-005. The blocking
// tests use a dispatcher that waits on a channel; nothing sleeps. They wait for
// goroutines with require.Eventually, never for time-dependent logic.

// nRebootEscalations arranges n reboots (one per device, up to 3) that all
// succeeded at 10:00, with their devices offline since 10:01, so they all
// escalate when the return bound is reached.
func threeRebootsNotBack(h *harness) []models.Command {
	var cs []models.Command
	for _, d := range allDevices {
		c := h.dispatch(d.id, models.ActionReboot)
		h.succeed(c)
		cs = append(cs, c)
	}
	h.at("10:01:00")
	for _, d := range allDevices {
		h.deviceOffline(d.id)
	}
	return cs
}

func TestEX_SEC003_BlockingPublisherDoesNotStallLoop(t *testing.T) {
	h := newHarness(t, withAsyncPublisher(8, 2))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	h.esc.block = func(c context.Context) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-c.Done():
			return c.Err()
		}
	}
	h.tr.StartPublisher(ctx)
	cs := threeRebootsNotBack(h)

	h.at("10:16:00") // all three windows reach the return bound; every push blocks

	require.Eventually(t, func() bool { return len(started) == 2 }, 5*time.Second, time.Millisecond, "two workers are stuck in the notification")
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 3,
		"[SEC-003] a second and third window still close and escalate while the first pushes are stuck")
	for _, c := range cs {
		assert.NotNil(t, h.command(c.ID).WindowClosedAt, "[SEC-003] every window was finished by the loop")
	}
	assert.Zero(t, h.tr.Stats().Windows, "[SEC-003] the loop kept evaluating")
	require.NoError(t, h.tr.Ready(), "[SEC-003] the loop is live: Tick returned")

	close(release)
	require.Eventually(t, func() bool { return len(h.esc.Pushes()) == 3 }, 5*time.Second, time.Millisecond, "released pushes complete")
}

func TestEX_SEC003_PublishQueueFullDropsPushKeepsEvent(t *testing.T) {
	h := newHarness(t, withAsyncPublisher(1, 1))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	started := make(chan struct{}, 32)
	h.esc.block = func(c context.Context) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-c.Done():
			return c.Err()
		}
	}
	h.tr.StartPublisher(ctx)
	c := h.dispatch("hawaii", models.ActionDockerStackRedeploy, withKeys(mediaKey))
	for i := 0; i < 6; i++ {
		require.True(t, h.containerDied("hawaii", fmt.Sprintf("svc-%d", i), mediaKey).Suppressed)
	}
	h.succeed(c)

	h.at("10:02:01") // six container escalations; one worker, a queue of one

	require.Eventually(t, func() bool { return len(started) >= 1 }, 5*time.Second, time.Millisecond, "the single worker is stuck")
	assert.Len(t, h.escalationsOf(models.EventContainerDied), 6,
		"[SEC-003] the escalation events are stored even when their pushes are dropped (R-3)")
	assert.Len(t, h.repo.EscalationMarkers(c.ID), 6, "[FR-024] six markers")
	assert.Contains(t, h.logs.String(), "publish queue full", "[SEC-003] a dropped push is logged")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "publish queue full; push dropped"), "[AD-012] the warning is throttled to one per minute")
	assert.NoError(t, h.tr.Ready(), "[SEC-003] the loop is unaffected")
	close(release)
}

// escalationBatch runs one stack redeploy whose 12 containers all stay down,
// so the window closes with 12 container escalations (12 pushes for hawaii).
func escalationBatch(t *testing.T, h *harness, startAt string) models.Command {
	t.Helper()
	h.at(startAt)
	c := h.dispatch("hawaii", models.ActionDockerStackRedeploy, withKeys(mediaKey))
	for i := 0; i < 12; i++ {
		require.True(t, h.containerDied("hawaii", fmt.Sprintf("svc-%02d", i), mediaKey).Suppressed)
	}
	h.succeed(c)
	h.advance(2*time.Minute + time.Second)
	return c
}

func summaries(h *harness) []models.Event {
	return h.warningEvents(models.EventExpectNotificationsHeld)
}

func TestEX_SEC005_PushCeilingHoldsAndSummarizes(t *testing.T) {
	h := newHarness(t)
	escalationBatch(t, h, "10:00:00")

	assert.Len(t, h.escalationsOf(models.EventContainerDied), 12, "[SEC-005] every escalation event is stored")
	assert.Len(t, h.esc.Pushes(), 5, "[SEC-005] at most 5 escalation notifications per device per hour")
	assert.Empty(t, summaries(h), "[SEC-005] no summary while the ceiling is still full")

	h.at("11:03:00") // the first pushes have left the rolling hour

	sum := summaries(h)
	require.Len(t, sum, 1, "[SEC-005] one summary event once capacity frees")
	assert.Equal(t, models.SeverityWarning, sum[0].Severity)
	assert.Equal(t, fmt.Sprintf("7 escalation notifications for hawaii were held since %s; see Alerts", h.local(time.Date(2026, 10, 26, 10, 2, 1, 0, time.UTC))), sum[0].Message,
		"[SEC-005] the summary says how many were held")
	pushes := h.esc.Pushes()
	require.Len(t, pushes, 6, "[SEC-005] 5 escalation pushes plus the one summary push")
	assert.Equal(t, models.EventExpectNotificationsHeld, pushes[5].Event.Type, "the summary goes out through the first held rule")
	assert.Equal(t, "container_died", pushes[5].Rule.Metric)
}

func TestEX_SEC005_HeldEscalationsStillStoredExactlyOnce(t *testing.T) {
	h := newHarness(t)
	c := escalationBatch(t, h, "10:00:00")
	h.at("11:03:00")

	esc := h.escalationsOf(models.EventContainerDied)
	names := map[string]int{}
	for _, e := range esc {
		names[e.ContainerName]++
	}
	assert.Len(t, names, 12, "[SEC-005] all 12 containers have their escalation event")
	for n, k := range names {
		assert.Equal(t, 1, k, "[FR-024] %s has exactly one escalation event", n)
	}
	assert.Len(t, h.repo.EscalationMarkers(c.ID), 12)

	h.restart(h.clock.Now())
	h.advance(10 * time.Minute)
	assert.Len(t, h.escalationsOf(models.EventContainerDied), 12, "[FR-024] a restart never re-raises a held escalation")
}

func TestEX_SEC005_SummaryAtMostOncePerHour(t *testing.T) {
	h := newHarness(t)
	escalationBatch(t, h, "10:00:00")
	h.at("11:03:00")
	require.Len(t, summaries(h), 1)

	escalationBatch(t, h, "11:05:00") // another 12 escalations: 5 pushed, 7 held
	h.at("11:30:00")
	assert.Len(t, summaries(h), 1, "[SEC-005] no second summary within the hour of the first")

	h.at("12:10:00")
	assert.Len(t, summaries(h), 2, "[SEC-005] the held count is reported once the hour is over")
	assert.Contains(t, summaries(h)[1].Message, "7 escalation notifications for hawaii were held")
	for _, p := range h.esc.Pushes() {
		assert.NotEmpty(t, p.Event.Message)
	}
	assert.LessOrEqual(t, len(h.esc.Pushes()), 12, "[SEC-005] at most 6 pushes per hour: 2 batches of 5 plus 2 summaries")
}
