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
