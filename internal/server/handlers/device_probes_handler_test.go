package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newListAllHandlers(t *testing.T) (*Handlers, *testutil.MockDeviceProbeRepo, *testutil.MockDeviceRepo) {
	t.Helper()
	deviceProbeRepo := testutil.NewMockDeviceProbeRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	h := &Handlers{
		deviceProbeRepo: deviceProbeRepo,
		devices:         deviceRepo,
	}
	return h, deviceProbeRepo, deviceRepo
}

// [AC-004] ListAllDeviceProbes returns enriched probes with device hostname
func TestListAllDeviceProbes_ReturnsEnrichedProbesWithHostname(t *testing.T) {
	h, probeRepo, deviceRepo := newListAllHandlers(t)

	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "web-server-01"}
	deviceRepo.Devices["dev-2"] = &models.Device{ID: "dev-2", Hostname: "db-server-01"}

	probeRepo.Probes = []models.DeviceProbe{
		{ID: 1, Name: "Check nginx", DeviceID: "dev-1", Type: "shell", Enabled: true, IntervalSeconds: 60},
		{ID: 2, Name: "Check postgres", DeviceID: "dev-2", Type: "port", Enabled: true, IntervalSeconds: 30},
	}

	req := httptest.NewRequest("GET", "/api/v1/device-probes", nil)
	rec := httptest.NewRecorder()
	h.ListAllDeviceProbes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "should return 200 OK")

	var results []models.DeviceProbeWithResultEnriched
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results), "response must be valid JSON")
	require.Len(t, results, 2, "should return all probes across all devices")

	assert.Equal(t, "web-server-01", results[0].DeviceHostname, "probe 1 must have device hostname")
	assert.Equal(t, "db-server-01", results[1].DeviceHostname, "probe 2 must have device hostname")
	assert.Equal(t, "Check nginx", results[0].Name, "probe 1 name must be preserved")
	assert.Equal(t, "Check postgres", results[1].Name, "probe 2 name must be preserved")
}

// [AC-004] ListAllDeviceProbes returns empty array when no device probes exist
func TestListAllDeviceProbes_Empty(t *testing.T) {
	h, _, _ := newListAllHandlers(t)

	req := httptest.NewRequest("GET", "/api/v1/device-probes", nil)
	rec := httptest.NewRecorder()
	h.ListAllDeviceProbes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "should return 200 OK with no probes")

	var results []models.DeviceProbeWithResultEnriched
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results), "response must be valid JSON array")
	assert.Len(t, results, 0, "should return empty array")
}

// [AC-004] ListAllDeviceProbes enriches probes with latest_result and success_rate
func TestListAllDeviceProbes_EnrichesWithResultStats(t *testing.T) {
	h, probeRepo, deviceRepo := newListAllHandlers(t)

	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "node-1"}
	probeRepo.Probes = []models.DeviceProbe{
		{ID: 1, Name: "Shell check", DeviceID: "dev-1", Type: "shell"},
	}
	probeRepo.Results = []models.DeviceProbeResult{
		{ID: 10, ProbeID: 1, DeviceID: "dev-1", Success: true, LatencyMs: 42.5},
		{ID: 11, ProbeID: 1, DeviceID: "dev-1", Success: false, LatencyMs: 10.0},
	}

	req := httptest.NewRequest("GET", "/api/v1/device-probes", nil)
	rec := httptest.NewRecorder()
	h.ListAllDeviceProbes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var results []models.DeviceProbeWithResultEnriched
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results))
	require.Len(t, results, 1)

	assert.NotNil(t, results[0].LatestResult, "latest_result must be populated")
	assert.NotNil(t, results[0].SuccessRate, "success_rate must be populated")
	assert.Equal(t, 2, results[0].TotalChecks, "total_checks must reflect result count")
	assert.InDelta(t, 0.5, *results[0].SuccessRate, 0.001, "success rate must be 50%% (1/2)")
}

// [AC-004] ListAllDeviceProbes returns probes across multiple devices
func TestListAllDeviceProbes_SpansMultipleDevices(t *testing.T) {
	h, probeRepo, deviceRepo := newListAllHandlers(t)

	deviceRepo.Devices["dev-a"] = &models.Device{ID: "dev-a", Hostname: "alpha"}
	deviceRepo.Devices["dev-b"] = &models.Device{ID: "dev-b", Hostname: "beta"}
	deviceRepo.Devices["dev-c"] = &models.Device{ID: "dev-c", Hostname: "gamma"}

	probeRepo.Probes = []models.DeviceProbe{
		{ID: 1, Name: "p1", DeviceID: "dev-a", Type: "http"},
		{ID: 2, Name: "p2", DeviceID: "dev-b", Type: "port"},
		{ID: 3, Name: "p3", DeviceID: "dev-c", Type: "shell"},
	}

	req := httptest.NewRequest("GET", "/api/v1/device-probes", nil)
	rec := httptest.NewRecorder()
	h.ListAllDeviceProbes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var results []models.DeviceProbeWithResultEnriched
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results))
	assert.Len(t, results, 3, "must return probes from all three devices")
}

// [AC-004] ListAllDeviceProbes returns 500 when the device probe repo fails
func TestListAllDeviceProbes_RepoError_Returns500(t *testing.T) {
	h, probeRepo, _ := newListAllHandlers(t)
	probeRepo.Err = errors.New("db connection lost")

	req := httptest.NewRequest("GET", "/api/v1/device-probes", nil)
	rec := httptest.NewRecorder()
	h.ListAllDeviceProbes(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code, "should return 500 on repo error")
}

// [AC-004] ListAllDeviceProbes returns 500 when the device repo fails during hostname lookup
func TestListAllDeviceProbes_DeviceRepoError_Returns500(t *testing.T) {
	h, probeRepo, deviceRepo := newListAllHandlers(t)

	probeRepo.Probes = []models.DeviceProbe{
		{ID: 1, Name: "probe", DeviceID: "dev-1", Type: "shell"},
	}
	deviceRepo.Err = errors.New("failed to list devices")

	req := httptest.NewRequest("GET", "/api/v1/device-probes", nil)
	rec := httptest.NewRecorder()
	h.ListAllDeviceProbes(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code, "should return 500 when device list fails")
}

// [AC-004] ListAllDeviceProbes uses empty string for hostname when device is not found
func TestListAllDeviceProbes_MissingDevice_UsesEmptyHostname(t *testing.T) {
	h, probeRepo, _ := newListAllHandlers(t)

	// No devices in repo, but probe references dev-1
	probeRepo.Probes = []models.DeviceProbe{
		{ID: 1, Name: "orphan", DeviceID: "dev-1", Type: "shell"},
	}

	req := httptest.NewRequest("GET", "/api/v1/device-probes", nil)
	rec := httptest.NewRecorder()
	h.ListAllDeviceProbes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var results []models.DeviceProbeWithResultEnriched
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results))
	require.Len(t, results, 1)
	assert.Equal(t, "", results[0].DeviceHostname, "hostname must be empty when device not found in map")
}
