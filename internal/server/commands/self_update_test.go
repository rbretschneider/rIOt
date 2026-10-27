package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-002 commands.Service tests (ADD section 8, "S"): recognition and
// attribution of agent_update_* events, the registration hook, the resolver, and
// the guarantee that a self-update row is never sent to an agent, never reported
// on by one, never cancelled and never swept.

const (
	suStartMsg = "Agent update started: 2.78.0 → 2.80.0"
	suHost     = "hawaii"
)

// suHarness is the commands harness with the self-update repository wired, a
// recording observer, and the event repository the rate-cap event lands in.
type suHarness struct {
	*harness
	events *testutil.MockEventRepo
	obs    *recordingObserver
}

func newSUHarness(t *testing.T, connected ...string) *suHarness {
	t.Helper()
	h := newHarness(t, connected...)
	evts := testutil.NewMockEventRepo()
	h.repo.LinkEvents(evts)
	obs := &recordingObserver{}
	logger, logs := captureLogger()
	h.logs = logs
	h.svc = New(Options{
		Commands: h.repo, Policies: h.pol, Telemetry: h.tel, Agents: h.agents, Broadcaster: h.bc, Observer: obs,
		Events: h.ev, SelfUpdates: h.repo, Now: h.clock.Now, Logger: logger,
	})
	return &suHarness{harness: h, events: evts, obs: obs}
}

func (h *suHarness) send(typ models.EventType, msg string) AgentUpdateEventLink {
	h.t.Helper()
	return h.svc.NoteAgentUpdateEvent(context.Background(), AgentUpdateEventInput{
		DeviceID: devA, Hostname: suHost, Type: typ, Message: msg, At: h.clock.Now(),
	})
}

func (h *suHarness) start() AgentUpdateEventLink {
	return h.send(models.EventAgentUpdateStarted, suStartMsg)
}

func (h *suHarness) records() []models.Command {
	var out []models.Command
	for _, c := range h.repo.All() {
		if c.Origin == models.OriginAgentSelfUpdate {
			out = append(out, c)
		}
	}
	return out
}

// onlyRecord returns the one self-update row.
func (h *suHarness) onlyRecord() models.Command {
	h.t.Helper()
	rows := h.records()
	require.Len(h.t, rows, 1, "exactly one self-update record")
	return rows[0]
}

func (h *suHarness) noLogsAboveDebug() {
	h.t.Helper()
	for _, l := range []string{"level=WARN", "level=ERROR", "level=INFO"} {
		assert.NotContains(h.t, h.logs.String(), l, "[NFR-005] no log line above debug")
	}
}

func (h *suHarness) setClock(t time.Time) { h.clock.Advance(t.Sub(h.clock.Now())) }

func (h *suHarness) dispatchAgentUpdate() models.Command {
	h.t.Helper()
	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: models.ActionAgentUpdate, ActorName: "ryan", ActorKind: "operator"})
	require.NoError(h.t, err)
	require.NotNil(h.t, res.Command)
	return *res.Command
}

func TestEX2_AC006_StartDuringDispatchedCommandAttributed(t *testing.T) {
	h := newSUHarness(t, devA)
	cmd := h.dispatchAgentUpdate()
	require.Equal(t, "dispatched", cmd.Status, "the connected agent got the command")
	h.clock.Advance(2 * time.Second)

	link := h.start()
	assert.Equal(t, cmd.ID, link.CommandID, "[AC-006] the start is linked to the command")
	assert.Equal(t, models.CommandAttrInferred, link.CommandAttr)
	assert.Empty(t, h.records(), "[AC-006] no record is created for a command's start")
	assert.Empty(t, link.Events, "no server event")

	// While the command runs and after it acknowledged, it still captures the start.
	h.report(ack(cmd.ID))
	h.clock.Advance(time.Minute)
	assert.Equal(t, cmd.ID, h.start().CommandID, "an acknowledged command still captures the start")
	h.report(progress(cmd.ID, "running", "download", nil, ""))
	assert.Equal(t, cmd.ID, h.start().CommandID, "a running command still captures the start")
	assert.Empty(t, h.records())
}

