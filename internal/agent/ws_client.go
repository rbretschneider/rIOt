package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// newWSDialer returns a websocket.Dialer configured for the agent's TLS settings.
func newWSDialer(serverCfg ServerConfig) *websocket.Dialer {
	dialer := *websocket.DefaultDialer

	// Use mTLS-aware TLS config if client cert or custom CA is configured
	if serverCfg.ClientCert != "" || serverCfg.CACertFile != "" || !serverCfg.TLSVerify {
		dialer.TLSClientConfig = TLSConfigFromServerConfig(serverCfg)
	}

	return &dialer
}

// AgentWSMessage is the envelope for messages between server and agent.
type AgentWSMessage struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"session_id,omitempty"`
	ContainerID string          `json:"container_id,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// agentWSConn is the subset of *websocket.Conn the client uses. It lets tests
// substitute a connection that detects overlapping writes (FR-010).
type agentWSConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

// wsDialFunc opens one WebSocket connection to the server.
type wsDialFunc func(ctx context.Context, url string, header http.Header) (agentWSConn, error)

// errNotConnected is returned by send when there is no live connection. Callers
// (the outbox, the notifier) fall back to HTTP instead of losing the message.
var errNotConnected = errors.New("agent ws: not connected")

const (
	// wsWriteTimeout bounds every single write so a stalled peer cannot hold
	// the write mutex forever.
	wsWriteTimeout = 10 * time.Second
	// wsInitialBackoff is the reconnect delay after a successful connect and
	// the first delay after a failed one (FR-065).
	wsInitialBackoff = time.Second
	wsMaxBackoff     = 30 * time.Second
)

// agentWSClient maintains a persistent WebSocket connection to the server.
//
// connMu guards the current-connection reference (FR-011). writeMu serializes
// every write on the connection so at most one is in progress at any instant
// (FR-010): command results, lifecycle messages and terminal output all go
// through send.
type agentWSClient struct {
	readSem   chan struct{}
	logs      agentLogState
	serverURL string
	apiKey    string
	deviceID  string
	agent     *Agent

	// dial and sleep are injectable for tests.
	dial  wsDialFunc
	sleep func(ctx context.Context, d time.Duration) bool

	connMu sync.RWMutex
	conn   agentWSConn

	writeMu sync.Mutex
}

func newAgentWSClient(a *Agent) *agentWSClient {
	c := &agentWSClient{
		serverURL: a.config.Server.URL,
		apiKey:    a.config.Server.APIKey,
		deviceID:  a.config.Agent.DeviceID,
		agent:     a,
		sleep:     sleepContext,
	}
	c.dial = func(ctx context.Context, url string, header http.Header) (agentWSConn, error) {
		conn, _, err := newWSDialer(a.config.Server).DialContext(ctx, url, header)
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
	return c
}

// sleepContext waits for d or until ctx is done. It reports false when ctx
// ended first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// run connects to the server and handles messages. Reconnects with backoff;
// the backoff returns to its initial value after every successful connect
// (FR-065), so a long-lived healthy connection does not leave the next
// reconnect waiting 30s.
func (c *agentWSClient) run(ctx context.Context) {
	backoff := wsInitialBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		connected, err := c.connect(ctx)
		if err != nil {
			slog.Debug("agent ws: connect failed", "error", err)
		}
		if connected {
			backoff = wsInitialBackoff
		}

		if !c.sleep(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, wsMaxBackoff)
	}
}

// connect dials once and serves the connection until it drops. connected
// reports whether the dial itself succeeded.
func (c *agentWSClient) connect(ctx context.Context) (connected bool, err error) {
	// Convert http(s):// to ws(s)://
	wsURL := strings.Replace(c.serverURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws/agent"

	header := http.Header{}
	header.Set("X-rIOt-Key", c.apiKey)
	header.Set("X-rIOt-Device", c.deviceID)

	conn, err := c.dial(ctx, wsURL, header)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if limited, ok := conn.(interface{ SetReadLimit(int64) }); ok {
		limited.SetReadLimit(256 << 10)
	}
	c.setConn(conn)
	defer func() {
		c.clearConn(conn)
		conn.Close()
	}()

	slog.Info("agent ws: connected to server")

	// Trigger an immediate telemetry push so the server has fresh data
	// (e.g. after a server restart, the DB has stale telemetry and the
	// dashboard would show outdated container status/uptime).
	c.agent.triggerTelemetry()
	if c.agent.out != nil {
		c.agent.out.OnWSConnected()
	}

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			return true, err
		}

		var msg AgentWSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			slog.Warn("agent ws: invalid message", "error", err)
			continue
		}

		switch msg.Type {
		case "logs_follow", "logs_unfollow", "logs_keep", "logs_backfill":
			c.handleLogs(ctx, msg)
		case "docker_read":
			if c.readSem == nil {
				c.readSem = make(chan struct{}, 4)
			}
			select {
			case c.readSem <- struct{}{}:
				go func() { defer func() { <-c.readSem }(); c.agent.handleDockerRead(ctx, msg) }()
			default:
				c.send(AgentWSMessage{Type: "docker_read_result", SessionID: msg.SessionID, Data: json.RawMessage(`{"error":"too_many_reads","reason":"too_many_reads"}`)})
			}
		case "terminal_start":
			go c.agent.handleTerminalStart(ctx, msg)
		case "terminal_input":
			c.agent.handleTerminalInput(msg)
		case "terminal_resize":
			c.agent.handleTerminalResize(msg)
		case "terminal_close":
			c.agent.handleTerminalClose(msg)
		case "command":
			c.agent.handleCommandMessage(msg, "ws")
		case "command_receipt":
			c.agent.handleReceipt(msg)
		default:
			slog.Debug("agent ws: unknown message type", "type", msg.Type)
		}
	}
}

func (c *agentWSClient) setConn(conn agentWSConn) {
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
}

// clearConn drops the reference only if it still points at conn, so a stale
// handler exiting cannot clear a newer connection.
func (c *agentWSClient) clearConn(conn agentWSConn) {
	c.connMu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.connMu.Unlock()
}

func (c *agentWSClient) currentConn() agentWSConn {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn
}

// connected reports whether a live connection is currently registered.
func (c *agentWSClient) connected() bool {
	return c.currentConn() != nil
}

// send writes one message. It returns errNotConnected when there is no
// connection and the write error otherwise; a failed write also closes the
// connection so the read loop ends and the client reconnects (FR-012). It never
// terminates the process.
func (c *agentWSClient) send(msg AgentWSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("agent ws: marshal %s: %w", msg.Type, err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	conn := c.currentConn()
	if conn == nil {
		return errNotConnected
	}
	if err := conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); err != nil {
		c.failConn(conn)
		return fmt.Errorf("agent ws: set write deadline: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.failConn(conn)
		return fmt.Errorf("agent ws: write %s: %w", msg.Type, err)
	}
	return nil
}

// sendWS writes msg through the agent's WebSocket client. The client is
// assigned before any loop starts (Run), so reading the field here is safe.
func (a *Agent) sendWS(msg AgentWSMessage) error {
	if a.wsClient == nil {
		return errNotConnected
	}
	return a.wsClient.send(msg)
}

// failConn forgets and closes a connection whose write failed.
func (c *agentWSClient) failConn(conn agentWSConn) {
	c.clearConn(conn)
	conn.Close()
}
