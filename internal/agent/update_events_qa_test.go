package agent

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer (EXPECT-007). Nothing here starts a process, performs an
// update or sleeps in real time: the server is the fake wire of update_events_test.go,
// the clock is fake, and the slow attempts advance the fake clock only.

// Covers AC-031, FR-032. The five-attempt schedule alone keeps the default policy
// inside 90 s, so the explicit "no attempt may start later than the budget"
// check only matters when attempts themselves are slow. A sink that takes 60 s of
// fake time per failing attempt must stop the sender before a third attempt
// would start at 120 s, with exactly one Warn that counts the attempts made.
func TestEX7_AC031_QA_SlowAttemptsNeverStartPastBudget(t *testing.T) {
	logs := captureEX7Logs(t)
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.wire.script = func(typ models.EventType, _ int) ex7Answer {
		if typ == ex7Started {
			return ex7Answer{status: 503, after: func() { ag.clk.Advance(60 * time.Second) }}
		}
		return ex7Answer{}
	}
	applied := 0
	ag.runUpdateCheck(context.Background(), func(context.Context, string, string, string, string) error {
		applied++
		return errUpdateScheduled
	}, func() {})

	starts := ag.wire.sent(ex7Started)
	require.NotEmpty(t, starts)
	first := starts[0].at
	for i, s := range starts {
		assert.LessOrEqual(t, s.at.Sub(first), models.UpdateEventRetryBudget,
			"[FR-032] attempt %d must not start more than 90 s after the first, even when attempts are slow", i+1)
	}
	assert.Len(t, starts, 2, "[FR-032] the first attempt ends at 60 s, the second starts at 60 s and ends at 120 s: no third attempt")
	assert.Equal(t, 1, applied, "[FR-033] the update proceeds after the budget is exhausted")
	warns := logs.atLeast(slog.LevelWarn, isNotDelivered)
	require.Len(t, warns, 1, "[FR-036] exactly one Warn for the abandoned event")
	assert.Equal(t, map[string]any{"type": "agent_update_started", "attempts": int64(2)}, normalizeAttrs(recordAttrs(warns[0])),
		"[FR-036] the Warn counts the attempts actually made")
}

// Covers AC-006, FR-003. The gate calls enter once per gate call, so the gate
// tests cannot tell a reporter that is not idempotent. FR-003 says "at most once
// per command": the reporter itself must hold that, whoever calls it.
func TestEX7_AC006_QA_ReporterEnterIsIdempotent(t *testing.T) {
	r, tt := ex7Rig(t, progressWorks, nil)
	cr := r.a.newCmdRun(models.CommandPayload{CommandID: idA, Action: models.ActionReboot})
	rep := newPowerWaitReporter(cr)

	assert.False(t, rep.entered(), "[FR-002] a reporter that was never entered reports nothing")
	rep.leave() // a no-op when the wait never began: no power step
	rep.enter()
	rep.enter()
	rep.enter()
	rep.settle()
	assert.True(t, rep.entered())

	barrier := make(chan struct{})
	r.a.notify.jobs <- func(context.Context) { close(barrier) }
	select {
	case <-barrier:
	case <-time.After(5 * time.Second):
		t.Fatal("the notifier did not drain")
	}
	assert.Equal(t, []string{models.StepWaitDpkgLock}, tt.sentSteps(),
		"[FR-003] three enter calls send exactly one wait report, and the no-op leave sent nothing")
	assert.Len(t, cr.steps, 1, "[FR-003] one step entry")
}

// Covers AC-006, FR-006. A nil reporter (the post-patch callers, any context
// without one) must be safe in every method the gate or a handler could call.
func TestEX7_AC009_QA_NilReporterIsInert(t *testing.T) {
	var rep *powerWaitReporter
	assert.NotPanics(t, func() {
		rep.enter()
		rep.settle()
		rep.leave()
	}, "[FR-007] a missing reporter changes nothing")
	assert.False(t, rep.entered())
	assert.Nil(t, powerWaitReporterFrom(context.Background()), "[FR-007] a plain context carries no reporter")
}
