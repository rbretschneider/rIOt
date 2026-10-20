package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenance_AC005_ExpectedSignedDigestPersistsAndUnrelatedInstallNeverCompletes(t *testing.T) {
	h := newLifecycleHarness(t)
	now := time.Now().UTC()
	expected := strings.Repeat("a", 64)
	require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionAgentUpdate, StartedAt: now}))
	require.NoError(t, h.a.persistSignedHandoff(updateHandoff{CommandID: idA, SHA256: expected, RequestedAt: now}))
	marker, ok := h.a.inflight.Get(idA)
	require.True(t, ok)
	assert.Equal(t, expected, marker.UpdateSHA256)
	h.a.updateStatusReader = func() (maintenance.UpdateStatus, error) {
		return maintenance.UpdateStatus{State: "installed", SHA256: strings.Repeat("b", 64), UpdatedAt: now.Add(time.Second)}, nil
	}
	h.a.updateDigestReader = func() (string, error) { return strings.Repeat("b", 64), nil }
	h.a.runner = func(context.Context, string, ...string) ([]byte, error) { return []byte("inactive"), nil }
	require.True(t, h.a.recoverSignedAgentUpdate(idA, marker))
	h.wait()
	assert.Equal(t, models.ResultStatusError, h.resultFor(idA).Status, "an unrelated later signed binary cannot satisfy this command")
	_, held := h.a.locks.Holder(models.TargetKeyHost)
	assert.False(t, held)
}

func TestMaintenance_AC005_AcceptedThenStopFailureReportsErrorAndReleasesHost(t *testing.T) {
	h := newLifecycleHarness(t)
	now := time.Now().UTC()
	digest := strings.Repeat("c", 64)
	require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionAgentUpdate, StartedAt: now}))
	handoff := updateHandoff{CommandID: idA, SHA256: digest, RequestedAt: now}
	require.NoError(t, h.a.persistSignedHandoff(handoff))
	_, _, ok := h.a.locks.TryLockAll([]string{models.TargetKeyHost}, idA, models.ActionAgentUpdate)
	require.True(t, ok)
	h.a.updateStatusReader = func() (maintenance.UpdateStatus, error) {
		return maintenance.UpdateStatus{State: "error", SHA256: digest, UpdatedAt: now, Message: "agent stop failed"}, nil
	}
	h.a.watchSignedHandoff(handoff)
	h.wait()
	assert.Equal(t, models.ResultStatusError, h.resultFor(idA).Status)
	assert.Contains(t, h.resultFor(idA).Message, "stop failed")
	_, held := h.a.locks.Holder(models.TargetKeyHost)
	assert.False(t, held)
	assert.Empty(t, h.a.inflight.All())
}

func TestMaintenance_AC006_StartupReservationIgnoresPriorTerminalUUID(t *testing.T) {
	h := newLifecycleHarness(t)
	m, rec := maintenanceFixture()
	var active atomic.Bool
	active.Store(true)
	rec.CommandID = idB
	rec.Outcome = "success"
	rec.FinishedAt = &rec.UpdatedAt
	m.current = func() (maintenance.Record, error) { return rec, nil }
	m.active = func(context.Context) (bool, error) { return active.Load(), nil }
	m.wait = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
			return nil
		}
	}
	h.a.maintenanceClient = m
	require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: rec.StartedAt}))
	h.a.reserveMaintenanceHost(context.Background())
	holder, held := h.a.locks.Holder(models.TargetKeyHost)
	require.True(t, held)
	assert.Equal(t, idA, holder.CmdID)
	active.Store(false)
	h.wait()
	_, held = h.a.locks.Holder(models.TargetKeyHost)
	assert.False(t, held)
	assert.Empty(t, h.a.out.Snapshot(), "reservation watcher does not invent a result")
}

func TestMaintenance_AC009_RebootReservesDelayRechecksWorkerAndNeverReplaysIntent(t *testing.T) {
	h := newLifecycleHarness(t)
	m, rec := maintenanceFixture()
	rec.Outcome = "success"
	rec.FinishedAt = &rec.UpdatedAt
	m.current = func() (maintenance.Record, error) { return rec, nil }
	var active atomic.Bool
	m.active = func(context.Context) (bool, error) { return active.Load(), nil }
	h.a.maintenanceClient = m
	h.a.config.Commands.AllowReboot = true
	entered, release := make(chan struct{}), make(chan struct{})
	h.a.pause = func(context.Context, time.Duration) bool { close(entered); <-release; return true }
	var reboot atomic.Int32
	h.a.runner = func(context.Context, string, ...string) ([]byte, error) { reboot.Add(1); return nil, nil }
	r := commandExecResult{RebootCommandID: idA, RebootClassApplied: []string{"linux-image-amd64"}}
	h.a.maybeRebootAfterPatch(r)
	<-entered
	_, _, ok := h.a.locks.TryLockAll([]string{models.TargetKeyHost}, idB, models.ActionOSUpdate)
	assert.False(t, ok, "new package work cannot enter the reboot delay")
	active.Store(true)
	close(release)
	require.Eventually(t, func() bool { _, held := h.a.locks.Holder(models.TargetKeyHost); return !held }, time.Second, time.Millisecond)
	assert.Zero(t, reboot.Load(), "fresh worker state cancels reboot")
	assert.False(t, h.a.claimPatchReboot(idA), "durable intent is never replayed even when reboot was skipped")
}

