package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDeviceTestHandlers(t *testing.T) (*Handlers, *testutil.MockDeviceRepo) {
	t.Helper()
	deviceRepo := testutil.NewMockDeviceRepo()
	hub := websocket.NewHub()
	go hub.Run()
	h := &Handlers{
		devices: deviceRepo,
		hub:     hub,
	}
	return h, deviceRepo
}

func TestListDevices_Empty(t *testing.T) {
	h, _ := newDeviceTestHandlers(t)

	req := httptest.NewRequest("GET", "/api/v1/devices", nil)
	rec := httptest.NewRecorder()

	h.ListDevices(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var devices []models.Device
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&devices))
	assert.Empty(t, devices)
}

func TestListDevices_Populated(t *testing.T) {
	h, repo := newDeviceTestHandlers(t)
	repo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host-a", Status: models.DeviceStatusOnline}
	repo.Devices["dev-2"] = &models.Device{ID: "dev-2", Hostname: "host-b", Status: models.DeviceStatusOffline}

	req := httptest.NewRequest("GET", "/api/v1/devices", nil)
	rec := httptest.NewRecorder()

	h.ListDevices(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var devices []map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&devices))
	assert.Len(t, devices, 2)
	// Each device should include agent_connected field
	for _, d := range devices {
		_, ok := d["agent_connected"]
		assert.True(t, ok, "expected agent_connected field in device response")
	}
}

func TestGetDevice_Found(t *testing.T) {
	h, repo := newDeviceTestHandlers(t)
	h.telemetry = testutil.NewMockTelemetryRepo()
	repo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "test-host"}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}", h.GetDevice)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	device := resp["device"].(map[string]interface{})
	assert.Equal(t, "test-host", device["hostname"])
	// Verify agent_connected field is present
	agentConn, ok := resp["agent_connected"]
	assert.True(t, ok, "expected agent_connected field in response")
	assert.Equal(t, false, agentConn) // no WS connection in tests
}

func TestGetDevice_NotFound(t *testing.T) {
	h, _ := newDeviceTestHandlers(t)

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}", h.GetDevice)

	req := httptest.NewRequest("GET", "/api/v1/devices/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteDevice(t *testing.T) {
	h, repo := newDeviceTestHandlers(t)
	repo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "doomed"}

	r := chi.NewRouter()
	r.Delete("/api/v1/devices/{id}", h.DeleteDevice)

	req := httptest.NewRequest("DELETE", "/api/v1/devices/dev-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, repo.Devices)
}

// Added by QA Engineer
// Covers AC-013: GPU telemetry stored in telemetry snapshot must appear under the
// "gpu_telemetry" key in the GET /api/v1/devices/{id} response JSON.
func TestGetDevice_GPUTelemetryInResponse(t *testing.T) {
	h, repo := newDeviceTestHandlers(t)
	telRepo := testutil.NewMockTelemetryRepo()
	h.telemetry = telRepo

	temp := 72
	power := 285.5
	snap := models.TelemetrySnapshot{
		ID:       1,
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			GPUTelemetry: &models.GPUTelemetry{
				GPUs: []models.GPUDeviceMetrics{
					{
						Index:        0,
						Name:         "NVIDIA GeForce RTX 3090",
						UUID:         "GPU-aaaa-bbbb-cccc-dddd",
						PCIBusID:     "00000000:01:00.0",
						TemperatureC: &temp,
						PowerDrawW:   &power,
					},
				},
			},
		},
	}
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{snap}
	repo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "gpu-host"}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}", h.GetDevice)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	// AC-013: the response must include latest_telemetry with gpu_telemetry key
	latestTel, ok := resp["latest_telemetry"].(map[string]interface{})
	require.True(t, ok, "[AC-013] response must contain latest_telemetry object")

	data, ok := latestTel["data"].(map[string]interface{})
	require.True(t, ok, "[AC-013] latest_telemetry must contain data object")

	gpuTel, ok := data["gpu_telemetry"].(map[string]interface{})
	require.True(t, ok, "[AC-013] data must contain gpu_telemetry key")

	gpus, ok := gpuTel["gpus"].([]interface{})
	require.True(t, ok, "[AC-013] gpu_telemetry must contain gpus array")
	require.Len(t, gpus, 1, "[AC-013] gpus array must contain exactly one entry")

	gpu := gpus[0].(map[string]interface{})
	assert.Equal(t, "NVIDIA GeForce RTX 3090", gpu["name"], "[AC-013] GPU name must match")
	assert.Equal(t, float64(0), gpu["index"], "[AC-013] GPU index must be 0")
}

func TestSummary(t *testing.T) {
	h, repo := newDeviceTestHandlers(t)
	repo.Devices["dev-1"] = &models.Device{ID: "dev-1", Status: models.DeviceStatusOnline}
	repo.Devices["dev-2"] = &models.Device{ID: "dev-2", Status: models.DeviceStatusOffline}

	req := httptest.NewRequest("GET", "/api/v1/summary", nil)
	rec := httptest.NewRecorder()

	h.Summary(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var summary models.FleetSummary
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&summary))
	assert.Equal(t, 2, summary.TotalDevices)
	assert.Equal(t, 1, summary.OnlineCount)
	assert.Equal(t, 1, summary.OfflineCount)
}
