package commands

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const devA = "dev-a"

// uid returns a canonical lowercase UUID for test command n.
func uid(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

// harness wires a Service to the faithful mocks, a fake clock and a captured log.
type harness struct {
	t      *testing.T
	svc    *Service
	repo   *testutil.MockCommandRepo
	agents *testutil.FakeAgents
	bc     *testutil.FakeBroadcaster
	ev     *testutil.FakeCompletionEvents
	pol    *testutil.MockAutoUpdateRepo
	tel    *testutil.MockTelemetryRepo
	clock  *fakeClock
	logs   *bytes.Buffer
}

func newHarness(t *testing.T, connected ...string) *harness {
	t.Helper()
	h := &harness{
		t: t, repo: testutil.NewMockCommandRepo(), agents: testutil.NewFakeAgents(connected...),
		bc: &testutil.FakeBroadcaster{}, ev: &testutil.FakeCompletionEvents{},
		pol: testutil.NewMockAutoUpdateRepo(), tel: testutil.NewMockTelemetryRepo(), clock: newFakeClock(),
	}
	logger, logs := captureLogger()
	h.logs = logs
	h.svc = New(Options{
		Commands: h.repo, Policies: h.pol, Telemetry: h.tel, Agents: h.agents,
		Broadcaster: h.bc, Events: h.ev, Now: h.clock.Now, Logger: logger,
	})
	return h
}

// seed stores a command directly in the mock, bypassing Dispatch.
func (h *harness) seed(id, action, status string, mod func(*models.Command)) *models.Command {
	h.t.Helper()
	now := h.clock.Now()
	c := &models.Command{
		ID: id, DeviceID: devA, Action: action, Params: map[string]interface{}{}, Status: status,
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
	h.repo.Commands[id] = c
	return c
}

func (h *harness) get(id string) models.Command {
	h.t.Helper()
	c, ok := h.repo.Get(id)
	require.True(h.t, ok, "command %s must exist", id)
	return c
}

func (h *harness) report(r models.CommandReport) ReportOutcome {
	h.t.Helper()
	out, err := h.svc.HandleAgentReport(context.Background(), devA, r)
	require.NoError(h.t, err)
	return out
}

func ack(id string) models.CommandReport {
	return models.CommandReport{Kind: models.ReportKindAck, CommandID: id}
}

func progress(id, state, step string, pct *int, msg string) models.CommandReport {
	return models.CommandReport{Kind: models.ReportKindProgress, CommandID: id, State: state, Step: step, Progress: pct, Message: msg}
}

func result(id string, r models.CommandResult) models.CommandReport {
	r.CommandID = id
	return models.CommandReport{Kind: models.ReportKindResult, CommandID: id, Result: &r}
}

func intp(v int) *int { return &v }

// ---------------------------------------------------------------------------
// AC-005, AC-006, AC-009: policy outcomes
// ---------------------------------------------------------------------------

func (h *harness) seedPolicy(failures int) int {
	h.t.Helper()
	require.NoError(h.t, h.pol.Upsert(context.Background(), &models.AutoUpdatePolicy{DeviceID: devA, Target: "web", Enabled: true}))
	p := h.pol.Policies[devA]
	last := h.clock.Now().Add(-time.Hour)
	p[0].ConsecutiveFailures = failures
	if failures > 0 {
		p[0].LastFailureAt = &last
	}
	return p[0].ID
}

func TestCL_AC005_FailedPolicyCommandKeepsLastTriggered(t *testing.T) {
	for _, outcome := range []struct {
		name  string
		drive func(h *harness, id string)
	}{
		{"failed result", func(h *harness, id string) {
			h.report(result(id, models.CommandResult{Status: "error", Message: "boom"}))
		}},
		{"timed out", func(h *harness, id string) {
			h.clock.Advance(time.Hour)
			h.svc.Sweep(context.Background())
		}},
		{"expired", func(h *harness, id string) {
			h.repo.Commands[id].Status = "queued"
			h.repo.Commands[id].DeadlineAt = nil
			h.clock.Advance(2 * time.Hour)
			h.svc.Sweep(context.Background())
		}},
		{"cancelled", func(h *harness, id string) {
			h.repo.Commands[id].Status = "queued"
			_, out, err := h.svc.Cancel(context.Background(), devA, id)
			require.NoError(t, err)
			require.Equal(t, CancelApplied, out)
		}},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			h := newHarness(t)
			polID := h.seedPolicy(0)
			h.seed(uid(1), "docker_update", "dispatched", func(c *models.Command) { c.PolicyID = &polID })

			outcome.drive(h, uid(1))

			p := h.pol.Policies[devA][0]
			assert.Nil(t, p.LastTriggeredAt, "[AC-005] a failed policy command must not start the cooldown")
			assert.Equal(t, 1, p.ConsecutiveFailures, "[AC-005] the failure must be counted for the backoff")
			require.NotNil(t, p.LastFailureAt)
		})
	}
}

func TestCL_AC006_SucceededSetsLastTriggeredToTransitionTime(t *testing.T) {
	h := newHarness(t)
	polID := h.seedPolicy(2)
	h.seed(uid(1), "docker_update", "dispatched", func(c *models.Command) { c.PolicyID = &polID })

	h.clock.Advance(7 * time.Minute)
	transitionTime := h.clock.Now()
	out := h.report(result(uid(1), models.CommandResult{Status: "success", Message: "ok"}))

	require.Equal(t, models.ReportAccepted, out.Disposition)
	p := h.pol.Policies[devA][0]
	require.NotNil(t, p.LastTriggeredAt, "[AC-006] success must start the cooldown")
	assert.True(t, p.LastTriggeredAt.Equal(transitionTime), "[AC-006] last_triggered_at must equal the transition time, got %s want %s", p.LastTriggeredAt, transitionTime)
}

func TestCL_AC009_SuccessResetsFailures(t *testing.T) {
	h := newHarness(t)
	polID := h.seedPolicy(3)
	h.seed(uid(1), "docker_update", "running", func(c *models.Command) { c.PolicyID = &polID })

	h.report(result(uid(1), models.CommandResult{Status: "success"}))

	p := h.pol.Policies[devA][0]
	assert.Equal(t, 0, p.ConsecutiveFailures, "[AC-009] n must reset to 0 on success")
	assert.Nil(t, p.LastFailureAt)
}

func TestPolicyOutcomeNotRecordedWithoutPolicy(t *testing.T) {
	h := newHarness(t)
	polID := h.seedPolicy(0)
	h.seed(uid(1), "docker_update", "dispatched", nil) // origin user, no policy

	h.report(result(uid(1), models.CommandResult{Status: "success"}))

	p := h.pol.Policies[devA][0]
	assert.Nil(t, p.LastTriggeredAt, "a command without policy_id must not touch %d", polID)
}

