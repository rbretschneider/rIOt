package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// REBOOT-ETA-001: PatchStatus carries the additive fields the reboot-ETA
// tooltip needs. Time-of-day assertions use a daily window so they hold
// regardless of when the suite runs.

// patchStatusHandler builds Handlers with osPatch saved as the automation
// config (when saveConfig) and one reboot-required device whose agent reports
// the given allow_reboot (nil = the agent does not report it).
func patchStatusHandler(t *testing.T, osPatch models.MaintenanceWindow, saveConfig bool, allowReboot *bool) *Handlers {
	t.Helper()
	adminRepo := testutil.NewMockAdminRepo("hash")
	if saveConfig {
		raw, err := json.Marshal(models.AutomationConfig{OSPatch: osPatch})
		require.NoError(t, err)
		require.NoError(t, adminRepo.SetConfig(nil, "automation_config", string(raw)))
	}

	telRepo := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host-1"}
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Updates: &models.UpdateInfo{PendingUpdates: 2, RebootRequired: true, AllowReboot: allowReboot},
		},
	}}
	return &Handlers{telemetry: telRepo, devices: deviceRepo, adminRepo: adminRepo}
}

type etaRow struct {
	DeviceID        string     `json:"device_id"`
	RebootRequired  bool       `json:"reboot_required"`
	OSPatchMode     string     `json:"os_patch_mode"`
	NextWindowStart *time.Time `json:"next_window_start"`
	CooldownMinutes int        `json:"os_patch_cooldown_minutes"`
	AllowReboot     *bool      `json:"allow_reboot"`
}

func getPatchStatusRows(t *testing.T, h *Handlers, query string) ([]etaRow, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/fleet/patch-status"+query, nil)
	rec := httptest.NewRecorder()
	h.PatchStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	var rows []etaRow
	require.NoError(t, json.Unmarshal([]byte(body), &rows), "response must stay a top-level JSON array")
	return rows, body
}

func boolPtr(b bool) *bool { return &b }

// [AC-006] Window mode: the row carries next_window_start at the configured
// UTC start time-of-day, not in the past.
func TestPatchStatus_AC006_WindowModeCarriesNextWindowStart(t *testing.T) {
	h := patchStatusHandler(t, models.MaintenanceWindow{
		Mode: "window", StartTime: "03:15", EndTime: "05:00", CooldownMinutes: 90, Frequency: "daily",
	}, true, boolPtr(true))

	before := time.Now().UTC().Add(-time.Second)
	rows, _ := getPatchStatusRows(t, h, "")

	require.Len(t, rows, 1)
	assert.Equal(t, "window", rows[0].OSPatchMode, "[AC-006] mode must be surfaced")
	require.NotNil(t, rows[0].NextWindowStart, "[AC-006] window mode with a valid start must carry next_window_start")
	ns := rows[0].NextWindowStart.UTC()
	assert.Equal(t, 3, ns.Hour(), "[AC-006] next start hour must be the configured UTC hour")
	assert.Equal(t, 15, ns.Minute(), "[AC-006] next start minute must be the configured UTC minute")
	assert.False(t, ns.Before(before), "[AC-006] next start must not be in the past")
}

// A fully-patched device awaiting only a reboot (PendingUpdates == 0,
// RebootRequired) must still be returned, or the fleet reboot indicator and its
// ETA tooltip never render. Regression for the previous PendingUpdates > 0
// filter that dropped it (REBOOT-ETA-001 follow-up).
func TestPatchStatus_RebootOnlyDeviceIsIncluded(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	raw, err := json.Marshal(models.AutomationConfig{OSPatch: models.MaintenanceWindow{
		Mode: "window", StartTime: "02:00", EndTime: "03:30", CooldownMinutes: 360, Frequency: "daily",
	}})
	require.NoError(t, err)
	require.NoError(t, adminRepo.SetConfig(nil, "automation_config", string(raw)))

	telRepo := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host-1"}
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Updates: &models.UpdateInfo{PendingUpdates: 0, RebootRequired: true, AllowReboot: boolPtr(true)},
		},
	}}
	h := &Handlers{telemetry: telRepo, devices: deviceRepo, adminRepo: adminRepo}

	rows, _ := getPatchStatusRows(t, h, "")
	require.Len(t, rows, 1, "a reboot-only device must still be returned")
	assert.True(t, rows[0].RebootRequired, "the row must carry reboot_required so the indicator renders")
}

// [AC-007] Anytime mode: mode + cooldown surfaced, no clock time.
func TestPatchStatus_AC007_AnytimeModeCarriesCooldownNoStart(t *testing.T) {
	h := patchStatusHandler(t, models.MaintenanceWindow{
		Mode: "anytime", StartTime: "03:00", EndTime: "05:00", CooldownMinutes: 120,
	}, true, boolPtr(true))

	rows, body := getPatchStatusRows(t, h, "")

	require.Len(t, rows, 1)
	assert.Equal(t, "anytime", rows[0].OSPatchMode, "[AC-007] mode must be surfaced")
	assert.Equal(t, 120, rows[0].CooldownMinutes, "[AC-007] cooldown must be surfaced for the anytime wording")
	assert.Nil(t, rows[0].NextWindowStart, "[AC-007] anytime mode must not carry a clock time")
	assert.NotContains(t, body, "next_window_start", "[AC-007] next_window_start must be omitted, not null")
}

