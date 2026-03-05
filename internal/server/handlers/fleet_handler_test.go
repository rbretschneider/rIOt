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