// ---------------------------------------------------------------------------
// AC-029: expires_at per class, creation semantics
// ---------------------------------------------------------------------------

func TestCL_AC029_ExpiresAtPerActionClass(t *testing.T) {
	cases := []struct {
		action string
		params map[string]interface{}
		ttl    time.Duration
		dl     int
	}{
		{"reboot", nil, 15 * time.Minute, 600},
		{"shutdown", nil, 15 * time.Minute, 600},
		{"os_update", nil, 30 * time.Minute, 2400},
		{"enable_auto_updates", nil, 30 * time.Minute, 2400},
		{"docker_update", map[string]interface{}{"container": "web"}, 60 * time.Minute, 2100},
		{"docker_bulk_update", map[string]interface{}{"container_ids": []interface{}{"a", "b"}}, 60 * time.Minute, 2*2100 + 300},
		{"agent_update", nil, 60 * time.Minute, 900},
		{"fetch_logs", nil, 30 * time.Minute, 900},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			h := newHarness(t)
			res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: tc.action, Params: tc.params})
			require.NoError(t, err)
			require.NotNil(t, res.Command)

			assert.True(t, res.Command.ExpiresAt.Equal(h.clock.Now().Add(tc.ttl)),
				"[AC-029] %s expires_at must be T + %s, got %s", tc.action, tc.ttl, res.Command.ExpiresAt.Sub(h.clock.Now()))
			assert.Equal(t, tc.dl, res.Command.ExecDeadlineS, "exec_deadline_s per class")
			assert.Equal(t, "queued", res.Command.Status, "no agent connected: the command stays queued")
			assert.Equal(t, models.OriginUser, res.Command.Origin)
		})
	}
}

func TestCL_AC029_WindowEndCapsExpiry(t *testing.T) {
	h := newHarness(t)
	windowEnd := h.clock.Now().Add(10 * time.Minute)

	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{
		DeviceID: devA, Action: "os_update", Origin: models.OriginAutoPatch, WindowEnd: windowEnd})
	require.NoError(t, err)

	assert.True(t, res.Command.ExpiresAt.Equal(windowEnd), "[AC-030] expires_at must be the earlier of the TTL and the window end")
	assert.Equal(t, models.OriginAutoPatch, res.Command.Origin)
}

func TestDispatch_RejectsUnknownOrigin(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot", Origin: "robot"})
	require.Error(t, err)
	assert.Zero(t, h.repo.CallCount("CreateIfNoConflict"), "an invalid origin must not reach the repository")
}

func TestDispatch_ConflictCreatesNoRow(t *testing.T) {
	h := newHarness(t)
	first, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})
	require.NoError(t, err)
	require.NotNil(t, first.Command)

	second, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})

	require.NoError(t, err)
	assert.Nil(t, second.Command)
	require.NotNil(t, second.Conflict, "[BR-008] a second host action while the first is non-terminal conflicts")
	assert.Equal(t, first.Command.ID, second.Conflict.ID)
	assert.Len(t, h.repo.Commands, 1, "[AC-068] no row may be created on conflict")
	assert.Len(t, h.bc.Updates(), 1, "a conflict broadcasts nothing")
	assert.Contains(t, h.logs.String(), "command conflict")
}

func TestDispatch_CommandWithoutTargetsNeverConflicts(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "fetch_logs"})
		require.NoError(t, err)
		require.NotNil(t, res.Command, "fetch_logs holds no target key, so it never returns 409")
	}
}

func TestDispatch_ConnectedAgentGetsClaimedAndSent(t *testing.T) {
	h := newHarness(t, devA)

	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "fetch_logs"})

	require.NoError(t, err)
	assert.Equal(t, "dispatched", res.Command.Status, "delivery goes through the claim")
	require.NotNil(t, res.Command.DeadlineAt)
	sent := h.agents.Sent()
	require.Len(t, sent, 1)
	assert.Equal(t, res.Command.ID, sent[0].Payload.CommandID)
	statuses := []string{}
	for _, u := range h.bc.Updates() {
		statuses = append(statuses, u.Status)
	}
	assert.Equal(t, []string{"queued", "dispatched"}, statuses, "created then claimed, each broadcast once")
}

func TestDispatch_CancelledBeforeClaimIsNeverSent(t *testing.T) {
	h := newHarness(t, devA)
	// The operator cancels the command between its creation and the WS claim.
	h.agents.OnConnectCheck = func() {
		for id := range h.repo.Commands {
			_, out, err := h.svc.Cancel(context.Background(), devA, id)
			assert.NoError(t, err)
			assert.Equal(t, CancelApplied, out)
		}
	}

	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "fetch_logs"})

	require.NoError(t, err)
	assert.Equal(t, "cancelled", res.Command.Status, "the result must reflect the cancel")
	assert.Empty(t, h.agents.Sent(), "[NFR-005] a cancelled command must never be sent")
}

func TestCL_Contract_ConflictReportsOverlappingKey(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "running", func(c *models.Command) {
		c.TargetKeys = []string{"docker:compose:/srv/app"}
	})

	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{
		DeviceID: devA, Action: "docker_update", Params: map[string]interface{}{"compose_work_dir": "/srv/app/"}})

	require.NoError(t, err)
	require.NotNil(t, res.Conflict)
	assert.Equal(t, uid(1), res.Conflict.ID, "existing_command_id must be set")
	assert.Equal(t, "docker:compose:/srv/app", res.ConflictKey, "target_key reports the overlapping key (trailing / normalized)")
}

func TestCL_Contract_UpdatedAtNeverGoesBackwards(t *testing.T) {
	h := newHarness(t, devA)
	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "docker_update",
		Params: map[string]interface{}{"container": "web"}})
	require.NoError(t, err)
	id := res.Command.ID

	h.clock.Advance(time.Second)
	h.report(ack(id))
	h.clock.Advance(time.Second)
	h.report(progress(id, "running", "pull", intp(5), "msg"))
	h.clock.Advance(2 * time.Second)
	h.report(progress(id, "verifying", "verify", nil, ""))
	h.clock.Advance(time.Second)
	h.report(result(id, models.CommandResult{Status: "success", Message: "final"}))

	var last time.Time
	for i, u := range h.bc.Updates() {
		assert.Equal(t, id, u.CommandID)
		assert.False(t, u.UpdatedAt.Before(last), "update %d (%s) moved updated_at backwards: %s < %s", i, u.Status, u.UpdatedAt, last)
		last = u.UpdatedAt
	}
	final := h.bc.Updates()[len(h.bc.Updates())-1]
	assert.Equal(t, "final", final.Message, "message is the row's result_msg")
}

