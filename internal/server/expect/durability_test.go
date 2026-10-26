package expect_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/expect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEX_AC031_WindowsSurviveRestart(t *testing.T) {
	h := newHarness(t)
	plex := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	reboot := h.dispatch("vermont", models.ActionReboot)
	h.succeed(reboot) // return phase, 10:00:00
	shutdownPoweredOff(t, h)
	h.at("10:03:30")

	h.restart(h.clock.Now().Add(2 * time.Minute))

	r := h.containerDied("hawaii", "plex")
	off := h.deviceOffline("vermont")
	assert.True(t, r.Suppressed, "[AC-031] the docker_update window survives the restart")
	assert.True(t, off.Covered, "[AC-031] the reboot return phase survives the restart")
	evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Equal(t, plex.ID, evts[0].CommandID, "[AC-031] linked to its command")
	offEvts := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, offEvts, 2, "[AC-031] picups powered off, vermont rebooting")
	a := h.activity("picups")
	require.NotNil(t, a, "[AC-031] picups still shows as powered off")
	assert.Equal(t, models.ActivityPoweredOff, a.Kind)
	assert.Empty(t, h.alerts, "[AC-031] nothing alerts")
}

func TestEX_AC032_DowntimeEscalationOnceAcrossRestarts(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	h.succeed(c) // F = 10:00:00
	h.at("10:01:00")
	require.True(t, h.deviceOffline("vermont").Covered)

	// The server is down until 10:30; the return bound passed at 10:15.
	h.clock.Set(time.Date(2026, 10, 26, 10, 30, 0, 0, time.UTC))
	h.restart(time.Date(2026, 10, 26, 10, 32, 0, 0, time.UTC))
	h.tick()
	assert.Empty(t, h.escalations(), "[AC-032] no online-state decision during the restart grace")

	h.at("10:32:00")
	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-032] the escalation that became due during the downtime fires once the grace is over")

	h.advance(3 * time.Minute)
	h.restart(h.clock.Now().Add(2 * time.Minute))
	h.advance(2*time.Minute + time.Second)
	h.advance(5 * time.Minute)

	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[AC-032] exactly one escalation exists for the command after the second restart")
	assert.Len(t, h.repo.EscalationMarkers(c.ID), 1, "[FR-024] one marker")
	assert.Len(t, h.esc.Pushes(), 1, "one push")
}

func TestEX_AC032_MarkerStopsDuplicateWhenWindowStaysPending(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	h.succeed(c)
	h.at("10:01:00")
	h.deviceOffline("vermont")
	h.at("10:16:00")
	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1)

	// The close of the first run was lost (a crash after the escalation): the
	// window is pending again after the restart. The marker prevents a second one.
	h.cmds.Commands[c.ID].WindowClosedAt = nil
	h.restart(h.clock.Now())
	h.advance(10 * time.Minute)

	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "[FR-024] a reprocessed window never escalates twice")
	assert.Len(t, h.esc.Pushes(), 1, "[FR-024] and never pushes twice")
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the reprocessed window closes")
}

func TestEX_AC033_SilentCommandsCloseAndAlertingResumes(t *testing.T) {
	// NFR-002: dispatch + execution deadline + post-terminal period.
	cases := []struct {
		action string
		keys   []string
		bound  time.Duration
	}{
		{models.ActionReboot, nil, 35 * time.Minute},
		{models.ActionOSUpdate, nil, 55 * time.Minute},
		{models.ActionShutdown, nil, 35 * time.Minute},
		{models.ActionAgentUpdate, nil, 25 * time.Minute},
		{models.ActionDockerUpdate, []string{plexKey}, 37 * time.Minute},
		{models.ActionDockerBulkUpdate, []string{plexKey}, 6*time.Hour + 2*time.Minute},
		{models.ActionDockerRestart, []string{plexKey}, 17 * time.Minute},
		{models.ActionDockerStop, []string{plexKey}, 17 * time.Minute},
		{models.ActionDockerStackRedeploy, []string{plexKey}, 37 * time.Minute},
		{models.ActionDockerStackRestart, []string{plexKey}, 37 * time.Minute},
		{models.ActionDockerStackUp, []string{plexKey}, 37 * time.Minute},
		{models.ActionDockerStackDown, []string{plexKey}, 37 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			build := func() *harness {
				h := newHarness(t)
				n := 1
				if tc.action == models.ActionDockerBulkUpdate {
					n = 50 // 50 x 35m exceeds the 6 h cap
				}
				dl := baseTime.Add(models.ExecDeadline(tc.action, n))
				h.seed(models.Command{ID: "cmd-" + tc.action, DeviceID: "hawaii", Action: tc.action, Status: "running",
					DispatchedAt: tp(baseTime), DeadlineAt: &dl, ExecDeadlineS: int(dl.Sub(baseTime) / time.Second), TargetKeys: tc.keys})
				h.restart(time.Time{})
				return h
			}
			probe := func(h *harness) (covered bool) {
				if tc.action == models.ActionAgentUpdate {
					return h.deviceOffline("hawaii").Covered
				}
				return h.containerDied("hawaii", "plex").Suppressed
			}

			before := build()
			before.clock.Set(baseTime.Add(tc.bound - time.Second))
			before.settle()
			assert.True(t, probe(before), "[AC-033] %s still covers one second before its bound of %s", tc.action, tc.bound)

			after := build()
			after.clock.Set(baseTime.Add(tc.bound))
			after.settle()
			assert.Zero(t, after.tr.Stats().Windows, "[AC-033] no window remains open for %s at its bound", tc.action)
			assert.False(t, probe(after), "[AC-033] normal alerting resumes for %s at its bound", tc.action)
		})
	}
}

