package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentVersionSummary(t *testing.T) {
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", AgentVersion: "1.0.0"}
	deviceRepo.Devices["dev-2"] = &models.Device{ID: "dev-2", AgentVersion: "1.0.0"}
	deviceRepo.Devices["dev-3"] = &models.Device{ID: "dev-3", AgentVersion: "1.1.0"}

	h := &Handlers{devices: deviceRepo}

	req := httptest.NewRequest("GET", "/api/v1/fleet/agent-versions", nil)
	rec := httptest.NewRecorder()
	h.AgentVersionSummary(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var versions []db.AgentVersionCount
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&versions))
	assert.NotEmpty(t, versions)

	// Check that we got back both versions
	versionMap := make(map[string]int)
	for _, v := range versions {
		versionMap[v.Version] = v.Count
	}
	assert.Equal(t, 2, versionMap["1.0.0"])
	assert.Equal(t, 1, versionMap["1.1.0"])
}

func TestAgentVersionSummary_Empty(t *testing.T) {
	deviceRepo := testutil.NewMockDeviceRepo()
	h := &Handlers{devices: deviceRepo}

	req := httptest.NewRequest("GET", "/api/v1/fleet/agent-versions", nil)
	rec := httptest.NewRecorder()
	h.AgentVersionSummary(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// [AC-021] PatchStatus rows carry the reboot-class count and
// reboot-required flag from the latest telemetry summaries.
func TestPatchStatus_AC021_RebootClassFields(t *testing.T) {
	telRepo := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "gpu-host"}
	deviceRepo.Devices["dev-2"] = &models.Device{ID: "dev-2", Hostname: "plain-host"}

	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Updates: &models.UpdateInfo{
				PendingUpdates:          5,
				PendingSecurityCount:    1,
				PendingRebootClassCount: 2,
				RebootRequired:          true,
			},
		},
	}}
	telRepo.Snapshots["dev-2"] = []models.TelemetrySnapshot{{
		DeviceID: "dev-2",
		Data: models.FullTelemetryData{
			// Pre-PATCH-GATE agent: new fields absent → zero values.
			Updates: &models.UpdateInfo{PendingUpdates: 1},
		},
	}}

	h := &Handlers{telemetry: telRepo, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/fleet/patch-status", nil)
	rec := httptest.NewRecorder()
	h.PatchStatus(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var rows []struct {
		DeviceID         string `json:"device_id"`
		RebootClassCount int    `json:"reboot_class_count"`
		RebootRequired   bool   `json:"reboot_required"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&rows))
	require.Len(t, rows, 2)

	byID := map[string]int{}
	for i, r := range rows {
		byID[r.DeviceID] = i
	}
	gpu := rows[byID["dev-1"]]
	assert.Equal(t, 2, gpu.RebootClassCount, "[AC-021] reboot-class count populated from summaries")
	assert.True(t, gpu.RebootRequired, "[AC-021] reboot-required flag surfaced at fleet level")

	plain := rows[byID["dev-2"]]
	assert.Equal(t, 0, plain.RebootClassCount, "[AC-021] absent fields decode to zero values")
	assert.False(t, plain.RebootRequired)
}
