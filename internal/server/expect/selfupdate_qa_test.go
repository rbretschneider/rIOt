package expect_test

import (
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (EXPECT-002). Edge probes that the story's own tests only
// reach incidentally. They use the EXPECT-001 harness: a fake clock, no sleeps.

// Covers AC-001, FR-012, NFR-002: coverage is strict at the bound. An offline
// dated 1 ms before the bound is covered, one dated exactly at the bound is not
// (it alerts), so no window ever reaches 10 minutes plus one tick.
func TestEX2_QA_FR012_CoverageStrictAtTheBound(t *testing.T) {
	just := newHarness(t)
	just.at("10:00:00")
	just.selfStart("hawaii")
	just.at("10:09:59")
	assert.True(t, just.deviceOffline("hawaii", baseTime.Add(10*time.Minute-time.Millisecond)).Covered,
		"[FR-012] an offline 1 ms before the bound is covered")

	at := newHarness(t)
	at.at("10:00:00")
	at.selfStart("hawaii")
	at.at("10:10:00")
	res := at.deviceOffline("hawaii", baseTime.Add(10*time.Minute))
	assert.False(t, res.Covered, "[FR-012] an offline dated exactly at the bound is not covered")
	offs := at.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1, "and it alerts")
	assert.True(t, offs[0].Notified)
}

// Covers FR-006, FR-013: a second start inside the 10 minutes of a record that is
// already verifying (the device came back, the outcome is pending) is absorbed by
// that record. It creates no record and extends nothing, so an offline that
// follows alerts. This pins the accepted behaviour (it fails toward alerting): two
// back-to-back releases are only quiet for the first.
func TestEX2_QA_FR006_SecondStartWhileVerifyingIsAbsorbedAndNotSuppressed(t *testing.T) {
	h := newHarness(t)
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:02:00")
	h.registerOn("hawaii", "2.80.0")
	require.Equal(t, "verifying", h.command(rec.ID).Status)

	h.at("10:03:00")
	link := h.selfStartVersions("hawaii", "2.80.0", "2.81.0")
	assert.Equal(t, rec.ID, link.CommandID, "[FR-006] the start is linked to the open (verifying) record")
	assert.Len(t, h.selfRecords("hawaii"), 1, "no second record")

	h.at("10:04:00")
	assert.False(t, h.deviceOffline("hawaii").Covered, "[FR-013] the window closed at the registration: the next offline is not covered")
	offs := h.alertsOf(models.EventDeviceOffline)
	require.Len(t, offs, 1)
	assert.True(t, offs[0].Notified, "and it notifies")
}

// Covers NFR-010, AD-012: a hostname with control, bidi and format characters
// never reaches a server-written message or an escalation, whichever phase
// writes it.
func TestEX2_QA_NFR010_HostileHostnameNeverReachesMessages(t *testing.T) {
	h := newHarness(t)
	h.devices.Devices["hawaii"].Hostname = "ev‮il\x00ho​st\nx"
	h.at("10:00:00")
	h.selfStart("hawaii")
	h.at("10:01:00")
	require.True(t, h.deviceOffline("hawaii").Covered)
	h.at("10:10:00")

	require.Len(t, h.escalationsOf(models.EventDeviceOffline), 1)
	checked := 0
	for _, e := range h.allEvents() {
		if e.Type == models.EventAgentUpdateStarted {
			continue // the agent's own text is stored as sent (sanitized at insert by the real repo)
		}
		checked++
		for _, r := range e.Message {
			assert.NotContains(t, []rune{0, '‮', '​', '\n'}, r, "event %s carries a hostile rune: %q", e.Type, e.Message)
		}
		assert.Contains(t, e.Message, "evilhostx", "the hostname is shown, cleaned")
	}
	assert.GreaterOrEqual(t, checked, 2, "at least the suppressed offline and the escalation were checked")
}

// Covers FR-015, X-3: a record whose registration was lost, with the device online
// at the bound and a heartbeat that reports an OLD version, is "not confirmed"
// (one warning, no push), not silently succeeded.
func TestEX2_QA_X3_OldVersionHeartbeatAtBoundIsNotConfirmed(t *testing.T) {
	h := newHarness(t, withDevices(func(h *harness) { h.devices.Devices["hawaii"].AgentVersion = "2.78.0" }))
	h.at("10:00:00")
	h.selfStart("hawaii")
	rec := h.selfRecord("hawaii")
	h.at("10:05:00")
	h.heartbeatOn("hawaii", "2.78.0")
	h.at("10:09:50")
	h.heartbeatOn("hawaii", "2.78.0")
	h.at("10:10:00")

	assert.Equal(t, "timed_out", h.command(rec.ID).Status)
	assert.Equal(t, models.SelfUpdateFailureUnconfirmedOnline, h.command(rec.ID).FailureStep)
	require.Len(t, h.eventsLinked(models.EventAgentSelfUpdateUnconfirmed, rec.ID), 1)
	requireNoAlerts(t, h, "no notification")
}
