package server

import (
	"log/slog"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (TIME-12H-001). These cross the resolver and the formatter:
// the zone that ResolveDisplayZone accepts is the zone every server-written text uses.

// Covers AC-063 and AC-042: a resolved zone formats with the same labels the dashboard shows.
func TestTIME12H_AC063_ResolvedZonesFormatLikeTheDashboard(t *testing.T) {
	at := time.Date(2026, 10, 27, 17, 5, 9, 0, time.UTC)
	cases := map[string]string{
		"America/Sao_Paulo":              "2026-10-27 2:05:09 PM GMT-3",
		"Asia/Kathmandu":                 "2026-10-27 10:50:09 PM GMT+5:45",
		"Asia/Kolkata":                   "2026-10-27 10:35:09 PM IST",
		"Etc/GMT+5":                      "2026-10-27 12:05:09 PM GMT-5",
		"America/Argentina/Buenos_Aires": "2026-10-27 2:05:09 PM GMT-3",
		"UTC":                            "2026-10-27 5:05:09 PM UTC",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			zone, rec := resolve(name, "")
			require.Equal(t, name, zone.Name)
			assert.Empty(t, rec.at(slog.LevelWarn))
			assert.Equal(t, want, models.FormatDisplayFull(at, zone.Location))
		})
	}
}

// Covers AC-062: the DST fall-back repeated hour stays distinguishable in the full form,
// and the spring-forward gap never renders a 2:xx time.
func TestTIME12H_AC062_DSTEdgesInFullForm(t *testing.T) {
	zone, _ := resolve("America/New_York", "")
	for in, want := range map[string]string{
		"2026-11-01T05:30:00Z": "2026-11-01 1:30:00 AM EDT",
		"2026-11-01T06:30:00Z": "2026-11-01 1:30:00 AM EST",
		"2026-03-08T06:59:59Z": "2026-03-08 1:59:59 AM EST",
		"2026-03-08T07:00:00Z": "2026-03-08 3:00:00 AM EDT",
		"2026-10-27T04:00:00Z": "2026-10-27 12:00:00 AM EDT",
		"2026-10-27T16:00:00Z": "2026-10-27 12:00:00 PM EDT",
	} {
		at, err := time.Parse(time.RFC3339, in)
		require.NoError(t, err)
		assert.Equal(t, want, models.FormatDisplayFull(at, zone.Location), in)
	}
}

// Covers AC-046 and bounded growth: a hostile value of any size produces one bounded warning
// and the process falls back to UTC, whatever the byte content.
func TestTIME12H_AC046_AdversarialInputsStayBounded(t *testing.T) {
	inputs := []string{
		strings.Repeat("A/", 5000),
		strings.Repeat("‮", 5000) + "America/New_York",
		"America/New_York\x00",
		"'; DROP TABLE events;--",
		"<script>alert(1)</script>",
		"%2e%2e/%2e%2e/etc/passwd",
		"America//New_York",
		"America/New_York/",
		"/America/New_York",
		"America/New York",
		"\xef\xbb\xbfAmerica/New_York",
	}
	for _, in := range inputs {
		zone, rec := resolve(in, "")
		assert.Equal(t, "UTC", zone.Name, "%.30q must fall back to UTC", in)
		warns := rec.at(slog.LevelWarn)
		require.Len(t, warns, 1, "%.30q must log exactly one warning", in)
		assert.LessOrEqual(t, len([]rune(warns[0].Attrs["value"])), 64)
		assertNoLoadLocationText(t, rec)
	}
}
