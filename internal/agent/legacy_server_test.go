package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// legacyServer is a pre-CMD-LIFECYCLE server: it accepts the agent WebSocket,
// reads and ignores every message type except command_result, never sends a
// receipt, and answers 404 with a plain-text body for every other path (so the
// report endpoint does not exist).
type legacyServer struct {
	srv   *httptest.Server
	conns chan *websocket.Conn

	mu       sync.Mutex
	messages []AgentWSMessage
	httpHits []string
}

func newLegacyServer(t *testing.T) *legacyServer {
	t.Helper()
	ls := &legacyServer{conns: make(chan *websocket.Conn, 4)}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/agent", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ls.conns <- conn
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg AgentWSMessage
			if json.Unmarshal(data, &msg) == nil {
				ls.mu.Lock()
				ls.messages = append(ls.messages, msg)
				ls.mu.Unlock()
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ls.mu.Lock()
		ls.httpHits = append(ls.httpHits, r.Method+" "+r.URL.Path)
		ls.mu.Unlock()
		http.NotFound(w, r)
	})
	ls.srv = httptest.NewServer(mux)
	t.Cleanup(ls.srv.Close)
	return ls
}

func (ls *legacyServer) typed(typ string) []AgentWSMessage {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	var out []AgentWSMessage
	for _, m := range ls.messages {
		if m.Type == typ {
			out = append(out, m)
		}
	}
	return out
}

func (ls *legacyServer) reportHits() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.httpHits)
}

// sendCommand pushes a `command` message to the agent, as a legacy server does.
func sendCommand(t *testing.T, conn *websocket.Conn, id, action string) {
	t.Helper()
	data, err := json.Marshal(models.CommandPayload{CommandID: id, Action: action, Params: map[string]interface{}{}})
	require.NoError(t, err)
	msg, err := json.Marshal(AgentWSMessage{Type: "command", Data: data})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, msg))
}

// AC-100 (FR-082): a new agent against a legacy server completes the command
// and delivers the final result over the WebSocket, despite the missing report
// endpoint and the ignored ack.
func TestCL_AC100_NewAgentAgainstLegacyServer(t *testing.T) {
	captureLogs(t)
	ls := newLegacyServer(t)

	clk := newFakeClock()
	a := &Agent{config: &Config{}, telemetryNow: make(chan struct{}, 1)}
	a.config.Server.URL = ls.srv.URL
	a.config.Server.APIKey = "key"
	a.config.Agent.DeviceID = "dev-1"
	a.client = NewHTTPClient(ls.srv.URL, "key")
	a.wsClient = newAgentWSClient(a)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.cmdCtx, a.cmdCancel = context.WithCancel(context.Background())
	defer a.cmdCancel()
	dir := t.TempDir()
	a.setupCommandSystem(a.cmdCtx, commandSystemDeps{
		clk: clk, fs: osStateFS{}, tr: agentTransport{a: a},
		outboxPath:   filepath.Join(dir, "command-outbox.json"),
		seenPath:     filepath.Join(dir, "seen-commands.json"),
		inflightPath: filepath.Join(dir, "inflight-commands.json"),
	})
	a.executor = func(cr *cmdRun) commandExecResult {
		cr.Step(models.StepRecreate)
		return commandExecResult{Status: models.ResultStatusSuccess, Message: "updated"}
	}

	var loops sync.WaitGroup
	loops.Add(2)
	go func() { defer loops.Done(); a.wsClient.run(ctx) }()
	go func() { defer loops.Done(); a.out.Run(ctx) }()
	defer func() {
		cancel()
		loops.Wait()
	}()

	var srvConn *websocket.Conn
	select {
	case srvConn = <-ls.conns:
	case <-time.After(10 * time.Second):
		t.Fatal("[AC-100] the agent never connected to the legacy server")
	}
	// Wait for the client to register its connection before commanding it.
	require.Eventually(t, a.wsClient.connected, 5*time.Second, time.Millisecond)
	defer srvConn.Close()

	// First command: ack is written (the agent does not know the server is
	// legacy yet), the result goes over WS, and the missing receipt then drives
	// the HTTP fallback which discovers the 404.
	sendCommand(t, srvConn, idA, "fetch_logs")
	require.Eventually(t, func() bool { return len(ls.typed("command_result")) == 1 }, 10*time.Second, time.Millisecond,
		"[AC-100] the final result must be delivered over the WebSocket")

	var res models.CommandResult
	require.NoError(t, json.Unmarshal(ls.typed("command_result")[0].Data, &res))
	assert.Equal(t, idA, res.CommandID)
	assert.Equal(t, "success", res.Status, "[AC-100] the legacy status vocabulary is preserved")
	assert.Equal(t, models.StepRecreate, res.Step)

	require.Eventually(t, func() bool { return len(clk.timerDurations()) > 0 }, 5*time.Second, time.Millisecond,
		"[AC-100] the outbox must be waiting for a receipt")
	clk.Advance(outboxReceiptWait)
	require.Eventually(t, func() bool { return a.out.Legacy() && !a.out.Has(idA) }, 10*time.Second, time.Millisecond,
		"[AC-100] the HTTP 404 must put the agent in legacy mode and clear the entry that was already written over WS")
	assert.GreaterOrEqual(t, ls.reportHits(), 1, "[AC-100] the agent must have probed the HTTP report endpoint once")

	// Second command in legacy mode: no ack, result removed right after the WS
	// write, no further HTTP.
	hits := ls.reportHits()
	sendCommand(t, srvConn, idB, "fetch_logs")
	require.Eventually(t, func() bool { return len(ls.typed("command_result")) == 2 }, 10*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return !a.out.Has(idB) }, 10*time.Second, time.Millisecond,
		"[AC-100] in legacy mode a successful WS write removes the entry")

	acks := ls.typed("command_ack")
	require.Len(t, acks, 1, "[AC-100] only the first command is acked; legacy mode skips it")
	assert.Equal(t, hits, ls.reportHits(), "[AC-100] legacy mode must not retry HTTP within the hour")
}