func TestEX2_AC006_StartWithinTenMinutesAfterCommandAttributed(t *testing.T) {
	h := newSUHarness(t, devA)
	cmd := h.dispatchAgentUpdate()
	h.report(result(cmd.ID, models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded, Message: "ok"}))
	require.Equal(t, "succeeded", h.get(cmd.ID).Status)

	h.clock.Advance(9*time.Minute + 59*time.Second)
	assert.Equal(t, cmd.ID, h.start().CommandID, "[AC-006] 9m59s after the command finished: attributed")
	h.clock.Advance(time.Second)
	assert.Equal(t, cmd.ID, h.start().CommandID, "[AC-006] exactly 10 minutes after: still attributed (the boundary is inclusive)")
	assert.Empty(t, h.records(), "no record in either case")
}

func TestEX2_AC007_OldCommandDoesNotCapture(t *testing.T) {
	h := newSUHarness(t, devA)
	cmd := h.dispatchAgentUpdate()
	h.report(result(cmd.ID, models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded, Message: "ok"}))
	h.clock.Advance(10*time.Minute + time.Second)

	link := h.start()
	rec := h.onlyRecord()
	assert.Equal(t, rec.ID, link.CommandID, "[AC-007] a command that finished 10m01s ago does not capture the start: a record is created")
	assert.NotEqual(t, cmd.ID, link.CommandID)
	assert.Equal(t, models.CommandAttrInferred, link.CommandAttr)
}

func TestEX2_AC007_QueuedCommandDoesNotCapture(t *testing.T) {
	h := newSUHarness(t) // the agent is not connected: the command stays queued
	cmd := h.dispatchAgentUpdate()
	require.Equal(t, "queued", cmd.Status)

	link := h.start()
	rec := h.onlyRecord()
	assert.Equal(t, rec.ID, link.CommandID, "[AC-007] a queued command that was never dispatched does not capture the start")
	assert.NotEqual(t, cmd.ID, link.CommandID)
	assert.Equal(t, "queued", h.get(cmd.ID).Status, "the command is untouched, and keeps its own window (FR-028)")
}

func TestEX2_AC017_RepeatedStartsOneRecord(t *testing.T) {
	h := newSUHarness(t, devA)
	first := h.start()
	rec := h.onlyRecord()
	require.Equal(t, rec.ID, first.CommandID)
	bound := *rec.DeadlineAt
	assert.True(t, bound.Equal(h.clock.Now().Add(10*time.Minute)), "the bound is the first start + 10 minutes")

	for i := 1; i < 50; i++ {
		h.clock.Advance(time.Second)
		link := h.start()
		assert.Equal(t, rec.ID, link.CommandID, "[AC-017] start %d links to the open record", i+1)
		assert.Equal(t, models.CommandAttrInferred, link.CommandAttr)
	}
	after := h.onlyRecord()
	assert.True(t, after.DeadlineAt.Equal(bound), "[AC-017] the window is not extended by repeats")
	assert.True(t, after.CreatedAt.Equal(rec.CreatedAt))
	assert.Equal(t, 1, countStatus(h.obs.statuses(), "running"), "one create broadcast; the 49 repeats broadcast nothing")
	h.noLogsAboveDebug()
}

func countStatus(all []string, s string) int {
	n := 0
	for _, v := range all {
		if v == s {
			n++
		}
	}
	return n
}

func TestEX2_AC017_RateCapSixPerDayOneWarning(t *testing.T) {
	h := newSUHarness(t, devA)
	base := h.clock.Now()
	var capEvents int
	created := 0
	for i := 0; i < 10; i++ {
		h.setClock(base.Add(time.Duration(i) * 2 * time.Hour))
		link := h.start()
		capEvents += len(link.Events)
		if i < models.SelfUpdateRateCap {
			require.NotEmpty(t, link.CommandID, "start %d is under the cap and creates a record", i+1)
			created++
			// The record closes before the next start (the agent completed).
			h.clock.Advance(90 * time.Second)
			h.send(models.EventAgentUpdateCompleted, "signed installation completion verified against running executable")
			require.Equal(t, "succeeded", h.get(link.CommandID).Status)
			continue
		}
		assert.Empty(t, link.CommandID, "[AC-017] start %d is over the cap: no link", i+1)
	}
	assert.Equal(t, 6, created)
	assert.Len(t, h.records(), 6, "[AC-017] 6 records exist; the 7th to 10th starts created none")

	var paused []models.Event
	for _, e := range h.events.Events {
		if e.Type == models.EventAgentSelfUpdatePaused {
			paused = append(paused, e)
		}
	}
	require.Len(t, paused, 1, "[AC-017] exactly one rate-cap event")
	assert.Equal(t, 1, capEvents, "the one cap event was handed back for broadcast, once")
	assert.Equal(t, models.SeverityWarning, paused[0].Severity)
	assert.Equal(t, models.SelfUpdateRateCapMessage(suHost), paused[0].Message, "[FR-007] the exact text")
	assert.Empty(t, paused[0].CommandID, "not linked to a record")
	assert.Empty(t, h.ev.Calls(), "no command_completed event for self-update records")

	// The window is rolling: a day after the oldest record, a new one is created.
	h.setClock(base.Add(24*time.Hour + time.Minute))
	link := h.start()
	assert.NotEmpty(t, link.CommandID, "[FR-007] once the oldest record is more than 24h old, a start creates a record again")
	h.noLogsAboveDebug()
}

