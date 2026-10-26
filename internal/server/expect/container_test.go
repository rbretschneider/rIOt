package expect_test

import (
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

const (
	plexKey  = "docker:container:plex"
	mediaKey = "docker:compose:/srv/media"
)

func TestEX_AC001_DockerUpdateDeathRecordedNotAlerted(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))

	r := h.containerDied("hawaii", "plex")

	assert.True(t, r.Suppressed, "[AC-001] a death inside a dispatched docker_update window must be covered")
	evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	require.Len(t, evts, 1, "[AC-001] exactly one suppressed event is stored")
	assert.Equal(t, models.SeverityInfo, evts[0].Severity, "[AC-001] suppressed events are info")
	assert.Equal(t, c.ID, evts[0].CommandID, "[AC-001] the event is linked to the covering command")
	assert.Equal(t, "Container plex died during Docker update by ryan (expected)", evts[0].Message,
		"[AC-001] the message names the action, the container and the actor")
	requireNoAlerts(t, h, "[AC-001] nothing may be notified")
}

func TestEX_AC002_ScheduledUpdateNamesPolicy(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.pol.Upsert(h.ctx(), &models.AutoUpdatePolicy{DeviceID: "hawaii", Target: "plex", Enabled: true}))
	policyID := h.pol.Policies["hawaii"][0].ID
	require.NoError(t, h.tr.Reconcile(h.ctx()), "the reconcile reads the policy targets")
	h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey), withOrigin(models.OriginAutoUpdate, policyID))

	r := h.containerDied("hawaii", "plex")

	assert.True(t, r.Suppressed, "[AC-002] a scheduled update is treated like Ryan's")
	evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Contains(t, evts[0].Message, "by auto-update policy for plex", "[AC-002] the actor phrase names the policy target")
	requireNoAlerts(t, h, "[AC-002] nothing may be notified")
}

func TestEX_AC003_RestartStopAndStackActionsCovered(t *testing.T) {
	cases := []struct {
		action string
		keys   []string
		dying  string
		dkeys  []string
		label  string
	}{
		{models.ActionDockerRestart, []string{plexKey}, "plex", nil, "Docker restart"},
		{models.ActionDockerStop, []string{plexKey}, "plex", nil, "Docker stop"},
		{models.ActionDockerStackRedeploy, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey}, "Docker stack redeploy"},
		{models.ActionDockerStackRestart, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey}, "Docker stack restart"},
		{models.ActionDockerStackUp, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey}, "Docker stack up"},
		{models.ActionDockerStackDown, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey}, "Docker stack down"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			h := newHarness(t)
			c := h.dispatch("hawaii", tc.action, withKeys(tc.keys...))

			r := h.containerDied("hawaii", tc.dying, tc.dkeys...)

			assert.True(t, r.Suppressed, "[AC-003] a targeted death inside a %s window must be covered", tc.action)
			evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
			require.Len(t, evts, 1)
			assert.Equal(t, c.ID, evts[0].CommandID, "[AC-003] linked to its command")
			assert.Contains(t, evts[0].Message, tc.label, "[AC-003] the message names the action")
			requireNoAlerts(t, h, "[AC-003] no notification")
		})
	}
}

func TestEX_AC004_BulkDeathAtThreeHoursSuppressed(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerBulkUpdate, withBulk(8), withKeys(plexKey, "docker:container:sonarr"))
	require.Equal(t, 4*3600+45*60, c.ExecDeadlineS, "the 8-target bulk update has the 4h45m execution deadline")

	h.advance(3 * time.Hour)
	r := h.containerDied("hawaii", "plex")

	assert.True(t, r.Suppressed, "[AC-004] a targeted death 3 h into a running bulk update is covered")
	requireNoAlerts(t, h, "[AC-004] no notification")
	assert.Equal(t, 1, h.tr.Stats().Windows, "[AC-004] the window is still open")
}

func TestEX_AC005_NonTargetContainerAlerts(t *testing.T) {
	h := newHarness(t)
	h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))

	r := h.containerDied("hawaii", "sonarr")

	assert.False(t, r.Suppressed, "[AC-005] an untargeted container is not covered")
	require.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-005] the alert fires")
	assert.True(t, h.alerts[0].Notified, "[AC-005] and notifies as with no window")
	assert.Empty(t, h.eventsOf(models.EventContainerDied, models.CommandAttrExpected), "nothing is recorded as expected")
}

func TestEX_AC006_DeathBeforeUpdateStartedSuppressed(t *testing.T) {
	h := newHarness(t)
	h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))

	// The agent's update_started event has not arrived yet: nothing in the
	// tracker depends on it (FR-013).
	r := h.containerDied("hawaii", "plex")

	assert.True(t, r.Suppressed, "[AC-006] a death before update_started is covered")
	requireNoAlerts(t, h, "[AC-006] no notification")
}

