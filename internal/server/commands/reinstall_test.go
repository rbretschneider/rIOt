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

// EXPECT-008 commands.Service tests (ADD section 8, "S8"): recognition of an
// authenticated reinstall notice, the registration hook, the resolver, and the
// guarantee that a reinstall row is never sent to an agent, never reported on by
// one, never cancelled and never swept.

const reHost = "PiVPN"

// reHarness is the commands harness with the reinstall and self-update
// repositories wired, a linked device repository (online PiVPN), a recording
// observer, and the event repository the rate-cap event lands in.
type reHarness struct {
	*harness
	events  *testutil.MockEventRepo
	devices *testutil.MockDeviceRepo
	obs     *recordingObserver
}

func newREHarness(t *testing.T, connected ...string) *reHarness {
	t.Helper()
	h := newHarness(t, connected...)
	evts := testutil.NewMockEventRepo()
	h.repo.LinkEvents(evts)
	devs := testutil.NewMockDeviceRepo()
	devs.Devices[devA] = &models.Device{ID: devA, Hostname: reHost, Status: models.DeviceStatusOnline}
	h.repo.LinkDevices(devs)
	obs := &recordingObserver{}
	logger, logs := captureLogger()
	h.logs = logs
	h.svc = New(Options{
		Commands: h.repo, Policies: h.pol, Telemetry: h.tel, Agents: h.agents, Broadcaster: h.bc, Observer: obs,
		Events: h.ev, SelfUpdates: h.repo, Reinstalls: h.repo, Now: h.clock.Now, Logger: logger,
	})
	return &reHarness{harness: h, events: evts, devices: devs, obs: obs}
}

func (h *reHarness) notice() ReinstallNoticeResult {
	h.t.Helper()
	return h.svc.NoteReinstallNotice(context.Background(), ReinstallNoticeInput{DeviceID: devA, Hostname: reHost, At: h.clock.Now()})
}

func (h *reHarness) records() []models.Command {
	var out []models.Command
	for _, c := range h.repo.All() {
		if c.Origin == models.OriginInstaller {
			out = append(out, c)
		}
	}
	return out
}

func (h *reHarness) onlyRecord() models.Command {
	h.t.Helper()
	rows := h.records()
	require.Len(h.t, rows, 1, "exactly one reinstall record")
	return rows[0]
}

func (h *reHarness) noLogsAboveDebug() {
	h.t.Helper()
	for _, l := range []string{"level=WARN", "level=ERROR", "level=INFO"} {
		assert.NotContains(h.t, h.logs.String(), l, "[NFR-005] no log line above debug")
	}
}

func (h *reHarness) setClock(t time.Time) { h.clock.Advance(t.Sub(h.clock.Now())) }

func (h *reHarness) setStatus(s models.DeviceStatus) { h.devices.Devices[devA].Status = s }

func TestEX8_AC016_FiftyNoticesOneRecord(t *testing.T) {
	h := newREHarness(t, devA)
	first := h.notice()
	require.Equal(t, models.ReinstallCreated, first.Disposition)
	rec := h.onlyRecord()
	bound := *rec.DeadlineAt
	assert.True(t, bound.Equal(h.clock.Now().Add(10*time.Minute)), "the bound is the first notice + 10 minutes")
	assert.True(t, rec.DispatchedAt.Equal(h.clock.Now()), "the start is the server receive time")
	assert.Equal(t, models.ReinstallStepWaitingForRestart, rec.Step)
	assert.Equal(t, "{}", fmtParams(rec.Params), "[FR-012] no params: nothing from the notice is stored")
	assert.Empty(t, rec.TargetKeys, "no target keys")

	for i := 1; i < 50; i++ {
		h.clock.Advance(time.Second)
		got := h.notice()
		assert.Equal(t, models.ReinstallOpenRecord, got.Disposition, "[AC-016] notice %d is absorbed", i+1)
		assert.Empty(t, got.Events, "no event for a repeat")
	}
	after := h.onlyRecord()
	assert.True(t, after.DeadlineAt.Equal(bound), "[AC-016] the window is not extended past 10 minutes after the first notice")
	assert.True(t, after.CreatedAt.Equal(rec.CreatedAt))
	assert.Equal(t, 1, countStatus(h.obs.statuses(), "running"), "one create broadcast; the 49 repeats broadcast nothing")
	assert.Empty(t, h.events.Events, "[AC-016] no event is stored for the repeats")
	h.noLogsAboveDebug()
}

