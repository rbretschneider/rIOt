package handlers

import (
	"net/http"
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err, "load zone %s", name)
	return loc
}

// summaryTimeHandlers returns summary handlers whose clock is fixed to now and
// whose display zone is loc (nil means UTC), with the device web-01 seeded.
func summaryTimeHandlers(t *testing.T, now time.Time, loc *time.Location) *Handlers {
	t.Helper()
	h, deviceRepo, telRepo := newSummaryHandlers(t)
	h.now = func() time.Time { return now }
	h.displayZone = loc
	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", ShortID: "abc12345", Hostname: "web-01", Arch: "amd64"}
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{{
		ID: 1, DeviceID: "dev-1", Timestamp: now,
		Data: models.FullTelemetryData{System: &models.SystemInfo{Hostname: "web-01", Arch: "amd64"}},
	}}
	return h
}

func TestTIME12H_AC058_FilenameUsesDisplayZoneDate(t *testing.T) {
	// 02:00 UTC on the 28th is still the evening of the 27th in New York.
	now := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)
	h := summaryTimeHandlers(t, now, mustZone(t, "America/New_York"))

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-1/summary")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, `attachment; filename="web-01-summary-2026-10-27.md"`, rec.Header().Get("Content-Disposition"),
		"[AC-058] the filename date is the display-zone date, not the UTC date 2026-10-28")
	assert.Contains(t, rec.Body.String(), "**Generated:** 2026-10-27 10:00:00 PM EDT",
		"[AC-058] the Generated line in the file uses the same instant and zone as the filename")
}

func TestTIME12H_AC058_FilenameWithoutZoneUsesUTCDate(t *testing.T) {
	now := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)
	h := summaryTimeHandlers(t, now, nil)

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-1/summary")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, `attachment; filename="web-01-summary-2026-10-28.md"`, rec.Header().Get("Content-Disposition"),
		"[AC-058] an unconfigured server names the file by the UTC date")
	assert.Contains(t, rec.Body.String(), "**Generated:** 2026-10-28 2:00:00 AM UTC")
}
