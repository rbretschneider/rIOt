package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// timeNow is the example instant of TIME-12H-001: 2026-10-27T17:05:09Z, which is
// 1:05:09 PM EDT in America/New_York.
var timeNow = time.Date(2026, 10, 27, 17, 5, 9, 0, time.UTC)

// datedRebootInput is a host whose pending patch needs a reboot that the window
// will perform: the window starts daily at start and ends two hours later.
func datedRebootInput(start, end string, loc *time.Location) patchBadgeInput {
	in := healthyInput()
	in.OSPatch = models.MaintenanceWindow{Mode: "window", StartTime: start, EndTime: end, CooldownMinutes: 60, RebootClass: "gated"}
	in.Updates.PendingUpdates = 3
	in.Updates.PendingRebootClassCount = 1
	in.UpdatesAt = &timeNow
	in.Zone = loc
	return in
}

func TestTIME12H_AC052_NextWindowLineInDisplayZone(t *testing.T) {
	b := computePatchBadge(datedRebootInput("06:00", "08:00", mustZone(t, "America/New_York")), timeNow)
	assert.Equal(t, "Yes. Next window starts 2026-10-28 2:00 AM EDT.", b.Explanation.Automatic,
		"[AC-052] the next 06:00 UTC window is 2:00 AM in New York")
}

func TestTIME12H_AC053_DatedRebootLineInDisplayZone(t *testing.T) {
	b := computePatchBadge(datedRebootInput("06:00", "08:00", mustZone(t, "America/New_York")), timeNow)
	assert.True(t, strings.HasSuffix(b.Explanation.Reboot, "on 2026-10-28 2:00 AM EDT (estimate)."),
		"[AC-053] the dated reboot line must end with the display-zone minute form, got %q", b.Explanation.Reboot)
	assert.NotContains(t, b.Explanation.Reboot, " UTC (estimate)", "[AC-053] the literal UTC suffix is gone")
}

func TestTIME12H_AC054_LabelDateInDisplayZoneRebootAtUTC(t *testing.T) {
	b := computePatchBadge(datedRebootInput("02:00", "04:00", mustZone(t, "America/New_York")), timeNow)

	assert.Equal(t, badgeAutoRebootDate, b.Kind)
	assert.Equal(t, "Auto (reboot on 2026-10-27)", b.Label,
		"[AC-054] the label date is the display-zone date of 2026-10-28T02:00Z")
	require.NotNil(t, b.RebootAt)

	raw, err := json.Marshal(b)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, "2026-10-28T02:00:00Z", decoded["reboot_at"],
		"[AC-054] reboot_at stays an RFC 3339 UTC instant, never the display zone")
}

func TestTIME12H_AC055_AsOfInDisplayZoneAgeUnchanged(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	for _, tc := range []struct {
		name string
		now  time.Time
		want string
	}{
		{"just now", timeNow, "2026-10-27 1:05 PM EDT (just now)"},
		{"three hours later", timeNow.Add(3 * time.Hour), "2026-10-27 1:05 PM EDT (3 h ago)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := computePatchBadge(datedRebootInput("06:00", "08:00", ny), tc.now)
			assert.Equal(t, tc.want, b.Explanation.AsOf, "[AC-055] as-of line")
		})
	}
}

func TestTIME12H_AC056_UTCDisplayZoneKeepsTwelveHour(t *testing.T) {
	b := computePatchBadge(datedRebootInput("06:00", "08:00", time.UTC), timeNow)
	assert.Equal(t, "Yes. Next window starts 2026-10-28 6:00 AM UTC.", b.Explanation.Automatic)
}

func TestTIME12H_AC056_NilZoneIsUTC(t *testing.T) {
	b := computePatchBadge(datedRebootInput("06:00", "08:00", nil), timeNow)
	assert.Equal(t, "Yes. Next window starts 2026-10-28 6:00 AM UTC.", b.Explanation.Automatic)
}

var rfc3339Value = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