func fmtParams(p map[string]interface{}) string {
	if len(p) == 0 {
		return "{}"
	}
	return "non-empty"
}

func TestEX8_AC016_RateCapSixPerDayOneWarning(t *testing.T) {
	h := newREHarness(t, devA)
	base := h.clock.Now()
	var capEvents int
	created := 0
	for i := 0; i < 10; i++ {
		h.setClock(base.Add(time.Duration(i) * 2 * time.Hour))
		got := h.notice()
		capEvents += len(got.Events)
		if i < models.ReinstallRateCap {
			require.Equal(t, models.ReinstallCreated, got.Disposition, "notice %d is under the cap", i+1)
			created++
			// The record closes before the next notice (the agent registered).
			h.clock.Advance(90 * time.Second)
			h.svc.NoteReinstallRegistration(context.Background(), devA, h.clock.Now())
			continue
		}
		assert.Equal(t, models.ReinstallCapped, got.Disposition, "[AC-016] notice %d is over the cap", i+1)
	}
	assert.Equal(t, 6, created)
	assert.Len(t, h.records(), 6, "[AC-016] 6 records exist; notices 7 to 10 created none")

	var paused []models.Event
	for _, e := range h.events.Events {
		if e.Type == models.EventAgentReinstallPaused {
			paused = append(paused, e)
		}
	}
	require.Len(t, paused, 1, "[AC-016] exactly one rate-cap event")
	assert.Equal(t, 1, capEvents, "the one cap event was handed back for broadcast, once")
	assert.Equal(t, models.SeverityWarning, paused[0].Severity)
	assert.Equal(t, models.ReinstallRateCapMessage(reHost), paused[0].Message, "[FR-010] the exact text")
	assert.Empty(t, paused[0].CommandID, "not linked to a record")
	assert.Empty(t, h.ev.Calls(), "no command_completed event for reinstall records")

	// The window is rolling.
	h.setClock(base.Add(24*time.Hour + time.Minute))
	assert.Equal(t, models.ReinstallCreated, h.notice().Disposition,
		"[FR-010] once the oldest record is more than 24h old, a notice creates a record again")
	h.noLogsAboveDebug()
}

func TestEX8_AC015_DeviceOfflineNoRecord(t *testing.T) {
	h := newREHarness(t, devA)
	h.setStatus(models.DeviceStatusOffline)
	got := h.notice()
	assert.Equal(t, models.ReinstallDeviceOffline, got.Disposition, "[AC-015] an offline device gets no record")
	assert.Empty(t, h.records())
	assert.Empty(t, h.events.Events, "no event")
	assert.Empty(t, h.obs.statuses(), "no broadcast")

	// An unknown device is the same.
	delete(h.devices.Devices, devA)
	assert.Equal(t, models.ReinstallDeviceOffline, h.notice().Disposition)

	// A warning device is live (SEC-004): it is moved offline like an online one.
	h.devices.Devices[devA] = &models.Device{ID: devA, Hostname: reHost, Status: models.DeviceStatusWarning}
	assert.Equal(t, models.ReinstallCreated, h.notice().Disposition, "a warning device is live")
}

