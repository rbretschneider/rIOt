package expect_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/expect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (EXPECT-001). These tests close gaps found in the AC
// audit: AC-005 and AC-008 were pinned for docker_update only, AC-009 for a
// container window only, AC-036 for the container path only, and AC-045 for
// container and reboot windows only. They are test-only: no implementation
// code is touched.

var qaContainerActions = []struct {
	action string
	keys   []string
	// dying is the covered container; its event keys are the container name key
	// plus, for a stack action, the compose project key.
	dying  string
	dkeys  []string
	other  string
	okeys  []string
	remark string
}{
	{models.ActionDockerUpdate, []string{plexKey}, "plex", nil, "sonarr", nil, ""},
	{models.ActionDockerBulkUpdate, []string{plexKey, "docker:container:radarr"}, "plex", nil, "sonarr", nil, ""},
	{models.ActionDockerRestart, []string{plexKey}, "plex", nil, "sonarr", nil, ""},
	{models.ActionDockerStop, []string{plexKey}, "plex", nil, "sonarr", nil, ""},
	{models.ActionDockerStackRedeploy, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey},
		"nginx", []string{"docker:container:nginx", "docker:compose:/srv/web"}, "other compose project"},
	{models.ActionDockerStackRestart, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey},
		"nginx", []string{"docker:container:nginx", "docker:compose:/srv/web"}, "other compose project"},
	{models.ActionDockerStackUp, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey},
		"nginx", []string{"docker:container:nginx", "docker:compose:/srv/web"}, "other compose project"},
	{models.ActionDockerStackDown, []string{mediaKey}, "sonarr", []string{"docker:container:sonarr", mediaKey},
		"nginx", []string{"docker:container:nginx", "docker:compose:/srv/web"}, "other compose project"},
}

// Covers AC-005: suppression is scoped to the command's own targets for every
// container-class action, not only docker_update. An unrelated container on the
// same device still alerts and notifies, and its occurrence is not linked to the
// command.
func TestEX_QA_AC005_EveryContainerActionCoversOnlyItsTargets(t *testing.T) {
	for _, tc := range qaContainerActions {
		t.Run(tc.action, func(t *testing.T) {
			h := newHarness(t)
			h.dispatch("hawaii", tc.action, withKeys(tc.keys...))

			unrelated := h.containerDied("hawaii", tc.other, tc.okeys...)
			covered := h.containerDied("hawaii", tc.dying, tc.dkeys...)

			assert.False(t, unrelated.Suppressed, "[AC-005] %s must not cover an unrelated container (%s)", tc.action, tc.remark)
			assert.True(t, covered.Suppressed, "[AC-005] %s still covers its own target", tc.action)
			alerts := h.alertsOf(models.EventContainerDied)
			require.Len(t, alerts, 1, "[AC-005] exactly the unrelated death alerts")
			assert.Equal(t, tc.other, alerts[0].Container)
			assert.True(t, alerts[0].Notified, "[AC-005] and it notifies as with no window")
		})
	}
}

// Covers AC-008 / FR-002: a container-class window never covers a device_offline
// transition of its device, and a window never covers another device.
func TestEX_QA_AC008_ContainerWindowsNeverCoverOffline(t *testing.T) {
	for _, tc := range qaContainerActions {
		t.Run(tc.action, func(t *testing.T) {
			h := newHarness(t)
			h.dispatch("hawaii", tc.action, withKeys(tc.keys...))

			off := h.deviceOffline("hawaii")

			assert.False(t, off.Covered, "[AC-008] a %s window must not hide the device going offline", tc.action)
			require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-008] the offline alert fires")
			assert.True(t, h.alerts[0].Notified)
		})
	}
}

// Covers AC-009 / FR-011: an OOM kill is never suppressed, also inside a host
// window that covers every container death.
func TestEX_QA_AC009_OOMDuringHostWindowStillAlerts(t *testing.T) {
	for _, action := range []string{models.ActionReboot, models.ActionOSUpdate, models.ActionShutdown} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t)
			c := h.dispatch("hawaii", action)

			h.containerOOM("hawaii", "plex")

			oom := h.alertsOf(models.EventContainerOOM)
			require.Len(t, oom, 1, "[AC-009] the OOM alert fires inside a %s window", action)
			assert.True(t, oom[0].Notified, "[AC-009] and notifies")
			assert.Contains(t, oom[0].Message, models.ShortCommandID(c.ID), "[AC-009] the message names the command")
			assert.Empty(t, h.eventsOf(models.EventContainerOOM, models.CommandAttrExpected), "[AC-009] an OOM is never recorded as expected")
		})
	}
}

// Covers AC-045 / NFR-010: the powered-off state belongs to one device. A
// powered-off device suppresses nothing for the others, and a second device going
// offline alerts while the first is powered off.
func TestEX_QA_AC045_PoweredOffIsPerDevice(t *testing.T) {
	h := newHarness(t)
	shutdownPoweredOff(t, h) // picups

	vermont := h.deviceOffline("vermont")
	died := h.containerDied("hawaii", "plex")

	assert.False(t, vermont.Covered, "[AC-045] picups being powered off covers no other device's offline")
	assert.False(t, died.Suppressed, "[AC-045] nor another device's container death")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-045] vermont alerts")
	assert.Equal(t, "vermont", h.alertsOf(models.EventDeviceOffline)[0].Device)
	assert.Nil(t, h.activity("vermont"), "[AC-045] vermont shows no expected activity")
	a := h.activity("picups")
	require.NotNil(t, a)
	assert.Equal(t, models.ActivityPoweredOff, a.Kind, "[AC-045] picups stays powered off")
}