func TestEX_AC034_QueuedRebootDoesNotSuppress(t *testing.T) {
	h := newHarness(t, withAgentsOffline())
	c := h.dispatch("vermont", models.ActionReboot)
	require.Equal(t, "queued", c.Status, "the agent is not connected: the command stays queued")

	off := h.deviceOffline("vermont")

	assert.False(t, off.Covered, "[AC-034] a queued command suppresses nothing")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-034] the device_offline alert fires normally")
	a := h.activity("vermont")
	require.NotNil(t, a, "[AC-034] the fleet shows the queued command")
	assert.Equal(t, models.PhaseQueued, a.Phase)
	assert.Equal(t, models.ActionReboot, a.Action)
	assert.Equal(t, models.ActivityHost, a.Kind)

	h.advance(16 * time.Minute) // queued TTL of a power command is 15 min
	h.sweep()
	h.tick()
	assert.Nil(t, h.activity("vermont"), "[AC-034] the label ends when the command expires")
}

func TestEX_AC036_FailOpenWhenNotLoaded(t *testing.T) {
	t.Run("never loaded", func(t *testing.T) {
		h := newHarness(t, withoutLoad())
		h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
		r := h.containerDied("hawaii", "plex")
		off := h.deviceOffline("hawaii")
		assert.False(t, r.Suppressed)
		assert.True(t, errors.Is(r.Err, expect.ErrNotReady), "[AC-036] Cover returns ErrNotReady, got %v", r.Err)
		assert.False(t, off.Covered)
		assert.True(t, errors.Is(off.Err, expect.ErrNotReady))
		assert.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-036] the normal alert fires")
		assert.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-036] the normal offline alert fires")
		assert.True(t, h.alerts[0].Notified)
	})
	t.Run("command records cannot be read", func(t *testing.T) {
		h := newHarness(t, withoutLoad())
		h.repo.FailOn("ListOpenWindows", errors.New("db down"))
		require.Error(t, h.tr.Load(h.ctx()), "Load fails when the command records cannot be read")
		r := h.containerDied("hawaii", "plex")
		assert.False(t, r.Suppressed)
		assert.True(t, errors.Is(r.Err, expect.ErrNotReady))
		assert.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-036] the normal alert fires")
	})
}

func TestEX_AC036_FailOpenWhenStale(t *testing.T) {
	h := newHarness(t)
	h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	require.True(t, h.containerDied("hawaii", "plex").Suppressed, "ready: covered")

	h.repo.FailOn("ListOpenWindows", errors.New("db down"))
	h.clock.Advance(100 * time.Second) // more than ExpectStaleAfter without a reconcile
	h.tick()                           // the loop is live, only the reconcile is stale
	r := h.containerDied("hawaii", "plex")

	assert.False(t, r.Suppressed, "[AC-036] a stale tracker never suppresses")
	assert.True(t, errors.Is(r.Err, expect.ErrStale), "[AC-036] ErrStale, got %v", r.Err)
	assert.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-036] the normal alert fires")
}

func TestEX_SEC003_CoverFailsOpenWhenLoopStalled(t *testing.T) {
	h := newHarness(t)
	h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	h.clock.Advance(16 * time.Second) // the loop has not completed a tick for more than 15 s

	r := h.containerDied("hawaii", "plex")
	assert.True(t, errors.Is(r.Err, expect.ErrLoopStalled), "[SEC-003] a tracker whose loop is not live fails open, got %v", r.Err)
	assert.False(t, r.Suppressed)
	assert.Len(t, h.alertsOf(models.EventContainerDied), 1, "[SEC-003] the occurrence alerts")

	h.tick()
	assert.NoError(t, h.tr.Ready(), "[SEC-003] a completed tick makes the tracker ready again")
}

