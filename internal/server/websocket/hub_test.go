package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHub(t *testing.T) (*Hub, *Client) {
	t.Helper()
	hub := NewHub()
	go hub.Run()

	client := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
	}
	hub.register <- client

	// Wait for registration to be processed
	time.Sleep(10 * time.Millisecond)
	return hub, client
}

func TestHub_RegisterClient(t *testing.T) {
	hub, _ := setupHub(t)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	assert.Equal(t, 1, count)
}

func TestHub_BroadcastReceived(t *testing.T) {
	hub, client := setupHub(t)

	hub.broadcast <- []byte(`{"type":"test"}`)

	select {
	case msg := <-client.send:
		assert.Equal(t, `{"type":"test"}`, string(msg))
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestHub_UnregisterRemoves(t *testing.T) {
	hub, client := setupHub(t)

	hub.unregister <- client
	time.Sleep(10 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	assert.Equal(t, 0, count)
}

func TestHub_BroadcastDeviceUpdate(t *testing.T) {
	hub, client := setupHub(t)

	device := &models.Device{
		ID:       "dev-123",
		Hostname: "test-host",
		Status:   models.DeviceStatusOnline,
	}

	hub.BroadcastDeviceUpdate(device)

	select {
	case msg := <-client.send:
		var wsMsg WSMessage
		require.NoError(t, json.Unmarshal(msg, &wsMsg))
		assert.Equal(t, "device_update", wsMsg.Type)
		assert.Equal(t, "dev-123", wsMsg.DeviceID)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for device update broadcast")
	}
}

func TestHub_BroadcastEvent(t *testing.T) {
	hub, client := setupHub(t)

	evt := &models.Event{
		ID:       42,
		DeviceID: "dev-456",
		Type:     models.EventMemHigh,
		Severity: models.SeverityWarning,
		Message:  "RAM at 95%",
	}

	hub.BroadcastEvent(evt)

	select {
	case msg := <-client.send:
		var wsMsg WSMessage
		require.NoError(t, json.Unmarshal(msg, &wsMsg))
		assert.Equal(t, "event", wsMsg.Type)
		assert.Equal(t, "dev-456", wsMsg.DeviceID)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event broadcast")
	}
}

// rawEnvelope decodes only the top level of a broadcast, so a test can tell a
// top-level field from one nested in data.
func rawEnvelope(t *testing.T, msg []byte) map[string]json.RawMessage {
	t.Helper()
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(msg, &top))
	return top
}

func receive(t *testing.T, client *Client) []byte {
	t.Helper()
	select {
	case msg := <-client.send:
		return msg
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for broadcast")
		return nil
	}
}

func TestHub_BroadcastTelemetryEnvelopeHasTopLevelTimestamps(t *testing.T) {
	hub, client := setupHub(t)
	collected := time.Date(2026, 10, 17, 11, 59, 58, 0, time.UTC)
	received := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	hub.BroadcastTelemetry("dev-1", &models.FullTelemetryData{}, collected, received)

	top := rawEnvelope(t, receive(t, client))
	assert.JSONEq(t, `"telemetry"`, string(top["type"]))
	assert.JSONEq(t, `"dev-1"`, string(top["device_id"]))
	assert.JSONEq(t, `"2026-10-17T11:59:58Z"`, string(top["collected_at"]), "collected_at is at the top level")
	assert.JSONEq(t, `"2026-10-17T12:00:00Z"`, string(top["received_at"]), "received_at is at the top level")
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(top["data"], &data))
	assert.NotContains(t, data, "collected_at", "the timestamps must not be nested in data")
	assert.NotContains(t, data, "received_at")
}

func TestHub_BroadcastHeartbeatEnvelopeHasReceivedAtOnly(t *testing.T) {
	hub, client := setupHub(t)
	received := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	hub.BroadcastHeartbeat("dev-1", &models.HeartbeatData{}, received)

	top := rawEnvelope(t, receive(t, client))
	assert.JSONEq(t, `"2026-10-17T12:00:00Z"`, string(top["received_at"]))
	assert.NotContains(t, top, "collected_at", "a heartbeat has no collection time")
}

func TestHub_BroadcastCommandUpdateEnvelope(t *testing.T) {
	hub, client := setupHub(t)
	pct := 40
	update := models.CommandUpdate{
		CommandID: "c-1", DeviceID: "dev-1", Action: "docker_update", TargetKey: "docker:container:web",
		TargetKeys: []string{"docker:container:web"}, Status: "running", Step: "pull", Progress: &pct,
		Message: "pulling", UpdatedAt: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}

	hub.BroadcastCommandUpdate(update)

	top := rawEnvelope(t, receive(t, client))
	assert.JSONEq(t, `"command_update"`, string(top["type"]))
	assert.JSONEq(t, `"dev-1"`, string(top["device_id"]), "the device id is on the envelope")
	assert.NotContains(t, top, "collected_at", "other messages keep their old shape")
	var data models.CommandUpdate
	require.NoError(t, json.Unmarshal(top["data"], &data))
	assert.Equal(t, update, data, "the payload round-trips")
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(top["data"], &raw))
	assert.Contains(t, raw, "device_id", "data.device_id is present for WP-WEB")
}

func TestHub_OtherEnvelopesCarryNoTimestamps(t *testing.T) {
	hub, client := setupHub(t)
	hub.BroadcastDeviceRemoved("dev-1")
	top := rawEnvelope(t, receive(t, client))
	assert.NotContains(t, top, "collected_at")
	assert.NotContains(t, top, "received_at")
}
