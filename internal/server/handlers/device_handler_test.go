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
	var devices []models.Device
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&devices))
	assert.Len(t, devices, 2)
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