func TestEX2_AC021_CompletionWithoutRecordUnlinked(t *testing.T) {
	h := newSUHarness(t, devA)
	for _, typ := range []models.EventType{models.EventAgentUpdateCompleted, models.EventAgentUpdateFailed} {
		link := h.send(typ, "whatever")
		assert.Empty(t, link.CommandID, "[AC-021] %s without a record is stored unlinked", typ)
		assert.Empty(t, link.Events, "no server event")
	}
	assert.Empty(t, h.records(), "[AC-021] completed and failed never create a record")
	assert.Empty(t, h.events.Events, "and no event is written")

	// A record older than 10 minutes does not attribute either (FR-014).
	h.start()
	rec := h.onlyRecord()
	h.clock.Advance(10 * time.Minute)
	assert.Equal(t, rec.ID, h.send(models.EventAgentUpdateCompleted, "x").CommandID, "exactly 10 minutes after the start: linked")
	h2 := newSUHarness(t, devA)
	h2.start()
	h2.clock.Advance(10*time.Minute + time.Second)
	assert.Empty(t, h2.send(models.EventAgentUpdateCompleted, "x").CommandID, "[FR-014] 10m01s after the start: not linked")
	assert.Equal(t, "running", h2.onlyRecord().Status, "and the record is not touched")

	// A command's completion is never linked to a command (X-16).
	h3 := newSUHarness(t, devA)
	cmd := h3.dispatchAgentUpdate()
	h3.report(result(cmd.ID, models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded, Message: "ok"}))
	assert.Empty(t, h3.send(models.EventAgentUpdateCompleted, "Agent updated to 2.80.0").CommandID, "[X-16] never linked to a command")
}

func TestEX2_AC009_CompletedAndFailedMoveTheRecord(t *testing.T) {
	t.Run("completed before the return", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.clock.Advance(40 * time.Second)
		link := h.send(models.EventAgentUpdateCompleted, "Agent updated to 2.80.0")
		assert.Equal(t, rec.ID, link.CommandID)
		got := h.get(rec.ID)
		assert.Equal(t, "succeeded", got.Status)
		assert.Equal(t, models.SelfUpdateStepConfirmed, got.Step)
		assert.Equal(t, "completed: Agent updated to 2.80.0", got.ResultMsg)
		assert.Nil(t, got.VerifyingAt, "[X-1] a completion before any return sets no verifying_at")
		require.NotNil(t, got.FinishedAt)
		assert.True(t, got.FinishedAt.Equal(h.clock.Now()))
	})
	t.Run("failed with the device online", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.clock.Advance(40 * time.Second)
		link := h.send(models.EventAgentUpdateFailed, "Agent update failed: download: connection reset")
		assert.Equal(t, rec.ID, link.CommandID)
		got := h.get(rec.ID)
		assert.Equal(t, "failed", got.Status)
		assert.Equal(t, models.SelfUpdateFailureAgentFailed, got.FailureStep)
		assert.Equal(t, "Agent update 2.78.0 → 2.80.0 by agent auto-update failed on hawaii: Agent update failed: download: connection reset", got.ResultMsg,
			"[AC-009] the FR-018 text")
		assert.Empty(t, h.events.Events, "the service writes no event: the tracker writes the one outcome event")
	})
	t.Run("failed after the return (AC-010)", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.clock.Advance(90 * time.Second)
		h.svc.NoteSelfUpdateRegistration(context.Background(), devA, suHost, "2.80.0", h.clock.Now())
		require.Equal(t, "verifying", h.get(rec.ID).Status)
		h.clock.Advance(5 * time.Second)
		h.send(models.EventAgentUpdateFailed, "installed digest does not match the running executable; completion remains unconfirmed")
		got := h.get(rec.ID)
		assert.Equal(t, "failed", got.Status, "[AC-010] verifying -> failed")
		assert.NotNil(t, got.VerifyingAt, "the registration time is kept")
	})
	t.Run("the outcome is set once (FR-022)", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.send(models.EventAgentUpdateFailed, "boom")
		first := h.get(rec.ID)
		before := len(h.obs.statuses())
		h.clock.Advance(time.Second)
		link := h.send(models.EventAgentUpdateCompleted, "later")
		assert.Equal(t, rec.ID, link.CommandID, "a later event is still linked")
		again := h.get(rec.ID)
		assert.Equal(t, "failed", again.Status, "[FR-022] the outcome is unchanged")
		assert.Equal(t, first.ResultMsg, again.ResultMsg)
		assert.Len(t, h.obs.statuses(), before, "and nothing is broadcast")
	})
}