// ---------------------------------------------------------------------------
// AC-073, AC-107: claim and cancel races
// ---------------------------------------------------------------------------

func TestCL_AC073_ConcurrentClaimsExactlyOne(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "queued", nil)

	var delivered atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payloads, err := h.svc.ClaimForHeartbeat(context.Background(), devA)
			assert.NoError(t, err)
			delivered.Add(int32(len(payloads)))
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), delivered.Load(), "[AC-073] exactly one of 16 racing heartbeats may receive the command")
	assert.Equal(t, "dispatched", h.get(uid(1)).Status)
}

func TestHeartbeatClaim_ExcludesAlreadyClaimed(t *testing.T) {
	h := newHarness(t, devA)
	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "fetch_logs"})
	require.NoError(t, err)
	require.Equal(t, "dispatched", res.Command.Status)

	payloads, err := h.svc.ClaimForHeartbeat(context.Background(), devA)

	require.NoError(t, err)
	assert.Empty(t, payloads, "[AC-074] a command already claimed via WS must not be in the heartbeat response")
}

func TestHeartbeatClaim_ReturnsOldestFirstAndHonoursLimit(t *testing.T) {
	h := newHarness(t)
	for i := 1; i <= 12; i++ {
		i := i
		h.seed(uid(i), "fetch_logs", "queued", func(c *models.Command) { c.CreatedAt = c.CreatedAt.Add(time.Duration(i) * time.Second) })
	}

	payloads, err := h.svc.ClaimForHeartbeat(context.Background(), devA)

	require.NoError(t, err)
	require.Len(t, payloads, models.HeartbeatClaimLimit)
	for i, p := range payloads {
		assert.Equal(t, uid(i+1), p.CommandID, "claims are delivered oldest first")
	}
	assert.Equal(t, "queued", h.get(uid(11)).Status, "commands beyond the limit stay queued")
}

func TestCL_AC107_CancelVsClaimExactlyOne(t *testing.T) {
	for i := 0; i < 50; i++ {
		h := newHarness(t)
		id := uid(1)
		h.seed(id, "fetch_logs", "queued", nil)

		var cancelled atomic.Bool
		var claimed atomic.Int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, out, err := h.svc.Cancel(context.Background(), devA, id)
			assert.NoError(t, err)
			cancelled.Store(out == CancelApplied)
		}()
		go func() {
			defer wg.Done()
			payloads, err := h.svc.ClaimForHeartbeat(context.Background(), devA)
			assert.NoError(t, err)
			claimed.Add(int32(len(payloads)))
		}()
		wg.Wait()

		won := claimed.Load() == 1
		assert.NotEqual(t, cancelled.Load(), won, "[AC-107] iteration %d: exactly one of cancel and claim may win", i)
		if won {
			assert.Equal(t, "dispatched", h.get(id).Status)
			_, out, err := h.svc.Cancel(context.Background(), devA, id)
			require.NoError(t, err)
			assert.Equal(t, CancelNotCancellable, out, "[AC-107] if the claim wins, the cancel is refused")
		} else {
			assert.Equal(t, "cancelled", h.get(id).Status)
		}
	}
}

func TestCancel_NotFoundForForeignOrMalformedID(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "queued", func(c *models.Command) { c.DeviceID = "other-device" })

	_, out, err := h.svc.Cancel(context.Background(), devA, uid(1))
	require.NoError(t, err)
	assert.Equal(t, CancelNotFound, out, "a foreign command reads as missing")

	_, out, err = h.svc.Cancel(context.Background(), devA, "../etc/passwd")
	require.NoError(t, err)
	assert.Equal(t, CancelNotFound, out)
	assert.Zero(t, h.repo.CallCount("GetForDevice")-1, "a malformed id must not reach the repository")
	assert.Equal(t, "queued", h.get(uid(1)).Status)
}

func TestCancel_BroadcastsAndRefusesDispatched(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "queued", nil)
	h.seed(uid(2), "fetch_logs", "dispatched", nil)

	cmd, out, err := h.svc.Cancel(context.Background(), devA, uid(1))
	require.NoError(t, err)
	assert.Equal(t, CancelApplied, out)
	assert.Equal(t, "cancelled", cmd.Status)
	assert.Equal(t, "cancelled by operator", cmd.ResultMsg)
	require.Len(t, h.bc.Updates(), 1)
	assert.Equal(t, "cancelled", h.bc.Updates()[0].Status)

	row, out, err := h.svc.Cancel(context.Background(), devA, uid(2))
	require.NoError(t, err)
	assert.Equal(t, CancelNotCancellable, out, "[AC-106] a dispatched command cannot be cancelled")
	assert.Equal(t, "dispatched", row.Status)
	assert.Len(t, h.bc.Updates(), 1, "a refused cancel broadcasts nothing")
}

// ---------------------------------------------------------------------------
// AC-085..AC-089, AC-097, AC-098, AC-102: agent report state machine
// ---------------------------------------------------------------------------

func TestCL_AC085_RejectedTransitionLogged(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "running", nil)

	out := h.report(ack(uid(1)))

	assert.Equal(t, models.ReportInvalidTransition, out.Disposition)
	assert.Equal(t, "running", h.get(uid(1)).Status, "[AC-085] a disallowed transition leaves the state unchanged")
	logs := h.logs.String()
	assert.Contains(t, logs, "command transition rejected", "[AC-085] a rejection must be logged")
	for _, field := range []string{"command_id=" + uid(1), "device_id=" + devA, "from=running", "to=acknowledged"} {
		assert.Contains(t, logs, field, "[NFR-013] the rejection log must carry %s", field)
	}
}

func TestCL_AC086_SucceededIgnoresLateFailed(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "succeeded", nil)

	out := h.report(result(uid(1), models.CommandResult{Status: "error", Message: "late"}))

	assert.Equal(t, models.ReportAlreadyTerminal, out.Disposition, "[AC-086] a different outcome on an immutable row is already_terminal")
	assert.Equal(t, "succeeded", out.Status)
	assert.Equal(t, "succeeded", h.get(uid(1)).Status, "[AC-086] the state must stay succeeded")
	assert.Empty(t, h.get(uid(1)).ResultMsg, "the late result must not rewrite the message")
}

func TestCL_AC086_SameOutcomeIsDuplicateAndUnchanged(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "dispatched", nil)
	first := h.report(result(uid(1), models.CommandResult{Status: "success", Message: "done"}))
	require.Equal(t, models.ReportAccepted, first.Disposition)
	before := h.get(uid(1))
	h.clock.Advance(time.Hour)
	updates := len(h.bc.Updates())

	again := h.report(result(uid(1), models.CommandResult{Status: "success", Message: "done again"}))

	assert.Equal(t, models.ReportDuplicate, again.Disposition, "[AC-023] the same outcome again is a duplicate")
	after := h.get(uid(1))
	assert.True(t, before.UpdatedAt.Equal(after.UpdatedAt), "[AC-023] updated_at must be unchanged")
	assert.Equal(t, "done", after.ResultMsg)
	assert.Len(t, h.bc.Updates(), updates, "a duplicate is not broadcast")
	assert.Len(t, h.ev.Calls(), 1, "a duplicate raises no second completion event")
}