func TestEX8_AC018_StoreFailureFailsOpen(t *testing.T) {
	h := newREHarness(t, devA)
	h.repo.FailOn("CreateReinstall", errors.New("database is down"))
	got := h.notice()
	assert.Equal(t, models.ReinstallUnavailable, got.Disposition, "[AC-018] a failing record store yields unavailable")
	assert.Empty(t, got.Events)
	assert.Empty(t, h.records(), "and no record")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "level=WARN"), "one throttled Warn")
	for i := 0; i < 30; i++ {
		h.notice()
	}
	assert.LessOrEqual(t, strings.Count(h.logs.String(), "level=WARN"), RejectionLogsPerMinute+1,
		"[NFR-005] repeated store errors go through the per-device log throttle")

	h.repo.FailOn("CreateReinstall", nil)
	h.repo.FailOn("OpenReinstall", errors.New("database is down"))
	h.svc.NoteReinstallRegistration(context.Background(), devA, h.clock.Now()) // must not panic

	// Without a wired repository the service behaves as before the story.
	plain := newHarness(t, devA)
	got = plain.svc.NoteReinstallNotice(context.Background(), ReinstallNoticeInput{DeviceID: devA, Hostname: reHost, At: plain.clock.Now()})
	assert.Equal(t, models.ReinstallUnavailable, got.Disposition, "nil Reinstalls: no record")
	assert.Empty(t, plain.repo.All())
	// An empty device id is unavailable too.
	assert.Equal(t, models.ReinstallUnavailable,
		h.svc.NoteReinstallNotice(context.Background(), ReinstallNoticeInput{At: h.clock.Now()}).Disposition)
}

func TestEX8_AC020_StaleOpenRecordRetiredByNextNotice(t *testing.T) {
	h := newREHarness(t, devA)
	h.notice()
	stale := h.onlyRecord()
	// Within an hour of the bound the open record still absorbs the notice.
	h.clock.Advance(10*time.Minute + time.Hour)
	assert.Equal(t, models.ReinstallOpenRecord, h.notice().Disposition, "exactly bound + 1h: the open record still absorbs the notice")

	h.clock.Advance(time.Second)
	assert.Equal(t, models.ReinstallCreated, h.notice().Disposition, "[NFR-004] a stale open record never blocks a new one")
	old := h.get(stale.ID)
	assert.Equal(t, "timed_out", old.Status)
	assert.Equal(t, models.ReinstallFailureUnresolved, old.FailureStep)
	assert.Equal(t, models.ReinstallStaleResultMessage, old.ResultMsg)
	assert.Len(t, h.records(), 2)

	// The retire and the create are both broadcast, in that order.
	assert.Equal(t, []string{"running", "timed_out", "running"}, h.obs.statuses(), "create, retire, create")
	assert.Empty(t, h.ev.Calls(), "no command_completed event for the retired record")
}

func TestEX8_AC022_NeverClaimedOrDelivered(t *testing.T) {
	h := newREHarness(t, devA)
	h.notice()
	rec := h.onlyRecord()
	ctx := context.Background()

	payloads, err := h.svc.ClaimForHeartbeat(ctx, devA)
	require.NoError(t, err)
	assert.Empty(t, payloads, "[AC-022] the heartbeat claim never returns the record")
	claimed, ok, err := h.repo.ClaimByID(ctx, rec.ID, h.clock.Now())
	require.NoError(t, err)
	assert.False(t, ok, "[AC-022] the WebSocket claim never matches the record")
	assert.Nil(t, claimed)
	queued, err := h.repo.ClaimQueued(ctx, devA, h.clock.Now(), 10)
	require.NoError(t, err)
	assert.Empty(t, queued)
	assert.Empty(t, h.agents.Sent(), "nothing was ever sent to the agent")

	_, err = h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: models.ActionAgentUpdate, Origin: models.OriginInstaller})
	assert.Error(t, err, "[AD-003] Service.Dispatch cannot create the origin")
	assert.Len(t, h.records(), 1, "and created nothing")

	assert.Empty(t, rec.TargetKeys, "no target keys")
	cmd := h.dispatchAgentUpdate()
	assert.Equal(t, "dispatched", cmd.Status, "an agent_update command is not blocked by an open record")

	h.clock.Advance(2 * time.Hour)
	_, err = h.svc.ClaimForHeartbeat(ctx, devA)
	require.NoError(t, err)
	assert.Equal(t, "running", h.get(rec.ID).Status)
}