func TestEX2_AC019_FailedReasonSanitizedInResult(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	reason := "bad\x00 ‮​reason \xff\xfe" + strings.Repeat("y", 2048)
	h.send(models.EventAgentUpdateFailed, reason)
	got := h.get(rec.ID)
	assert.LessOrEqual(t, len(got.ResultMsg), models.SelfUpdateResultMsgBytes, "[AC-019] the result text is bounded")
	assert.NotContains(t, got.ResultMsg, "‮")
	assert.NotContains(t, got.ResultMsg, "\x00")
	assert.True(t, strings.HasPrefix(got.ResultMsg, "Agent update 2.78.0 → 2.80.0 by agent auto-update failed on hawaii: bad "), "got %q", got.ResultMsg)
}

func TestEX2_AC024_NeverClaimedOrDelivered(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	ctx := context.Background()

	payloads, err := h.svc.ClaimForHeartbeat(ctx, devA)
	require.NoError(t, err)
	assert.Empty(t, payloads, "[AC-024] the heartbeat claim never returns the record")
	claimed, ok, err := h.repo.ClaimByID(ctx, rec.ID, h.clock.Now())
	require.NoError(t, err)
	assert.False(t, ok, "[AC-024] the WebSocket claim never matches the record")
	assert.Nil(t, claimed)
	queued, err := h.repo.ClaimQueued(ctx, devA, h.clock.Now(), 10)
	require.NoError(t, err)
	assert.Empty(t, queued)
	assert.Empty(t, h.agents.Sent(), "nothing was ever sent to the agent")

	_, err = h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: models.ActionAgentUpdate, Origin: models.OriginAgentSelfUpdate})
	assert.Error(t, err, "[AD-001] Service.Dispatch cannot create the origin")
	assert.Len(t, h.records(), 1, "and created nothing")

	// The record holds no target key, so it never conflicts with a command.
	assert.Empty(t, rec.TargetKeys, "no target keys")
	cmd := h.dispatchAgentUpdate()
	assert.Equal(t, "dispatched", cmd.Status, "[FR-028] an agent_update command is not blocked by an open record")

	// Expiry and revert never touch it.
	h.clock.Advance(2 * time.Hour)
	_, err = h.svc.ClaimForHeartbeat(ctx, devA)
	require.NoError(t, err)
	assert.Equal(t, "running", h.get(rec.ID).Status)
}

func TestEX2_AC024_AgentReportNotFoundHTTPAndWS(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	before := h.get(rec.ID)
	broadcasts := len(h.bc.Updates())
	pct := 50
	reports := map[string]models.CommandReport{
		"ack":                ack(rec.ID),
		"progress running":   progress(rec.ID, "running", "download", &pct, "50%"),
		"progress verifying": progress(rec.ID, "verifying", "verify", nil, ""),
		"result success":     result(rec.ID, models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded, Message: "ok", Output: "out", Stderr: "err"}),
		"result error":       result(rec.ID, models.CommandResult{Status: "error", Outcome: models.OutcomeFailed, Message: "boom"}),
	}
	for name, rep := range reports {
		out, err := h.svc.HandleAgentReport(context.Background(), devA, rep)
		require.NoError(t, err, name)
		assert.Equal(t, models.ReportNotFound, out.Disposition, "[AC-024] %s on a self-update record reads as an unknown command", name)
		assert.Empty(t, out.Status, "%s: the status is not revealed", name)
	}
	assert.Equal(t, before, h.get(rec.ID), "[AC-024] the row is untouched by every report")
	assert.Len(t, h.bc.Updates(), broadcasts, "no broadcast")
	assert.Empty(t, h.ev.Calls(), "no completion event")
	assert.Empty(t, h.svc.gate.entries, "no progress-gate entry is allocated")
	assert.Zero(t, h.repo.CallCount("SaveCommandOutput"), "no output is stored")

	// The late-result edge of a timed_out row is closed too.
	_, applied, err := h.repo.Transition(context.Background(), models.CommandTransition{ID: rec.ID, DeviceID: devA, Actor: models.ActorSelfUpdate,
		To: models.CommandTimedOut, Now: h.clock.Now()})
	require.NoError(t, err)
	require.True(t, applied)
	out, err := h.svc.HandleAgentReport(context.Background(), devA, reports["result success"])
	require.NoError(t, err)
	assert.Equal(t, models.ReportNotFound, out.Disposition, "[AC-024] a late result on a timed_out record is not found")
	assert.Equal(t, "timed_out", h.get(rec.ID).Status)

	// Another device's report is not found either, as for any command.
	out, err = h.svc.HandleAgentReport(context.Background(), "other-device", reports["ack"])
	require.NoError(t, err)
	assert.Equal(t, models.ReportNotFound, out.Disposition)
}