func TestCL_AC087_TimedOutAcceptsOneLateResult(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "timed_out", nil)

	first := h.report(result(uid(1), models.CommandResult{Status: "success", Message: "finished late"}))
	second := h.report(result(uid(1), models.CommandResult{Status: "error", Message: "second"}))

	assert.Equal(t, models.ReportAccepted, first.Disposition)
	assert.Equal(t, "succeeded", h.get(uid(1)).Status, "[AC-087] a late success moves timed_out to succeeded")
	assert.Equal(t, models.ReportAlreadyTerminal, second.Disposition, "[AC-087] a later error is ignored")
	assert.Equal(t, "succeeded", h.get(uid(1)).Status)
}

func TestCL_AC088_AckAfterRunningIgnored(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "running", nil)

	out := h.report(ack(uid(1)))

	assert.Equal(t, models.ReportInvalidTransition, out.Disposition)
	assert.Equal(t, "running", h.get(uid(1)).Status, "[AC-088] an ack after running must not move the state backwards")
	assert.Empty(t, h.bc.Updates(), "a rejected report is not broadcast")
}

func TestCL_AC089_ReportsOnExpiredCancelledIgnoredLogged(t *testing.T) {
	for _, status := range []string{"expired", "cancelled"} {
		for name, rep := range map[string]models.CommandReport{
			"ack":      ack(uid(1)),
			"progress": progress(uid(1), "running", "pull", nil, ""),
			"result":   result(uid(1), models.CommandResult{Status: "success"}),
		} {
			t.Run(status+"/"+name, func(t *testing.T) {
				h := newHarness(t)
				h.seed(uid(1), "fetch_logs", status, nil)

				out := h.report(rep)

				assert.NotEqual(t, models.ReportAccepted, out.Disposition, "[AC-089] a report on %s must be ignored", status)
				assert.Equal(t, status, h.get(uid(1)).Status, "[AC-089] the state must be unchanged")
				assert.Contains(t, h.logs.String(), "command transition rejected", "[AC-089] the rejection must be logged")
				assert.Empty(t, h.bc.Updates())
			})
		}
	}
}

func TestCL_AC097_LegacySuccessFromDispatched(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "dispatched", nil)

	// A legacy agent sends only status success, with no ack, progress or outcome.
	out := h.report(result(uid(1), models.CommandResult{Status: "success", Message: "ok"}))

	assert.Equal(t, models.ReportAccepted, out.Disposition)
	assert.Equal(t, "succeeded", h.get(uid(1)).Status, "[AC-097] a legacy success from dispatched must be succeeded")
}

func TestCL_AC098_LegacyErrorFromDispatched(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "dispatched", nil)

	out := h.report(result(uid(1), models.CommandResult{Status: "error", Message: "bad"}))

	assert.Equal(t, models.ReportAccepted, out.Disposition)
	got := h.get(uid(1))
	assert.Equal(t, "failed", got.Status, "[AC-098] a legacy error from dispatched must be failed")
	assert.Equal(t, "bad", got.ResultMsg)
}

func TestResult_OutcomeFieldOverridesLegacyStatus(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "running", nil)

	h.report(result(uid(1), models.CommandResult{Status: "error", Outcome: models.OutcomeSucceeded, Message: "already up to date"}))

	assert.Equal(t, "succeeded", h.get(uid(1)).Status, "the new outcome field outranks the legacy status")
}

func TestResult_UnrecognizedStatusIsFailedAndWarns(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "running", nil)

	h.report(result(uid(1), models.CommandResult{Status: "weird"}))

	assert.Equal(t, "failed", h.get(uid(1)).Status)
	assert.Contains(t, h.logs.String(), "unrecognized status")
}

func TestCL_AC102_LegacySuccessTreatedAsSucceeded(t *testing.T) {
	h := newHarness(t)
	// A row written by a pre-lifecycle server and never migrated (rollback window).
	h.seed(uid(1), "fetch_logs", "success", nil)
	h.seed(uid(2), "fetch_logs", "sent", nil)

	same := h.report(result(uid(1), models.CommandResult{Status: "success"}))
	other := h.report(result(uid(1), models.CommandResult{Status: "error"}))
	fromSent := h.report(result(uid(2), models.CommandResult{Status: "success"}))

	assert.Equal(t, models.ReportDuplicate, same.Disposition, "[AC-102] a legacy success row must behave as succeeded")
	assert.Equal(t, models.ReportAlreadyTerminal, other.Disposition)
	assert.Equal(t, models.ReportAccepted, fromSent.Disposition, "[AC-102] a legacy sent row accepts a result like dispatched")
	assert.Equal(t, "succeeded", h.get(uid(2)).Status)
}

func TestReport_AckThenProgressThenResultFullLifecycle(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "dispatched", nil)

	require.Equal(t, models.ReportAccepted, h.report(ack(uid(1))).Disposition)
	h.clock.Advance(2 * time.Second)
	require.Equal(t, models.ReportAccepted, h.report(progress(uid(1), "running", "pull", intp(10), "")).Disposition)
	h.clock.Advance(2 * time.Second)
	require.Equal(t, models.ReportAccepted, h.report(progress(uid(1), "verifying", "verify", nil, "")).Disposition)
	h.clock.Advance(2 * time.Second)
	out := h.report(result(uid(1), models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded, Step: "cleanup", Message: "updated"}))

	require.Equal(t, models.ReportAccepted, out.Disposition)
	got := h.get(uid(1))
	assert.Equal(t, "succeeded", got.Status)
	for name, ts := range map[string]*time.Time{"dispatched_at": got.DispatchedAt, "acknowledged_at": got.AcknowledgedAt,
		"started_at": got.StartedAt, "verifying_at": got.VerifyingAt, "finished_at": got.FinishedAt} {
		assert.NotNil(t, ts, "[AC-091] %s must be set", name)
	}
	assert.Len(t, h.ev.Calls(), 1)
	assert.Equal(t, "success", h.ev.Calls()[0].Status, "the completion event keeps the legacy vocabulary")
}

