package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	ws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSocket is an agentSocket that records writes and closes.
type fakeSocket struct {
	mu       sync.Mutex
	writes   []agentWSMessage
	closed   bool
	writeErr error
}

func (f *fakeSocket) WriteJSON(v interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	msg, ok := v.(agentWSMessage)
	if !ok {
		return fmt.Errorf("fakeSocket: unexpected %T", v)
	}
	f.writes = append(f.writes, msg)
	return nil
}

func (f *fakeSocket) WriteControl(int, []byte, time.Time) error { return nil }

func (f *fakeSocket) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeSocket) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// messages returns the recorded writes of one type.
func (f *fakeSocket) messages(typ string) []agentWSMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []agentWSMessage
	for _, m := range f.writes {
		if m.Type == typ {
			out = append(out, m)
		}
	}
	return out
}

// receipts decodes the command_receipt messages written to the socket.
func (f *fakeSocket) receipts(t *testing.T) []models.CommandReceipt {
	t.Helper()
	var out []models.CommandReceipt
	for _, m := range f.messages("command_receipt") {
		var r models.CommandReceipt
		require.NoError(t, json.Unmarshal(m.Data, &r))
		out = append(out, r)
	}
	return out
}

// testClock is a settable clock.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock {
	return &testClock{t: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// uid returns a canonical lowercase UUID for test command n.
func uid(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

// lcEnv is a Handlers wired to the faithful mocks, a private agent registry, a
// fake clock and a captured log, with two devices (dev-1, dev-2).
type lcEnv struct {
	t     *testing.T
	h     *Handlers
	repo  *testutil.MockCommandRepo
	devs  *testutil.MockDeviceRepo
	svc   *commands.Service
	reg   *agentRegistry
	sock  *fakeSocket // dev-1's connected agent socket
	conn  *AgentConn
	bc    *testutil.FakeBroadcaster
	clock *testClock
	logs  *bytes.Buffer
}

func newLCEnv(t *testing.T) *lcEnv {
	t.Helper()
	e := &lcEnv{
		t: t, repo: testutil.NewMockCommandRepo(), devs: testutil.NewMockDeviceRepo(),
		reg: newAgentRegistry(), sock: &fakeSocket{}, bc: &testutil.FakeBroadcaster{}, clock: newTestClock(),
		logs: &bytes.Buffer{},
	}
	e.devs.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host-1", Status: models.DeviceStatusOnline}
	e.devs.Devices["dev-2"] = &models.Device{ID: "dev-2", Hostname: "host-2", Status: models.DeviceStatusOnline}
	e.devs.APIKeys["key-1"] = "dev-1"
	e.devs.APIKeys["key-2"] = "dev-2"
	logger := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.svc = commands.New(commands.Options{
		Commands: e.repo, Agents: agentSender{reg: e.reg}, Broadcaster: e.bc,
		Now: e.clock.Now, Logger: logger,
	})
	e.conn = &AgentConn{DeviceID: "dev-1", Conn: e.sock}
	e.reg.replace("dev-1", e.conn)
	hub := websocket.NewHub()
	go hub.Run()
	gen := events.NewGenerator(testutil.NewMockEventRepo(), hub, testutil.NewMockAlertRuleRepo(), testutil.NewMockDispatcher(), e.repo)
	e.h = &Handlers{commandRepo: e.repo, devices: e.devs, commands: e.svc, agents: e.reg, now: e.clock.Now, hub: hub, eventGen: gen}
	return e
}

// seed stores a command directly in the mock.
func (e *lcEnv) seed(id, deviceID, action, status string, mod func(*models.Command)) *models.Command {
	e.t.Helper()
	now := e.clock.Now()
	c := &models.Command{
		ID: id, DeviceID: deviceID, Action: action, Params: map[string]interface{}{}, Status: status,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
		ExecDeadlineS: models.ExecDeadlineSeconds(action, 1), Origin: models.OriginUser,
	}
	switch models.CommandState(status).Canonical() {
	case models.CommandDispatched, models.CommandAcknowledged, models.CommandRunning, models.CommandVerifying:
		dl := now.Add(time.Duration(c.ExecDeadlineS) * time.Second)
		c.DispatchedAt, c.DeadlineAt = &now, &dl
	}
	if mod != nil {
		mod(c)
	}
	e.repo.Commands[id] = c
	return c
}

func (e *lcEnv) get(id string) models.Command {
	e.t.Helper()
	c, ok := e.repo.Get(id)
	require.True(e.t, ok, "command %s must exist", id)
	return c
}

// wsMsg builds an agent WS message of the given type.
func wsMsg(t *testing.T, typ string, data interface{}) agentWSMessage {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	return agentWSMessage{Type: typ, Data: raw}
}

// ---------------------------------------------------------------------------
// AC-076, AC-077: connection registry
// ---------------------------------------------------------------------------

func TestCL_AC076_OldHandlerExitKeepsNewConn(t *testing.T) {
	reg := newAgentRegistry()
	a := &AgentConn{DeviceID: "dev-1", Conn: &fakeSocket{}}
	b := &AgentConn{DeviceID: "dev-1", Conn: &fakeSocket{}}
	reg.replace("dev-1", a)
	reg.replace("dev-1", b)

	removed := reg.unregister("dev-1", a) // A's handler exits after B registered

	assert.False(t, removed, "[AC-076] a stale handler must not remove the entry")
	assert.Same(t, b, reg.get("dev-1"), "[AC-076] B must still be registered")
	assert.True(t, reg.connected("dev-1"), "[AC-076] the device must still report as connected")

	assert.True(t, reg.unregister("dev-1", b), "the owner can remove its own entry")
	assert.False(t, reg.connected("dev-1"))
}

func TestCL_AC077_NewRegistrationClosesPrevious(t *testing.T) {
	reg := newAgentRegistry()
	sockA, sockB := &fakeSocket{}, &fakeSocket{}
	reg.replace("dev-1", &AgentConn{DeviceID: "dev-1", Conn: sockA})

	reg.replace("dev-1", &AgentConn{DeviceID: "dev-1", Conn: sockB})

	assert.True(t, sockA.isClosed(), "[AC-077] registering B must close A")
	assert.False(t, sockB.isClosed())
}

func TestAgentRegistry_ConcurrentReplaceAndUnregister(t *testing.T) {
	reg := newAgentRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ac := &AgentConn{DeviceID: "dev-1", Conn: &fakeSocket{}}
			reg.replace("dev-1", ac)
			reg.unregister("dev-1", ac)
			_ = reg.connected("dev-1")
		}()
	}
	wg.Wait()
}