func TestEX8_AC022_AgentReportNotFoundHTTPAndWS(t *testing.T) {
	h := newREHarness(t, devA)
	h.notice()
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
		assert.Equal(t, models.ReportNotFound, out.Disposition, "[AC-022] %s on a reinstall record reads as an unknown command", name)
		assert.Empty(t, out.Status, "%s: the status is not revealed", name)
	}
	assert.Equal(t, before, h.get(rec.ID), "[AC-022] the row is untouched by every report")
	assert.Len(t, h.bc.Updates(), broadcasts, "no broadcast")
	assert.Empty(t, h.ev.Calls(), "no completion event")
	assert.Empty(t, h.svc.gate.entries, "no progress-gate entry is allocated")
	assert.Zero(t, h.repo.CallCount("SaveCommandOutput"), "no output is stored")

	// The late-result edge of a timed_out row is closed too.
	_, applied, err := h.repo.Transition(context.Background(), models.CommandTransition{ID: rec.ID, DeviceID: devA, Actor: models.ActorReinstall,
		To: models.CommandTimedOut, Now: h.clock.Now()})
	require.NoError(t, err)
	require.True(t, applied)
	out, err := h.svc.HandleAgentReport(context.Background(), devA, reports["result success"])
	require.NoError(t, err)
	assert.Equal(t, models.ReportNotFound, out.Disposition, "[AC-022] a late result on a timed_out record is not found")
	assert.Equal(t, "timed_out", h.get(rec.ID).Status)

	out, err = h.svc.HandleAgentReport(context.Background(), "other-device", reports["ack"])
	require.NoError(t, err)
	assert.Equal(t, models.ReportNotFound, out.Disposition)
}

func TestEX8_AC022_CancelNotCancellable(t *testing.T) {
	h := newREHarness(t, devA)
	h.notice()
	rec := h.onlyRecord()
	for _, status := range []string{"running", "succeeded", "timed_out"} {
		h.repo.Commands[rec.ID].Status = status
		row, outcome, err := h.svc.Cancel(context.Background(), devA, rec.ID)
		require.NoError(t, err)
		assert.Equal(t, CancelNotCancellable, outcome, "[AC-022] a %s reinstall record cannot be cancelled", status)
		require.NotNil(t, row)
		assert.Equal(t, status, row.Status, "the 409 reports the current status")
		assert.Equal(t, status, h.get(rec.ID).Status, "the row is untouched")
	}
}

// TestEX8_MockGuardMatchesDesign checks the mock's five-actor origin guard
// against the design, without a database. The PostgreSQL parity of the same
// table is TestEX8_MockGuardParity (internal/server/db).
func TestEX8_MockGuardMatchesDesign(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 27, 13, 15, 0, 0, time.UTC)
	now := t0.Add(time.Minute)
	actors := []models.CommandActor{models.ActorServer, models.ActorAgent, models.ActorOperator, models.ActorSelfUpdate, models.ActorReinstall}
	origins := []string{models.OriginUser, models.OriginAutoUpdate, models.OriginAutoPatch, models.OriginAgentSelfUpdate, models.OriginInstaller}
	compared := 0
	for _, origin := range origins {
		for _, from := range models.AllCommandStates() {
			for _, to := range models.AllCommandStates() {
				if to == models.CommandDispatched {
					continue
				}
				for _, actor := range actors {
					repo := testutil.NewMockCommandRepo()
					deadline := t0.Add(10 * time.Minute)
					repo.Seed(&models.Command{ID: "row", DeviceID: devA, Action: "agent_update", Params: map[string]interface{}{},
						Status: string(from), Origin: origin, CreatedAt: t0, UpdatedAt: t0, ExpiresAt: t0, ExecDeadlineS: 600,
						DeadlineAt: &deadline, DispatchedAt: &t0, TargetKeys: []string{}})
					_, applied, err := repo.Transition(ctx, models.CommandTransition{ID: "row", DeviceID: devA, Actor: actor, To: to, Now: now})
					require.NoError(t, err)
					want := models.CanTransition(from, to, actor) &&
						(actor == models.ActorSelfUpdate) == (origin == models.OriginAgentSelfUpdate) &&
						(actor == models.ActorReinstall) == (origin == models.OriginInstaller)
					assert.Equal(t, want, applied, "[C-2] origin %s, actor %s, %s -> %s", origin, actor, from, to)
					compared++
				}
			}
		}
	}
	assert.Greater(t, compared, 1000, "the table is large enough to mean something (%d)", compared)

	// Without the device scope the reinstall actor never applies.
	h := newREHarness(t, devA)
	h.notice()
	rec := h.onlyRecord()
	_, applied, err := h.repo.Transition(ctx, models.CommandTransition{ID: rec.ID, Actor: models.ActorReinstall, To: models.CommandSucceeded, Now: now})
	require.NoError(t, err)
	assert.False(t, applied, "[C-2] ActorReinstall needs the device scope")
	_, applied, err = h.repo.Transition(ctx, models.CommandTransition{ID: rec.ID, DeviceID: "other", Actor: models.ActorReinstall, To: models.CommandSucceeded, Now: now})
	require.NoError(t, err)
	assert.False(t, applied, "[C-2] ActorReinstall never applies to another device's record")
}