// [AC-008] Disabled mode: no estimated time on the wire.
func TestPatchStatus_AC008_DisabledModeOmitsNextWindowStart(t *testing.T) {
	h := patchStatusHandler(t, models.MaintenanceWindow{
		Mode: "disabled", StartTime: "03:00", EndTime: "05:00", CooldownMinutes: 60,
	}, true, boolPtr(true))

	rows, body := getPatchStatusRows(t, h, "")

	require.Len(t, rows, 1)
	assert.Equal(t, "disabled", rows[0].OSPatchMode, "[AC-008] mode must be surfaced")
	assert.NotContains(t, body, "next_window_start", "[AC-008] disabled mode must carry no time")
}

// [AC-009] No window configured (nothing saved): defaults to disabled, no time.
func TestPatchStatus_AC009_DefaultConfigOmitsNextWindowStart(t *testing.T) {
	h := patchStatusHandler(t, models.MaintenanceWindow{}, false, nil)

	rows, body := getPatchStatusRows(t, h, "")

	require.Len(t, rows, 1)
	assert.Equal(t, "disabled", rows[0].OSPatchMode, "[AC-009] the default config is disabled")
	assert.NotContains(t, body, "next_window_start", "[AC-009] default config must carry no time")
}

// [AC-012] allow_reboot tri-state is passed through to the row exactly as reported.
func TestPatchStatus_AC012_AllowRebootTriState(t *testing.T) {
	win := models.MaintenanceWindow{Mode: "window", StartTime: "03:00", EndTime: "05:00", CooldownMinutes: 60}

	t.Run("false is reported as false", func(t *testing.T) {
		rows, _ := getPatchStatusRows(t, patchStatusHandler(t, win, true, boolPtr(false)), "")
		require.Len(t, rows, 1)
		require.NotNil(t, rows[0].AllowReboot, "[AC-012] an explicit false must not be dropped")
		assert.False(t, *rows[0].AllowReboot, "[AC-012] allow_reboot=false must reach the row")
	})
	t.Run("true is reported as true", func(t *testing.T) {
		rows, _ := getPatchStatusRows(t, patchStatusHandler(t, win, true, boolPtr(true)), "")
		require.Len(t, rows, 1)
		require.NotNil(t, rows[0].AllowReboot)
		assert.True(t, *rows[0].AllowReboot, "[AC-012] allow_reboot=true must reach the row")
	})
	t.Run("unreported is omitted", func(t *testing.T) {
		rows, body := getPatchStatusRows(t, patchStatusHandler(t, win, true, nil), "")
		require.Len(t, rows, 1)
		assert.Nil(t, rows[0].AllowReboot, "[AC-012] a pre-upgrade agent must read as unknown (nil)")
		assert.NotContains(t, body, "allow_reboot", "[AC-012] unreported must be omitted from the wire")
	})
}

// [AC-019] Window mode with an unusable start: next_window_start omitted.
func TestPatchStatus_AC019_UnusableStartOmitsNextWindowStart(t *testing.T) {
	h := patchStatusHandler(t, models.MaintenanceWindow{
		Mode: "window", StartTime: "not-a-time", EndTime: "05:00", CooldownMinutes: 60,
	}, true, boolPtr(true))

	rows, body := getPatchStatusRows(t, h, "")

	require.Len(t, rows, 1)
	assert.Equal(t, "window", rows[0].OSPatchMode, "[AC-019] mode still surfaced")
	assert.NotContains(t, body, "next_window_start", "[AC-019] not-found must omit the field, never emit a partial or zero time")
}

// [AC-020] The contract stays a backward-compatible array with the existing
// fields intact, and detail mode carries the same additive fields.
func TestPatchStatus_AC020_AdditiveContractInBothModes(t *testing.T) {
	h := patchStatusHandler(t, models.MaintenanceWindow{
		Mode: "window", StartTime: "03:00", EndTime: "05:00", CooldownMinutes: 60,
	}, true, boolPtr(true))

	plain, plainBody := getPatchStatusRows(t, h, "")
	detail, _ := getPatchStatusRows(t, h, "?detail=true")

	require.Len(t, plain, 1)
	require.Len(t, detail, 1)
	assert.Equal(t, "dev-1", plain[0].DeviceID, "[AC-020] existing device_id unchanged")
	assert.True(t, plain[0].RebootRequired, "[AC-020] existing reboot_required unchanged")
	assert.Contains(t, plainBody, `"pending_updates"`, "[AC-020] existing keys still present")
	require.NotNil(t, plain[0].NextWindowStart, "[AC-020] plain mode carries next_window_start")
	require.NotNil(t, detail[0].NextWindowStart, "[AC-020] detail mode must carry the same additive fields (FR-005)")
	assert.Equal(t, plain[0].OSPatchMode, detail[0].OSPatchMode, "[AC-020] os_patch_mode identical in both modes")
	assert.Equal(t, plain[0].CooldownMinutes, detail[0].CooldownMinutes, "[AC-020] cooldown identical in both modes")
	require.NotNil(t, plain[0].AllowReboot)
	require.NotNil(t, detail[0].AllowReboot)
	assert.Equal(t, *plain[0].AllowReboot, *detail[0].AllowReboot, "[AC-020] allow_reboot identical in both modes")
}