func TestEX_AC006_NoUpdateStartedSuppressed(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
	h.advance(5 * time.Minute)

	r1 := h.containerDied("hawaii", "plex")
	h.succeed(c)
	h.advance(30 * time.Second)
	r2 := h.containerDied("hawaii", "plex")

	assert.True(t, r1.Suppressed, "[AC-006] an agent that never sends update_started is covered while the command runs")
	assert.True(t, r2.Suppressed, "[AC-006] and after update_completed, inside the settle period")
	requireNoAlerts(t, h, "[AC-006] no notification")
}

func TestEX_AC007_DeathAfterSettleAlerts(t *testing.T) {
	succeedAt := func(h *harness) {
		c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
		h.succeed(c) // finished_at = 10:00:00
	}
	t.Run("10:01:50 is suppressed", func(t *testing.T) {
		h := newHarness(t)
		succeedAt(h)
		h.at("10:01:50")
		r := h.containerDied("hawaii", "plex")
		assert.True(t, r.Suppressed, "[AC-007] 110 s after success is inside the 2 min settle period")
		requireNoAlerts(t, h, "[AC-007] no notification")
	})
	t.Run("10:02:10 alerts", func(t *testing.T) {
		h := newHarness(t)
		succeedAt(h)
		h.at("10:02:10")
		r := h.containerDied("hawaii", "plex")
		assert.False(t, r.Suppressed, "[AC-007] 130 s after success the window is closed")
		require.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-007] the death alerts normally")
		assert.True(t, h.alerts[0].Notified)
	})
}

func TestEX_AC008_ActionsOutsideTableAlert(t *testing.T) {
	for _, tc := range []struct {
		action string
		keys   []string
	}{
		{"docker_start", []string{plexKey}},
		{"docker_stack_pull", []string{mediaKey}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			h := newHarness(t)
			h.dispatch("hawaii", tc.action, withKeys(tc.keys...))
			assert.Equal(t, 0, h.tr.Stats().Windows, "[AC-008] %s opens no window", tc.action)

			r := h.containerDied("hawaii", "plex", plexKey, mediaKey)

			assert.False(t, r.Suppressed, "[AC-008] the death is not covered")
			require.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-008] the alert fires as today")
			assert.True(t, h.alerts[0].Notified)
		})
	}
}

func TestEX_AC009_OOMAlertsAndNamesAction(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))

	h.containerOOM("hawaii", "plex")

	oom := h.alertsOf(models.EventContainerOOM)
	require.Len(t, oom, 1, "[AC-009] the OOM alert fires")
	assert.True(t, oom[0].Notified, "[AC-009] and notifies")
	assert.Equal(t, fmt.Sprintf("Container plex OOM killed (during Docker update by ryan, command %s)", models.ShortCommandID(c.ID)),
		oom[0].Message, "[AC-009] the message names the in-progress action")
	assert.Empty(t, h.eventsOf(models.EventContainerOOM, models.CommandAttrExpected), "an OOM is never recorded as expected")
}

// Generator contract: the exclusion check runs before the tracker is asked
// (X-9), so an excluded container behaves identically with and without a window.
func TestEX_AC010_ExcludedContainerSameInsideAndOutside(t *testing.T) {
	run := func(withWindow bool) (events, alerts int) {
		h := newHarness(t)
		h.rules.Rules[0].ExcludeContainers = "plex"
		if withWindow {
			h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))
		}
		h.containerDied("hawaii", "plex")
		return len(h.allEvents()), len(h.alerts)
	}
	e1, a1 := run(false)
	e2, a2 := run(true)
	assert.Equal(t, e1, e2, "[AC-010] the same events with and without a window")
	assert.Equal(t, a1, a2, "[AC-010] the same alerts with and without a window")
	assert.Zero(t, e1+a1, "[AC-010] an excluded container produces nothing")
}

func TestEX_AC044_OlderAgentRestartSuppressed(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerRestart, withKeys(plexKey))

	// An older agent attaches no command ID and sends no update_started: the
	// server derives the event's target keys itself, so only keys are passed.
	r := h.containerDied("hawaii", "plex", plexKey)

	assert.True(t, r.Suppressed, "[AC-044] the death is covered without any agent-supplied command id")
	evts := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Equal(t, c.ID, evts[0].CommandID, "[AC-044] the death is linked to the command")
}