func TestEX_AC037_RebootWindowEventCountsLogsMemory(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionReboot)
	names := make([]string, 12)
	for i := range names {
		names[i] = fmt.Sprintf("svc-%02d", i)
		h.containerDied("hawaii", names[i])
	}
	h.at("10:02:00")
	h.succeed(c)
	h.at("10:02:30")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:04:00")
	h.register("hawaii")
	h.at("10:04:30")
	for _, n := range names[:11] {
		h.containerStarted("hawaii", n, h.clock.Now())
	}

	h.at("10:07:30")

	suppressed := len(h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)) +
		len(h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected))
	assert.Equal(t, 13, suppressed, "[AC-037] exactly 13 suppressed info events (12 deaths and the offline)")
	assert.Len(t, h.eventsOf(models.EventDeviceOnline, models.CommandAttrExpected), 1, "[AC-037] one online event")
	esc := h.escalations()
	require.Len(t, esc, 1, "[AC-037] one escalation for the container still down")
	assert.Equal(t, "svc-11", esc[0].ContainerName)

	infoLines := 0
	for _, line := range strings.Split(h.logs.String(), "\n") {
		assert.NotContains(t, line, "level=WARN", "[AC-037] no warning is logged for suppressed occurrences")
		assert.NotContains(t, line, "level=ERROR")
		if strings.Contains(line, "level=INFO") && strings.Contains(line, "expected-activity") {
			infoLines++
		}
	}
	assert.Equal(t, 1, infoLines, "[AC-037] at most one info-level line, for the escalation")

	assert.Zero(t, h.tr.Stats().Windows, "[AC-037] no window is held after the close")
	assert.Equal(t, 1, h.tr.Stats().ClosedRecent, "the closed id is kept briefly so a late hook cannot reopen it")
	h.advance(5*time.Minute + time.Second)
	assert.Zero(t, h.tr.Stats().ClosedRecent, "[AC-037] within 5 minutes of the close no memory is held for the window")
}

func TestEX_SEC004_TrackerUsesSameLivenessPredicate(t *testing.T) {
	h := newHarness(t, withDevices(func(h *harness) { h.devices.Devices["vermont"].Status = models.DeviceStatusWarning }))
	c := h.dispatch("vermont", models.ActionOSUpdate)
	h.succeed(c) // 10:00:00
	h.observe()

	h.at("10:03:01")

	assert.NoError(t, h.tr.Ready())
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "[SEC-004] a warning device counts as online: the patch window closes 3 min after success")
	assert.Empty(t, h.escalations(), "[SEC-004] and nothing escalates")
}

func TestEX_LateResultAfterTimeoutDoesNotReopen(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	h.clock.Set(time.Date(2026, 10, 26, 10, 21, 0, 0, time.UTC))
	h.sweep() // timed_out
	require.Equal(t, "timed_out", h.command(c.ID).Status)
	stale := h.command(c.ID) // the row as the hook saw it, window not yet closed
	h.at("10:24:00")
	require.NotNil(t, h.command(c.ID).WindowClosedAt, "the window was processed")
	require.Zero(t, h.tr.Stats().Windows)

	h.succeed(c) // the late agent result: timed_out -> succeeded
	h.tr.CommandChanged(stale)
	late := h.command(c.ID)
	late.WindowClosedAt = nil // a row read before the close became visible
	h.tr.CommandChanged(late)
	off := h.deviceOffline("vermont") // before any tick could finish a reopened window

	assert.Equal(t, "succeeded", h.command(c.ID).Status, "the late result was accepted")
	assert.Zero(t, h.tr.Stats().Windows, "a processed window is never reopened by a late result")
	assert.False(t, off.Covered, "so a later offline transition is not covered")
	assert.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "and alerts normally")
}

func TestEX_Tracker_CapPerDevice(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < models.ExpectPerDeviceWindowCap+5; i++ {
		d := baseTime.Add(time.Duration(i) * time.Second)
		dl := d.Add(35 * time.Minute)
		h.tr.CommandChanged(models.Command{ID: fmt.Sprintf("c-%03d", i), DeviceID: "hawaii", Action: models.ActionDockerUpdate,
			Status: "running", CreatedAt: d, UpdatedAt: d, DispatchedAt: &d, DeadlineAt: &dl, ExecDeadlineS: 2100,
			TargetKeys: []string{fmt.Sprintf("docker:container:c%03d", i)}, Origin: models.OriginUser})
	}
	assert.Equal(t, models.ExpectPerDeviceWindowCap, h.tr.Stats().Windows, "[AD-012] at most 200 windows per device are cached")
}