func TestReport_ResultSavesOutputOnlyWhenAccepted(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "running", nil)

	h.report(result(uid(1), models.CommandResult{Status: "success", Output: "line one\nline two"}))
	h.report(result(uid(1), models.CommandResult{Status: "success", Output: "REPLAYED OUTPUT"}))

	outs, err := h.repo.GetCommandOutput(context.Background(), uid(1))
	require.NoError(t, err)
	require.Len(t, outs, 1, "a duplicate result must not store its output")
	assert.Equal(t, "line one\nline two", outs[0].Content)
	assert.Equal(t, "combined", outs[0].Stream)
}

func TestReport_ForeignAndUnknownAreIndistinguishable(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "dispatched", func(c *models.Command) { c.DeviceID = "other-device" })
	before := h.get(uid(1))

	foreign := h.report(ack(uid(1)))
	unknown := h.report(ack(uid(2)))

	assert.Equal(t, models.ReportNotFound, foreign.Disposition)
	assert.Equal(t, unknown, foreign, "[AC-024] a foreign command must read exactly like a missing one")
	assert.Equal(t, before, h.get(uid(1)), "[AC-024] the foreign row must be unchanged")
	assert.Empty(t, h.bc.Updates())
	assert.Empty(t, h.repo.Outputs)
	assert.Empty(t, h.ev.Calls())
}

func TestReport_MalformedIDNeverReachesRepository(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"", "not-a-uuid", "../../telemetry", strings.Repeat("a", 1<<20), strings.ToUpper(validID)} {
		_, err := h.svc.HandleAgentReport(context.Background(), devA, ack(id))
		require.ErrorIs(t, err, ErrMalformedCommandID)
	}
	assert.Empty(t, h.repo.Calls(), "[SEC-001] a malformed id must be rejected before any repository call")
}

func TestReport_UnknownKindAndStateAreInvalid(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.HandleAgentReport(context.Background(), devA, models.CommandReport{Kind: "boom", CommandID: uid(1)})
	require.ErrorIs(t, err, ErrInvalidReport)
	_, err = h.svc.HandleAgentReport(context.Background(), devA, progress(uid(1), "succeeded", "x", nil, ""))
	require.ErrorIs(t, err, ErrInvalidReport)
	_, err = h.svc.HandleAgentReport(context.Background(), devA, models.CommandReport{Kind: models.ReportKindResult, CommandID: uid(1)})
	require.ErrorIs(t, err, ErrInvalidReport)
	assert.Empty(t, h.repo.Calls())
}

func TestReport_RepositoryErrorSurfaces(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "dispatched", nil)
	h.repo.FailOn("Transition", fmt.Errorf("db down"))

	_, err := h.svc.HandleAgentReport(context.Background(), devA, ack(uid(1)))

	require.Error(t, err, "a database error must not be reported as a rejection")
	assert.Empty(t, h.bc.Updates())
}

// ---------------------------------------------------------------------------
// AC-094, AC-095, AC-096
// ---------------------------------------------------------------------------

func TestCL_AC094_StderrTail8KiBAndFailureStep(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "running", nil)
	stderr := strings.Repeat("a", 12<<10) + strings.Repeat("b", 8<<10) // 20 KiB, last 8 KiB are b

	h.report(result(uid(1), models.CommandResult{
		Status: "error", Outcome: models.OutcomeFailed, Step: "recreate", Message: "failed", Stderr: stderr}))

	got := h.get(uid(1))
	assert.Equal(t, strings.Repeat("b", 8<<10), got.StderrTail, "[AC-094] stderr_tail must be the last 8 KiB")
	assert.Equal(t, "recreate", got.FailureStep, "[AC-094] failure_step must be set")
	assert.Equal(t, "recreate", got.Step)
}

func TestResult_SuccessDoesNotSetFailureStep(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "running", nil)

	h.report(result(uid(1), models.CommandResult{Status: "success", Step: "cleanup"}))

	got := h.get(uid(1))
	assert.Equal(t, "cleanup", got.Step)
	assert.Empty(t, got.FailureStep)
}

func TestCL_AC095_OutOfRangeProgressStoredNull(t *testing.T) {
	for _, bad := range []int{150, -1, 101} {
		h := newHarness(t)
		h.seed(uid(1), "docker_update", "dispatched", nil)

		out := h.report(progress(uid(1), "running", "pull", intp(bad), ""))

		assert.Equal(t, models.ReportAccepted, out.Disposition, "[AC-095] the state change must still apply for percent %d", bad)
		got := h.get(uid(1))
		assert.Equal(t, "running", got.Status)
		assert.Nil(t, got.Progress, "[AC-095] an out-of-range percent (%d) must be stored as null", bad)
	}
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "dispatched", nil)
	h.report(progress(uid(1), "running", "pull", intp(100), ""))
	require.NotNil(t, h.get(uid(1)).Progress)
	assert.Equal(t, 100, *h.get(uid(1)).Progress, "100 is a valid percent")
}

func TestCL_AC096_OnlyAcceptedBroadcast(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "dispatched", func(c *models.Command) {
		c.TargetKeys = []string{"docker:container:web"}
	})

	accepted := h.report(progress(uid(1), "running", "pull", intp(40), "pulling layers"))
	rejected := h.report(ack(uid(1)))

	assert.Equal(t, models.ReportAccepted, accepted.Disposition)
	assert.Equal(t, models.ReportInvalidTransition, rejected.Disposition)
	updates := h.bc.Updates()
	require.Len(t, updates, 1, "[AC-096] exactly one broadcast for one accepted and one rejected transition")
	u := updates[0]
	assert.Equal(t, uid(1), u.CommandID)
	assert.Equal(t, devA, u.DeviceID)
	assert.Equal(t, "docker:container:web", u.TargetKey)
	assert.Equal(t, "running", u.Status)
	assert.Equal(t, "pull", u.Step)
	require.NotNil(t, u.Progress)
	assert.Equal(t, 40, *u.Progress)
	assert.Equal(t, "pulling layers", u.Message)
}

// ---------------------------------------------------------------------------
// AC-104: FR-085 revert
// ---------------------------------------------------------------------------

