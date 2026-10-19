package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
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

// agentSocket is the part of a gorilla connection the agent registry needs. The
// interface lets tests substitute a fake.
type agentSocket interface {
	WriteJSON(v interface{}) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	Close() error
}

// AgentConn represents a connected agent's WebSocket.
type AgentConn struct {
	DeviceID string
	Conn     agentSocket
	mu       sync.Mutex
}

func (ac *AgentConn) Send(msg agentWSMessage) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.Conn.WriteJSON(msg)
}

// agentRegistry maps a device to its current agent connection. Replacing and
// removing are compare-and-swap operations on the connection identity, so a
// reconnecting agent's old handler can never remove the new connection
// (FR-064).
type agentRegistry struct {
	mu sync.RWMutex
	m  map[string]*AgentConn
}

func newAgentRegistry() *agentRegistry {
	return &agentRegistry{m: make(map[string]*AgentConn)}
}

// defaultAgentRegistry is shared by every Handlers built by handlers.New, so the
// connections survive a router rebuild (applyTLSAndRestart).
var defaultAgentRegistry = newAgentRegistry()

// register stores ac for deviceID and returns the connection it replaced, if any.
func (r *agentRegistry) register(deviceID string, ac *AgentConn) (previous *AgentConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous = r.m[deviceID]
	r.m[deviceID] = ac
	return previous
}

// replace registers ac and closes the connection it replaced (FR-064).
func (r *agentRegistry) replace(deviceID string, ac *AgentConn) {
	if previous := r.register(deviceID, ac); previous != nil {
		if err := previous.Conn.Close(); err != nil {
			slog.Debug("close replaced agent ws", "device", deviceID, "error", err.Error())
		}
	}
}

// unregister removes deviceID's entry only if it still holds ac, and reports
// whether it did.
func (r *agentRegistry) unregister(deviceID string, ac *AgentConn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m[deviceID] != ac {
		return false
	}
	delete(r.m, deviceID)
	return true
}

func (r *agentRegistry) get(deviceID string) *AgentConn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[deviceID]
}

func (r *agentRegistry) connected(deviceID string) bool {
	return r.get(deviceID) != nil
}

// registry returns the Handlers' registry, defaulting to the shared one.
func (h *Handlers) registry() *agentRegistry {
	if h.agents != nil {
		return h.agents
	}
	return defaultAgentRegistry
}

// agentSender adapts the registry to commands.AgentSender.
type agentSender struct{ reg *agentRegistry }

func (s agentSender) IsConnected(deviceID string) bool { return s.reg.connected(deviceID) }

// SendCommand writes a command to the device's agent socket. sent=false with a
// nil error means the device is not connected; an error means a write was
// attempted and failed (it may still have reached the agent).
func (s agentSender) SendCommand(deviceID string, payload models.CommandPayload) (bool, error) {
	ac := s.reg.get(deviceID)
	if ac == nil {
		return false, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	if err := ac.Send(agentWSMessage{Type: "command", Data: data}); err != nil {
		return false, err
	}
	return true, nil
}

// Agents returns the sender over the shared registry, for the command service
// built in server.Start.
func Agents() commands.AgentSender {
	return agentSender{reg: defaultAgentRegistry}
}

// terminalBrowser is the browser side of a terminal session.
type terminalBrowser interface {
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// terminalSession binds a browser connection to the device whose agent serves
// it (SEC-014): an agent socket can only write to or close sessions of its own
// device.
type terminalSession struct {
	conn     terminalBrowser
	deviceID string
}

// terminalBrowserConns stores session_id -> session.
var terminalBrowserConns = struct {
	sync.RWMutex
	m map[string]terminalSession
}{m: make(map[string]terminalSession)}

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
		slog.Error("agent ws upgrade failed", "error", err.Error())
		return
	}
	conn.SetReadLimit(models.MaxAgentWSMessageBytes)

	reg := h.registry()
	ac := &AgentConn{DeviceID: deviceID, Conn: conn}
	reg.replace(deviceID, ac)

	slog.Info("agent ws connected", "device", deviceID)

	defer func() {
		reg.unregister(deviceID, ac)
		conn.Close()
		slog.Info("agent ws disconnected", "device", deviceID)
	}()

	// Keepalive: send pings and expect pongs from agent
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-ticker.C:
				ac.mu.Lock()
				err := conn.WriteControl(ws.PingMessage, nil, time.Now().Add(10*time.Second))
				ac.mu.Unlock()
				if err != nil {
					return
				}
			}
		}
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
		h.handleAgentMessage(r.Context(), ac, msg)
	}
}