// Covers AC-045: an escalation on one device never labels or covers another.
func TestEX_QA_AC045_EscalationIsPerDevice(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("vermont", models.ActionReboot)
	h.succeed(c)
	h.at("10:01:00")
	require.True(t, h.deviceOffline("vermont").Covered)
	h.at("10:16:00")
	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1)

	off := h.deviceOffline("hawaii")

	assert.False(t, off.Covered, "[AC-045] vermont's escalated reboot covers nothing on hawaii")
	assert.Nil(t, h.activity("hawaii"), "[AC-045] hawaii shows no activity")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1)
	assert.Equal(t, "hawaii", h.alertsOf(models.EventDeviceOffline)[0].Device)
}

// Covers AC-036 / SEC-003: the offline path fails open too, whether the
// reconcile is stale or the loop stalled, and the tracker self-heals: once a
// reconcile and a tick succeed, coverage resumes.
func TestEX_QA_AC036_OfflineFailsOpenWhenStaleOrStalledAndSelfHeals(t *testing.T) {
	t.Run("stale reconcile", func(t *testing.T) {
		h := newHarness(t)
		h.dispatch("vermont", models.ActionReboot)
		h.repo.FailOn("ListOpenWindows", errors.New("db down"))
		h.clock.Advance(100 * time.Second) // more than ExpectStaleAfter
		h.tick()                           // the loop is live, only the reconcile is stale

		off := h.deviceOffline("vermont")

		assert.False(t, off.Covered, "[AC-036] a stale tracker never covers an offline transition")
		assert.True(t, errors.Is(off.Err, expect.ErrStale), "[AC-036] ErrStale, got %v", off.Err)
		require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[AC-036] the offline alert fires")
		assert.True(t, h.alerts[0].Notified)

		h.repo.FailOn("ListOpenWindows", nil)
		require.NoError(t, h.tr.Reconcile(h.ctx()))
		h.tick()
		assert.NoError(t, h.tr.Ready(), "[AC-036] the tracker is ready again after one good reconcile")
	})
	t.Run("stalled loop", func(t *testing.T) {
		h := newHarness(t)
		h.dispatch("vermont", models.ActionReboot)
		h.clock.Advance(16 * time.Second) // no completed tick for more than ExpectLoopStaleAfter

		off := h.deviceOffline("vermont")

		assert.False(t, off.Covered)
		assert.True(t, errors.Is(off.Err, expect.ErrLoopStalled), "[SEC-003] ErrLoopStalled, got %v", off.Err)
		require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[SEC-003] the offline alert fires")
		assert.True(t, h.alerts[0].Notified)
	})
}

// Covers AC-033 / NFR-002: a command that is stuck active forever (no sweeper,
// no report) on a device that stays online closes at its hard bound (the FR-022
// check may escalate the container that never came back), and a later death
// alerts: nothing is left "expected".
func TestEX_QA_AC033_StuckActiveCommandOnOnlineDeviceLeavesNothingExpected(t *testing.T) {
	h := newHarness(t)
	c := h.dispatch("hawaii", models.ActionReboot) // 10:00:00, never reported
	require.True(t, h.containerDied("hawaii", "plex").Suppressed, "inside the window the death is covered")

	h.at("10:35:00") // D + 20 min deadline + 15 min return bound
	h.advance(time.Minute)

	assert.NotNil(t, h.command(c.ID).WindowClosedAt, "[AC-033] the window is processed")
	assert.Zero(t, h.tr.Stats().Windows, "[AC-033] nothing is held")
	assert.Nil(t, h.activity("hawaii"), "[AC-033] no label remains")
	assert.False(t, h.containerDied("hawaii", "plex").Suppressed, "[AC-033] a later death alerts")
	assert.False(t, h.deviceOffline("hawaii").Covered, "[AC-033] and so does a later offline transition")
}

// Covers AC-047 / C-8: the automatic scan of TestEX_AC047_NoPowerOrAgentExecution
// takes every file under internal/server/expect/, offline*.go and every test file
// that contains the story marker. The shared test double of the story
// (internal/testutil/mocks_expect.go), the story's production files that sit
// outside that set, and the pre-existing test files the story edited are scanned
// here with the same scanner, so no test or helper can start a process.
func TestEX_QA_AC047_HelpersAndEditedFilesAreScannedToo(t *testing.T) {
	root := repoRoot(t)
	files := []string{
		"internal/testutil/mocks_expect.go",
		"internal/testutil/mocks.go",
		"internal/models/expect.go",
		"internal/server/db/expect_repo.go",
		"internal/server/handlers/activity.go",
		"internal/server/events/generator.go",
		"internal/server/db/command_repo_pg_test.go",
		"internal/server/events/generator_integration_test.go",
		"internal/server/storage/command_budget_pg_test.go",
	}
	var violations []string
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err, "read %s", rel)
		v, err := scanSource(rel, src)
		require.NoError(t, err)
		violations = append(violations, v...)
	}
	assert.Empty(t, violations, "[AC-047] no helper may run a real power action or execute riot-agent:\n%s", strings.Join(violations, "\n"))
}