func TestCL_AC104_SendFailureRevertUnlessReported(t *testing.T) {
	t.Run("a failed send reverts the claim to queued", func(t *testing.T) {
		h := newHarness(t, devA)
		h.agents.SendErr = fmt.Errorf("write: broken pipe")

		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "fetch_logs"})

		require.NoError(t, err)
		assert.Equal(t, "queued", res.Command.Status, "[AC-104] a failed server send must leave the command queued")
		stored := h.get(res.Command.ID)
		assert.Equal(t, "queued", stored.Status)
		assert.Nil(t, stored.DeadlineAt, "a revert clears the deadline")
		assert.True(t, stored.SendAmbiguous, "[SEC-016] the revert flags the send as ambiguous")
		assert.Contains(t, h.logs.String(), "send command via ws failed")
	})

	t.Run("a revert after the agent already reported is rejected", func(t *testing.T) {
		h := newHarness(t, devA)
		h.agents.SendErr = fmt.Errorf("write: deadline exceeded")
		// The bytes reached the agent and its ack raced the failed write's return.
		h.agents.OnSend = func(deviceID string, p models.CommandPayload) {
			out, err := h.svc.HandleAgentReport(context.Background(), deviceID, ack(p.CommandID))
			assert.NoError(t, err)
			assert.Equal(t, models.ReportAccepted, out.Disposition)
		}

		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "fetch_logs"})

		require.NoError(t, err)
		assert.Equal(t, "acknowledged", res.Command.Status, "[AC-104] the revert must be rejected once an agent report has arrived")
		assert.Equal(t, "acknowledged", h.get(res.Command.ID).Status)
		assert.Contains(t, h.logs.String(), "command transition rejected")
	})

	t.Run("a connection that vanished before the send reverts without a send error", func(t *testing.T) {
		h := newHarness(t, devA)
		h.agents.OnSend = nil
		h.agents.DisconnectAfterCheck = true

		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "fetch_logs"})

		require.NoError(t, err)
		assert.Equal(t, "queued", res.Command.Status)
		assert.Contains(t, res.Command.ResultMsg, "re-queued for heartbeat delivery")
		assert.NotContains(t, res.Command.ResultMsg, "ws send failed")
	})
}

// ---------------------------------------------------------------------------
// SEC-001: coalescing, throttling, rate limit
// ---------------------------------------------------------------------------

func TestCL_SEC001_IdenticalProgressNoWriteNoBroadcast(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "dispatched", nil)
	first := h.report(progress(uid(1), "running", "pull", intp(10), ""))
	require.Equal(t, models.ReportAccepted, first.Disposition)
	transitions := h.repo.CallCount("Transition")
	broadcasts := len(h.bc.Updates())

	h.clock.Advance(30 * time.Second) // well past the 1s gate: only identity can coalesce
	second := h.report(progress(uid(1), "running", "pull", intp(10), "different message"))

	assert.Equal(t, models.ReportCoalesced, second.Disposition, "[SEC-001] an identical running report must be coalesced")
	assert.Equal(t, transitions, h.repo.CallCount("Transition"), "[SEC-001] a coalesced report must not write")
	assert.Len(t, h.bc.Updates(), broadcasts, "[SEC-001] a coalesced report must not broadcast")
}

func TestCL_SEC001_ProgressThrottledPerCommand(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "dispatched", nil)
	h.report(progress(uid(1), "running", "pull", intp(0), "")) // state change: always written
	writes := h.repo.CallCount("Transition")
	broadcasts := len(h.bc.Updates())

	coalesced := 0
	for i := 1; i <= 25; i++ {
		h.clock.Advance(20 * time.Millisecond) // 25 reports inside one second
		out := h.report(progress(uid(1), "running", "pull", intp(i), ""))
		if out.Disposition == models.ReportCoalesced {
			coalesced++
		}
	}

	assert.Equal(t, 25, coalesced, "[SEC-001] reports inside the 1s gate must all be coalesced")
	assert.Equal(t, writes, h.repo.CallCount("Transition"), "[SEC-001] N reports within a second produce no extra write")
	assert.Len(t, h.bc.Updates(), broadcasts, "[SEC-001] ... and no extra broadcast")

	h.clock.Advance(time.Second)
	out := h.report(progress(uid(1), "running", "pull", intp(50), ""))
	assert.Equal(t, models.ReportAccepted, out.Disposition, "the next report after the gate interval is written")
	assert.Equal(t, writes+1, h.repo.CallCount("Transition"))
}

func TestProgressGate_StateChangesAreNeverGated(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "dispatched", nil)
	h.report(progress(uid(1), "running", "pull", nil, ""))

	// Within the same instant: running -> verifying and then the result.
	verifying := h.report(progress(uid(1), "verifying", "verify", nil, ""))
	done := h.report(result(uid(1), models.CommandResult{Status: "success"}))

	assert.Equal(t, models.ReportAccepted, verifying.Disposition, "a state change is never gated")
	assert.Equal(t, models.ReportAccepted, done.Disposition, "a result is never gated")
}

func TestProgressGate_UnknownCommandsDoNotAllocateEntries(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 50; i++ {
		h.report(progress(uid(1000+i), "running", "pull", nil, ""))
	}
	h.svc.gate.mu.Lock()
	n := len(h.svc.gate.entries)
	h.svc.gate.mu.Unlock()
	assert.Zero(t, n, "[SEC-001] only accepted writes may create gate entries, so random ids cannot grow memory")
}

func TestProgressGate_TerminalTransitionForgetsEntry(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "dispatched", nil)
	h.report(progress(uid(1), "running", "pull", nil, ""))
	h.report(result(uid(1), models.CommandResult{Status: "success"}))

	h.svc.gate.mu.Lock()
	_, exists := h.svc.gate.entries[uid(1)]
	h.svc.gate.mu.Unlock()
	assert.False(t, exists, "a terminal transition must delete the gate entry")
}

func TestAllowReport_PerDeviceBudgetAndThrottledLog(t *testing.T) {
	h := newHarness(t)
	allowed := 0
	for i := 0; i < ReportBurst+40; i++ {
		if h.svc.AllowReport("noisy") {
			allowed++
		}
	}
	assert.Equal(t, ReportBurst, allowed, "[SEC-001] one device gets the burst and no more")
	assert.True(t, h.svc.AllowReport("quiet"), "[SEC-001] another device is unaffected")
	assert.LessOrEqual(t, strings.Count(h.logs.String(), "agent report rate limited"), RejectionLogsPerMinute,
		"the rate-limit Warn goes through the per-device log throttle")
}

func TestCL_SEC001_RejectionLogsThrottledPerDevice(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "fetch_logs", "succeeded", nil)

	for i := 0; i < 40; i++ {
		_, err := h.svc.HandleAgentReport(context.Background(), devA, ack(uid(1)))
		require.NoError(t, err)
	}
	assert.Equal(t, RejectionLogsPerMinute, strings.Count(h.logs.String(), "command transition rejected"),
		"[SEC-001] at most 10 rejection records per device per minute")

	_, err := h.svc.HandleAgentReport(context.Background(), "dev-b", ack(uid(1)))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(h.logs.String(), "command report for unknown or foreign command"),
		"[SEC-001] another device has its own budget (the foreign id is logged as unknown, not as a rejection)")

	h.clock.Advance(61 * time.Second)
	_, err = h.svc.HandleAgentReport(context.Background(), devA, ack(uid(1)))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(h.logs.String(), "rejection logs suppressed"), "the next window emits one summary")
	assert.Contains(t, h.logs.String(), "count=30")
}