// handleAgentMessage processes one message from an authenticated agent socket.
// The socket's device ID, validated against the API key at connect time, scopes
// every effect: command reports and terminal sessions of other devices are
// ignored.
func (h *Handlers) handleAgentMessage(ctx context.Context, ac *AgentConn, msg agentWSMessage) {
	switch msg.Type {
	case "terminal_output":
		terminalBrowserConns.RLock()
		sess, ok := terminalBrowserConns.m[msg.SessionID]
		terminalBrowserConns.RUnlock()
		if ok && sess.deviceID == ac.DeviceID {
			var output string
			if err := json.Unmarshal(msg.Data, &output); err != nil {
				slog.Debug("terminal: undecodable output", "device", ac.DeviceID)
				return
			}
			if err := sess.conn.WriteMessage(ws.TextMessage, []byte(output)); err != nil {
				slog.Debug("terminal: write to browser failed", "device", ac.DeviceID, "error", err.Error())
			}
		}
	case "terminal_close":
		terminalBrowserConns.RLock()
		sess, ok := terminalBrowserConns.m[msg.SessionID]
		terminalBrowserConns.RUnlock()
		if ok && sess.deviceID == ac.DeviceID {
			if err := sess.conn.Close(); err != nil {
				slog.Debug("terminal: close browser conn", "device", ac.DeviceID, "error", err.Error())
			}
			terminalBrowserConns.Lock()
			delete(terminalBrowserConns.m, msg.SessionID)
			terminalBrowserConns.Unlock()
		}

	case "command_ack", "command_progress", "command_result":
		h.handleAgentCommandMessage(ctx, ac, msg)
	}
}

// decodeWSReport turns a command_ack, command_progress or command_result
// message into the neutral report.
func decodeWSReport(msg agentWSMessage) (models.CommandReport, error) {
	switch msg.Type {
	case "command_ack":
		var a models.CommandAck
		if err := json.Unmarshal(msg.Data, &a); err != nil {
			return models.CommandReport{}, err
		}
		return a.Report(), nil
	case "command_progress":
		var p models.CommandProgress
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			return models.CommandReport{}, err
		}
		return p.Report(), nil
	default:
		var res models.CommandResult
		if err := json.Unmarshal(msg.Data, &res); err != nil {
			return models.CommandReport{}, err
		}
		return res.Report(), nil
	}
}

// handleAgentCommandMessage applies a command_ack, command_progress or
// command_result from an agent socket. Processing order (SEC C-1): per-device
// rate limit, decode, then the service (UUID check, sanitize, guarded
// transition). A rate-limited, undecodable or malformed message is dropped with
// no receipt; every processed result gets a command_receipt.
func (h *Handlers) handleAgentCommandMessage(ctx context.Context, ac *AgentConn, msg agentWSMessage) {
	if h.commandRepo == nil {
		return
	}
	svc := h.cmdSvc()
	if !svc.AllowReport(ac.DeviceID) {
		return
	}
	report, err := decodeWSReport(msg)
	if err != nil {
		slog.Debug("agent ws: undecodable command message", "device", ac.DeviceID, "type", msg.Type)
		return
	}
	outcome, err := svc.HandleAgentReport(ctx, ac.DeviceID, report)
	switch {
	case errors.Is(err, commands.ErrMalformedCommandID), errors.Is(err, commands.ErrInvalidReport):
		return // dropped; the service already logged a length-only record
	case err != nil:
		slog.Error("agent ws: command report failed", "device", ac.DeviceID, "type", msg.Type, "error", err.Error())
		return
	}
	if report.Kind != models.ReportKindResult {
		return
	}
	receipt, err := json.Marshal(models.CommandReceipt{
		CommandID: report.CommandID, Result: outcome.Disposition, Status: outcome.Status,
	})
	if err != nil {
		slog.Error("agent ws: encode receipt", "device", ac.DeviceID, "error", err.Error())
		return
	}
	if err := ac.Send(agentWSMessage{Type: "command_receipt", Data: receipt}); err != nil {
		slog.Warn("agent ws: send receipt failed", "device", ac.DeviceID, "error", err.Error())
	}
}

// HandleTerminalWS bridges browser ↔ server ↔ agent for terminal sessions.
func (h *Handlers) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")
	containerID := chi.URLParam(r, "containerId")

	ac := h.registry().get(deviceID)
	if ac == nil {
		http.Error(w, "agent not connected", http.StatusBadGateway)
		return
	}

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("terminal ws upgrade failed", "error", err.Error())
		return
	}

	sessionID := generateTerminalSessionID()

	terminalBrowserConns.Lock()
	terminalBrowserConns.m[sessionID] = terminalSession{conn: conn, deviceID: deviceID}
	terminalBrowserConns.Unlock()

	// Audit log: record terminal session start
	if h.terminalRepo != nil {
		if err := h.terminalRepo.LogSessionStart(r.Context(), deviceID, containerID, sessionID, r.RemoteAddr); err != nil {
			slog.Error("terminal: failed to log session start", "error", err.Error())
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
				slog.Error("terminal: failed to log session end", "error", err.Error())
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
		slog.Error("terminal: failed to send start to agent", "error", err.Error())
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
