package summary

import (
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func zoneFor(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err, "load zone %s", name)
	return loc
}

func TestTIME12H_AC057_GeneratedInDisplayZone(t *testing.T) {
	out, err := Render(fullDeviceFixture(), fullTelemetryFixture(), fixedGenerated, zoneFor(t, "America/New_York"))
	require.NoError(t, err)
	assert.Contains(t, out, "**Generated:** 2026-10-27 1:05:09 PM EDT\n",
		"[AC-057] Generated must be the full 12-hour form in the display zone with its label")
	assert.NotContains(t, out, "17:05:09", "[AC-057] the old 24-hour UTC time must be gone")
}

func TestTIME12H_AC057_NilZoneIsUTC(t *testing.T) {
	out, err := Render(fullDeviceFixture(), fullTelemetryFixture(), fixedGenerated, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "**Generated:** 2026-10-27 5:05:09 PM UTC\n",
		"[AC-057] a nil zone renders UTC in 12-hour form")
}

func TestTIME12H_AC063_NumericAbbreviationsNormalised(t *testing.T) {
	cases := []struct {
		zone string
		want string
	}{
		{"America/Sao_Paulo", "2026-10-27 2:05:09 PM GMT-3"},
		{"Asia/Kathmandu", "2026-10-27 10:50:09 PM GMT+5:45"},
		{"Asia/Kolkata", "2026-10-27 10:35:09 PM IST"},
	}
	for _, tc := range cases {
		t.Run(tc.zone, func(t *testing.T) {
			out, err := Render(fullDeviceFixture(), fullTelemetryFixture(), fixedGenerated, zoneFor(t, tc.zone))
			require.NoError(t, err)
			assert.Contains(t, out, "**Generated:** "+tc.want+"\n",
				"[AC-063] Generated in %s must read %q", tc.zone, tc.want)
		})
	}
}
