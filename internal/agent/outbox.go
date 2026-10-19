package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// outboxReceiptWait is how long a result written over the WebSocket waits for
// the server's command_receipt before the entry falls back to HTTP.
const outboxReceiptWait = 10 * time.Second

// outboxHTTPTimeout bounds one HTTP delivery attempt. It is a fresh budget,
// never derived from the operation that produced the result (FR-046).
const outboxHTTPTimeout = 10 * time.Second

// outboxTransport is how the outbox reaches the server. The production
// implementation wraps the agent's WebSocket client and HTTP client.
type outboxTransport interface {
	// WSConnected reports whether the WebSocket is currently up.
	WSConnected() bool
	// SendWS writes one message over the WebSocket.
	SendWS(msg AgentWSMessage) error
	// PostReport sends a command report over HTTP.
	PostReport(ctx context.Context, commandID string, rep models.CommandReport) (*ReportReply, error)
}

// outboxEntry is one undelivered final result (§5.4).
type outboxEntry struct {
	CommandID     string               `json:"command_id"`
	Result        models.CommandResult `json:"result"`
	CreatedAt     time.Time            `json:"created_at"`
	Attempts      int                  `json:"attempts"`
	NextAttemptAt time.Time            `json:"next_attempt_at"`
	WSWritten     bool                 `json:"ws_written"`
}

type outboxFile struct {
	Version int            `json:"version"`
	Entries []*outboxEntry `json:"entries"`
}

// outbox keeps final command results until the server confirms them (FR-013 to
// FR-018, AD-010). A result is persisted before its first delivery attempt,
// delivered over the WebSocket when connected and over HTTP otherwise, and
// survives an agent restart.
type outbox struct {
	mu   sync.Mutex
	file *atomicJSONFile
	clk  clock
	tr   outboxTransport

	entries []*outboxEntry
	// legacyUntil is the end of legacy-server mode (FR-022): WS-only delivery.
	legacyUntil time.Time
	// kick wakes the delivery loop.
	kick chan struct{}
}

func newOutbox(path string, fs stateFS, clk clock, tr outboxTransport) *outbox {
	return &outbox{
		file: newAtomicJSONFile(path, fs),
		clk:  clk,
		tr:   tr,
		kick: make(chan struct{}, 1),
	}
}

// Load reads the persisted entries (FR-018) and drops those older than
// OutboxMaxAge. A corrupt file starts empty and is reported.
func (o *outbox) Load() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	var doc outboxFile
	found, err := o.file.Load(&doc)
	if err != nil {
		o.entries = nil
		if found {
			slog.Warn("outbox: persisted file is corrupt, starting empty", "error", err)
			return nil
		}
		return err
	}
	o.entries = o.entries[:0]
	for _, e := range doc.Entries {
		if e != nil && e.CommandID != "" {
			o.entries = append(o.entries, e)
		}
	}
	o.pruneLocked(o.clk.Now())
	return nil
}

// Add stores a final result and wakes the delivery loop. The entry is kept in
// memory even when it cannot be written to disk (degraded mode); the returned
// error then reports the persistence failure. An ID that is already queued is
// left untouched.
func (o *outbox) Add(res models.CommandResult) error {
	o.mu.Lock()
	for _, e := range o.entries {
		if e.CommandID == res.CommandID {
			o.mu.Unlock()
			return nil
		}
	}
	now := o.clk.Now()
	e := &outboxEntry{CommandID: res.CommandID, Result: res, CreatedAt: now, NextAttemptAt: now}
	fitEntryBytes(e)
	o.entries = append(o.entries, e)
	o.pruneLocked(now)
	err := o.persistLocked()
	o.mu.Unlock()

	if err != nil {
		slog.Error("outbox: cannot persist entries, holding them in memory", "command_id", res.CommandID, "error", err)
	}
	o.Kick()
	return err
}

// Has reports whether an entry for id is still queued.
func (o *outbox) Has(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, e := range o.entries {
		if e.CommandID == id {
			return true
		}
	}
	return false
}

