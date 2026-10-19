package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ---------------------------------------------------------------------------
// WebSocket fakes
// ---------------------------------------------------------------------------

// fakeWSConn is an agentWSConn that fails the test's assertions when two
// writes overlap (FR-010) and records every message written.
type fakeWSConn struct {
	mu       sync.Mutex
	writes   [][]byte
	writeErr error
	closed   bool

	inWrite atomic.Int32
	overlap atomic.Bool

	// reads feeds ReadMessage. A nil channel makes ReadMessage return an error
	// immediately (a connection that drops at once).
	reads  chan []byte
	closeC chan struct{}
	once   sync.Once
}

func newFakeWSConn() *fakeWSConn {
	return &fakeWSConn{reads: make(chan []byte, 16), closeC: make(chan struct{})}
}

func (f *fakeWSConn) ReadMessage() (int, []byte, error) {
	if f.reads == nil {
		return 0, nil, errors.New("fake ws: connection dropped")
	}
	select {
	case msg := <-f.reads:
		return 1, msg, nil
	case <-f.closeC:
		return 0, nil, errors.New("fake ws: closed")
	}
}

func (f *fakeWSConn) WriteMessage(_ int, data []byte) error {
	if f.inWrite.Add(1) > 1 {
		f.overlap.Store(true)
	}
	defer f.inWrite.Add(-1)
	// Yield so an unserialized second writer has a chance to enter.
	runtime.Gosched()

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, append([]byte(nil), data...))
	return nil
}

func (f *fakeWSConn) SetWriteDeadline(time.Time) error { return nil }

func (f *fakeWSConn) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	if f.closeC != nil {
		f.once.Do(func() { close(f.closeC) })
	}
	return nil
}

func (f *fakeWSConn) written() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.writes...)
}

func (f *fakeWSConn) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// newTestWSClient builds a client over conn without touching the network.
func newTestWSClient(conn agentWSConn) *agentWSClient {
	a := &Agent{config: &Config{}, telemetryNow: make(chan struct{}, 1)}
	c := &agentWSClient{
		serverURL: "http://server.test",
		apiKey:    "key",
		deviceID:  "dev-1",
		agent:     a,
		sleep:     func(context.Context, time.Duration) bool { return false },
	}
	c.dial = func(context.Context, string, http.Header) (agentWSConn, error) { return conn, nil }
	a.wsClient = c
	return c
}

// ---------------------------------------------------------------------------
// Log capture
// ---------------------------------------------------------------------------

// logCapture collects slog records emitted while it is installed as the
// default logger.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// captureLogs installs a text logger at Debug level and restores the previous
// default when the test ends. Tests that use it must not run in parallel.
func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	lc := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(lc, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return lc
}

func (l *logCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logCapture) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// ---------------------------------------------------------------------------
// Clock
// ---------------------------------------------------------------------------

// fakeClock is a manual clock: timers fire only when Advance moves time past
// their deadline, so a test controls exactly when a limit or a backoff elapses.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
	// created records the duration of every timer ever requested, in order.
	created []time.Duration
	// timerCreated is signalled (non-blocking) whenever NewTimer is called.
	timerCreated chan struct{}
}

type fakeTimer struct {
	c        chan time.Time
	deadline time.Time
	stopped  bool
	fired    bool
	clk      *fakeClock
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		now:          time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		timerCreated: make(chan struct{}, 64),
	}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) NewTimer(d time.Duration) clockTimer {
	f.mu.Lock()
	t := &fakeTimer{c: make(chan time.Time, 1), deadline: f.now.Add(d), clk: f}
	f.timers = append(f.timers, t)
	f.created = append(f.created, d)
	f.mu.Unlock()
	select {
	case f.timerCreated <- struct{}{}:
	default:
	}
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	active := !t.stopped && !t.fired
	t.stopped = true
	return active
}

// Advance moves time forward and fires every timer whose deadline has passed.
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	var fire []*fakeTimer
	for _, t := range f.timers {
		if !t.stopped && !t.fired && !t.deadline.After(f.now) {
			t.fired = true
			fire = append(fire, t)
		}
	}
	now := f.now
	f.mu.Unlock()
	for _, t := range fire {
		t.c <- now
	}
}

