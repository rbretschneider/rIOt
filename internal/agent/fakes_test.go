package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