// ---------------------------------------------------------------------------
// SEC-002, SEC-016 and the C-11 controls
// ---------------------------------------------------------------------------

func TestCL_SEC002_ReportWithNULAndInvalidUTF8Stored(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "running", nil)
	hostile := "layer\x00 pull \xff\xfe failed\x00"

	out := h.report(result(uid(1), models.CommandResult{
		Status: "error", Step: "pull", Message: hostile, Stderr: hostile, Output: hostile}))

	require.Equal(t, models.ReportAccepted, out.Disposition, "[SEC-002] a content problem must never reject the report")
	got := h.get(uid(1))
	for name, v := range map[string]string{"result_msg": got.ResultMsg, "stderr_tail": got.StderrTail, "step": got.Step, "failure_step": got.FailureStep} {
		assert.NotContains(t, v, "\x00", "[SEC-002] %s must be NUL-free", name)
		assert.True(t, utf8.ValidString(v), "[SEC-002] %s must be valid UTF-8", name)
	}
	outs, err := h.repo.GetCommandOutput(context.Background(), uid(1))
	require.NoError(t, err)
	require.Len(t, outs, 1)
	assert.NotContains(t, outs[0].Content, "\x00")
	assert.True(t, utf8.ValidString(outs[0].Content))
	for _, u := range h.bc.Updates() {
		assert.True(t, utf8.ValidString(u.Message) && !strings.Contains(u.Message, "\x00"), "[SEC-002] broadcasts are sanitized too")
	}
	assert.Contains(t, h.ev.Calls()[0].Message, "layer", "the content is kept")
}

func TestCL_SEC016_AmbiguousSendMessages(t *testing.T) {
	t.Run("revert wording", func(t *testing.T) {
		h := newHarness(t, devA)
		h.agents.SendErr = fmt.Errorf("write: broken pipe")
		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		assert.Equal(t, "ws send failed; delivery uncertain; re-queued for heartbeat delivery", res.Command.ResultMsg)
	})

	t.Run("expiry wording after an ambiguous send", func(t *testing.T) {
		h := newHarness(t, devA)
		h.agents.SendErr = fmt.Errorf("write: broken pipe")
		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		h.clock.Advance(16 * time.Minute)

		h.svc.Sweep(context.Background())

		got := h.get(res.Command.ID)
		assert.Equal(t, "expired", got.Status)
		assert.Equal(t, "expired: delivery unconfirmed within 15m; an earlier send attempt may have reached the agent", got.ResultMsg)
	})

	t.Run("plain expiry wording without a revert", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		h.clock.Advance(16 * time.Minute)

		h.svc.Sweep(context.Background())

		assert.Equal(t, "expired: not delivered within 15m", h.get(res.Command.ID).ResultMsg)
	})

	t.Run("cancel wording and the late-report warning", func(t *testing.T) {
		h := newHarness(t, devA)
		h.agents.SendErr = fmt.Errorf("write: broken pipe")
		res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		id := res.Command.ID

		cancelled, outcome, err := h.svc.Cancel(context.Background(), devA, id)
		require.NoError(t, err)
		require.Equal(t, CancelApplied, outcome)
		assert.Equal(t, "cancelled by operator; an earlier send attempt may have reached the agent", cancelled.ResultMsg)

		late := h.report(result(id, models.CommandResult{Status: "success"}))
		assert.Equal(t, models.ReportAlreadyTerminal, late.Disposition, "the cancelled row stays immutable")
		logs := h.logs.String()
		assert.Contains(t, logs, "agent reported on command after ambiguous send")
		for _, field := range []string{"command_id=" + id, "from=cancelled", "to=cancelled"} {
			assert.Contains(t, logs, field, "[SEC-016] the warning must carry %s", field)
		}
	})

	t.Run("the warning is not throttled away", func(t *testing.T) {
		h := newHarness(t)
		h.seed(uid(1), "reboot", "cancelled", func(c *models.Command) { c.SendAmbiguous = true })
		for i := 0; i < 30; i++ {
			h.report(ack(uid(1)))
		}
		assert.Equal(t, 1, strings.Count(h.logs.String(), "agent reported on command after ambiguous send"),
			"exempt from the throttle but bounded to once per command")
	})
}

func TestCL_SEC_ProgressDoesNotExtendDeadline(t *testing.T) {
	h := newHarness(t)
	cmd := h.seed(uid(1), "docker_update", "dispatched", nil)
	deadline := *cmd.DeadlineAt

	for i := 1; i <= 5; i++ {
		h.clock.Advance(5 * time.Minute)
		h.report(progress(uid(1), "running", "pull", intp(i*10), ""))
	}
	h.report(progress(uid(1), "verifying", "verify", nil, ""))

	got := h.get(uid(1))
	require.NotNil(t, got.DeadlineAt)
	assert.True(t, got.DeadlineAt.Equal(deadline), "[SEC] progress must never move deadline_at (was %s, now %s)", deadline, got.DeadlineAt)
}

func TestCL_SEC_ActiveNonQueuedRowsAlwaysHaveDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		rep  func(id string) models.CommandReport
	}{
		{"ack out of queued", ack},
		{"running out of queued", func(id string) models.CommandReport { return progress(id, "running", "pull", nil, "") }},
		{"verifying out of queued", func(id string) models.CommandReport { return progress(id, "verifying", "verify", nil, "") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed(uid(1), "docker_update", "queued", nil) // never claimed
			reportAt := h.clock.Now().Add(3 * time.Minute)
			h.clock.Advance(3 * time.Minute)

			out := h.report(tc.rep(uid(1)))

			require.Equal(t, models.ReportAccepted, out.Disposition, "an agent report out of queued is proof of delivery")
			got := h.get(uid(1))
			require.NotNil(t, got.DeadlineAt, "[SEC] an active non-queued row must have a deadline_at or the sweeper cannot time it out")
			assert.True(t, got.DeadlineAt.Equal(reportAt.Add(2100*time.Second)), "deadline = report time + exec_deadline_s")
			require.NotNil(t, got.DispatchedAt)
		})
	}

	t.Run("claim stamps the deadline", func(t *testing.T) {
		h := newHarness(t)
		h.seed(uid(1), "docker_update", "queued", nil)
		_, err := h.svc.ClaimForHeartbeat(context.Background(), devA)
		require.NoError(t, err)
		got := h.get(uid(1))
		require.NotNil(t, got.DeadlineAt)
		assert.True(t, got.DeadlineAt.Equal(h.clock.Now().Add(2100*time.Second)))
	})
}