func TestEX8_AC023_SelfUpdateStartNotAttributedToReinstall(t *testing.T) {
	h := newREHarness(t, devA)
	h.notice()
	rec := h.onlyRecord()
	h.clock.Advance(2 * time.Minute)

	link := h.svc.NoteAgentUpdateEvent(context.Background(), AgentUpdateEventInput{
		DeviceID: devA, Hostname: reHost, Type: models.EventAgentUpdateStarted, Message: suStartMsg, At: h.clock.Now()})
	require.NotEmpty(t, link.CommandID, "[AD-017] the start is recorded")
	assert.NotEqual(t, rec.ID, link.CommandID, "[AD-017] a running reinstall row never captures a self-update start")
	selfRows := 0
	for _, c := range h.repo.All() {
		if c.Origin == models.OriginAgentSelfUpdate {
			selfRows++
		}
	}
	assert.Equal(t, 1, selfRows, "[AC-023] the self-update record exists next to the reinstall record")
	assert.Equal(t, "running", h.get(rec.ID).Status, "and the reinstall record is untouched")

	// A reinstall notice during an open self-update record opens its own record.
	assert.Equal(t, models.ReinstallOpenRecord, h.notice().Disposition, "one open reinstall record per device")
}

func TestEX8_AC023_ReinstallDuringOpenSelfUpdateRecord(t *testing.T) {
	h := newREHarness(t, devA)
	link := h.svc.NoteAgentUpdateEvent(context.Background(), AgentUpdateEventInput{
		DeviceID: devA, Hostname: reHost, Type: models.EventAgentUpdateStarted, Message: suStartMsg, At: h.clock.Now()})
	require.NotEmpty(t, link.CommandID)
	h.clock.Advance(2 * time.Minute)
	assert.Equal(t, models.ReinstallCreated, h.notice().Disposition,
		"[AC-023] an open self-update record does not block a reinstall record (separate unique indexes)")
	assert.Equal(t, "running", h.get(link.CommandID).Status)
}

func TestEX8_SweeperSkipsReinstall(t *testing.T) {
	h := newREHarness(t, devA)
	h.notice()
	rec := h.onlyRecord()
	h.seed(uid(1), "docker_update", "running", nil)

	h.clock.Advance(3 * time.Hour)
	res := h.svc.Sweep(context.Background())
	assert.Equal(t, 1, res.TimedOut, "[AD-009] only the ordinary command is timed out")
	got := h.get(rec.ID)
	assert.Equal(t, "running", got.Status, "the reinstall row is not swept, however late")
	assert.Nil(t, got.FinishedAt)
	for _, c := range h.ev.Calls() {
		assert.NotContains(t, c.Action, "agent_update", "[NFR-005] no command_completed event for the record")
	}
	assert.NotContains(t, h.logs.String(), rec.ID, "no 'command swept' line names the record")

	msg := "no result from agent"
	_, applied, err := h.repo.Transition(context.Background(), models.CommandTransition{
		ID: rec.ID, Actor: models.ActorServer, To: models.CommandTimedOut, ResultMsg: &msg, Now: h.clock.Now()})
	require.NoError(t, err)
	assert.False(t, applied, "the server edge cannot touch a reinstall row")
}

