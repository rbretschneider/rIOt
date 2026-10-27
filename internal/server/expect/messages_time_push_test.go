package expect_test

import (
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err, "load America/New_York")
	return loc
}

// The held summary is the one expected-activity message that becomes a push
// (TIME-12H-001 AD-016): the pushed event carries the labelled time unchanged.
func TestTIME12H_AC064_HeldSummaryPushCarriesLabelledTime(t *testing.T) {
	h := newHarness(t, withDisplayZone(newYork(t)))
	escalationBatch(t, h, "10:00:00")
	h.at("11:03:00")

	const want = "7 escalation notifications for hawaii were held since 6:02 AM EDT; see Alerts"
	sum := summaries(h)
	require.Len(t, sum, 1, "one summary event is stored")
	assert.Equal(t, want, sum[0].Message, "the stored summary names the held time in the display zone")
	pushes := h.esc.Pushes()
	require.Len(t, pushes, 6, "5 escalation pushes plus the summary push")
	assert.Equal(t, models.EventExpectNotificationsHeld, pushes[5].Event.Type)
	assert.Equal(t, want, pushes[5].Event.Message, "[AC-064] the pushed event carries the labelled time unchanged")
}

func TestTIME12H_AC059_TrackerStampsDisplayZoneOnPoweredOff(t *testing.T) {
	h := newHarness(t, withDisplayZone(newYork(t)))
	shutdownPoweredOff(t, h)

	evts := h.eventsOf(models.EventDeviceOffline, models.CommandAttrExpected)
	require.Len(t, evts, 1)
	assert.Equal(t, "Device PiCUPS powered off by ryan at 6:02 AM EDT", evts[0].Message,
		"[AC-059] the tracker hands its display zone to the coverage it returns")
}