func maintenanceFixture() (*maintenanceClient, maintenance.Record) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	r := maintenance.Record{Version: 1, CommandID: idA, Mode: "full", StartedAt: now, UpdatedAt: now, Phase: "configure", Outcome: "running"}
	m := &maintenanceClient{ready: func(context.Context) error { return nil }, write: func(maintenance.Request) error { return nil }, current: func() (maintenance.Record, error) { return r, nil }, terminal: func(string) (maintenance.Record, error) { return maintenance.Record{}, os.ErrNotExist }, active: func(context.Context) (bool, error) { return false, nil }, present: func() bool { return true }, wait: func(context.Context) error { return nil }}
	return m, r
}

func TestMaintenance_AC001_ClosedRequestBeforeAnyHandoff(t *testing.T) {
	m, _ := maintenanceFixture()
	calls := 0
	m.ready = func(context.Context) error { calls++; return nil }
	a := &Agent{maintenanceClient: m}
	for _, tc := range []struct {
		id, mode string
		include  bool
	}{{idA, "remove", false}, {"../../root", "full", false}, {idA, "repair", true}, {idA, "full; reboot", false}} {
		r := a.runDetachedMaintenance(context.Background(), models.CommandPayload{CommandID: tc.id}, tc.mode, tc.include)
		assert.Equal(t, models.ResultStatusError, r.Status)
	}
	assert.Zero(t, calls, "[MAINT-RECOVERY AC-001] invalid inputs never reach privilege readiness")
}

func TestMaintenance_AC006_NondurableMarkerRefusesBeforeRootServiceOrUpdate(t *testing.T) {
	for _, action := range []string{models.ActionOSUpdate, models.ActionAgentUpdate} {
		t.Run(action, func(t *testing.T) {
			h := newLifecycleHarness(t)
			bad := newFaultFS()
			bad.failOn("create", errors.New("storage unavailable"))
			h.a.inflight.file.fs = bad
			runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })
			h.submit(idA, action, nil)
			h.wait()
			assert.Zero(t, runs.Load(), "[MAINT-RECOVERY AC-006] root work cannot start without a durable recovery marker")
			assert.Equal(t, models.ReasonPersistence, h.resultFor(idA).Reason)
		})
	}
}

func TestMaintenance_AC002_FixedUnitHandoffAndValidatedResult(t *testing.T) {
	m, rec := maintenanceFixture()
	started := false
	var commands [][]string
	var request maintenance.Request
	m.write = func(r maintenance.Request) error { request = r; return nil }
	m.terminal = func(id string) (maintenance.Record, error) {
		if !started {
			return maintenance.Record{}, os.ErrNotExist
		}
		rec.Outcome = "success"
		rec.Phase = "finished"
		rec.FinishedAt = &rec.UpdatedAt
		return rec, nil
	}
	a := &Agent{maintenanceClient: m, runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		started = true
		return nil, nil
	}}
	result := a.runDetachedMaintenance(context.Background(), models.CommandPayload{CommandID: idA}, "full", false)
	require.Equal(t, models.ResultStatusSuccess, result.Status)
	assert.Equal(t, maintenance.Request{Version: 1, CommandID: idA, Mode: "full"}, request)
	assert.Equal(t, [][]string{{"sudo", "-n", "/usr/bin/systemctl", "start", "--no-block", maintenance.UnitName}}, commands, "[MAINT-RECOVERY AC-002] only the fixed root service is started; no package children in agent")
}

func TestMaintenance_AC012_MissingPolicyHasNoLegacyAptFallback(t *testing.T) {
	m, _ := maintenanceFixture()
	m.ready = func(context.Context) error { return errors.New("missing signed helper") }
	calls := 0
	a := &Agent{maintenanceClient: m, runner: func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil }}
	result := a.runDetachedMaintenance(context.Background(), models.CommandPayload{CommandID: idA}, "repair", false)
	assert.Equal(t, models.ResultStatusError, result.Status)
	assert.Contains(t, result.Message, "verified root installer")
	assert.Zero(t, calls, "[MAINT-RECOVERY AC-012] unsupported safe policy never falls back to sudo apt/dpkg")
}

func TestMaintenance_AC006_DetachedObserverKeepsMarkerAndHostReservation(t *testing.T) {
	h := newLifecycleHarness(t)
	h.exec(func(cr *cmdRun) commandExecResult {
		_, _, ok := cr.lock(models.TargetKeyHost)
		require.True(t, ok)
		return commandExecResult{Detached: true}
	})
	h.submit(idA, models.ActionOSUpdate, nil)
	h.wait()
	assert.Empty(t, h.a.out.Snapshot())
	assert.Len(t, h.a.inflight.All(), 1)
	holder, held := h.a.locks.Holder(models.TargetKeyHost)
	assert.True(t, held)
	assert.Equal(t, idA, holder.CmdID)
}