func TestCL_SEC_ClaimSkipsExpiredEvenIfExpireStepFails(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "reboot", "queued", func(c *models.Command) { c.ExpiresAt = h.clock.Now().Add(-time.Minute) })
	h.repo.FailOn("ListExpiredQueued", fmt.Errorf("db hiccup"))

	payloads, err := h.svc.ClaimForHeartbeat(context.Background(), devA)

	require.NoError(t, err, "a failing expire step must not fail the heartbeat")
	assert.Empty(t, payloads, "[SEC] an expired queued reboot must never be delivered, even if the expire step failed")
	assert.Equal(t, "queued", h.get(uid(1)).Status)
	assert.Contains(t, h.logs.String(), "list expired queued commands")

	// And through the WS claim path.
	_, claimed, err := h.repo.ClaimByID(context.Background(), uid(1), h.clock.Now())
	require.NoError(t, err)
	assert.False(t, claimed, "[SEC] ClaimByID must also exclude an expired command")
}

func TestHeartbeatClaim_ExpiresDueCommandsOfThisDevice(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "reboot", "queued", func(c *models.Command) { c.ExpiresAt = h.clock.Now().Add(-time.Minute) })
	h.seed(uid(2), "reboot", "queued", func(c *models.Command) {
		c.DeviceID = "other-device"
		c.ExpiresAt = h.clock.Now().Add(-time.Minute)
	})

	payloads, err := h.svc.ClaimForHeartbeat(context.Background(), devA)

	require.NoError(t, err)
	assert.Empty(t, payloads)
	assert.Equal(t, "expired", h.get(uid(1)).Status, "[AC-028] the heartbeat must expire the device's overdue reboot")
	assert.Equal(t, "queued", h.get(uid(2)).Status, "another device's rows are left to the sweeper")
}

func TestCL_SEC_ReportContentNotLogged(t *testing.T) {
	h := newHarness(t)
	const secret = "TOPSECRET-token-xyz"
	h.seed(uid(1), "docker_update", "dispatched", nil)
	h.seed(uid(2), "docker_update", "succeeded", nil)
	hostileID := "evil-id-" + secret

	h.report(progress(uid(1), "running", "pull", nil, "pulling "+secret))
	h.report(ack(uid(1))) // rejected: running -> acknowledged
	h.report(result(uid(2), models.CommandResult{Status: "error", Message: secret, Stderr: secret, Output: secret}))
	h.report(result(uid(3), models.CommandResult{Status: "error", Message: secret, Stderr: secret, Output: secret}))
	_, err := h.svc.HandleAgentReport(context.Background(), devA, ack(hostileID))
	require.ErrorIs(t, err, ErrMalformedCommandID)
	h.report(result(uid(1), models.CommandResult{Status: "error", Message: secret, Stderr: secret, Output: secret}))

	assert.NotContains(t, h.logs.String(), secret, "[SEC-001] logs must carry lengths, never report content or a raw id")
	assert.Contains(t, h.logs.String(), "malformed command_id from agent")
	assert.Contains(t, h.logs.String(), fmt.Sprintf("length=%d", len(hostileID)))
}

// ---------------------------------------------------------------------------
// Bulk dispatch (FR-060)
// ---------------------------------------------------------------------------

func TestBulk_SkipsConflictingTargetsAndCreatesTheRest(t *testing.T) {
	h := newHarness(t)
	h.tel.Snapshots[devA] = []models.TelemetrySnapshot{{DeviceID: devA, Data: models.FullTelemetryData{
		Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
			{ID: "id-web", Name: "web"}, {ID: "id-db", Name: "db"},
		}}}}}
	busy := h.seed(uid(1), "docker_update", "running", func(c *models.Command) {
		c.TargetKeys = []string{"docker:container:web"}
	})

	res, err := h.svc.DispatchBulkDocker(context.Background(), devA, []string{"id-web", "id-db"})

	require.NoError(t, err)
	require.NotNil(t, res.Command, "[AC-071] the non-conflicting target must still get a command")
	assert.Equal(t, []interface{}{"id-db"}, toInterfaces(res.Command.Params["container_ids"]))
	require.Len(t, res.Conflicts, 1)
	assert.Equal(t, models.BulkConflict{ContainerID: "id-web", TargetKey: "docker:container:web", ExistingCommandID: busy.ID}, res.Conflicts[0],
		"[AC-071] web is reported as conflict with the existing id")
	assert.Equal(t, []string{"docker:container:db"}, res.Command.TargetKeys)
}

func TestBulk_AllConflictingCreatesNoCommand(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "docker_update", "queued", func(c *models.Command) { c.TargetKeys = []string{"docker:id:id-web"} })

	res, err := h.svc.DispatchBulkDocker(context.Background(), devA, []string{"id-web", "id-web"})

	require.NoError(t, err)
	assert.Nil(t, res.Command)
	assert.Len(t, res.Conflicts, 1, "duplicate ids collapse to one conflict")
	assert.Len(t, h.repo.Commands, 1, "no row may be created when every target conflicts")
}

func TestBulk_DeadlineScalesWithTargetCount(t *testing.T) {
	h := newHarness(t)

	res, err := h.svc.DispatchBulkDocker(context.Background(), devA, []string{"a", "b", "c"})

	require.NoError(t, err)
	require.NotNil(t, res.Command)
	assert.Equal(t, 3*2100+300, res.Command.ExecDeadlineS, "docker_bulk_update deadline is min(35m*N+5m, 6h)")
}

func toInterfaces(v interface{}) []interface{} {
	switch s := v.(type) {
	case []string:
		out := make([]interface{}, len(s))
		for i := range s {
			out[i] = s[i]
		}
		return out
	case []interface{}:
		return s
	}
	return nil
}

// AUTOUPD-HARDEN-001: an agent refusal of a compose directory is a precheck
// failure like any other, so it counts toward the policy backoff.
func TestAUTOUPD_RefusalCountsAsFailureForBackoff(t *testing.T) {
	h := newHarness(t)
	polID := h.seedPolicy(0)
	h.seed(uid(1), "docker_update", "dispatched", func(c *models.Command) {
		c.PolicyID = &polID
		c.Params = map[string]interface{}{"compose_project": "media", "compose_work_dir": "/srv/media"}
	})

	h.report(result(uid(1), models.CommandResult{
		Status: "error", Step: models.StepPrecheck,
		Message: `compose directory refused (owner): "/srv/media": "/srv/media" is owned by uid 2000, which has no Docker rights`,
	}))

	p := h.pol.Policies[devA][0]
	assert.Equal(t, 1, p.ConsecutiveFailures, "a refusal is counted as a failed run")
	require.NotNil(t, p.LastFailureAt)
	assert.True(t, p.InBackoff(h.clock.Now()), "and starts the backoff, so the stack is not retried every telemetry cycle")
	assert.Nil(t, p.LastTriggeredAt, "a refusal does not start the success cooldown")
}
