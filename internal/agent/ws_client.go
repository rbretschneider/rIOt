package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// AgentWSMessage is the envelope for messages between server and agent.
type AgentWSMessage struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"session_id,omitempty"`
	ContainerID string          `json:"container_id,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// agentWSClient maintains a persistent WebSocket connection to the server.
type agentWSClient struct {
	serverURL string
	apiKey    string
	deviceID  string
	agent     *Agent
	conn      *websocket.Conn
}

func newAgentWSClient(a *Agent) *agentWSClient {
	return &agentWSClient{
		serverURL: a.config.Server.URL,
		apiKey:    a.config.Server.APIKey,
		deviceID:  a.config.Agent.DeviceID,
		agent:     a,
	}
}

// run connects to the server and handles messages. Reconnects with backoff.
func (c *agentWSClient) run(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.connect(ctx)
		if err != nil {
			slog.Debug("agent ws: connect failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (c *agentWSClient) connect(ctx context.Context) error {
	// Convert http(s):// to ws(s)://
	wsURL := strings.Replace(c.serverURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws/agent"

	header := http.Header{}
	header.Set("X-rIOt-Key", c.apiKey)
	header.Set("X-rIOt-Device", c.deviceID)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return err
	}
	c.conn = conn
	defer func() {
		conn.Close()
		c.conn = nil
	}()

	slog.Info("agent ws: connected to server")

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var msg AgentWSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			slog.Warn("agent ws: invalid message", "error", err)
			continue
		}

		switch msg.Type {
		case "terminal_start":
			go c.agent.handleTerminalStart(ctx, conn, msg)
		case "terminal_input":
			c.agent.handleTerminalInput(msg)
		case "terminal_resize":
			c.agent.handleTerminalResize(msg)
		case "terminal_close":
			c.agent.handleTerminalClose(msg)
		case "command":
			go c.agent.handleCommand(ctx, msg)
		default:
			slog.Debug("agent ws: unknown message type", "type", msg.Type)
		}
	}
}

func (c *agentWSClient) send(msg AgentWSMessage) error {
	if c.conn == nil {
		return nil
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}
