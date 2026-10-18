package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

const (
	idA = "11111111-1111-4111-8111-111111111111"
	idB = "22222222-2222-4222-8222-222222222222"
)

// uuidN builds a distinct canonical lowercase UUID.
func uuidN(n int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", n, n)
}

// AC-015 (FR-012): a failed delivery keeps the result in the outbox.
func TestCL_AC015_WriteErrorKeepsOutboxEntry(t *testing.T) {
	o, tr, _, _ := newTestOutbox(t)
	tr.wsUp = true
	tr.wsErr = errors.New("write: broken pipe")
	tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) {
		return nil, errors.New("dial tcp: connection refused")
	}
	require.NoError(t, o.Add(testResult(idA)))

	o.processDue(context.Background())

	require.Equal(t, 1, o.Len(), "[AC-015] a failed WS write plus a failed HTTP post must leave the entry queued")
	e := o.Snapshot()[0]
	assert.Equal(t, 1, e.Attempts, "[AC-015] the failed delivery must be counted")
	assert.False(t, e.WSWritten, "[AC-015] a failed write must not mark the entry as written")
}

// AC-016 (FR-013, FR-014): with the WS down, the result is persisted first and
// then POSTed to the HTTP endpoint.
func TestCL_AC016_DisconnectedResultPersistedThenPosted(t *testing.T) {
	o, tr, _, path := newTestOutbox(t)
	tr.wsUp = false
	var persistedBeforePost bool
	tr.postHandler = func(id string, rep models.CommandReport) (*ReportReply, error) {
		data, err := os.ReadFile(path)
		persistedBeforePost = err == nil && strings.Contains(string(data), id)
		return replyOK(id), nil
	}

	require.NoError(t, o.Add(testResult(idA)))
	o.processDue(context.Background())

	require.Equal(t, 1, tr.postCount(), "[AC-016] a disconnected agent must fall back to HTTP")
	assert.True(t, persistedBeforePost, "[AC-016] the result must be on disk before the first delivery attempt")
	assert.Equal(t, models.ReportKindResult, tr.posts[0].Report.Kind, "[AC-016] the HTTP body must be a result report")
	assert.Equal(t, idA, tr.posts[0].CommandID)
	assert.Equal(t, 0, o.Len(), "[AC-016] a confirmed delivery must remove the entry")
	assert.Empty(t, tr.wsSent, "[AC-016] nothing may be written to a down WebSocket")
}

// AC-016 (FR-015): a WS receipt removes the entry; without one the entry falls
// back to HTTP when its wait expires.
func TestCL_AC016_ReceiptRemovesEntryElseHTTPFallback(t *testing.T) {
	t.Run("receipt removes the entry", func(t *testing.T) {
		o, tr, _, _ := newTestOutbox(t)
		tr.wsUp = true
		require.NoError(t, o.Add(testResult(idA)))

		o.processDue(context.Background())
		require.Len(t, tr.wsMessages("command_result"), 1, "[AC-016] the result must go over the connected WebSocket")
		require.Equal(t, 1, o.Len(), "[AC-016] a WS write alone is not a confirmation")
		assert.True(t, o.Snapshot()[0].WSWritten)

		o.Ack(idA)
		assert.Equal(t, 0, o.Len(), "[AC-016] a command_receipt must remove the entry")
		assert.Equal(t, 0, tr.postCount(), "[AC-016] no HTTP post is needed once the receipt arrived")
	})

	t.Run("no receipt falls back to HTTP", func(t *testing.T) {
		o, tr, clk, _ := newTestOutbox(t)
		tr.wsUp = true
		require.NoError(t, o.Add(testResult(idA)))
		o.processDue(context.Background())

		clk.Advance(outboxReceiptWait - time.Second)
		o.processDue(context.Background())
		assert.Equal(t, 0, tr.postCount(), "[AC-016] the receipt wait has not elapsed yet")

		clk.Advance(time.Second)
		o.processDue(context.Background())
		assert.Equal(t, 1, tr.postCount(), "[AC-016] a missing receipt must trigger the HTTP fallback")
		assert.Equal(t, 0, o.Len())
	})
}