func TestEX_AC045_CrossDeviceCommandIDIgnored(t *testing.T) {
	h := newHarness(t)
	h.dispatch("hawaii", models.ActionDockerUpdate, withKeys(plexKey))

	// A Docker event from vermont (it may name hawaii's command id; the id is
	// never an input of coverage): plex on vermont dies.
	r := h.containerDied("vermont", "plex")

	assert.False(t, r.Suppressed, "[AC-045] hawaii's window never covers vermont's container")
	require.Len(t, h.alertsOf(models.EventContainerDied), 1, "[AC-045] the vermont death alerts normally")
	assert.Equal(t, "vermont", h.alerts[0].Device)
	assert.Empty(t, h.eventsOf(models.EventContainerDied, models.CommandAttrExpected), "[AC-045] and is not linked to the hawaii command")
}

func TestEX_AC045_HostWindowOnOtherDeviceIgnored(t *testing.T) {
	h := newHarness(t)
	h.dispatch("vermont", models.ActionReboot)

	// vermont has a host window open; a hawaii event that names vermont's
	// command id must not be covered by it (INFO-2).
	r := h.containerDied("hawaii", "plex")
	off := h.deviceOffline("hawaii")

	assert.False(t, r.Suppressed, "[AC-045] a vermont host window covers no hawaii container")
	assert.False(t, off.Covered, "[AC-045] and no hawaii offline transition")
	assert.Len(t, h.alerts, 2, "[AC-045] both hawaii occurrences alert")
	assert.Empty(t, h.eventsOf(models.EventContainerDied, models.CommandAttrExpected))
}

func TestEX_AC046_SuppressedAndEscalatedMessagesSanitized(t *testing.T) {
	assertClean := func(t *testing.T, label, s string) {
		t.Helper()
		assert.True(t, utf8.ValidString(s), "[AC-046] %s is valid UTF-8: %q", label, s)
		for _, r := range s {
			assert.True(t, unicode.IsPrint(r), "[AC-046] %s has only printable runes, got %U", label, r)
		}
	}
	dirty := strings.Repeat("a", 300) + "\x00\x1b[31m" + string([]byte{0xff, 0xfe}) + "‮z"
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionDockerStackRedeploy, withKeys(mediaKey))
	h.containerDied("hawaii", dirty, models.TargetKeyContainer(dirty), mediaKey)
	// A 129-byte shared-prefix pair: two containers, two events, two markers.
	prefix := strings.Repeat("x", 128)
	h.containerDied("hawaii", prefix+"A", mediaKey)
	h.containerDied("hawaii", prefix+"B", mediaKey)
	h.succeed(c)
	h.advance(2*time.Minute + time.Second)

	suppressed := h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)
	require.Len(t, suppressed, 3, "[AC-046] three distinct containers died")
	for _, e := range suppressed {
		assertClean(t, "suppressed message", e.Message)
		assertClean(t, "suppressed container name", e.ContainerName)
		assert.LessOrEqual(t, len(e.ContainerName), 128, "[AC-046] the name part is at most 128 bytes")
	}
	esc := h.escalationsOf(models.EventContainerDied)
	require.Len(t, esc, 3, "[AC-046] three escalations: the 129-byte shared-prefix pair yields two, not one")
	for _, e := range esc {
		assertClean(t, "escalation message", e.Message)
		assertClean(t, "escalation container name", e.ContainerName)
		assert.LessOrEqual(t, len(e.ContainerName), 128)
	}
	assert.Len(t, h.repo.EscalationMarkers(c.ID), 3, "[AC-046] one marker per distinct container")
}

func TestEX_SEC006_EventFieldsSanitized(t *testing.T) {
	h := newHarness(t, withDevices(func(h *harness) { h.devices.Devices["vermont"].Hostname = "ver‮\x00mont" }))
	h.dispatch("vermont", models.ActionReboot, byActor("ry‮an\x00\x07"))

	h.containerDied("vermont", "pl​ex")
	h.deviceOffline("vermont")

	for _, e := range h.allEvents() {
		for _, bad := range []string{"‮", "​", "\x00", "\x07"} {
			assert.NotContains(t, e.Message, bad, "[SEC-006] event message %q must not carry %q", e.Message, bad)
			assert.NotContains(t, e.ContainerName, bad, "[SEC-006] container name %q must not carry %q", e.ContainerName, bad)
		}
		assert.True(t, utf8.ValidString(e.Message), "[SEC-006] message is valid UTF-8")
	}
	var died models.Event
	for _, e := range h.allEvents() {
		if e.Type == models.EventContainerDied {
			died = e
		}
	}
	assert.Contains(t, died.Message, "by ryan", "[SEC-006] the actor name is cleaned, not dropped")
	assert.NotEqual(t, "plex", died.ContainerName, "[SEC-006] an altered name must not collapse onto the clean name")
}
