package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// flushServer records every telemetry body it receives (by the snapshot's
// DeviceID label) and answers according to respond.
type flushServer struct {
	srv *httptest.Server

	mu       sync.Mutex
	received []string
	respond  func(label string, nth int) (status int, retryAfter string)
	seen     map[string]int
}

func newFlushServer(t *testing.T, respond func(label string, nth int) (int, string)) *flushServer {
	t.Helper()
	fs := &flushServer{respond: respond, seen: map[string]int{}}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var snap models.TelemetrySnapshot
		_ = json.NewDecoder(r.Body).Decode(&snap)
		fs.mu.Lock()
		fs.received = append(fs.received, snap.DeviceID)
		fs.seen[snap.DeviceID]++
		nth := fs.seen[snap.DeviceID]
		respond := fs.respond
		fs.mu.Unlock()

		status, retry := respond(snap.DeviceID, nth)
		if retry != "" {
			w.Header().Set("Retry-After", retry)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"status":"scripted"}`))
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (f *flushServer) setRespond(r func(string, int) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respond = r
}

func (f *flushServer) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.received...)
}

func (f *flushServer) count(label string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[label]
}

func always(status int) func(string, int) (int, string) {
	return func(string, int) (int, string) { return status, "" }
}

// flushAgent wires an agent to a fake server and a real temp-file buffer
// pre-loaded with items labelled by labels (oldest first).
func flushAgent(t *testing.T, fs *flushServer, labels ...string) (*Agent, *manualNow) {
	t.Helper()
	captureLogs(t)
	buf := openTestBuffer(t, filepath.Join(t.TempDir(), "buffer.db"), 50<<20)
	for _, l := range labels {
		require.NoError(t, buf.Store(bufSnap(l, 50)))
	}
	clk := &manualNow{t: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	client := NewHTTPClient(fs.srv.URL, "key")
	client.now = clk.now

	cfg := &Config{}
	cfg.Agent.DeviceID = "dev"
	return &Agent{
		config:   cfg,
		client:   client,
		buffer:   buf,
		registry: collectors.NewRegistry(),
	}, clk
}

func queuedLabels(t *testing.T, a *Agent) []string {
	t.Helper()
	return drainLabels(t, a.buffer)
}

func TestGROWTH_AC061_AckedItemNeverResent(t *testing.T) {
	fs := newFlushServer(t, func(label string, _ int) (int, string) {
		if label == "item-2" {
			return http.StatusServiceUnavailable, ""
		}
		return http.StatusOK, ""
	})
	a, _ := flushAgent(t, fs, "item-1", "item-2", "item-3")

	a.flushBuffer(context.Background())

	assert.Equal(t, []string{"item-1", "item-2"}, fs.got(),
		"[AC-061] the flush must stop at the first item the server could not take")
	assert.Equal(t, []string{"item-2", "item-3"}, queuedLabels(t, a),
		"[AC-061] the acknowledged item must already be gone from the buffer")

	fs.setRespond(always(http.StatusOK))
	a.flushBuffer(context.Background())

	assert.Equal(t, 1, fs.count("item-1"), "[AC-061] item-1 must be sent exactly once in total")
	assert.Equal(t, []string{"item-1", "item-2", "item-2", "item-3"}, fs.got(),
		"[AC-061] the second flush must start at item-2")
	assert.Empty(t, queuedLabels(t, a))
}

func TestGROWTH_AC062_Rejected400ItemDroppedOthersDelivered(t *testing.T) {
	fs := newFlushServer(t, func(label string, _ int) (int, string) {
		if label == "B" {
			return http.StatusBadRequest, ""
		}
		return http.StatusOK, ""
	})
	a, _ := flushAgent(t, fs, "A", "B", "C")
	logs := captureLogs(t)

	a.flushBuffer(context.Background())

	assert.Equal(t, []string{"A", "B", "C"}, fs.got(), "[AC-062] A and C are delivered and B was offered once")
	assert.Empty(t, queuedLabels(t, a), "[AC-062] the rejected B must be removed from the buffer")
	assert.Equal(t, 1, strings.Count(logs.String(), "server rejected a buffered item"), "[AC-062] one log line records the rejection")
	assert.Contains(t, logs.String(), "status=400")

	// Not retried forever: a later flush has nothing to resend.
	a.flushBuffer(context.Background())
	assert.Equal(t, 1, fs.count("B"), "[AC-062] a rejected item must never be offered again")
}

func TestGROWTH_AC062_PermanentStatusesDropAndTransientOnesKeep(t *testing.T) {
	cases := []struct {
		status   int
		wantKept bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusNotFound, false},
		{http.StatusRequestEntityTooLarge, false},
		{http.StatusUnprocessableEntity, false},
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusServiceUnavailable, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			fs := newFlushServer(t, always(tc.status))
			a, _ := flushAgent(t, fs, "only")

			a.flushBuffer(context.Background())

			if tc.wantKept {
				assert.Equal(t, []string{"only"}, queuedLabels(t, a), "[AC-062] %d must keep the item", tc.status)
			} else {
				assert.Empty(t, queuedLabels(t, a), "[AC-062] %d is permanent and must drop the item", tc.status)
			}
		})
	}
}

func TestGROWTH_AC062_401And403KeepItem(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fs := newFlushServer(t, always(status))
			a, clk := flushAgent(t, fs, "one", "two")

			a.flushBuffer(context.Background())
			assert.Equal(t, []string{"one", "two"}, queuedLabels(t, a),
				"[AC-062/C-9] a %d (a server database blip answers 401) must not cost any buffered item", status)
			assert.Equal(t, []string{"one"}, fs.got(), "[AC-062] the flush stops at the first refusal")

			// Backed off: an immediate retry sends nothing.
			a.flushBuffer(context.Background())
			assert.Equal(t, []string{"one"}, fs.got(), "[AC-062] telemetry must back off after %d", status)

			// Once the server recovers and the back-off passes, everything is delivered.
			fs.setRespond(always(http.StatusOK))
			clk.advance(31 * time.Second)
			a.flushBuffer(context.Background())
			assert.Empty(t, queuedLabels(t, a))
			assert.Equal(t, 1, fs.count("two"))
		})
	}
}

func TestGROWTH_AC063_429HonoursRetryAfter_Flush(t *testing.T) {
	fs := newFlushServer(t, func(string, int) (int, string) { return http.StatusTooManyRequests, "30" })
	a, clk := flushAgent(t, fs, "queued")

	a.flushBuffer(context.Background())
	require.Equal(t, []string{"queued"}, queuedLabels(t, a), "[AC-063] the item stays in the buffer on a 429")
	require.Equal(t, 1, len(fs.got()))

	clk.advance(29 * time.Second)
	a.flushBuffer(context.Background())
	assert.Equal(t, 1, len(fs.got()), "[AC-063] no retry before the 30 s Retry-After (injected clock)")

	fs.setRespond(always(http.StatusOK))
	clk.advance(time.Second)
	a.flushBuffer(context.Background())
	assert.Equal(t, 2, len(fs.got()), "[AC-063] the retry happens once 30 s have passed")
	assert.Empty(t, queuedLabels(t, a))
}

func TestGROWTH_AC062_LivePushRejectedIsDroppedNotBuffered(t *testing.T) {
	fs := newFlushServer(t, always(http.StatusBadRequest))
	a, _ := flushAgent(t, fs)

	a.sendTelemetry(context.Background())

	assert.Equal(t, 1, len(fs.got()), "the live push is attempted once")
	assert.Empty(t, queuedLabels(t, a), "[AC-062] a live push that gets a permanent 4xx must not be buffered")
}

func TestGROWTH_AC062_LivePushKeptOnRetryableStatuses(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fs := newFlushServer(t, always(status))
			a, _ := flushAgent(t, fs)

			a.sendTelemetry(context.Background())

			assert.Len(t, queuedLabels(t, a), 1, "[AC-062] a live push answered %d must be buffered for later", status)
		})
	}
}

func TestGROWTH_AC062_LivePushDuringBackoffIsBufferedWithoutNetwork(t *testing.T) {
	fs := newFlushServer(t, func(string, int) (int, string) { return http.StatusTooManyRequests, "60" })
	a, clk := flushAgent(t, fs)

	a.sendTelemetry(context.Background())
	clk.advance(10 * time.Second)
	a.sendTelemetry(context.Background())

	assert.Equal(t, 1, len(fs.got()), "[AC-063] the second push must not reach the server inside the back-off")
	assert.Len(t, queuedLabels(t, a), 2, "[AC-063] both snapshots are kept for later")
}

func TestGROWTH_AC061_SuccessfulLivePushFlushesTheBacklog(t *testing.T) {
	fs := newFlushServer(t, always(http.StatusOK))
	a, _ := flushAgent(t, fs, "old-1", "old-2")

	a.sendTelemetry(context.Background())

	assert.Equal(t, []string{"dev", "old-1", "old-2"}, fs.got(), "the live push goes first, then the backlog oldest first")
	assert.Empty(t, queuedLabels(t, a))
}

func TestGROWTH_AC060_OversizedLiveSnapshotIsDroppedNotStored(t *testing.T) {
	logs := captureLogs(t)
	small := openTestBuffer(t, filepath.Join(t.TempDir(), "small.db"), 2000)
	a := &Agent{config: &Config{}, buffer: small}

	a.bufferSnapshot(bufSnap("huge", 5000))

	assert.Equal(t, 0, small.Len(), "[AC-060] an item that cannot fit must never be stored")
	assert.Contains(t, logs.String(), "could not buffer telemetry")
}

func TestGROWTH_D36_BodyGuardDropsContainerLogsThenJournal(t *testing.T) {
	line := strings.Repeat("l", 1000)
	containerLogs := func(n int) []models.ContainerLogEntry {
		out := make([]models.ContainerLogEntry, n)
		for i := range out {
			out[i] = models.ContainerLogEntry{ContainerID: "abc", Stream: "stdout", Line: line}
		}
		return out
	}
	journal := func(n int) []models.LogEntry {
		out := make([]models.LogEntry, n)
		for i := range out {
			out[i] = models.LogEntry{Message: line}
		}
		return out
	}
	const limit = 1 << 20

	t.Run("under the guard nothing is touched", func(t *testing.T) {
		captureLogs(t)
		snap := &models.TelemetrySnapshot{Data: models.FullTelemetryData{ContainerLogs: containerLogs(100), Logs: journal(100)}}
		guardTelemetryBody(snap, limit)
		assert.Len(t, snap.Data.ContainerLogs, 100)
		assert.Len(t, snap.Data.Logs, 100)
	})

	t.Run("container logs are dropped first and the journal is kept when that is enough", func(t *testing.T) {
		captureLogs(t)
		snap := &models.TelemetrySnapshot{Data: models.FullTelemetryData{ContainerLogs: containerLogs(1500), Logs: journal(100)}}
		guardTelemetryBody(snap, limit)
		assert.Empty(t, snap.Data.ContainerLogs, "[D-36] container logs go first")
		assert.Len(t, snap.Data.Logs, 100, "[D-36] the journal survives when dropping container logs suffices")
		assert.LessOrEqual(t, encodedSize(snap), limit)
	})

	t.Run("the journal is dropped next when container logs alone were not enough", func(t *testing.T) {
		captureLogs(t)
		snap := &models.TelemetrySnapshot{Data: models.FullTelemetryData{ContainerLogs: containerLogs(200), Logs: journal(1500)}}
		guardTelemetryBody(snap, limit)
		assert.Empty(t, snap.Data.ContainerLogs)
		assert.Empty(t, snap.Data.Logs, "[D-36] then the journal")
		assert.LessOrEqual(t, encodedSize(snap), limit)
	})
}