// AC-017 (FR-015, FR-016): HTTP 500 keeps the entry; retries after 5s then 10s;
// a 2xx removes it.
func TestCL_AC017_Http500BackoffThenRemovedOn2xx(t *testing.T) {
	o, tr, clk, _ := newTestOutbox(t)
	status := 500
	tr.postHandler = func(id string, _ models.CommandReport) (*ReportReply, error) {
		if status == 500 {
			return replyStatus(500, ""), nil
		}
		return replyOK(id), nil
	}
	ctx := context.Background()
	require.NoError(t, o.Add(testResult(idA)))

	o.processDue(ctx)
	require.Equal(t, 1, tr.postCount(), "[AC-017] first attempt happens immediately")
	require.Equal(t, 1, o.Len(), "[AC-017] a 500 must keep the entry")

	clk.Advance(4 * time.Second)
	o.processDue(ctx)
	require.Equal(t, 1, tr.postCount(), "[AC-017] no retry before 5s")

	clk.Advance(time.Second) // t+5s
	o.processDue(ctx)
	require.Equal(t, 2, tr.postCount(), "[AC-017] second attempt after 5s")

	clk.Advance(9 * time.Second) // t+14s
	o.processDue(ctx)
	require.Equal(t, 2, tr.postCount(), "[AC-017] the second backoff is 10s, not 5s")

	status = 200
	clk.Advance(time.Second) // t+15s
	o.processDue(ctx)
	assert.Equal(t, 3, tr.postCount(), "[AC-017] third attempt after 10s")
	assert.Equal(t, 0, o.Len(), "[AC-017] a 2xx must remove the entry")
}

// AC-017 (FR-015): the HTTP reply table.
func TestCL_AC017_ReplyClassificationTable(t *testing.T) {
	cases := []struct {
		name       string
		reply      *ReportReply
		err        error
		wantLen    int
		wantLegacy bool
	}{
		{"2xx echoing the id removes", replyOK(idA), nil, 0, false},
		{"2xx not echoing the id is kept (captive portal)", &ReportReply{StatusCode: 200}, nil, 1, false},
		{"409 already_terminal removes", replyStatus(409, models.CodeAlreadyTerminal), nil, 0, false},
		{"409 invalid_transition removes", replyStatus(409, models.CodeInvalidTransition), nil, 0, false},
		{"409 unknown code is kept", replyStatus(409, "other"), nil, 1, false},
		{"404 command_not_found removes", replyStatus(404, models.CodeCommandNotFound), nil, 0, false},
		{"404 without code enters legacy mode", replyStatus(404, ""), nil, 1, true},
		{"405 without code enters legacy mode", replyStatus(405, ""), nil, 1, true},
		{"400 invalid_report drops", replyStatus(400, models.CodeInvalidReport), nil, 0, false},
		{"413 drops", replyStatus(413, ""), nil, 0, false},
		{"401 retries", replyStatus(401, ""), nil, 1, false},
		{"403 retries", replyStatus(403, ""), nil, 1, false},
		{"503 retries", replyStatus(503, ""), nil, 1, false},
		{"network error retries", nil, errors.New("connection reset"), 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureLogs(t)
			o, tr, _, _ := newTestOutbox(t)
			tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) { return tc.reply, tc.err }
			require.NoError(t, o.Add(testResult(idA)))

			o.processDue(context.Background())

			assert.Equal(t, tc.wantLen, o.Len(), "[AC-017] entry count after reply")
			assert.Equal(t, tc.wantLegacy, o.Legacy(), "[AC-017] legacy mode after reply")
		})
	}
}