// timerDurations returns the duration of every timer requested so far.
func (f *fakeClock) timerDurations() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.created...)
}

// ---------------------------------------------------------------------------
// State-file fakes
// ---------------------------------------------------------------------------

// faultFS wraps the real file system with an operation log and injectable
// failures. Operation names: mkdir, create, write, sync, close, rename, syncdir.
type faultFS struct {
	inner osStateFS

	mu   sync.Mutex
	log  []string
	fail map[string]error
	// temps records the base name of every temp file created.
	temps []string
}

func newFaultFS() *faultFS { return &faultFS{fail: map[string]error{}} }

func (f *faultFS) record(op string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, op)
	return f.fail[op]
}

// failOn makes every later operation of that kind return err (nil clears it).
func (f *faultFS) failOn(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.fail, op)
		return
	}
	f.fail[op] = err
}

// ops returns the operation log.
func (f *faultFS) ops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *faultFS) MkdirAll(path string, perm os.FileMode) error {
	if err := f.record("mkdir"); err != nil {
		return err
	}
	return f.inner.MkdirAll(path, perm)
}

func (f *faultFS) CreateTemp(dir, pattern string) (stateFile, error) {
	if err := f.record("create"); err != nil {
		return nil, err
	}
	file, err := f.inner.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.temps = append(f.temps, filepath.Base(file.Name()))
	f.mu.Unlock()
	return &faultFile{stateFile: file, fs: f}, nil
}

func (f *faultFS) Rename(oldpath, newpath string) error {
	if err := f.record("rename"); err != nil {
		return err
	}
	return f.inner.Rename(oldpath, newpath)
}

func (f *faultFS) ReadFile(path string) ([]byte, error) { return f.inner.ReadFile(path) }

func (f *faultFS) Remove(path string) error {
	f.mu.Lock()
	f.log = append(f.log, "remove")
	f.mu.Unlock()
	return f.inner.Remove(path)
}

func (f *faultFS) SyncDir(dir string) error {
	if err := f.record("syncdir"); err != nil {
		return err
	}
	return f.inner.SyncDir(dir)
}

type faultFile struct {
	stateFile
	fs *faultFS
}

func (f *faultFile) Write(p []byte) (int, error) {
	if err := f.fs.record("write"); err != nil {
		return 0, err
	}
	return f.stateFile.Write(p)
}

func (f *faultFile) Sync() error {
	if err := f.fs.record("sync"); err != nil {
		return err
	}
	return f.stateFile.Sync()
}

func (f *faultFile) Close() error {
	if err := f.fs.record("close"); err != nil {
		f.stateFile.Close()
		return err
	}
	return f.stateFile.Close()
}

// memStateFS is an in-memory stateFS for tests that write many documents; it
// avoids the cost of a real fsync per write.
type memStateFS struct {
	mu    sync.Mutex
	files map[string][]byte
	seq   int
}

func newMemStateFS() *memStateFS { return &memStateFS{files: map[string][]byte{}} }

func (m *memStateFS) MkdirAll(string, os.FileMode) error { return nil }

func (m *memStateFS) CreateTemp(dir, pattern string) (stateFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	name := filepath.Join(dir, strings.Replace(pattern, "*", fmt.Sprintf("%06d", m.seq), 1))
	return &memFile{fs: m, name: name}, nil
}

func (m *memStateFS) Rename(oldpath, newpath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[oldpath]
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldpath, Err: fs.ErrNotExist}
	}
	m.files[newpath] = data
	delete(m.files, oldpath)
	return nil
}

func (m *memStateFS) ReadFile(path string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[path]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), data...), nil
}

func (m *memStateFS) Remove(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[path]; !ok {
		return &fs.PathError{Op: "remove", Path: path, Err: fs.ErrNotExist}
	}
	delete(m.files, path)
	return nil
}

func (m *memStateFS) SyncDir(string) error { return nil }

type memFile struct {
	fs   *memStateFS
	name string
	buf  bytes.Buffer
}

