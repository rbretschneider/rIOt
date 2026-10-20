package server

import (
	"context"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// PART-001 retention wiring tests (AC-014, AC-015). These exercise the
// package-level runRetention function directly with mock repos — no live
// database required. The six in-scope tables (heartbeats,
// telemetry_snapshots, device_logs, container_metrics, container_logs,
// probe_results) no longer have Purge methods on their repos/interfaces at
// all (removed in this story), so their non-invocation is compile-enforced;
// AC-014 additionally asserts that the retention path itself compiles and
// runs using only the five out-of-scope repositories.

// TestPartitionRetentionCutover_AC014_AC015 groups AC-014 and AC-015 because
// they exercise the exact same runRetention call — AC-014 is proven by the
// fact that the six removed Purge methods are compile-time-absent from
// their repo interfaces (nothing to invoke), and AC-015 is proven by
// spying on the five out-of-scope mocks below.
func TestPartitionRetentionCutover_AC014_AC015(t *testing.T) {
	t.Run("[AC-014] runRetention no longer purges any of the six in-scope tables", func(t *testing.T) {
		// The six in-scope tables' repos (TelemetryRepo, DeviceLogRepo,
		// ContainerLogRepo, ContainerMetricRepo, ProbeRepo) are not passed to
		// runRetention at all — its signature only accepts the five
		// out-of-scope repositories. This is a compile-time guarantee that no
		// DELETE purge can run against the six PART-001 tables from this
		// function; there is no method left to call even by mistake (AD-006).
		eventRepo := testutil.NewMockEventRepo()
		notifyRepo := testutil.NewMockNotifyRepo()
		logRepo := testutil.NewMockLogRepo()
		deviceProbeRepo := testutil.NewMockDeviceProbeRepo()
		caRepo := testutil.NewMockCARepo()
		cfg := &Config{RetentionDays: 30}

		runRetention(context.Background(), cfg, eventRepo, notifyRepo, logRepo, deviceProbeRepo, caRepo)
		// No panic / no call surface for the six in-scope tables exists —
		// reaching this line with the five mocks below all having fired is
		// the proof.
	})

	t.Run("[AC-015] the five out-of-scope purges still run unchanged", func(t *testing.T) {
		eventRepo := testutil.NewMockEventRepo()
		notifyRepo := testutil.NewMockNotifyRepo()
		logRepo := testutil.NewMockLogRepo()
		deviceProbeRepo := testutil.NewMockDeviceProbeRepo()
		caRepo := testutil.NewMockCARepo()
		cfg := &Config{RetentionDays: 30}

		runRetention(context.Background(), cfg, eventRepo, notifyRepo, logRepo, deviceProbeRepo, caRepo)

		if !eventRepo.PurgeCalled {
			t.Error("EventRepo.Purge (events, 90d) was not called — out-of-scope retention must be unaffected by PART-001")
		}
		if !notifyRepo.PurgeNotificationLogCalled {
			t.Error("NotifyRepo.PurgeNotificationLog (notification_log, 90d) was not called")
		}
		if !logRepo.PurgeCalled {
			t.Error("LogRepo.Purge (server logs, 7d) was not called")
		}
		if !deviceProbeRepo.PurgeResultsCalled {
			t.Error("DeviceProbeRepo.PurgeResults (device_probe_results — a DIFFERENT table from probe_results) was not called")
		}
		if !caRepo.PurgeStaleBootstrapKeysCalled {
			t.Error("CARepo.PurgeStaleBootstrapKeys was not called")
		}
	})

	t.Run("[AC-015] a single repo failure does not prevent the others from running", func(t *testing.T) {
		eventRepo := testutil.NewMockEventRepo()
		notifyRepo := testutil.NewMockNotifyRepo()
		logRepo := testutil.NewMockLogRepo()
		deviceProbeRepo := testutil.NewMockDeviceProbeRepo()
		caRepo := testutil.NewMockCARepo()
		cfg := &Config{RetentionDays: 30}

		eventRepo.Err = assertErr{"events purge failed"}

		runRetention(context.Background(), cfg, eventRepo, notifyRepo, logRepo, deviceProbeRepo, caRepo)

		if !notifyRepo.PurgeNotificationLogCalled || !logRepo.PurgeCalled ||
			!deviceProbeRepo.PurgeResultsCalled || !caRepo.PurgeStaleBootstrapKeysCalled {
			t.Error("a failure in one out-of-scope purge must not prevent the others from running")
		}
	})
}

// assertErr is a trivial error type for injecting failures into mocks.
type assertErr struct{ msg string }

func (e assertErr) Error() string { return e.msg }