// AC-018 (FR-018): an undelivered entry survives an agent restart and is
// delivered afterwards.
func TestCL_AC018_EntrySurvivesRestart(t *testing.T) {
	o, tr, clk, path := newTestOutbox(t)
	tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) {
		return replyStatus(503, ""), nil
	}
	require.NoError(t, o.Add(testResult(idA)))
	o.processDue(context.Background())
	require.Equal(t, 1, o.Len())

	restarted := newOutbox(path, osStateFS{}, clk, tr)
	require.NoError(t, restarted.Load())
	require.Equal(t, 1, restarted.Len(), "[AC-018] the entry must be reloaded from disk")
	assert.Equal(t, 1, restarted.Snapshot()[0].Attempts, "[AC-018] the attempt counter must survive the restart")

	tr.postHandler = nil
	clk.Advance(5 * time.Second)
	restarted.processDue(context.Background())
	assert.Equal(t, 0, restarted.Len(), "[AC-018] the reloaded entry must be delivered after the restart")
}

// AC-019 (FR-017): the 201st entry drops the oldest and logs a warning.
func TestCL_AC019_OverflowDropsOldestWithWarning(t *testing.T) {
	logs := captureLogs(t)
	o, _, _ := newMemOutbox(t)
	for i := 1; i <= models.OutboxMaxEntries; i++ {
		require.NoError(t, o.Add(testResult(uuidN(i))))
	}
	require.Equal(t, models.OutboxMaxEntries, o.Len())

	require.NoError(t, o.Add(testResult(uuidN(201))))

	assert.Equal(t, models.OutboxMaxEntries, o.Len(), "[AC-019] the outbox must stay at 200 entries")
	assert.False(t, o.Has(uuidN(1)), "[AC-019] the oldest entry must be the one dropped")
	assert.True(t, o.Has(uuidN(201)), "[AC-019] the new entry must be kept")
	assert.Contains(t, logs.String(), "level=WARN", "[AC-019] the drop must be logged at warning level")
	assert.Contains(t, logs.String(), "outbox entry dropped", "[AC-019] the drop must be logged")
	assert.Contains(t, logs.String(), uuidN(1), "[AC-019] the log must name the dropped command")
}

// AC-020 (FR-017): entries older than 24h are pruned; exactly 24h is kept.
func TestCL_AC020_EntryOlderThan24hPruned(t *testing.T) {
	logs := captureLogs(t)
	o, tr, clk, _ := newTestOutbox(t)
	tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) {
		return replyStatus(503, ""), nil
	}
	require.NoError(t, o.Add(testResult(idA)))

	clk.Advance(models.OutboxMaxAge)
	o.processDue(context.Background())
	assert.True(t, o.Has(idA), "[AC-020] an entry exactly 24h old must still be kept")

	clk.Advance(time.Second)
	o.processDue(context.Background())
	assert.False(t, o.Has(idA), "[AC-020] an entry 24h + 1s old must be dropped")
	assert.Contains(t, logs.String(), "outbox entry dropped", "[AC-020] the prune must be logged")
}