func TestEX8_NoCompletionEventForReinstall(t *testing.T) {
	ctx := context.Background()
	for name, finish := range map[string]func(h *reHarness, id string){
		"registration": func(h *reHarness, id string) { h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now()) },
		"resolved unconfirmed online": func(h *reHarness, id string) {
			_, ok, err := h.svc.ResolveReinstall(ctx, id, devA, models.ReinstallUnconfirmedOnline, "not confirmed", h.clock.Now())
			require.NoError(t, err)
			require.True(t, ok)
		},
		"resolved unconfirmed offline": func(h *reHarness, id string) {
			_, ok, err := h.svc.ResolveReinstall(ctx, id, devA, models.ReinstallUnconfirmedOffline, "not confirmed", h.clock.Now())
			require.NoError(t, err)
			require.True(t, ok)
		},
	} {
		h := newREHarness(t, devA)
		h.notice()
		rec := h.onlyRecord()
		h.clock.Advance(time.Minute)
		finish(h, rec.ID)
		assert.True(t, models.CommandState(h.get(rec.ID).Status).IsTerminal(), "%s: the record ended", name)
		assert.Empty(t, h.ev.Calls(), "[NFR-005] %s: no command_completed event", name)
		assert.Empty(t, h.pol.Policies, "%s: no policy outcome", name)
	}
}

func TestEX8_TransitionsLeaveResultColumnsAndGateUntouched(t *testing.T) {
	ctx := context.Background()
	h := newREHarness(t, devA)
	h.notice()
	rec := h.onlyRecord()
	h.clock.Advance(time.Minute)
	h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now())
	got := h.get(rec.ID)
	require.Equal(t, "succeeded", got.Status)
	assert.Equal(t, models.ReinstallStepConfirmed, got.Step)
	assert.Equal(t, models.ReinstallConfirmedResultMessage, got.ResultMsg)
	assert.True(t, got.FinishedAt.Equal(h.clock.Now()), "finished_at is the registration time")

	assert.Nil(t, got.ExitCode, "[C-6] exit_code")
	assert.Nil(t, got.DurationMs, "[C-6] duration_ms")
	assert.Empty(t, got.StderrTail, "[C-6] stderr_tail")
	assert.Empty(t, got.Steps, "[C-6] steps")
	assert.Empty(t, got.TargetResults, "[C-6] target_results")
	assert.Nil(t, got.Progress, "no progress")
	assert.LessOrEqual(t, len(got.ResultMsg), models.ReinstallResultMsgBytes)
	assert.Empty(t, h.repo.Outputs, "[C-6] no command output is stored")
	assert.Zero(t, h.repo.CallCount("SaveCommandOutput"))
	assert.Empty(t, h.svc.gate.entries, "[C-6] the progress gate is never touched")
	assert.Equal(t, 0, h.svc.gate.writes, "no gate write either")
}