func TestAgentSender_SendAndNotConnected(t *testing.T) {
	e := newLCEnv(t)
	sender := agentSender{reg: e.reg}

	sent, err := sender.SendCommand("dev-1", models.CommandPayload{CommandID: uid(1), Action: "reboot"})
	require.NoError(t, err)
	assert.True(t, sent)
	msgs := e.sock.messages("command")
	require.Len(t, msgs, 1)
	var p models.CommandPayload
	require.NoError(t, json.Unmarshal(msgs[0].Data, &p))
	assert.Equal(t, uid(1), p.CommandID)

	sent, err = sender.SendCommand("dev-2", models.CommandPayload{CommandID: uid(2)})
	require.NoError(t, err, "a device that is not connected is not an error")
	assert.False(t, sent)

	e.sock.writeErr = fmt.Errorf("broken pipe")
	sent, err = sender.SendCommand("dev-1", models.CommandPayload{CommandID: uid(3)})
	assert.False(t, sent)
	require.Error(t, err, "a failed write must surface so the service can revert the claim")
}

// ---------------------------------------------------------------------------
// WS reports are scoped to the authenticated socket (AC-024, SEC-014, C-11)
// ---------------------------------------------------------------------------

func TestCL_AC024_WSForeignResultIgnored(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-2", "docker_update", "dispatched", nil) // belongs to device B
	before := e.get(uid(1))

	// Device A's socket reports a result for B's command.
	e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result",
		models.CommandResult{CommandID: uid(1), Status: "success", Message: "forged", Output: "forged output"}))

	assert.Equal(t, before, e.get(uid(1)), "[AC-024] a foreign result must leave the row unchanged")
	assert.Empty(t, e.repo.Outputs, "[AC-024] ... and must not store output")
	assert.Empty(t, e.bc.Updates(), "[AC-024] ... and must not broadcast")
}

