package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poweredOffRig returns a rig whose oracle answers a powered-off coverage whose
// message is written in America/New_York, with a notifying device_offline rule
// present so a push would happen if the path dispatched.
func poweredOffRig(t *testing.T) *expectRig {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err, "load America/New_York")
	r := newExpectRig(t)
	r.addRule("device_offline", 300, true, "")
	r.oracle.off, r.oracle.offOK = models.OfflineCoverage{
		Kind: models.OfflineKindPoweredOff, CommandID: "cmd-bbbbbbbb-2222", ActorPhrase: "by ryan",
		ShutdownAt: time.Date(2026, 10, 27, 17, 5, 9, 0, time.UTC), Zone: ny,
	}, true
	return r
}

// R-1 / AD-016: powered-off and expected-offline events are stored and
// broadcast but never pushed. The labelled time is checked on the stored event.
func TestTIME12H_AC064_PoweredOffEventLabelledAndNeverPushed(t *testing.T) {
	r := poweredOffRig(t)

	r.gen.DeviceOfflineAt(context.Background(), "hawaii", "hawaii", nil)

	evts := r.stored()
	require.Len(t, evts, 1, "one event is stored")
	assert.Equal(t, "Device hawaii powered off by ryan at 1:05 PM EDT", evts[0].Message,
		"[AC-064] the stored message carries the labelled display-zone time")
	assert.Empty(t, r.disp.Alerts, "[AC-064] the powered-off event is still not dispatched (EXPECT-001 AD-005)")
}

func TestTIME12H_AC064_ExpectedOfflineEventLabelledAndNeverPushed(t *testing.T) {
	r := poweredOffRig(t)
	back := time.Date(2026, 10, 27, 17, 20, 0, 0, time.UTC)
	r.oracle.off = models.OfflineCoverage{
		Kind: models.OfflineKindWindow, CommandID: "cmd-aaaaaaaa-1111", ActionLabel: "Reboot", ActorPhrase: "by ryan",
		BackBy: &back, Zone: r.oracle.off.Zone,
	}

	r.gen.DeviceOfflineAt(context.Background(), "hawaii", "hawaii", nil)

	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, "Device hawaii went offline during Reboot by ryan (expected, back by 1:20 PM EDT)", evts[0].Message)
	assert.Empty(t, r.disp.Alerts, "[AC-064] the expected-offline event is not dispatched")
}

func TestTIME12H_AC065_StoredEventCreatedAtIsUTC(t *testing.T) {
	r := poweredOffRig(t)

	r.gen.DeviceOfflineAt(context.Background(), "hawaii", "hawaii", nil)

	evts := r.stored()
	require.Len(t, evts, 1)
	_, offset := evts[0].CreatedAt.Zone()
	assert.Equal(t, 0, offset, "[AC-065] created_at keeps a zero offset whatever the display zone")
	assert.True(t, evts[0].CreatedAt.Equal(expectBase), "[AC-065] created_at is the generator clock instant, got %v", evts[0].CreatedAt)
	raw, err := json.Marshal(evts[0].CreatedAt)
	require.NoError(t, err)
	assert.Equal(t, `"2026-10-26T10:00:00Z"`, string(raw), "[AC-065] created_at serialises as RFC 3339 UTC")
}