func TestEX8_RegistrationAfterBoundIgnored(t *testing.T) {
	ctx := context.Background()
	t.Run("at and after the bound", func(t *testing.T) {
		for _, late := range []time.Duration{10 * time.Minute, 10*time.Minute + time.Second, time.Hour} {
			h := newREHarness(t, devA)
			h.notice()
			rec := h.onlyRecord()
			h.clock.Advance(late)
			h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now())
			assert.Equal(t, "running", h.get(rec.ID).Status, "[FR-015] a registration %s after the start is ignored", late)
		}
	})
	t.Run("just before the bound", func(t *testing.T) {
		h := newREHarness(t, devA)
		h.notice()
		rec := h.onlyRecord()
		h.clock.Advance(10*time.Minute - time.Second)
		h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now())
		assert.Equal(t, "succeeded", h.get(rec.ID).Status, "[FR-015] a registration 1s before the bound closes the record")
	})
	t.Run("only a running record is moved, once", func(t *testing.T) {
		h := newREHarness(t, devA)
		h.notice()
		rec := h.onlyRecord()
		h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now())
		first := *h.get(rec.ID).FinishedAt
		h.clock.Advance(time.Minute)
		h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now())
		assert.True(t, h.get(rec.ID).FinishedAt.Equal(first), "a second registration changes nothing")
		assert.Equal(t, []string{"running", "succeeded"}, h.obs.statuses(), "[NFR-005] one broadcast per change")
	})
	t.Run("a registration before the start is ignored", func(t *testing.T) {
		h := newREHarness(t, devA)
		h.notice()
		rec := h.onlyRecord()
		h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now().Add(-time.Second))
		assert.Equal(t, "running", h.get(rec.ID).Status, "[FR-015 a] only a registration received after the start closes it")
	})
	t.Run("no record", func(t *testing.T) {
		h := newREHarness(t, devA)
		h.svc.NoteReinstallRegistration(ctx, devA, h.clock.Now())
		assert.Empty(t, h.records())
	})
}

func TestEX8_ResolveReinstall(t *testing.T) {
	ctx := context.Background()
	h := newREHarness(t, devA)
	h.notice()
	rec := h.onlyRecord()
	at := h.clock.Now().Add(10 * time.Minute)

	_, _, err := h.svc.ResolveReinstall(ctx, rec.ID, devA, models.ReinstallOutcome("bogus"), "x", at)
	assert.Error(t, err, "an unknown outcome is refused")

	row, applied, err := h.svc.ResolveReinstall(ctx, rec.ID, "other-device", models.ReinstallUnconfirmedOffline, "x", at)
	require.NoError(t, err)
	assert.False(t, applied, "another device's id never resolves the record")
	assert.Nil(t, row)

	row, applied, err = h.svc.ResolveReinstall(ctx, rec.ID, devA, models.ReinstallUnconfirmedOffline, models.ReinstallUnconfirmedOfflineMessage(reHost), at)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, "timed_out", row.Status)
	assert.Equal(t, models.ReinstallFailureUnconfirmedOffline, row.FailureStep)
	assert.Equal(t, "not confirmed: PiVPN was offline 10m after the start", row.ResultMsg)
	assert.Contains(t, h.obs.statuses(), "timed_out", "the observer (the tracker) sees the resolution")
	assert.Contains(t, broadcastStatuses(h.harness), "timed_out", "and so do the dashboards")

	row, applied, err = h.svc.ResolveReinstall(ctx, rec.ID, devA, models.ReinstallUnconfirmedOnline, "x", at)
	require.NoError(t, err)
	assert.False(t, applied, "a terminal record is not resolved twice")
	assert.Nil(t, row)

	// A long message is bounded.
	h2 := newREHarness(t, devA)
	h2.notice()
	rec2 := h2.onlyRecord()
	row, applied, err = h2.svc.ResolveReinstall(ctx, rec2.ID, devA, models.ReinstallUnconfirmedOnline, strings.Repeat("x", 5000), at)
	require.NoError(t, err)
	require.True(t, applied)
	assert.LessOrEqual(t, len(row.ResultMsg), models.ReinstallResultMsgBytes, "result_msg is at most 512 bytes")
	assert.Equal(t, models.ReinstallFailureUnconfirmedOnline, row.FailureStep)
}

func (h *reHarness) dispatchAgentUpdate() models.Command {
	h.t.Helper()
	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: models.ActionAgentUpdate, ActorName: "ryan", ActorKind: "operator"})
	require.NoError(h.t, err)
	require.NotNil(h.t, res.Command)
	return *res.Command
}
