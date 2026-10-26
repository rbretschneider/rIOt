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

// Window end-of-life tests (ADD section 8, "F"): SEC-002 / C-2, no window
// ends without its backstop.

func deviceMarkers(h *harness, id string) int {
	n := 0
	for _, m := range h.repo.EscalationMarkers(id) {
		if m.Target == models.EscalationTargetDevice {
			n++
		}
	}
	return n
}

func TestEX_SEC002_HardBoundWithoutSweeperEscalates(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot) // 10:00:00, no result ever, no sweeper
	h.at("10:05:00")
	require.True(t, h.deviceOffline("vermont").Covered, "the offline inside the running reboot is suppressed")

	h.at("10:34:59")
	assert.Empty(t, h.escalations(), "nothing before the hard bound (20 min deadline + 15 min)")
	h.at("10:35:00")

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[SEC-002 b] a window that reaches its hard bound with the command still active escalates")
	assert.Equal(t, fmt.Sprintf("vermont is offline: Reboot by ryan sent no result within 35m (command %s)", models.ShortCommandID(c.ID)), esc[0].Message)
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the window is closed after the backstop ran")
	h.advance(time.Hour)
	assert.Len(t, h.escalationsOf(models.EventDeviceOffline), 1, "exactly once")
	a := h.activity("vermont")
	require.NotNil(t, a)
	assert.Equal(t, models.EscalationReasonNoResult, a.Escalation.Reason)
}

// lapsedScenario builds a reboot that succeeded long ago whose window was never
// processed (the server was down), with a suppressed death and the device
// still offline. The server comes back 25 h after the return bound.
func lapsedScenario(t *testing.T) (*harness, models.Command) {
	t.Helper()
	h := newHarness(t)
	d := baseTime.Add(-10 * time.Minute)
	dl := d.Add(20 * time.Minute)
	f := baseTime
	c := h.seed(models.Command{ID: "reboot-lapsed", DeviceID: "vermont", Action: models.ActionReboot, Status: "succeeded",
		DispatchedAt: &d, DeadlineAt: &dl, FinishedAt: &f, UpdatedAt: f})
	require.NoError(t, h.events.Create(h.ctx(), &models.Event{DeviceID: "vermont", Type: models.EventContainerDied, Severity: models.SeverityInfo,
		ContainerName: "plex", CommandID: c.ID, CommandAttr: models.CommandAttrExpected, CreatedAt: baseTime.Add(-5 * time.Minute)}))
	hb := baseTime.Add(15 * time.Second)
	h.devices.Devices["vermont"].Status = models.DeviceStatusOffline
	h.devices.Devices["vermont"].LastHeartbeat = &hb
	h.clock.Set(baseTime.Add(15*time.Minute + 25*time.Hour))
	h.restart(time.Time{})
	h.tick()
	return h, c
}

func TestEX_SEC002_LapseRunsBackstopAndWarns(t *testing.T) {
	h, c := lapsedScenario(t)

	esc := h.escalationsOf(models.EventDeviceOffline)
	require.Len(t, esc, 1, "[SEC-002 a] a lapsing window whose device is still offline escalates")
	assert.Contains(t, esc[0].Message, "still offline 15m after Reboot by ryan")
	warn := h.warningEvents(models.EventExpectBackstopSkipped)
	require.Len(t, warn, 1, "[SEC-002 c] a lapse that skips the container check writes a warning event")
	assert.Equal(t, models.SeverityWarning, warn[0].Severity)
	assert.Contains(t, warn[0].Message, "were not run", "the dropped check is visible")
	assert.NotNil(t, h.command(c.ID).WindowClosedAt)
	outcome, _ := h.repo.WindowOutcome(c.ID)
	assert.Equal(t, models.WindowOutcomeLapsed, outcome)
	assert.Empty(t, h.escalationsOf(models.EventContainerDied), "no container escalation: the device did not return")
}