// AC-025 (FR-022): HTTP 404 enters legacy-server mode; the entry is removed
// after a successful WS write.
func TestCL_AC025_404EntersLegacyModeRemovesAfterWSWrite(t *testing.T) {
	t.Run("pending entry waits for the WebSocket then is removed on write", func(t *testing.T) {
		captureLogs(t)
		o, tr, clk, _ := newTestOutbox(t)
		tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) {
			return replyStatus(404, ""), nil
		}
		ctx := context.Background()
		require.NoError(t, o.Add(testResult(idA)))

		o.processDue(ctx)
		require.True(t, o.Legacy(), "[AC-025] a 404 without a code must enter legacy-server mode")
		require.Equal(t, 1, o.Len(), "[AC-025] nothing was written over WS yet, so the entry stays")

		tr.wsUp = true
		o.OnWSConnected()
		o.processDue(ctx)
		assert.Len(t, tr.wsMessages("command_result"), 1, "[AC-025] the result must be written over the WebSocket")
		assert.Equal(t, 0, o.Len(), "[AC-025] in legacy mode a successful WS write removes the entry")

		postsBefore := tr.postCount()
		require.NoError(t, o.Add(testResult(idB)))
		clk.Advance(time.Minute)
		o.processDue(ctx)
		assert.Equal(t, postsBefore, tr.postCount(), "[AC-025] HTTP must not be retried inside the legacy window")
	})

	t.Run("entry already written over WS is removed when the 404 arrives", func(t *testing.T) {
		captureLogs(t)
		o, tr, clk, _ := newTestOutbox(t)
		tr.wsUp = true
		tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) {
			return replyStatus(404, ""), nil
		}
		ctx := context.Background()
		require.NoError(t, o.Add(testResult(idA)))
		o.processDue(ctx) // written over WS, waiting for a receipt that a legacy server never sends

		clk.Advance(outboxReceiptWait)
		o.processDue(ctx)

		assert.True(t, o.Legacy(), "[AC-025] the 404 must be recorded")
		assert.Equal(t, 0, o.Len(), "[AC-025] an entry already written over WS is removed once legacy mode is known")
	})

	t.Run("HTTP detection is retried after an hour", func(t *testing.T) {
		captureLogs(t)
		o, tr, clk, _ := newTestOutbox(t)
		tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) {
			return replyStatus(404, ""), nil
		}
		require.NoError(t, o.Add(testResult(idA)))
		o.processDue(context.Background())
		require.True(t, o.Legacy())

		clk.Advance(models.LegacyServerModeDuration)
		assert.False(t, o.Legacy(), "[AC-025] legacy mode must expire after 1h so a server upgrade is noticed")
	})
}

// SEC C-1: a rate-limited report (HTTP 429) is retried with backoff, never
// dropped.
func TestCL_SEC001_Outbox429RetriesWithBackoff(t *testing.T) {
	o, tr, clk, _ := newTestOutbox(t)
	tr.postHandler = func(string, models.CommandReport) (*ReportReply, error) {
		return replyStatus(429, models.CodeRateLimited), nil
	}
	ctx := context.Background()
	require.NoError(t, o.Add(testResult(idA)))

	start := clk.Now()
	for attempt := 1; attempt <= 4; attempt++ {
		o.processDue(ctx)
		require.Equal(t, 1, o.Len(), "[SEC-001] a 429 must keep the entry (attempt %d)", attempt)
		e := o.Snapshot()[0]
		assert.Equal(t, attempt, e.Attempts, "[SEC-001] attempts must count every 429")
		wantDelay := models.OutboxBackoff(attempt)
		assert.Equal(t, wantDelay, e.NextAttemptAt.Sub(clk.Now()),
			"[SEC-001] the next attempt must follow the 5s-doubling backoff (attempt %d)", attempt)
		clk.Advance(wantDelay)
	}
	assert.Equal(t, 4, tr.postCount())
	assert.Equal(t, start.Add(5*time.Second+10*time.Second+20*time.Second+40*time.Second), clk.Now())

	tr.postHandler = nil
	o.processDue(ctx)
	assert.Equal(t, 0, o.Len(), "[SEC-001] once the limiter lets the report through the entry is removed")
}

// FR-017 / NFR-008: an oversized result is shrunk to the per-entry bound.
func TestCL_AC019_OversizedOutputFitsPerEntryBound(t *testing.T) {
	o, _, _, _ := newTestOutbox(t)
	res := testResult(idA)
	res.Output = strings.Repeat("x", 3*models.OutboxMaxEntryBytes)

	require.NoError(t, o.Add(res))

	e := o.Snapshot()[0]
	assert.Less(t, len(e.Result.Output), models.OutboxMaxEntryBytes, "[AC-019] output must be cut to fit the entry bound")
	assert.Contains(t, e.Result.Output, models.TruncationMarker, "[AC-019] the cut must be head+tail with a marker")
}
