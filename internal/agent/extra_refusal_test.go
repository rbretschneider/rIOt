package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DIETPI-PATH-001 FR-012 and FR-014 on the agent: a refused updater leaves a
// successful run successful and raises one warning event.

func refusedRecord(reason string) maintenance.Record {
	return v2Record(maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterSkipped, Reason: reason, Note: "x"}, false)
}

func observeOnce(t *testing.T, h *lifecycleHarness, rec maintenance.Record, recovered bool) commandExecResult {
	t.Helper()
	m, _ := maintenanceFixture()
	m.terminal = func(string) (maintenance.Record, error) { return rec, nil }
	h.a.maintenanceClient = m
	cr := h.a.newCmdRun(models.CommandPayload{CommandID: idA, Action: models.ActionOSUpdate, Params: map[string]interface{}{"mode": "full"}})
	defer cr.cancel()
	res := h.a.observeMaintenanceWith(context.Background(), idA, recovered, cr)
	h.wait()
	return res
}

func sentEvents(h *lifecycleHarness) []*models.AgentEvent {
	h.tr.mu.Lock()
	defer h.tr.mu.Unlock()
	return append([]*models.AgentEvent(nil), h.tr.agentEvents...)
}

func TestExtraEvents_DP_AC011_OneRefusalEvent(t *testing.T) {
	refusals := []string{
		maintenance.SkipUnsafePath, maintenance.SkipDpkgLocked, maintenance.SkipInsufficientTime, maintenance.SkipMinInterval,
		maintenance.SkipBusy, maintenance.SkipTmpNotPrivate, maintenance.SkipMountPropagationUnver, maintenance.SkipRunDirNotPrivate,
		maintenance.SkipNonDefaultSource,
	}
	for _, reason := range refusals {
		t.Run("[AC-011] a refusal for "+reason+" sends exactly one warning finished event carrying the reason", func(t *testing.T) {
			h := newLifecycleHarness(t)
			require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
			res := observeOnce(t, h, refusedRecord(reason), false)
			assert.Equal(t, models.ResultStatusSuccess, res.Status, "a refusal keeps the command successful")
			events := sentEvents(h)
			require.Len(t, events, 1, "exactly one event and no start event")
			e := events[0]
			assert.Equal(t, models.EventExtraUpdaterFinished, e.Type)
			assert.Equal(t, models.SeverityWarning, e.Severity)
			assert.Equal(t, maintenance.UpdaterSkipped, e.Outcome)
			assert.Equal(t, reason, e.Reason)
			assert.Equal(t, idA, e.CommandID)
			assert.Equal(t, "dietpi", e.Updater)
		})
	}
	t.Run("[AC-011] the refusal event is recorded under the shared dietpi:end marker and sent once across an agent restart", func(t *testing.T) {
		dir := t.TempDir()
		h := newLifecycleHarnessAt(t, dir)
		require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		observeOnce(t, h, refusedRecord(maintenance.SkipUnsafePath), false)
		marker, _ := h.a.inflight.Get(idA)
		assert.Equal(t, []string{"dietpi:end"}, marker.ExtraEvents)
		assert.Len(t, sentEvents(h), 1)

		again := newLifecycleHarnessAt(t, dir)
		require.NoError(t, again.a.inflight.Load())
		observeOnce(t, again, refusedRecord(maintenance.SkipUnsafePath), true)
		assert.Empty(t, sentEvents(again), "the restarted agent finds the durable marker and sends nothing")
		observeOnce(t, h, refusedRecord(maintenance.SkipUnsafePath), true)
		assert.Len(t, sentEvents(h), 1, "the same agent observing the record again sends nothing")
	})
	t.Run("[AC-011] a refusal adds no progress step", func(t *testing.T) {
		h := newLifecycleHarness(t)
		require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		m, _ := maintenanceFixture()
		m.terminal = func(string) (maintenance.Record, error) { return refusedRecord(maintenance.SkipUnsafePath), nil }
		h.a.maintenanceClient = m
		cr := h.a.newCmdRun(models.CommandPayload{CommandID: idA, Action: models.ActionOSUpdate, Params: map[string]interface{}{"mode": "full"}})
		defer cr.cancel()
		h.a.observeMaintenanceWith(context.Background(), idA, false, cr)
		h.wait()
		for _, s := range cr.stepNames() {
			assert.NotContains(t, s, "extra_dietpi", "no updater step for a step that never started")
		}
	})
	t.Run("[AC-011] a neutral skip and apt_not_successful send no event", func(t *testing.T) {
		for _, reason := range []string{maintenance.SkipNotEnabled, maintenance.SkipStatusUnknown, maintenance.SkipNothingPending} {
			h := newLifecycleHarness(t)
			require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
			observeOnce(t, h, refusedRecord(reason), false)
			assert.Empty(t, sentEvents(h), "neutral skip %s raises nothing", reason)
		}
		h := newLifecycleHarness(t)
		require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		rec := refusedRecord(maintenance.SkipAptNotSuccessful)
		rec.Outcome, rec.ExitCode, rec.AptOutcome = "error", 100, "error"
		observeOnce(t, h, rec, false)
		assert.Empty(t, sentEvents(h), "apt_not_successful is explained by the apt failure itself")
	})
	t.Run("[AC-011] an updater that ran still sends its start and end events, unchanged", func(t *testing.T) {
		h := newLifecycleHarness(t)
		require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		started := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
		observeOnce(t, h, v2Record(maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterSuccess, StartedAt: &started, FinishedAt: &started}, false), false)
		events := sentEvents(h)
		require.Len(t, events, 2)
		assert.Empty(t, events[1].Reason, "only a skipped event carries a reason")
	})
	t.Run("[AC-011] the event is not sent when the durable marker cannot be written", func(t *testing.T) {
		h := newLifecycleHarness(t)
		require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		bad := newFaultFS()
		bad.failOn("create", assert.AnError)
		h.a.inflight.file.fs = bad
		observeOnce(t, h, refusedRecord(maintenance.SkipUnsafePath), false)
		assert.Empty(t, sentEvents(h), "an event that was not recorded first is never sent")
	})
}