func (f *memFile) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *memFile) Sync() error                 { return nil }
func (f *memFile) Name() string                { return f.name }
func (f *memFile) Close() error {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	f.fs.files[f.name] = append([]byte(nil), f.buf.Bytes()...)
	return nil
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

// fakeTransport records every message the agent would send and lets a test
// script the HTTP answers. It implements cmdTransport and outboxTransport.
type fakeTransport struct {
	mu sync.Mutex

	wsUp        bool
	wsErr       error
	wsSent      []AgentWSMessage
	posts       []fakePost
	events      []*models.DockerEvent
	agentEvents []*models.AgentEvent

	// postHandler answers PostReport. Nil answers 200 echoing the command ID.
	postHandler func(commandID string, rep models.CommandReport) (*ReportReply, error)
	// log is an optional shared ordered log of "ws:<type>", "post:<kind>" and
	// "event:<action>" entries.
	log *eventLog
}

type fakePost struct {
	CommandID string
	Report    models.CommandReport
}

func (f *fakeTransport) WSConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wsUp
}

func (f *fakeTransport) SendWS(msg AgentWSMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.wsErr != nil {
		return f.wsErr
	}
	if !f.wsUp {
		return errNotConnected
	}
	f.wsSent = append(f.wsSent, msg)
	f.log.add("ws:" + msg.Type)
	return nil
}

func (f *fakeTransport) PostReport(_ context.Context, commandID string, rep models.CommandReport) (*ReportReply, error) {
	f.mu.Lock()
	f.posts = append(f.posts, fakePost{CommandID: commandID, Report: rep})
	h := f.postHandler
	f.mu.Unlock()
	f.log.add("post:" + string(rep.Kind))
	if h != nil {
		return h(commandID, rep)
	}
	return replyOK(commandID), nil
}

func (f *fakeTransport) SendDockerEvent(_ context.Context, evt *models.DockerEvent) error {
	f.mu.Lock()
	f.events = append(f.events, evt)
	f.mu.Unlock()
	f.log.add("event:" + evt.Action)
	return nil
}

func (f *fakeTransport) ReportEvent(_ context.Context, evt *models.AgentEvent) error {
	f.mu.Lock()
	f.agentEvents = append(f.agentEvents, evt)
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) wsMessages(typ string) []AgentWSMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []AgentWSMessage
	for _, m := range f.wsSent {
		if m.Type == typ {
			out = append(out, m)
		}
	}
	return out
}

func (f *fakeTransport) postCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

func (f *fakeTransport) dockerEventActions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	for i, e := range f.events {
		out[i] = e.Action
	}
	return out
}

func replyOK(commandID string) *ReportReply {
	return &ReportReply{StatusCode: 200, Response: models.ReportResponse{
		CommandID: commandID, Result: models.ReportAccepted, Status: "succeeded"}}
}

func replyStatus(status int, code string) *ReportReply {
	return &ReportReply{StatusCode: status, Response: models.ReportResponse{Code: code}}
}

// eventLog is a goroutine-safe ordered log shared by several fakes so a test
// can assert the relative order of events from different components.
type eventLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *eventLog) add(s string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.entries = append(l.entries, s)
	l.mu.Unlock()
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// index returns the position of the first entry equal to s, or -1.
func (l *eventLog) index(s string) int {
	for i, e := range l.all() {
		if e == s {
			return i
		}
	}
	return -1
}

// indexPrefix returns the position of the first entry with the prefix, or -1.
func (l *eventLog) indexPrefix(p string) int {
	for i, e := range l.all() {
		if strings.HasPrefix(e, p) {
			return i
		}
	}
	return -1
}

// newTestOutbox builds an outbox over a temp directory with a fake clock and
// transport.
func newTestOutbox(t *testing.T) (*outbox, *fakeTransport, *fakeClock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "command-outbox.json")
	clk := newFakeClock()
	tr := &fakeTransport{}
	return newOutbox(path, osStateFS{}, clk, tr), tr, clk, path
}

// newMemOutbox is newTestOutbox over an in-memory file system, for tests that
// add hundreds of entries.
func newMemOutbox(t *testing.T) (*outbox, *fakeTransport, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	tr := &fakeTransport{}
	return newOutbox("/state/command-outbox.json", newMemStateFS(), clk, tr), tr, clk
}

// testResult builds a failed-or-succeeded final result for a command ID.
func testResult(id string) models.CommandResult {
	return models.CommandResult{CommandID: id, Status: models.ResultStatusSuccess, Message: "done",
		Outcome: models.OutcomeSucceeded}
}