// Len returns the number of queued entries.
func (o *outbox) Len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.entries)
}

// Snapshot returns copies of the queued entries, oldest first.
func (o *outbox) Snapshot() []outboxEntry {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]outboxEntry, len(o.entries))
	for i, e := range o.entries {
		out[i] = *e
	}
	return out
}

// Ack removes the entry the server has confirmed with a command_receipt. Any
// receipt confirms delivery: accepted, duplicate, already terminal, or unknown
// all mean the server will not need this result again.
func (o *outbox) Ack(id string) {
	o.mu.Lock()
	removed := o.removeLocked(id)
	var err error
	if removed {
		err = o.persistLocked()
	}
	o.mu.Unlock()
	if err != nil {
		slog.Error("outbox: cannot persist after receipt", "command_id", id, "error", err)
	}
}

// Kick wakes the delivery loop without making any entry due.
func (o *outbox) Kick() {
	select {
	case o.kick <- struct{}{}:
	default:
	}
}

// OnWSConnected makes every entry that has not been written over the
// WebSocket due immediately, so a reconnect delivers without waiting for an
// HTTP backoff, then wakes the loop.
func (o *outbox) OnWSConnected() {
	o.mu.Lock()
	now := o.clk.Now()
	for _, e := range o.entries {
		if !e.WSWritten {
			e.NextAttemptAt = now
		}
	}
	o.mu.Unlock()
	o.Kick()
}

// Legacy reports whether the agent is in legacy-server mode (FR-022).
func (o *outbox) Legacy() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.clk.Now().Before(o.legacyUntil)
}

// ObserveHTTPStatus lets any HTTP report path (ack, progress) feed legacy
// detection: a 404 or 405 without an error code means the server has no report
// endpoint.
func (o *outbox) ObserveHTTPStatus(rep *ReportReply) {
	if rep != nil && isLegacyReply(rep) {
		o.enterLegacy()
	}
}

func (o *outbox) enterLegacy() {
	o.mu.Lock()
	now := o.clk.Now()
	already := now.Before(o.legacyUntil)
	o.legacyUntil = now.Add(models.LegacyServerModeDuration)
	o.mu.Unlock()
	if !already {
		slog.Info("outbox: server has no report endpoint, delivering over WebSocket only",
			"retry_http_after", models.FormatDurationShort(models.LegacyServerModeDuration))
	}
}

