package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newHeartbeatTestHandlers builds the minimal Handlers for heartbeat tests.
// Mirrors the pattern used in device_handler_test.go and fleet_dashboard_test.go.
func newHeartbeatTestHandlers(t *testing.T) (*Handlers, *testutil.MockTelemetryRepo, *testutil.MockDeviceRepo) {
	t.Helper()
	telRepo := testutil.NewMockTelemetryRepo()
	devRepo := testutil.NewMockDeviceRepo()

	hub := websocket.NewHub()
	go hub.Run()

	// Create a minimal event generator with mock repos (no nil-pointer panic on
	// CheckHeartbeatThresholds, which is called unconditionally in Heartbeat).
	eventRepo := testutil.NewMockEventRepo()
	alertRuleRepo := testutil.NewMockAlertRuleRepo()
	dispatcher := testutil.NewMockDispatcher()
	commandRepo := testutil.NewMockCommandRepo()
	gen := events.NewGenerator(eventRepo, hub, alertRuleRepo, dispatcher, commandRepo)

	h := &Handlers{
		telemetry: telRepo,
		devices:   devRepo,
		hub:       hub,
		eventGen:  gen,
	}
	return h, telRepo, devRepo
}

// postHeartbeat sends a POST to /api/v1/devices/{id}/heartbeat with the given body.
func postHeartbeat(t *testing.T, h *Handlers, deviceID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/heartbeat", h.Heartbeat)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+deviceID+"/heartbeat",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// [AC-010] / [AC-019]: Server accepts heartbeat from old agent without net fields
// ---------------------------------------------------------------------------

func TestHeartbeat_OldAgentNoNetFields(t *testing.T) {
	t.Run("[AC-010] [AC-019] server accepts heartbeat without net rate fields", func(t *testing.T) {
		h, telRepo, devRepo := newHeartbeatTestHandlers(t)
		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "node-1"}

		// Old-agent payload — no net_rx_bytes_sec or net_tx_bytes_sec keys.
		payload := `{
			"device_id": "dev-1",
			"timestamp": "2026-04-27T12:00:00Z",
			"data": {
				"uptime": 3600,
				"cpu_percent": 45.2,
				"mem_percent": 61.3,
				"load_avg_1m": 1.5,
				"disk_root_percent": 72.1
			}
		}`

		rec := postHeartbeat(t, h, "dev-1", payload)

		assert.Equal(t, http.StatusOK, rec.Code,
			"server must respond 2xx to old-agent heartbeat without net fields")

		// Confirm the heartbeat was stored with zero net rates (Go zero-value).
		stored := telRepo.Heartbeats["dev-1"]
		require.Len(t, stored, 1, "heartbeat must be persisted")
		assert.Equal(t, float64(0), stored[0].Data.NetRxBytesPerSec,
			"missing net_rx_bytes_sec must decode to 0")
		assert.Equal(t, float64(0), stored[0].Data.NetTxBytesPerSec,
			"missing net_tx_bytes_sec must decode to 0")
	})

	t.Run("[AC-010] response body is valid JSON with status ok", func(t *testing.T) {
		h, _, devRepo := newHeartbeatTestHandlers(t)
		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "node-1"}

		payload := `{"device_id":"dev-1","data":{"uptime":100,"cpu_percent":5,"mem_percent":10,"load_avg_1m":0.1,"disk_root_percent":20}}`
		rec := postHeartbeat(t, h, "dev-1", payload)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.Equal(t, "ok", resp["status"])
	})
}

// ---------------------------------------------------------------------------
// Round-trip: new-agent fields survive the encode→decode→store cycle
// ---------------------------------------------------------------------------

func TestHeartbeat_NewAgentFieldsRoundTrip(t *testing.T) {
	t.Run("[AC-001] net rate fields survive the heartbeat handler round-trip", func(t *testing.T) {
		h, telRepo, devRepo := newHeartbeatTestHandlers(t)
		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "node-1"}

		payload := `{
			"device_id": "dev-1",
			"data": {
				"uptime": 3600,
				"cpu_percent": 45.0,
				"mem_percent": 60.0,
				"load_avg_1m": 1.0,
				"disk_root_percent": 70.0,
				"net_rx_bytes_sec": 1234.5,
				"net_tx_bytes_sec": 5678.9
			}
		}`

		rec := postHeartbeat(t, h, "dev-1", payload)
		assert.Equal(t, http.StatusOK, rec.Code)

		stored := telRepo.Heartbeats["dev-1"]
		require.Len(t, stored, 1)
		assert.InDelta(t, 1234.5, stored[0].Data.NetRxBytesPerSec, 0.001,
			"net_rx_bytes_sec must survive the handler round-trip")
		assert.InDelta(t, 5678.9, stored[0].Data.NetTxBytesPerSec, 0.001,
			"net_tx_bytes_sec must survive the handler round-trip")
	})
}

// ---------------------------------------------------------------------------
// SEC-FLEET-NET-001: Non-finite values in the payload must not crash the handler
// ---------------------------------------------------------------------------

func TestHeartbeat_NonFiniteValuesHandledGracefully(t *testing.T) {
	t.Run("[SEC-FLEET-NET-001] payload with 1e400 (overflows to +Inf in IEEE-754) is rejected at JSON decode boundary", func(t *testing.T) {
		// Go's encoding/json decodes 1e400 to +Inf when the target field is
		// float64. The standard library returns an error for this case
		// (json.UnmarshalTypeError or a decode error), causing the handler to
		// return 400. Verify the handler does NOT panic or return 500.
		h, _, devRepo := newHeartbeatTestHandlers(t)
		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "node-1"}

		// 1e400 exceeds float64 range → encoding/json returns error
		payload := `{"device_id":"dev-1","data":{"uptime":100,"cpu_percent":5,"mem_percent":10,"load_avg_1m":0.1,"disk_root_percent":20,"net_rx_bytes_sec":1e400}}`
		rec := postHeartbeat(t, h, "dev-1", payload)

		// The handler must not panic. We accept either 400 (rejected at decode)
		// or 200 (server is permissive and clamps later). Either way: no 500.
		assert.NotEqual(t, http.StatusInternalServerError, rec.Code,
			"non-finite float must not cause a 500 server error")
		// In practice Go's json.Decoder treats 1e400 as an overflow → decode error
		// → handler returns 400.
		assert.Equal(t, http.StatusBadRequest, rec.Code,
			"overflow float 1e400 must result in a 400 decode error")
	})

	t.Run("[SEC-FLEET-NET-001] verify 1e400 JSON decode behavior directly", func(t *testing.T) {
		// Confirm the standard library rejects 1e400 into float64 (makes the
		// handler test above meaningful).
		payload := `{"net_rx_bytes_sec":1e400}`
		var data models.HeartbeatData
		err := json.NewDecoder(strings.NewReader(payload)).Decode(&data)
		assert.Error(t, err, "encoding/json must return an error for 1e400 → float64")
	})
}