func TestEX2_AC024_CancelNotCancellable(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	for _, status := range []string{"running", "verifying", "succeeded"} {
		h.repo.Commands[rec.ID].Status = status
		row, outcome, err := h.svc.Cancel(context.Background(), devA, rec.ID)
		require.NoError(t, err)
		assert.Equal(t, CancelNotCancellable, outcome, "[AC-024] a %s self-update record cannot be cancelled", status)
		require.NotNil(t, row)
		assert.Equal(t, status, row.Status, "the 409 reports the current status")
		assert.Equal(t, status, h.get(rec.ID).Status, "the row is untouched")
	}
}

func TestEX2_AC024_MockRejectsOtherActorsOnSelfUpdate(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	now := h.clock.Now()
	for _, tr := range []models.CommandTransition{
		{ID: rec.ID, DeviceID: devA, Actor: models.ActorAgent, To: models.CommandSucceeded},
		{ID: rec.ID, DeviceID: devA, Actor: models.ActorAgent, To: models.CommandRunning},
		{ID: rec.ID, DeviceID: devA, Actor: models.ActorOperator, To: models.CommandCancelled},
		{ID: rec.ID, Actor: models.ActorServer, To: models.CommandTimedOut},
		{ID: rec.ID, DeviceID: devA, Actor: models.ActorServer, To: models.CommandTimedOut},
		{ID: rec.ID, Actor: models.ActorSelfUpdate, To: models.CommandTimedOut},
	} {
		tr.Now = now
		_, applied, err := h.repo.Transition(context.Background(), tr)
		require.NoError(t, err)
		assert.False(t, applied, "[C-2] %s -> %s must not apply (device scope %q)", tr.Actor, tr.To, tr.DeviceID)
	}
	assert.Equal(t, "running", h.get(rec.ID).Status)
}

func TestEX2_Q1_RegistrationAfterBoundIgnored(t *testing.T) {
	ctx := context.Background()
	t.Run("at and after the bound", func(t *testing.T) {
		for _, late := range []time.Duration{10 * time.Minute, 10*time.Minute + time.Second, time.Hour} {
			h := newSUHarness(t, devA)
			h.start()
			rec := h.onlyRecord()
			h.clock.Advance(late)
			h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "2.80.0", h.clock.Now())
			assert.Equal(t, "running", h.get(rec.ID).Status, "[Q-1, X-15] a registration %s after the start is ignored", late)
		}
	})
	t.Run("just before the bound", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.clock.Advance(10*time.Minute - time.Second)
		h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "v2.80.0", h.clock.Now())
		got := h.get(rec.ID)
		assert.Equal(t, "verifying", got.Status, "[FR-011 c] a registration on <to> (a leading v is ignored) moves the record to verifying")
		assert.Equal(t, models.SelfUpdateStepWaitingForConfirmation, got.Step)
		require.NotNil(t, got.VerifyingAt)
		assert.True(t, got.VerifyingAt.Equal(h.clock.Now()), "verifying_at is the registration time")
	})
	t.Run("only a running record is moved", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "2.80.0", h.clock.Now())
		verifiedAt := *h.get(rec.ID).VerifyingAt
		h.clock.Advance(time.Minute)
		h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "2.78.0", h.clock.Now())
		got := h.get(rec.ID)
		assert.Equal(t, "verifying", got.Status, "a second registration changes nothing")
		assert.True(t, got.VerifyingAt.Equal(verifiedAt))
	})
	t.Run("wrong version", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.clock.Advance(110 * time.Second)
		h.svc.NoteSelfUpdateRegistration(ctx, devA, "PiVPN", "2.78.0", h.clock.Now())
		got := h.get(rec.ID)
		assert.Equal(t, "failed", got.Status, "[FR-019] a registration on another version fails the record")
		assert.Equal(t, models.SelfUpdateFailureWrongVersion, got.FailureStep)
		assert.Equal(t, "PiVPN came back on agent 2.78.0 after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)", got.ResultMsg)
	})
	t.Run("an unrecognised reported version is not echoed", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "<script>alert(1)</script>", h.clock.Now())
		got := h.get(rec.ID)
		assert.Equal(t, "failed", got.Status)
		assert.Contains(t, got.ResultMsg, "came back on agent an unrecognised version", "[X-4]")
		assert.NotContains(t, got.ResultMsg, "script")
	})
	t.Run("no record, or another device's record", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "2.80.0", h.clock.Now())
		h.start()
		rec := h.onlyRecord()
		h.svc.NoteSelfUpdateRegistration(ctx, "other-device", suHost, "2.80.0", h.clock.Now())
		assert.Equal(t, "running", h.get(rec.ID).Status, "[NFR-010] another device's registration never touches the record")
	})
}

