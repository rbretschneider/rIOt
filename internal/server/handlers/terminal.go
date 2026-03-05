package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	ws "github.com/gorilla/websocket"
)

// agentWSMessage mirrors the agent's message format.
type agentWSMessage struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"session_id,omitempty"`
	ContainerID string          `json:"container_id,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// AgentConn represents a connected agent's WebSocket.
type AgentConn struct {
	DeviceID string
	Conn     *ws.Conn
	mu       sync.Mutex
}

func (ac *AgentConn) Send(msg agentWSMessage) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.Conn.WriteJSON(msg)
}

// agentConnections stores device_id → agent WebSocket connection.
var agentConnections = struct {
	sync.RWMutex
	m map[string]*AgentConn
}{m: make(map[string]*AgentConn)}

// IsAgentConnected reports whether a device has an active agent WebSocket.
func IsAgentConnected(deviceID string) bool {
	agentConnections.RLock()
	defer agentConnections.RUnlock()
	_, ok := agentConnections.m[deviceID]
	return ok
}

// terminalBrowserConns stores session_id → browser WebSocket connection.
var terminalBrowserConns = struct {
	sync.RWMutex
	m map[string]*ws.Conn
}{m: make(map[string]*ws.Conn)}

var terminalUpgrader = ws.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     middleware.CheckWSOrigin,
}

// HandleAgentWS accepts WebSocket connections from agents.
func (h *Handlers) HandleAgentWS(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-rIOt-Device")
	apiKey := r.Header.Get("X-rIOt-Key")
	if deviceID == "" || apiKey == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Validate the API key against the database
	keyDeviceID, err := h.devices.LookupAPIKey(r.Context(), apiKey)
	if err != nil || keyDeviceID != deviceID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("agent ws upgrade failed", "error", err)
		return
	}

	ac := &AgentConn{DeviceID: deviceID, Conn: conn}
	agentConnections.Lock()
	agentConnections.m[deviceID] = ac
	agentConnections.Unlock()

	slog.Info("agent ws connected", "device", deviceID)

	defer func() {
		agentConnections.Lock()
		delete(agentConnections.m, deviceID)
		agentConnections.Unlock()
		conn.Close()
		slog.Info("agent ws disconnected", "device", deviceID)
	}()

	// Read messages from agent and relay terminal output to browsers
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg agentWSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "terminal_output":
			terminalBrowserConns.RLock()
			browserConn := terminalBrowserConns.m[msg.SessionID]
			terminalBrowserConns.RUnlock()
			if browserConn != nil {
				var output string
				json.Unmarshal(msg.Data, &output)
				browserConn.WriteMessage(ws.TextMessage, []byte(output))
			}
		case "terminal_close":
			terminalBrowserConns.RLock()
			browserConn := terminalBrowserConns.m[msg.SessionID]
			terminalBrowserConns.RUnlock()
			if browserConn != nil {
				browserConn.Close()
				terminalBrowserConns.Lock()
				delete(terminalBrowserConns.m, msg.SessionID)
				terminalBrowserConns.Unlock()
			}

		case "command_result":
			h.handleCommandResult(r.Context(), msg)
		}
	}
}

// handleCommandResult processes a command result from an agent.
func (h *Handlers) handleCommandResult(ctx context.Context, msg agentWSMessage) {
	var result models.CommandResult
	if err := json.Unmarshal(msg.Data, &result); err != nil {
		slog.Warn("terminal: invalid command_result", "error", err)
		return
	}
	if h.commandRepo != nil {
		if err := h.commandRepo.UpdateStatus(ctx, result.CommandID, result.Status, result.Message); err != nil {
			slog.Error("terminal: update command status", "error", err)
		}
	}
	// Broadcast to dashboard clients
	h.hub.BroadcastCommandResult(result.CommandID, &result)
	slog.Info("command result", "id", result.CommandID, "status", result.Status)
}

// HandleTerminalWS bridges browser ↔ server ↔ agent for terminal sessions.
func (h *Handlers) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")
	containerID := chi.URLParam(r, "containerId")

	agentConnections.RLock()
	ac := agentConnections.m[deviceID]
	agentConnections.RUnlock()

	if ac == nil {
		http.Error(w, "agent not connected", http.StatusBadGateway)
		return
	}

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("terminal ws upgrade failed", "error", err)
		return
	}

	sessionID := generateTerminalSessionID()

	terminalBrowserConns.Lock()
	terminalBrowserConns.m[sessionID] = conn
	terminalBrowserConns.Unlock()

	// Audit log: record terminal session start
	if h.terminalRepo != nil {
		if err := h.terminalRepo.LogSessionStart(r.Context(), deviceID, containerID, sessionID, r.RemoteAddr); err != nil {
			slog.Error("terminal: failed to log session start", "error", err)
		}
	}

	defer func() {
		terminalBrowserConns.Lock()
		delete(terminalBrowserConns.m, sessionID)
		terminalBrowserConns.Unlock()
		conn.Close()

		// Audit log: record terminal session end
		if h.terminalRepo != nil {
			if err := h.terminalRepo.LogSessionEnd(r.Context(), sessionID); err != nil {
				slog.Error("terminal: failed to log session end", "error", err)
			}
		}

		ac.Send(agentWSMessage{
			Type:      "terminal_close",
			SessionID: sessionID,
		})
	}()

	if err := ac.Send(agentWSMessage{
		Type:        "terminal_start",
		SessionID:   sessionID,
		ContainerID: containerID,
	}); err != nil {
		slog.Error("terminal: failed to send start to agent", "error", err)
		return
	}

	slog.Info("terminal session started", "session", sessionID, "device", deviceID, "container", containerID)

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		switch msgType {
		case ws.TextMessage:
			inputData, _ := json.Marshal(string(data))
			ac.Send(agentWSMessage{
				Type:      "terminal_input",
				SessionID: sessionID,
				Data:      inputData,
			})
		case ws.BinaryMessage:
			ac.Send(agentWSMessage{
				Type:      "terminal_resize",
				SessionID: sessionID,
				Data:      data,
			})
		}
	}
}

func generateTerminalSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