// collectTimes gathers every string in a decoded JSON value that looks like an
// RFC 3339 instant, keyed by its JSON path.
func collectTimes(path string, v any, out map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			collectTimes(path+"."+k, child, out)
		}
	case []any:
		for _, child := range x {
			collectTimes(path+"[]", child, out)
		}
	case string:
		if rfc3339Value.MatchString(x) {
			out[path] = x
		}
	}
}

func TestTIME12H_AC065_PatchStatusTimeFieldsStayUTC(t *testing.T) {
	f := newBadgeFixture(t, false)
	f.h.displayZone = mustZone(t, "America/New_York")
	// A host with a dated automatic reboot, so reboot_at is present.
	f.tel.Snapshots["dev-pending"][0].Data.Updates.PendingRebootClassCount = 1

	req := httptest.NewRequest("GET", "/api/v1/fleet/patch-status?all=true", nil)
	rec := httptest.NewRecorder()
	f.h.PatchStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var decoded any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decoded))
	times := map[string]string{}
	collectTimes("$", decoded, times)

	paths := []string{}
	for path, value := range times {
		paths = append(paths, path)
		assert.True(t, strings.HasSuffix(value, "Z"), "[AC-065] %s = %q must be RFC 3339 UTC (suffix Z)", path, value)
	}
	assert.Contains(t, paths, "$[].patch_badge.reboot_at", "the fixture must exercise reboot_at")
	assert.Contains(t, paths, "$[].next_window_start", "the fixture must exercise next_window_start")
	assert.Contains(t, paths, "$[].updates_at", "the fixture must exercise updates_at")
}

// BR-003: the maintenance-window math is UTC whatever the process zone and the
// display zone are. Not parallel: time.Local is process-wide and is restored in
// t.Cleanup.
func TestTIME12H_AC066_MaintenanceWindowIgnoresProcessAndDisplayZone(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })

	window := models.MaintenanceWindow{Mode: "window", StartTime: "23:30", EndTime: "01:30", CooldownMinutes: 60}
	instants := []time.Time{
		time.Date(2026, 10, 27, 23, 15, 0, 0, time.UTC),
		time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC),
		time.Date(2026, 10, 28, 0, 45, 0, 0, time.UTC),
		time.Date(2026, 10, 28, 1, 30, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 5, 59, 59, 0, time.UTC),
	}
	type result struct {
		in    bool
		next  time.Time
		found bool
	}
	compute := func(at time.Time) result {
		next, ok := nextMaintenanceWindowStart(window, at)
		return result{in: inMaintenanceWindow(window, at), next: next, found: ok}
	}
	want := make([]result, len(instants))
	for i, at := range instants {
		want[i] = compute(at)
	}

	time.Local = mustZone(t, "Pacific/Kiritimati")
	for i, at := range instants {
		got := compute(at.In(time.Local))
		assert.Equal(t, want[i].in, got.in, "[AC-066] inMaintenanceWindow(%s) under process zone Kiritimati", at)
		assert.True(t, want[i].next.Equal(got.next), "[AC-066] nextMaintenanceWindowStart(%s) = %s, want %s", at, got.next, want[i].next)
		assert.Equal(t, want[i].found, got.found, "[AC-066] next-window found flag for %s", at)
	}

	// The display zone only changes the words: the badge's instant is the same.
	bUTC := computePatchBadge(datedRebootInput("23:30", "01:30", time.UTC), timeNow)
	bNY := computePatchBadge(datedRebootInput("23:30", "01:30", mustZone(t, "America/New_York")), timeNow)
	require.NotNil(t, bUTC.RebootAt)
	require.NotNil(t, bNY.RebootAt)
	assert.True(t, bUTC.RebootAt.Equal(*bNY.RebootAt), "[AC-066] reboot_at must not depend on the display zone: %s vs %s", bUTC.RebootAt, bNY.RebootAt)
	assert.Equal(t, bUTC.Kind, bNY.Kind)
}