func TestOSUpdate_DP_AC010_AgentSideSkipKeepsSuccessAndRaisesEvent(t *testing.T) {
	newAgentRig := func(t *testing.T) (*lifecycleHarness, *extraRig) {
		root := dietpiRoot(t)
		g := newExtraRig(t, root, enabledCommands())
		h := newLifecycleHarness(t)
		h.a.config = g.a.config
		h.a.maintenanceClient = g.client
		h.a.extra.fs = extraupd.DirFSOwnedByCaller(root)
		require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		return h, g
	}
	t.Run("[AC-010] enabled and pending with no budget stamp: the run stays successful and one insufficient_time event is sent", func(t *testing.T) {
		h, _ := newAgentRig(t)
		plan := h.a.planExtras(context.Background(), "full", map[string]interface{}{})
		out := h.a.applyExtraV1Lines(context.Background(), idA, plan, commandExecResult{Status: models.ResultStatusSuccess, Message: "Package state verified"})
		h.wait()
		assert.Equal(t, models.ResultStatusSuccess, out.Status)
		assert.Equal(t, "apt: succeeded; dietpi: skipped (insufficient_time)", firstLineOf(out.Message))
		events := sentEvents(h)
		require.Len(t, events, 1)
		assert.Equal(t, maintenance.SkipInsufficientTime, events[0].Reason)
		assert.Equal(t, models.SeverityWarning, events[0].Severity)
		assert.Equal(t, maintenance.UpdaterSkipped, events[0].Outcome)
	})
	t.Run("[AC-011] the same command re-planned sends the event once", func(t *testing.T) {
		h, _ := newAgentRig(t)
		for i := 0; i < 3; i++ {
			plan := h.a.planExtras(context.Background(), "full", map[string]interface{}{})
			h.a.applyExtraV1Lines(context.Background(), idA, plan, commandExecResult{Status: models.ResultStatusSuccess})
		}
		h.wait()
		assert.Len(t, sentEvents(h), 1)
	})
	t.Run("[AC-011] neutral agent-side skips (not enabled, excluded) send no event", func(t *testing.T) {
		h, _ := newAgentRig(t)
		plan := h.a.planExtras(context.Background(), "full", map[string]interface{}{models.ParamExtraUpdaterBudget: true, models.ParamExcludeExtraUpdaters: true})
		out := h.a.applyExtraV1Lines(context.Background(), idA, plan, commandExecResult{Status: models.ResultStatusSuccess})
		h.wait()
		assert.Contains(t, out.Message, "dietpi: skipped (excluded_by_operator)")
		assert.Empty(t, sentEvents(h))
	})
	t.Run("[AC-011] after an apt failure the skip is apt_not_successful and no event is sent", func(t *testing.T) {
		h, _ := newAgentRig(t)
		plan := h.a.planExtras(context.Background(), "full", map[string]interface{}{})
		out := h.a.applyExtraV1Lines(context.Background(), idA, plan, commandExecResult{Status: models.ResultStatusError, Message: "upgrade failed"})
		h.wait()
		assert.Equal(t, models.ResultStatusError, out.Status)
		assert.Empty(t, sentEvents(h))
	})
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