func TestEX2_Q2_OneLinkPerEvent(t *testing.T) {
	h := newSUHarness(t, devA)
	cmd := h.dispatchAgentUpdate()

	// A start while a command is in flight is the command's, and creates no record.
	link := h.start()
	assert.Equal(t, cmd.ID, link.CommandID, "[Q-2] the start links to the command")
	assert.Empty(t, h.records())
	// A completion without a record is linked to nothing, never to the command (X-16).
	assert.Empty(t, h.send(models.EventAgentUpdateCompleted, "Agent updated to 2.80.0").CommandID, "[Q-2] a completion never links to a command")
	assert.Empty(t, h.send(models.EventAgentUpdateFailed, "boom").CommandID)

	// A record that started before a later command: completions link the record only.
	h2 := newSUHarness(t, devA)
	h2.start()
	rec := h2.onlyRecord()
	h2.clock.Advance(30 * time.Second)
	later := h2.dispatchAgentUpdate()
	require.NotEqual(t, rec.ID, later.ID)
	got := h2.send(models.EventAgentUpdateCompleted, "Agent updated to 2.80.0")
	assert.Equal(t, rec.ID, got.CommandID, "[Q-2] the completion links to the record, not to the command dispatched after it")
	assert.Equal(t, "dispatched", h2.get(later.ID).Status, "the command is untouched")
	// Every call returned exactly one link id (a single column).
	assert.NotContains(t, got.CommandID, ",")
}

func TestEX2_SweeperSkipsSelfUpdate(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	// A control: an ordinary overdue command is still swept.
	h.seed(uid(1), "docker_update", "running", nil)

	h.clock.Advance(3 * time.Hour)
	res := h.svc.Sweep(context.Background())
	assert.Equal(t, 1, res.TimedOut, "[AD-007 row 7] only the ordinary command is timed out")
	got := h.get(rec.ID)
	assert.Equal(t, "running", got.Status, "the self-update row is not swept, however late")
	assert.Nil(t, got.FinishedAt)
	for _, c := range h.ev.Calls() {
		assert.NotContains(t, c.Action, "agent_update", "[FR-022] no command_completed event for the record")
	}
	assert.NotContains(t, h.logs.String(), rec.ID, "no 'command swept' line names the record")
	assert.Equal(t, "timed_out", h.get(uid(1)).Status)

	// Even a direct attempt through the server edge does nothing (C-2).
	msg := "no result from agent"
	_, applied, err := h.repo.Transition(context.Background(), models.CommandTransition{
		ID: rec.ID, Actor: models.ActorServer, To: models.CommandTimedOut, ResultMsg: &msg, Now: h.clock.Now()})
	require.NoError(t, err)
	assert.False(t, applied)
}

