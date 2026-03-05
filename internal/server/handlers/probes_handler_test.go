package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProbeTestHandlers(t *testing.T) (*Handlers, *testutil.MockProbeRepo) {
	t.Helper()
	probeRepo := testutil.NewMockProbeRepo()
	h := &Handlers{
		probeRepo: probeRepo,
	}
	return h, probeRepo
}

func TestListProbes_Empty(t *testing.T) {
	h, _ := newProbeTestHandlers(t)

	req := httptest.NewRequest("GET", "/api/v1/probes", nil)
	rec := httptest.NewRecorder()
	h.ListProbes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestListProbes_WithResults(t *testing.T) {
	h, probeRepo := newProbeTestHandlers(t)
	probeRepo.Probes = []models.Probe{
		{ID: 1, Name: "Google HTTP", Type: "http", Enabled: true},
		{ID: 2, Name: "DNS Check", Type: "dns", Enabled: true},
	}
	probeRepo.Results = []models.ProbeResult{
		{ID: 1, ProbeID: 1, Success: true, LatencyMs: 42.5},
	}

	req := httptest.NewRequest("GET", "/api/v1/probes", nil)
	rec := httptest.NewRecorder()
	h.ListProbes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var results []json.RawMessage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results))
	assert.Len(t, results, 2)
}

func TestCreateProbe(t *testing.T) {
	h, probeRepo := newProbeTestHandlers(t)

	body, _ := json.Marshal(map[string]interface{}{
		"name":    "Test Probe",
		"type":    "http",
		"enabled": true,
		"config":  map[string]interface{}{"url": "https://example.com"},
	})
	req := httptest.NewRequest("POST", "/api/v1/probes", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateProbe(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, probeRepo.Probes, 1)
	assert.Equal(t, "Test Probe", probeRepo.Probes[0].Name)
	assert.Equal(t, 60, probeRepo.Probes[0].IntervalSeconds, "should default interval")
	assert.Equal(t, 10, probeRepo.Probes[0].TimeoutSeconds, "should default timeout")
}

func TestCreateProbe_MissingFields(t *testing.T) {
	h, _ := newProbeTestHandlers(t)

	body, _ := json.Marshal(map[string]interface{}{"name": "test"})
	req := httptest.NewRequest("POST", "/api/v1/probes", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateProbe(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeleteProbe(t *testing.T) {
	h, probeRepo := newProbeTestHandlers(t)
	probeRepo.Probes = []models.Probe{{ID: 1, Name: "Doomed"}}

	r := chi.NewRouter()
	r.Delete("/api/v1/probes/{id}", h.DeleteProbe)

	req := httptest.NewRequest("DELETE", "/api/v1/probes/1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, probeRepo.Probes)
}

func TestGetProbeResults(t *testing.T) {
	h, probeRepo := newProbeTestHandlers(t)
	probeRepo.Results = []models.ProbeResult{
		{ID: 1, ProbeID: 5, Success: true, LatencyMs: 10},
		{ID: 2, ProbeID: 5, Success: false, ErrorMsg: "timeout"},
		{ID: 3, ProbeID: 99, Success: true}, // different probe
	}

	r := chi.NewRouter()
	r.Get("/api/v1/probes/{id}/results", h.GetProbeResults)

	req := httptest.NewRequest("GET", "/api/v1/probes/5/results", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var results []models.ProbeResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results))
	assert.Len(t, results, 2, "only results for probe 5")
}

func TestUpdateProbe(t *testing.T) {
	h, probeRepo := newProbeTestHandlers(t)
	probeRepo.Probes = []models.Probe{{ID: 1, Name: "Old", Type: "http"}}

	body, _ := json.Marshal(map[string]interface{}{
		"name": "Updated",
		"type": "dns",
	})

	r := chi.NewRouter()
	r.Put("/api/v1/probes/{id}", h.UpdateProbe)

	req := httptest.NewRequest("PUT", "/api/v1/probes/1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Updated", probeRepo.Probes[0].Name)
}