func TestEX_SEC002_OverflowKeepsNewestAndWarns(t *testing.T) {
	h := newHarness(t)
	total := models.ExpectPerDeviceWindowCap + 5
	for i := 0; i < total-1; i++ { // older, finished, unprocessed
		d := baseTime.Add(-48*time.Hour + time.Duration(i)*time.Minute)
		dl := d.Add(35 * time.Minute)
		f := d.Add(10 * time.Minute)
		h.seed(models.Command{ID: fmt.Sprintf("old-%03d", i), DeviceID: "hawaii", Action: models.ActionDockerUpdate, Status: "succeeded",
			DispatchedAt: &d, DeadlineAt: &dl, FinishedAt: &f, CreatedAt: d, UpdatedAt: f, TargetKeys: []string{fmt.Sprintf("docker:container:o%03d", i)}})
	}
	d := baseTime
	dl := d.Add(35 * time.Minute)
	h.seed(models.Command{ID: "newest-active", DeviceID: "hawaii", Action: models.ActionDockerUpdate, Status: "running",
		DispatchedAt: &d, DeadlineAt: &dl, CreatedAt: d, UpdatedAt: d, TargetKeys: []string{plexKey}})

	h.restart(time.Time{})

	assert.Equal(t, models.ExpectPerDeviceWindowCap, h.tr.Stats().Windows, "[SEC-002 c] the cache holds 200 windows")
	assert.True(t, h.containerDied("hawaii", "plex").Suppressed, "[SEC-002 c] the currently covering window is never the one excluded")
	warn := h.warningEvents(models.EventExpectBackstopSkipped)
	require.Len(t, warn, 1, "[SEC-002 c] one durable warning event for the overflow")
	assert.Contains(t, warn[0].Message, "expected-activity windows on hawaii exceed the tracking limit")

	// Deferred, not skipped: the excluded windows stay pending and are processed
	// by later reconciles once the cache has room.
	for i := 0; i < 4; i++ {
		h.advance(31 * time.Second)
	}
	for i := 0; i < total-1; i++ {
		assert.NotNil(t, h.command(fmt.Sprintf("old-%03d", i)).WindowClosedAt, "[SEC-002 c] window old-%03d is processed, not skipped", i)
	}
	assert.Nil(t, h.command("newest-active").WindowClosedAt, "the active window is still open")
}

func TestEX_SEC002_DeletedRowWritesBackstopSkipped(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	require.Equal(t, 1, h.tr.Stats().Windows)
	delete(h.cmds.Commands, c.ID) // retention, a manual delete or a device cascade

	h.advance(31 * time.Second)

	warn := h.warningEvents(models.EventExpectBackstopSkipped)
	require.Len(t, warn, 1, "[SEC-002 e] a removed command record leaves a visible warning")
	assert.Equal(t, fmt.Sprintf("Checks for Reboot by ryan on vermont were skipped: the command record was removed (command %s)", models.ShortCommandID(c.ID)), warn[0].Message)
	assert.Zero(t, h.tr.Stats().Windows, "the window leaves the cache")
}

func TestEX_SEC002_EvictionDoesNotClose(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	h.succeed(c) // 10:00:00, the window ends at 10:02:00
	h.repo.FailOn("CloseWindow", errors.New("db down"))

	h.at("10:02:30")
	require.Equal(t, 1, h.tr.Stats().Windows, "the failed close leaves the window in place for a retry")
	for _, hms := range []string{"10:04:00", "10:06:00", "10:07:29"} {
		h.at(hms)
		require.Equal(t, 1, h.tr.Stats().Windows, "still retried at %s", hms)
	}
	h.at("10:07:31")

	assert.Zero(t, h.tr.Stats().Windows, "[AD-009] a window that cannot be finished for 5 minutes leaves the cache")
	assert.Nil(t, h.command(c.ID).WindowClosedAt, "[AD-009] eviction never closes the row: it stays pending")

	h.repo.FailOn("CloseWindow", nil)
	h.advance(31 * time.Second)
	h.tick()
	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "[AD-009] the evicted window is reloaded and finished")
}

func TestEX_SEC002_ContainerRecheckOnReturn(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprintf("container started=%v", started), func(t *testing.T) {
			h := newHarness(t)
			c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
			h.at("10:00:30")
			require.True(t, h.containerDied("hawaii", "plex").Suppressed)
			h.at("10:01:00")
			h.succeed(c)
			h.at("10:01:30")
			h.deviceOffline("hawaii") // container windows do not cover offline: this alerts

			h.at("10:03:01") // the window ended while the device is offline
			assert.Empty(t, h.escalations(), "[SEC-002 d] the container check cannot run while the device is offline")
			assert.Nil(t, h.command(c.ID).WindowClosedAt, "[SEC-002 d] the window stays pending, its check deferred")
			assert.Equal(t, 1, h.tr.Stats().Windows)

			h.at("10:10:00")
			h.register("hawaii")
			if started {
				h.at("10:10:30")
				h.containerStarted("hawaii", "plex", h.clock.Now())
			}
			h.at("10:12:59")
			assert.Nil(t, h.command(c.ID).WindowClosedAt, "[SEC-002 d] the check waits until the device has been back for 3 min")
			h.at("10:13:01")

			assert.NotNil(t, h.command(c.ID).WindowClosedAt, "the re-check ran and the window closed")
			esc := h.escalationsOf(models.EventContainerDied)
			if started {
				assert.Empty(t, esc, "[SEC-002 d] a container that came back does not escalate")
				return
			}
			require.Len(t, esc, 1, "[SEC-002 d] a container that did not come back escalates on the device's return")
			assert.Equal(t, fmt.Sprintf("Container plex still not running 3m after Docker update by ryan (succeeded, command %s)", models.ShortCommandID(c.ID)), esc[0].Message)
		})
	}
}