// v267Server behaves like a v2.67.0 server on the telemetry route: it decodes
// the snapshot into the fields that release knew, answers 200 {"status":"ok"}
// or a plain-text error, and knows nothing of Retry-After or throttling.
type v267Server struct {
	srv *httptest.Server

	mu        sync.Mutex
	down      bool
	delivered map[string]int
	topKeys   map[string]bool
}

func newV267Server(t *testing.T) *v267Server {
	t.Helper()
	s := &v267Server{delivered: map[string]int{}, topKeys: map[string]bool{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.down {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for k := range raw {
			s.topKeys[k] = true
		}
		var deviceID string
		_ = json.Unmarshal(raw["device_id"], &deviceID)
		s.delivered[deviceID]++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *v267Server) setDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
}

func (s *v267Server) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.delivered {
		n += c
	}
	return n
}

// AC-073 (NFR-010): a new agent pushes and flushes against a v2.67.0 server,
// and all data is delivered. That server answers 503 with a plain-text body
// while it is down, never sends Retry-After, and knows only the original
// snapshot fields.
func TestGROWTH_AC073_NewAgentDeliversToLegacyServer(t *testing.T) {
	captureLogs(t)
	srv := newV267Server(t)
	buf := openTestBuffer(t, filepath.Join(t.TempDir(), "buffer.db"), 50<<20)
	cfg := &Config{}
	cfg.Agent.DeviceID = "dev"
	a := &Agent{
		config:   cfg,
		client:   NewHTTPClient(srv.srv.URL, "key"),
		buffer:   buf,
		registry: collectors.NewRegistry(),
	}

	// The server is down for three pushes: each is buffered, none is lost, and
	// there is no back-off to delay recovery (a 503 without Retry-After).
	srv.setDown(true)
	for i := 0; i < 3; i++ {
		a.sendTelemetry(context.Background())
	}
	require.Equal(t, 3, buf.Len(), "[AC-073] the pushes made while the server is down are buffered")
	require.Equal(t, 0, srv.total())

	// Two more items of another origin are also waiting.
	require.NoError(t, buf.Store(bufSnap("extra-1", 10)))
	require.NoError(t, buf.Store(bufSnap("extra-2", 10)))

	// The server returns. The next push delivers itself and the whole backlog.
	srv.setDown(false)
	a.sendTelemetry(context.Background())

	assert.Equal(t, 0, buf.Len(), "[AC-073] the buffer must be empty after the flush")
	assert.Equal(t, 6, srv.total(), "[AC-073] 1 live push + 3 buffered pushes + 2 extra items must all arrive")
	srv.mu.Lock()
	defer srv.mu.Unlock()
	assert.Equal(t, 4, srv.delivered["dev"])
	assert.Equal(t, 1, srv.delivered["extra-1"])
	assert.Equal(t, 1, srv.delivered["extra-2"])
	for key := range srv.topKeys {
		assert.Contains(t, []string{"device_id", "timestamp", "data"}, key,
			"[AC-073] the snapshot must carry no top-level field the v2.67.0 server did not know")
	}
}