func TestEX2_NoCompletionEventForSelfUpdate(t *testing.T) {
	ctx := context.Background()
	for name, finish := range map[string]func(h *suHarness, id string){
		"completed event": func(h *suHarness, id string) { h.send(models.EventAgentUpdateCompleted, "done") },
		"failed event":    func(h *suHarness, id string) { h.send(models.EventAgentUpdateFailed, "bad") },
		"registration then completed": func(h *suHarness, id string) {
			h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "2.80.0", h.clock.Now())
			h.send(models.EventAgentUpdateCompleted, "done")
		},
		"wrong version": func(h *suHarness, id string) {
			h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "2.70.0", h.clock.Now())
		},
		"resolved confirmed": func(h *suHarness, id string) {
			_, ok, err := h.svc.ResolveSelfUpdate(ctx, id, devA, models.SelfUpdateConfirmed, "confirmed: the agent reports 2.80.0", h.clock.Now())
			require.NoError(t, err)
			require.True(t, ok)
		},
		"resolved unconfirmed online": func(h *suHarness, id string) {
			_, ok, err := h.svc.ResolveSelfUpdate(ctx, id, devA, models.SelfUpdateUnconfirmedOnline, "not confirmed", h.clock.Now())
			require.NoError(t, err)
			require.True(t, ok)
		},
	} {
		h := newSUHarness(t, devA)
		h.start()
		rec := h.onlyRecord()
		h.clock.Advance(time.Minute)
		finish(h, rec.ID)
		assert.True(t, models.CommandState(h.get(rec.ID).Status).IsTerminal(), "%s: the record ended", name)
		assert.Empty(t, h.ev.Calls(), "[FR-022] %s: no command_completed event", name)
		assert.Empty(t, h.pol.Policies, "%s: no policy outcome", name)
	}
}

func TestEX2_SelfUpdateTransitionsLeaveResultColumnsAndGateUntouched(t *testing.T) {
	ctx := context.Background()
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	h.clock.Advance(time.Minute)
	h.svc.NoteSelfUpdateRegistration(ctx, devA, suHost, "2.80.0", h.clock.Now())
	h.send(models.EventAgentUpdateCompleted, strings.Repeat("big output ", 4000))
	got := h.get(rec.ID)
	require.Equal(t, "succeeded", got.Status)

	assert.Nil(t, got.ExitCode, "[C-6] exit_code")
	assert.Nil(t, got.DurationMs, "[C-6] duration_ms")
	assert.Empty(t, got.StderrTail, "[C-6] stderr_tail")
	assert.Empty(t, got.Steps, "[C-6] steps")
	assert.Empty(t, got.TargetResults, "[C-6] target_results")
	assert.Nil(t, got.Progress, "no progress")
	assert.LessOrEqual(t, len(got.ResultMsg), models.SelfUpdateResultMsgBytes, "the result text is at most 512 bytes")
	assert.Empty(t, h.repo.Outputs, "[C-6] no command output is stored")
	assert.Zero(t, h.repo.CallCount("SaveCommandOutput"))
	assert.Empty(t, h.svc.gate.entries, "[C-6] the progress gate is never touched")
	assert.Equal(t, 0, h.svc.gate.writes, "no gate write either")
}

func TestEX2_StaleOpenRecordRetiredByNextStart(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	stale := h.onlyRecord()
	// Within an hour of the bound the open record still absorbs the start.
	h.clock.Advance(10*time.Minute + time.Hour)
	assert.Equal(t, stale.ID, h.start().CommandID, "exactly bound + 1h: the open record still absorbs the start")

	h.clock.Advance(time.Second)
	link := h.start()
	require.NotEmpty(t, link.CommandID)
	assert.NotEqual(t, stale.ID, link.CommandID, "[C-4] a stale open record never blocks a new one")
	old := h.get(stale.ID)
	assert.Equal(t, "timed_out", old.Status)
	assert.Equal(t, models.SelfUpdateFailureUnresolved, old.FailureStep)
	assert.Equal(t, models.SelfUpdateStaleResultMessage, old.ResultMsg)
	assert.Equal(t, "running", h.get(link.CommandID).Status)

	// The retire and the create are both broadcast, in that order.
	statuses := h.obs.statuses()
	require.GreaterOrEqual(t, len(statuses), 3)
	assert.Equal(t, []string{"running", "timed_out", "running"}, statuses, "create, retire, create")
	assert.Empty(t, h.ev.Calls(), "no command_completed event for the retired record")
}