func TestEX_SEC002_RecheckDeferralBounded24h(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	h.at("10:00:30")
	require.True(t, h.containerDied("hawaii", "plex").Suppressed)
	h.at("10:01:00")
	h.succeed(c)
	h.at("10:01:30")
	h.deviceOffline("hawaii")
	h.at("10:03:01")
	require.Nil(t, h.command(c.ID).WindowClosedAt, "deferred")

	h.at("10:02:59") // clock moves back by one second only for the next jump: stay deterministic
	h.clock.Set(time.Date(2026, 10, 27, 10, 3, 1, 0, time.UTC))
	h.settle()

	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "[AD-011] the deferral is bounded: it lapses 24 h after the close time")
	outcome, _ := h.repo.WindowOutcome(c.ID)
	assert.Equal(t, models.WindowOutcomeLapsed, outcome)
	warn := h.warningEvents(models.EventExpectBackstopSkipped)
	require.Len(t, warn, 1, "[AD-011] one warning event says the check was not run")
	assert.Equal(t, fmt.Sprintf("Container checks for Docker update by ryan on hawaii were not run: the device did not return within 24h (command %s)", models.ShortCommandID(c.ID)), warn[0].Message)
	assert.Empty(t, h.escalationsOf(models.EventContainerDied), "nothing can be checked while the device is away")
}

func TestEX_SEC002_AnyEndWithDeviceOfflineEscalatesOnce(t *testing.T) {
	type scenario struct {
		reason models.WindowEndReason
		run    func(t *testing.T) (*harness, models.Command)
		want   int // device escalations
		warn   int // expect_backstop_skipped events
	}
	offlineCovered := func(h *harness, device string) { require.True(t, h.deviceOffline(device).Covered) }
	scenarios := []scenario{
		{models.EndReturnBound, func(t *testing.T) (*harness, models.Command) {
			h := newHarness(t)
			c := h.dispatch("vermont", models.ActionReboot)
			h.succeed(c)
			h.at("10:01:00")
			offlineCovered(h, "vermont")
			h.at("10:16:00")
			return h, c
		}, 1, 0},
		{models.EndHardBound, func(t *testing.T) (*harness, models.Command) {
			h := newHarness(t)
			c := h.dispatch("vermont", models.ActionReboot)
			h.at("10:05:00")
			offlineCovered(h, "vermont")
			h.at("10:36:00")
			return h, c
		}, 1, 0},
		{models.EndNonSuccessSettle, func(t *testing.T) (*harness, models.Command) {
			h := newHarness(t)
			c := h.dispatch("vermont", models.ActionReboot)
			h.at("10:05:00")
			offlineCovered(h, "vermont")
			h.clock.Set(time.Date(2026, 10, 26, 10, 21, 0, 0, time.UTC))
			h.sweep()
			h.tick()
			h.at("10:30:00")
			return h, c
		}, 1, 0},
		{models.EndSettled, func(t *testing.T) (*harness, models.Command) {
			h := newHarness(t)
			c := h.dispatch("vermont", models.ActionOSUpdate)
			h.succeed(c)
			h.at("10:04:00") // the device stayed online: closes 3 min after success
			return h, c
		}, 0, 0},
		{models.EndPoweredOff, func(t *testing.T) (*harness, models.Command) {
			h := newHarness(t)
			c := shutdownPoweredOff(t, h)
			h.at("10:30:00")
			return h, c
		}, 0, 0},
		{models.EndNotOpened, func(t *testing.T) (*harness, models.Command) {
			h := newHarness(t, withAgentsOffline())
			c := h.dispatch("vermont", models.ActionReboot)
			h.deviceOffline("vermont")
			h.advance(16 * time.Minute)
			h.sweep() // the queued command expires
			h.tick()
			return h, c
		}, 0, 0},
		{models.EndLapsed, func(t *testing.T) (*harness, models.Command) {
			h, c := lapsedScenario(t)
			return h, c
		}, 1, 1},
		{models.EndDeleted, func(t *testing.T) (*harness, models.Command) {
			h := newHarness(t)
			c := h.dispatch("vermont", models.ActionReboot)
			h.at("10:05:00")
			offlineCovered(h, "vermont")
			delete(h.cmds.Commands, c.ID)
			h.advance(31 * time.Second)
			return h, c
		}, 0, 1},
	}
	for _, sc := range scenarios {
		t.Run(string(sc.reason), func(t *testing.T) {
			h, c := sc.run(t)
			for i := 0; i < 3; i++ { // more ticks and reconciles never add a second escalation
				h.advance(time.Minute)
			}
			h.restart(h.clock.Now())
			h.advance(5 * time.Minute)

			assert.Len(t, h.escalationsOf(models.EventDeviceOffline), sc.want,
				"[SEC-002 a] end reason %s: device escalations", sc.reason)
			assert.LessOrEqual(t, deviceMarkers(h, c.ID), 1, "[FR-024] at most one device marker for the command")
			assert.Len(t, h.warningEvents(models.EventExpectBackstopSkipped), sc.warn, "end reason %s: warning events", sc.reason)
			if sc.reason != models.EndDeleted {
				assert.NotNil(t, h.command(c.ID).WindowClosedAt, "end reason %s: the window left processing", sc.reason)
			}
		})
	}
}