func TestCL_AC024_WSForeignAckAndProgressIgnored(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-2", "docker_update", "dispatched", nil)
	before := e.get(uid(1))

	e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_ack", models.CommandAck{CommandID: uid(1)}))
	e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_progress",
		models.CommandProgress{CommandID: uid(1), State: "running", Step: "pull"}))
	e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_progress",
		models.CommandProgress{CommandID: uid(1), State: "verifying", Step: "verify"}))

	assert.Equal(t, before, e.get(uid(1)), "[C-11] a foreign ack or progress must leave the row unchanged")
	assert.Empty(t, e.bc.Updates())
	assert.Empty(t, e.sock.receipts(t), "acks and progress never get a receipt")
}

func TestCL_SEC_WSForeignAndUnknownResultReceiptsIdentical(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-2", "docker_update", "dispatched", nil)

	for _, id := range []string{uid(1), uid(99)} {
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result",
			models.CommandResult{CommandID: id, Status: "success"}))
	}

	receipts := e.sock.receipts(t)
	require.Len(t, receipts, 2)
	assert.Equal(t, models.ReportNotFound, receipts[0].Result)
	assert.Equal(t, "", receipts[0].Status, "[C-11] a foreign command must not leak its status")
	assert.Equal(t, receipts[0].Result, receipts[1].Result)
	assert.Equal(t, receipts[0].Status, receipts[1].Status, "[C-11] foreign and unknown receipts must be indistinguishable")
}

func TestWS_ResultReceiptForEveryProcessedOutcome(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)

	send := func(status string) {
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result",
			models.CommandResult{CommandID: uid(1), Status: status, Message: "m"}))
	}
	send("success") // accepted
	send("success") // duplicate
	send("error")   // already_terminal

	var got []models.ReportDisposition
	for _, r := range e.sock.receipts(t) {
		assert.Equal(t, uid(1), r.CommandID)
		got = append(got, r.Result)
	}
	assert.Equal(t, []models.ReportDisposition{models.ReportAccepted, models.ReportDuplicate, models.ReportAlreadyTerminal}, got)
	assert.Equal(t, "succeeded", e.sock.receipts(t)[0].Status)
}

func TestWS_UndecodableMessageDroppedWithoutReceipt(t *testing.T) {
	e := newLCEnv(t)
	e.h.handleAgentMessage(context.Background(), e.conn, agentWSMessage{Type: "command_result", Data: json.RawMessage(`{"command_id": 12}`)})
	assert.Empty(t, e.sock.receipts(t))
	assert.Empty(t, e.repo.Calls())
}

