package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: middleware.CheckWSOrigin,
}

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type     string      `json:"type"`
	DeviceID string      `json:"device_id,omitempty"`
	Data     interface{} `json:"data,omitempty"`
}

// Hub manages WebSocket clients and broadcasts.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			slog.Info("ws client connected", "clients", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			slog.Info("ws client disconnected", "clients", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// HandleWS upgrades an HTTP connection to WebSocket.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade", "error", err.Error())
		return
	}
	client := &Client{hub: h, conn: conn, send: make(chan []byte, 256)}
	h.register <- client
	go client.writePump()
	go client.readPump()
}

func (h *Hub) broadcastMsg(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("ws marshal", "error", err.Error())
		return
	}
	h.broadcast <- data
}

func (h *Hub) BroadcastDeviceUpdate(device *models.Device) {
	h.broadcastMsg(WSMessage{Type: "device_update", DeviceID: device.ID, Data: device})
}

func (h *Hub) BroadcastHeartbeat(deviceID string, data *models.HeartbeatData) {
	h.broadcastMsg(WSMessage{Type: "heartbeat", DeviceID: deviceID, Data: data})
}

func (h *Hub) BroadcastTelemetry(deviceID string, data *models.FullTelemetryData) {
	h.broadcastMsg(WSMessage{Type: "telemetry", DeviceID: deviceID, Data: data})
}

func (h *Hub) BroadcastEvent(event *models.Event) {
	h.broadcastMsg(WSMessage{Type: "event", DeviceID: event.DeviceID, Data: event})
}

func (h *Hub) BroadcastDeviceRemoved(deviceID string) {
	h.broadcastMsg(WSMessage{Type: "device_removed", DeviceID: deviceID})
}

func (h *Hub) BroadcastDockerUpdate(deviceID string, data interface{}) {
	h.broadcastMsg(WSMessage{Type: "docker_update", DeviceID: deviceID, Data: data})
}

func (h *Hub) BroadcastCommandResult(deviceID string, commandID string, data interface{}) {
	h.broadcastMsg(WSMessage{Type: "command_result", DeviceID: deviceID, Data: data})
}

func (h *Hub) BroadcastProbeResult(probeID int64, data interface{}) {
	h.broadcastMsg(WSMessage{Type: "probe_result", Data: data})
}