func TestMaintenance_AC006_RestartReattachesAndReportsOnceWithoutReplay(t *testing.T) {
	h := newLifecycleHarness(t)
	m, rec := maintenanceFixture()
	var finished atomic.Bool
	m.active = func(context.Context) (bool, error) { return !finished.Load(), nil }
	m.terminal = func(id string) (maintenance.Record, error) {
		if !finished.Load() {
			return maintenance.Record{}, os.ErrNotExist
		}
		r := rec
		r.Outcome = "success"
		r.FinishedAt = &r.UpdatedAt
		r.IncludeRebootClass = true
		r.VersionChanges = []maintenance.VersionChange{{Package: "linux-image-amd64", Before: "1", After: "2"}}
		return r, nil
	}
	m.wait = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
			return nil
		}
	}
	writes := 0
	m.write = func(maintenance.Request) error { writes++; return nil }
	h.a.maintenanceClient = m
	marker := inflightMarker{Action: models.ActionOSUpdate, StartedAt: rec.StartedAt}
	require.True(t, h.a.recoverMaintenanceCommand(idA, marker))
	holder, held := h.a.locks.Holder(models.TargetKeyHost)
	require.True(t, held)
	assert.Equal(t, idA, holder.CmdID)
	finished.Store(true)
	h.wait()
	result := h.resultFor(idA)
	assert.Equal(t, models.ResultStatusSuccess, result.Status)
	assert.Contains(t, result.Message, "automatic reboot not replayed")
	assert.Zero(t, writes, "[MAINT-RECOVERY AC-006] restart observes the original worker without writing or starting a request")
	require.True(t, h.a.recoverMaintenanceCommand(idA, marker))
	assert.Len(t, h.a.out.Snapshot(), 1)
}

func TestMaintenance_AC007_ObserverCancellationNeverSignalsWorker(t *testing.T) {
	m, _ := maintenanceFixture()
	m.active = func(context.Context) (bool, error) { return true, nil }
	m.wait = func(context.Context) error { return context.Canceled }
	a := &Agent{maintenanceClient: m}
	assert.True(t, a.observeMaintenance(context.Background(), idA, false).Detached, "[MAINT-RECOVERY AC-007] cancellation detaches, leaving independent package work intact")
}

func TestMaintenance_AC009_RepairAndCleanupFailuresNeverRequestReboot(t *testing.T) {
	_, rec := maintenanceFixture()
	rec.Outcome = "success"
	rec.FinishedAt = &rec.UpdatedAt
	rec.Mode = "repair"
	rec.VersionChanges = []maintenance.VersionChange{{Package: "linux-image-amd64", Before: "1", After: "2"}}
	assert.Empty(t, maintenanceResult(rec).RebootClassApplied)
	rec.Mode = "full"
	rec.IncludeRebootClass = true
	rec.HoldCleanupFailed = true
	result := maintenanceResult(rec)
	assert.Equal(t, models.ResultStatusError, result.Status)
	assert.Empty(t, result.RebootClassApplied)
}

func TestMaintenance_AC005_AutomaticUpdateDefersUnderHostOrDockerLocks(t *testing.T) {
	for _, key := range []string{models.TargetKeyHost, "docker:container:web"} {
		t.Run(key, func(t *testing.T) {
			fx := newUpdateCycleFixture(t, testCurrentVer, nil, availableUpdate(testLatestVer))
			fx.agent.locks = newTargetLocks()
			_, _, ok := fx.agent.locks.TryLockAll([]string{key}, idB, models.ActionOSUpdate)
			require.True(t, ok)
			fx.agent.runUpdateCheck(context.Background(), fx.apply(nil), fx.restart)
			assert.Zero(t, fx.appliedCount())
			assert.Zero(t, fx.restartCount())
			assert.Zero(t, fx.eventCount(models.EventAgentUpdateCompleted))
		})
	}
}

func TestMaintenance_AC005_AcceptedSignedUpdateDoesNotClaimCompletionOrRestart(t *testing.T) {
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, availableUpdate(testLatestVer))
	fx.agent.runUpdateCheck(context.Background(), fx.apply(errUpdateScheduled), fx.restart)
	assert.Zero(t, fx.restartCount())
	assert.Zero(t, fx.eventCount(models.EventAgentUpdateCompleted))
	assert.Zero(t, fx.eventCount(models.EventAgentUpdateFailed))
}

func TestMaintenance_AC005_StaleDigestAcknowledgementIsRejected(t *testing.T) {
	fx := newReleaseFixture(t)
	fx.pipeline.status = func() (maintenance.UpdateStatus, error) {
		return maintenance.UpdateStatus{Version: 1, State: "installed", SHA256: sha256HexString(fx.binary), UpdatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	}
	fx.pipeline.wait = func(context.Context) error { return context.DeadlineExceeded }
	assert.ErrorIs(t, fx.update(), context.DeadlineExceeded, "[MAINT-RECOVERY AC-005] matching hash alone cannot reuse an old acknowledgement")
}