func TestCL_SEC014_TerminalMessagesScopedToDevice(t *testing.T) {
	browser := &fakeBrowser{}
	terminalBrowserConns.Lock()
	terminalBrowserConns.m["sess-sec014"] = terminalSession{conn: browser, deviceID: "dev-1"}
	terminalBrowserConns.Unlock()
	t.Cleanup(func() {
		terminalBrowserConns.Lock()
		delete(terminalBrowserConns.m, "sess-sec014")
		terminalBrowserConns.Unlock()
	})
	e := newLCEnv(t)
	foreign := &AgentConn{DeviceID: "dev-2", Conn: &fakeSocket{}}
	out, _ := json.Marshal("rm -rf /")

	// dev-2's agent tries to write to, and to close, dev-1's session.
	e.h.handleAgentMessage(context.Background(), foreign, agentWSMessage{Type: "terminal_output", SessionID: "sess-sec014", Data: out})
	e.h.handleAgentMessage(context.Background(), foreign, agentWSMessage{Type: "terminal_close", SessionID: "sess-sec014"})

	assert.Empty(t, browser.written(), "[SEC-014] a foreign agent must not write to the session")
	assert.False(t, browser.isClosed(), "[SEC-014] a foreign agent must not close the session")
	terminalBrowserConns.RLock()
	_, stillThere := terminalBrowserConns.m["sess-sec014"]
	terminalBrowserConns.RUnlock()
	assert.True(t, stillThere, "the session stays registered")

	// The owning device can.
	e.h.handleAgentMessage(context.Background(), e.conn, agentWSMessage{Type: "terminal_output", SessionID: "sess-sec014", Data: out})
	assert.Equal(t, []string{"rm -rf /"}, browser.written(), "the owner's output reaches the browser")
	e.h.handleAgentMessage(context.Background(), e.conn, agentWSMessage{Type: "terminal_close", SessionID: "sess-sec014"})
	assert.True(t, browser.isClosed())
}

type fakeBrowser struct {
	mu     sync.Mutex
	data   []string
	closed bool
}

func (f *fakeBrowser) WriteMessage(_ int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = append(f.data, string(data))
	return nil
}

func (f *fakeBrowser) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeBrowser) written() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.data...)
}

func (f *fakeBrowser) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// ---------------------------------------------------------------------------
// End to end over a real WebSocket
// ---------------------------------------------------------------------------

func dialAgent(t *testing.T, srv *httptest.Server, deviceID, key string) *ws.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/agent"
	hdr := http.Header{}
	hdr.Set("X-rIOt-Device", deviceID)
	hdr.Set("X-rIOt-Key", key)
	conn, resp, err := ws.DefaultDialer.Dial(url, hdr)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestHandleAgentWS_EndToEndReceiptAndReconnect(t *testing.T) {
	e := newLCEnv(t)
	e.reg.unregister("dev-1", e.conn) // the fake connection is replaced by real ones
	r := chi.NewRouter()
	r.Get("/ws/agent", e.h.HandleAgentWS)
	srv := httptest.NewServer(r)
	defer srv.Close()
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)

	first := dialAgent(t, srv, "dev-1", "key-1")
	require.Eventually(t, func() bool { return e.reg.connected("dev-1") }, 3*time.Second, 5*time.Millisecond)

	// A result over the real socket gets a receipt back.
	raw, _ := json.Marshal(models.CommandResult{CommandID: uid(1), Status: "success", Message: "ok"})
	require.NoError(t, first.WriteJSON(agentWSMessage{Type: "command_result", Data: raw}))
	require.NoError(t, first.SetReadDeadline(time.Now().Add(3*time.Second)))
	var reply agentWSMessage
	require.NoError(t, first.ReadJSON(&reply))
	assert.Equal(t, "command_receipt", reply.Type)
	var receipt models.CommandReceipt
	require.NoError(t, json.Unmarshal(reply.Data, &receipt))
	assert.Equal(t, models.ReportAccepted, receipt.Result)
	assert.Equal(t, "succeeded", e.get(uid(1)).Status)

	// The agent reconnects before the old handler exits: the old connection is
	// closed, and the old handler's exit must not deregister the new one.
	second := dialAgent(t, srv, "dev-1", "key-1")
	require.NoError(t, first.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err := first.ReadMessage()
	require.Error(t, err, "[AC-077] the previous connection must be closed")

	time.Sleep(100 * time.Millisecond) // let the old handler run its deferred cleanup
	assert.True(t, e.reg.connected("dev-1"), "[AC-076] the device must still report as connected")
	_ = second
}

func TestHandleAgentWS_RejectsWrongKey(t *testing.T) {
	e := newLCEnv(t)
	r := chi.NewRouter()
	r.Get("/ws/agent", e.h.HandleAgentWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	hdr := http.Header{}
	hdr.Set("X-rIOt-Device", "dev-1")
	hdr.Set("X-rIOt-Key", "key-2") // dev-2's key
	_, resp, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/agent", hdr)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
