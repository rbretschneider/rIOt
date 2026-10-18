package agent

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC-013: no two writes may be in progress at the same instant.
func TestCL_AC013_ConcurrentSendsNeverOverlap(t *testing.T) {
	conn := newFakeWSConn()
	c := newTestWSClient(conn)
	c.setConn(conn)

	const senders = 50
	var wg sync.WaitGroup
	errs := make(chan error, senders)
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			typ := "command_result"
			if i%2 == 0 {
				typ = "terminal_output"
			}
			errs <- c.send(AgentWSMessage{Type: typ, SessionID: "s"})
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "[AC-013] every concurrent send must succeed")
	}
	assert.False(t, conn.overlap.Load(), "[AC-013] two writes were in progress at the same instant")
	assert.Len(t, conn.written(), senders, "[AC-013] every message must be written exactly once")
}

// AC-014: connect, disconnect and send loops must be race free. The assertion
// is made by the race detector (Linux CI runs go test -race); on other hosts
// the test still proves nothing panics or deadlocks.
func TestCL_AC014_ConnectDisconnectSendRaceFree(t *testing.T) {
	captureLogs(t) // connect logs once per cycle; keep the test output readable
	c := newTestWSClient(nil)
	// Each dial returns a connection whose read side drops immediately, so
	// connect cycles as fast as the loop can spin.
	c.dial = func(context.Context, string, http.Header) (agentWSConn, error) {
		conn := newFakeWSConn()
		conn.reads = nil
		return conn, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := c.connect(ctx); err == nil {
				t.Error("[AC-014] connect on a dropped connection must return the read error")
				return
			}
		}
	}()
	for s := 0; s < 4; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				// Either outcome is valid; the point is the unsynchronised access.
				_ = c.send(AgentWSMessage{Type: "terminal_output"})
				_ = c.connected()
			}
		}()
	}
	wg.Wait()
}

// AC-015 (FR-012): a failed write is returned to the caller and does not
// terminate the process; the connection is dropped so the client reconnects.
func TestCL_AC015_SendReturnsWriteError(t *testing.T) {
	boom := errors.New("write: broken pipe")
	conn := newFakeWSConn()
	conn.writeErr = boom
	c := newTestWSClient(conn)
	c.setConn(conn)

	err := c.send(AgentWSMessage{Type: "command_result"})

	require.Error(t, err, "[AC-015] a failed write must surface to the caller")
	assert.ErrorIs(t, err, boom, "[AC-015] the write error must be wrapped, not replaced")
	assert.True(t, conn.isClosed(), "[AC-015] a failed write must close the connection so the read loop ends")
	assert.False(t, c.connected(), "[AC-015] the failed connection must be forgotten")
	assert.ErrorIs(t, c.send(AgentWSMessage{Type: "command_result"}), errNotConnected,
		"[AC-015] with no connection, send reports errNotConnected instead of silently succeeding")
}

// AC-078 (FR-065): after the backoff has grown to its cap, a successful connect
// followed by a drop waits only the initial delay.
func TestCL_AC078_BackoffResetsAfterConnect(t *testing.T) {
	var delays []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := newTestWSClient(nil)
	c.sleep = func(_ context.Context, d time.Duration) bool {
		delays = append(delays, d)
		return len(delays) < 9
	}
	dials := 0
	c.dial = func(context.Context, string, http.Header) (agentWSConn, error) {
		dials++
		switch {
		case dials <= 7:
			return nil, errors.New("dial: connection refused")
		case dials == 8:
			conn := newFakeWSConn()
			conn.reads = nil // connects, then drops at once
			return conn, nil
		default:
			return nil, errors.New("dial: connection refused")
		}
	}

	c.run(ctx)

	want := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 30 * time.Second, 30 * time.Second, // failures: grown to the cap
		1 * time.Second, // dial 8 succeeded then dropped: reset to the initial delay
		2 * time.Second, // next failure grows again from the initial delay
	}
	assert.Equal(t, want, delays, "[AC-078] delay after a successful connect must be the initial delay")
}
