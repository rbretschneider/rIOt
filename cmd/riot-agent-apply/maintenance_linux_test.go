//go:build linux

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/stretchr/testify/require"
)

type testMaintenanceLock struct{ closed bool }

func (l *testMaintenanceLock) Close() error { l.closed = true; return nil }

func TestMaintenanceRecoveryAC005ApplyRefusesBeforeServiceOrStagingMutation(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-005] busy root lock preserves running agent and staged inputs", func(t *testing.T) {
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)
		fx.in.acquireMaintenance = func() (io.Closer, error) { return nil, maintenance.ErrBusy }
		before, err := os.ReadFile(fx.staged.binary)
		require.NoError(t, err)
		require.Equal(t, exitDeferred, runApply(fx.in, fx.svc, fx.fakeSleep))
		require.Empty(t, fx.svc.calls)
		after, err := os.ReadFile(fx.staged.binary)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
	t.Run("[MAINT-RECOVERY AC-005] boot skips while package worker holds exclusion", func(t *testing.T) {
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)
		fx.in.acquireMaintenance = func() (io.Closer, error) { return nil, maintenance.ErrBusy }
		require.Equal(t, exitOK, runBoot(fx.in))
		got, err := os.ReadFile(fx.targetAt)
		require.NoError(t, err)
		require.Equal(t, oldTargetBytes, string(got))
	})
	t.Run("[MAINT-RECOVERY AC-005] lingering unit or unresolved hold cleanup closes lock and defers", func(t *testing.T) {
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)
		lock := &testMaintenanceLock{}
		fx.in.acquireMaintenance = func() (io.Closer, error) { return lock, nil }
		fx.in.maintenanceIdle = func() error { return errors.New("hold restoration pending") }
		require.Equal(t, exitDeferred, runApply(fx.in, fx.svc, fx.fakeSleep))
		require.True(t, lock.closed)
		require.Empty(t, fx.svc.calls)
	})
}

func TestMaintenanceRecoveryAC007UpdaterDurableOutcome(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-007] accepted launch is followed by verified installation outcome", func(t *testing.T) {
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)
		var statuses []maintenance.UpdateStatus
		fx.in.writeUpdateStatus = func(s maintenance.UpdateStatus) error { statuses = append(statuses, s); return nil }
		require.Equal(t, exitOK, runApply(fx.in, fx.svc, fx.fakeSleep))
		require.Len(t, statuses, 2)
		require.Equal(t, "accepted", statuses[0].State)
		require.Equal(t, "installed", statuses[1].State)
		require.Equal(t, fx.in.verifiedSHA256, statuses[1].SHA256)
		require.Equal(t, statuses[0].SHA256, statuses[1].SHA256)
	})
	t.Run("[MAINT-RECOVERY AC-007] refusal retains hash-bound error after rejected staging is removed", func(t *testing.T) {
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)
		require.NoError(t, os.WriteFile(fx.staged.binary, []byte(evilBinaryBytes), 0600))
		var statuses []maintenance.UpdateStatus
		fx.in.writeUpdateStatus = func(s maintenance.UpdateStatus) error { statuses = append(statuses, s); return nil }
		require.Equal(t, exitFailure, runApply(fx.in, fx.svc, fx.fakeSleep))
		require.Len(t, statuses, 2)
		require.Equal(t, "error", statuses[1].State)
		require.Equal(t, statuses[0].SHA256, statuses[1].SHA256)
		require.Equal(t, []string{"stop", "start"}, fx.svc.calls)
	})
	t.Run("[MAINT-RECOVERY AC-007] unavailable durable handoff never stops the agent", func(t *testing.T) {
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)
		fx.in.writeUpdateStatus = func(maintenance.UpdateStatus) error { return errors.New("disk full") }
		require.Equal(t, exitFailure, runApply(fx.in, fx.svc, fx.fakeSleep))
		require.Empty(t, fx.svc.calls)
	})
}

func TestMaintenanceRecoveryAC005RealFlockApplyAndBootContention(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-005] apply and boot honor the worker's real flock until completion", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("requires disposable Linux root fixture for trusted root-owned lock paths")
		}
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)
		path := filepath.Join(fx.dir, "maintenance.lock")
		workerLock, err := maintenance.AcquireLockAt(path)
		require.NoError(t, err)
		defer workerLock.Close()
		fx.in.acquireMaintenance = func() (io.Closer, error) { return maintenance.AcquireLockAt(path) }
		require.Equal(t, exitDeferred, runApply(fx.in, fx.svc, fx.fakeSleep))
		require.Equal(t, exitOK, runBoot(fx.in))
		require.Empty(t, fx.svc.calls)
		fx.assertTargetUntouched()
		require.NoError(t, workerLock.Close())
		require.Equal(t, exitOK, runApply(fx.in, fx.svc, fx.fakeSleep))
		require.Equal(t, newBinaryBytes, fx.installedBytes())
		next, err := maintenance.AcquireLockAt(path)
		require.NoError(t, err, "apply must release exclusion after service start")
		require.NoError(t, next.Close())
	})
}