func TestEX2_ResolveSelfUpdate(t *testing.T) {
	ctx := context.Background()
	h := newSUHarness(t, devA)
	h.start()
	rec := h.onlyRecord()
	at := h.clock.Now().Add(10 * time.Minute)

	_, _, err := h.svc.ResolveSelfUpdate(ctx, rec.ID, devA, models.SelfUpdateOutcome("bogus"), "x", at)
	assert.Error(t, err, "an unknown outcome is refused")

	row, applied, err := h.svc.ResolveSelfUpdate(ctx, rec.ID, "other-device", models.SelfUpdateUnconfirmedOffline, "x", at)
	require.NoError(t, err)
	assert.False(t, applied, "[NFR-010] another device's id never resolves the record")
	assert.Nil(t, row)

	row, applied, err = h.svc.ResolveSelfUpdate(ctx, rec.ID, devA, models.SelfUpdateUnconfirmedOffline, models.SelfUpdateUnconfirmedOfflineMessage(suHost), at)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, "timed_out", row.Status)
	assert.Equal(t, models.SelfUpdateFailureUnconfirmedOffline, row.FailureStep)
	assert.Equal(t, "not confirmed: hawaii was offline 10m after the start", row.ResultMsg)
	assert.Contains(t, h.obs.statuses(), "timed_out", "the observer (the tracker) sees the resolution")
	assert.Contains(t, broadcastStatuses(h.harness), "timed_out", "and so do the dashboards")

	row, applied, err = h.svc.ResolveSelfUpdate(ctx, rec.ID, devA, models.SelfUpdateConfirmed, "x", at)
	require.NoError(t, err)
	assert.False(t, applied, "a terminal record is not resolved twice")
	assert.Nil(t, row)

	// confirmed
	h2 := newSUHarness(t, devA)
	h2.start()
	rec2 := h2.onlyRecord()
	row, applied, err = h2.svc.ResolveSelfUpdate(ctx, rec2.ID, devA, models.SelfUpdateConfirmed, "confirmed: the agent reports 2.80.0", at)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, "succeeded", row.Status)
	assert.Equal(t, models.SelfUpdateStepConfirmed, row.Step)
	assert.Empty(t, row.FailureStep)
}

func TestEX2_AC020_StoreFailureFailsOpen(t *testing.T) {
	h := newSUHarness(t, devA)
	h.repo.FailOn("CreateSelfUpdate", errors.New("database is down"))
	link := h.start()
	assert.Empty(t, link.CommandID, "[AC-020] a failing record store yields no link")
	assert.Empty(t, link.Events)
	assert.Empty(t, h.records(), "and no record")
	assert.Contains(t, h.logs.String(), "level=WARN", "one throttled Warn")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "level=WARN"))

	h.repo.FailOn("CreateSelfUpdate", nil)
	h.repo.FailOn("LatestSelfUpdateSince", errors.New("database is down"))
	assert.Empty(t, h.send(models.EventAgentUpdateCompleted, "x").CommandID, "[AC-020] completed with a failing store: unlinked")
	h.repo.FailOn("LatestSelfUpdateSince", nil)
	h.repo.FailOn("OpenSelfUpdate", errors.New("database is down"))
	h.svc.NoteSelfUpdateRegistration(context.Background(), devA, suHost, "2.80.0", h.clock.Now()) // must not panic

	// Without a wired repository the service behaves as before the story.
	plain := newHarness(t, devA)
	got := plain.svc.NoteAgentUpdateEvent(context.Background(), AgentUpdateEventInput{
		DeviceID: devA, Hostname: suHost, Type: models.EventAgentUpdateStarted, Message: suStartMsg, At: plain.clock.Now()})
	assert.Equal(t, AgentUpdateEventLink{}, got, "nil SelfUpdates: no record, no link")
}

func TestEX2_MalformedStartCreatesNothing(t *testing.T) {
	h := newSUHarness(t, devA)
	for _, msg := range []string{
		"Agent update started: 2.78.0 -> 2.80.0",
		"Agent update started: dev → 2.80.0",
		"Agent update started: 2.80.0 → 2.80.0",
		"Agent update started: 2.78.0 → 2.80.0\u0000x",
		"agent update started: 2.78.0 → 2.80.0",
		strings.Repeat("A", 10<<10),
	} {
		link := h.send(models.EventAgentUpdateStarted, msg)
		assert.Equal(t, AgentUpdateEventLink{}, link, "[AC-016] %.40q is not recognised", msg)
	}
	assert.Empty(t, h.records(), "[AC-016] no record")
	assert.Zero(t, h.repo.CallCount("CreateSelfUpdate"), "the repository is never called for a malformed message")
	// Other agent update types never create a record.
	assert.Equal(t, AgentUpdateEventLink{}, h.send(models.EventAgentUpdateAvail, suStartMsg), "[AC-027] available never creates a record")
	assert.Empty(t, h.records())
}