// Run delivers entries until ctx ends. Delivery is attempted when an entry is
// due, when Add or a reconnect wakes the loop, and at the next scheduled
// attempt.
func (o *outbox) Run(ctx context.Context) {
	for {
		next, ok := o.processDue(ctx)

		var timerC <-chan time.Time
		var timer clockTimer
		if ok {
			wait := next.Sub(o.clk.Now())
			if wait < 0 {
				wait = 0
			}
			timer = o.clk.NewTimer(wait)
			timerC = timer.C()
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-o.kick:
		case <-timerC:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// processDue makes one delivery pass over every due entry. It returns the time
// of the earliest remaining attempt, and false when nothing is queued.
func (o *outbox) processDue(ctx context.Context) (next time.Time, ok bool) {
	o.mu.Lock()
	now := o.clk.Now()
	if o.pruneLocked(now) {
		if err := o.persistLocked(); err != nil {
			slog.Error("outbox: cannot persist after pruning", "error", err)
		}
	}
	var due []string
	for _, e := range o.entries {
		if !e.NextAttemptAt.After(now) {
			due = append(due, e.CommandID)
		}
	}
	o.mu.Unlock()

	for _, id := range due {
		if ctx.Err() != nil {
			break
		}
		o.attempt(ctx, id)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	for _, e := range o.entries {
		if !ok || e.NextAttemptAt.Before(next) {
			next, ok = e.NextAttemptAt, true
		}
	}
	return next, ok
}

// attempt delivers one entry according to the AD-010 table.
func (o *outbox) attempt(ctx context.Context, id string) {
	o.mu.Lock()
	var e outboxEntry
	found := false
	for _, cur := range o.entries {
		if cur.CommandID == id {
			e, found = *cur, true
			break
		}
	}
	legacy := o.clk.Now().Before(o.legacyUntil)
	o.mu.Unlock()
	if !found {
		return
	}

	// 1. WebSocket, when connected and not written yet.
	if !e.WSWritten && o.tr.WSConnected() {
		data, err := json.Marshal(e.Result)
		if err == nil {
			err = o.tr.SendWS(AgentWSMessage{Type: "command_result", Data: data})
		}
		if err == nil {
			if legacy {
				o.finish(id, "delivered over websocket (legacy server)")
				return
			}
			o.update(id, func(cur *outboxEntry) {
				cur.WSWritten = true
				cur.NextAttemptAt = o.clk.Now().Add(outboxReceiptWait)
			})
			return
		}
		slog.Debug("outbox: websocket write failed, falling back to HTTP", "command_id", id, "error", err)
	}

	// 2. Legacy servers have no HTTP endpoint: wait for a connection.
	if legacy {
		o.retryLater(id)
		return
	}

	// 3. HTTP.
	httpCtx, cancel := context.WithTimeout(ctx, outboxHTTPTimeout)
	defer cancel()
	res := e.Result
	reply, err := o.tr.PostReport(httpCtx, id, models.CommandReport{
		Kind: models.ReportKindResult, CommandID: id, Result: &res,
	})
	if err != nil {
		slog.Debug("outbox: HTTP delivery failed", "command_id", id, "error", err)
		o.retryLater(id)
		return
	}

	switch classifyReply(reply, id) {
	case verdictDone:
		o.finish(id, "delivered over HTTP")
	case verdictDrop:
		slog.Error("outbox: server rejected the result as malformed, dropping it",
			"command_id", id, "status", reply.StatusCode)
		o.finish(id, "rejected by server")
	case verdictLegacy:
		o.enterLegacy()
		if e.WSWritten {
			o.finish(id, "written over websocket (legacy server)")
			return
		}
		o.retryLater(id)
	default:
		slog.Debug("outbox: delivery not confirmed, will retry",
			"command_id", id, "status", reply.StatusCode, "code", reply.Response.Code)
		o.retryLater(id)
	}
}

// finish removes an entry that no longer needs delivery.
func (o *outbox) finish(id, why string) {
	o.mu.Lock()
	removed := o.removeLocked(id)
	var err error
	if removed {
		err = o.persistLocked()
	}
	o.mu.Unlock()
	if err != nil {
		slog.Error("outbox: cannot persist after delivery", "command_id", id, "error", err)
	}
	if removed {
		slog.Debug("outbox: entry removed", "command_id", id, "why", why)
	}
}

// retryLater counts a failed attempt and schedules the next one with the
// 5s-doubling backoff capped at 5m (FR-016).
func (o *outbox) retryLater(id string) {
	o.update(id, func(cur *outboxEntry) {
		cur.Attempts++
		cur.NextAttemptAt = o.clk.Now().Add(models.OutboxBackoff(cur.Attempts))
	})
}

func (o *outbox) update(id string, fn func(*outboxEntry)) {
	o.mu.Lock()
	var err error
	for _, cur := range o.entries {
		if cur.CommandID == id {
			fn(cur)
			err = o.persistLocked()
			break
		}
	}
	o.mu.Unlock()
	if err != nil {
		slog.Error("outbox: cannot persist entry update", "command_id", id, "error", err)
	}
}

func (o *outbox) removeLocked(id string) bool {
	for i, e := range o.entries {
		if e.CommandID == id {
			o.entries = append(o.entries[:i], o.entries[i+1:]...)
			return true
		}
	}
	return false
}

// pruneLocked drops entries older than OutboxMaxAge and enforces the entry
// count and file-size bounds, oldest first (FR-017, NFR-008). It reports
// whether anything was dropped.
func (o *outbox) pruneLocked(now time.Time) bool {
	dropped := false
	drop := func(e *outboxEntry, reason string) {
		slog.Warn("outbox entry dropped", "command_id", e.CommandID, "reason", reason)
		dropped = true
	}

	kept := o.entries[:0]
	for _, e := range o.entries {
		if now.Sub(e.CreatedAt) > models.OutboxMaxAge {
			drop(e, "older than 24h")
			continue
		}
		kept = append(kept, e)
	}
	o.entries = kept

	for len(o.entries) > models.OutboxMaxEntries {
		drop(o.entries[0], "entry limit")
		o.entries = o.entries[1:]
	}
	for len(o.entries) > 1 && o.encodedSizeLocked() > models.OutboxMaxFileBytes {
		drop(o.entries[0], "file size limit")
		o.entries = o.entries[1:]
	}
	return dropped
}

func (o *outbox) encodedSizeLocked() int {
	data, err := json.Marshal(outboxFile{Version: 1, Entries: o.entries})
	if err != nil {
		return 0
	}
	return len(data)
}

func (o *outbox) persistLocked() error {
	return o.file.Save(outboxFile{Version: 1, Entries: o.entries})
}

// fitEntryBytes shrinks an entry's text fields until its encoded size is within
// OutboxMaxEntryBytes. Output is cut head+tail first because it is the bulky
// field; message and stderr keep their documented tails.
func fitEntryBytes(e *outboxEntry) {
	e.Result.Message = models.TruncateTail(e.Result.Message, models.MaxResultMsgBytes)
	e.Result.Stderr = models.TruncateTail(e.Result.Stderr, models.MaxStderrTailBytes)
	for i := 0; i < 32; i++ {
		raw, err := json.Marshal(e)
		if err != nil || len(raw) <= models.OutboxMaxEntryBytes {
			return
		}
		keep := len(e.Result.Output) / 2
		e.Result.Output = models.TruncateHeadTail(e.Result.Output, keep)
		if keep == 0 {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP reply classification (AD-010 table)
// ---------------------------------------------------------------------------

type replyVerdict int

const (
	verdictRetry  replyVerdict = iota // keep the entry, back off
	verdictDone                       // the server holds the outcome
	verdictDrop                       // the server will never accept this result
	verdictLegacy                     // the server has no report endpoint
)

// isLegacyReply reports whether rep is a 404 or 405 that carries no error code:
// the shape of a server that predates the report endpoint.
func isLegacyReply(rep *ReportReply) bool {
	return (rep.StatusCode == http.StatusNotFound || rep.StatusCode == http.StatusMethodNotAllowed) &&
		rep.Response.Code == ""
}

// classifyReply decides what to do with an outbox entry after an HTTP report.
// A 409 or 404 removes the entry only when it carries a rIOt error code, so a
// proxy's generic error page cannot discard a result the server never saw. A
// 400 or 413 is dropped because resending the same body can never succeed; every
// other failure is retried (AD-010).
func classifyReply(rep *ReportReply, commandID string) replyVerdict {
	switch {
	case rep.StatusCode >= 200 && rep.StatusCode < 300:
		// Only a body that echoes the command ID proves the report reached the
		// rIOt server; a captive portal's 200 must not discard the result.
		if rep.Response.CommandID == commandID {
			return verdictDone
		}
		return verdictRetry
	case rep.StatusCode == http.StatusConflict:
		switch rep.Response.Code {
		case models.CodeAlreadyTerminal, models.CodeInvalidTransition:
			return verdictDone
		}
		return verdictRetry
	case rep.StatusCode == http.StatusNotFound && rep.Response.Code == models.CodeCommandNotFound:
		return verdictDone
	case isLegacyReply(rep):
		return verdictLegacy
	case rep.StatusCode == http.StatusBadRequest || rep.StatusCode == http.StatusRequestEntityTooLarge:
		return verdictDrop
	default:
		// 429, 5xx, 401, 403 and anything unexpected: retry with backoff.
		return verdictRetry
	}
}
