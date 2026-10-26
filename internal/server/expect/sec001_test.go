package expect_test

import (
	"errors"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SEC-001 / C-1: a stale powered-off row must never hide a later outage. Every
// scenario below leaves picups with a powered-off row that the device outlived
// (it came back without the tracker seeing a clean offline-to-online
// transition); the second offline transition must then alert.

// secondOfflineAlerts is the common closing assertion.
func secondOfflineAlerts(t *testing.T, h *harness) {
	t.Helper()
	before := len(h.alertsOf(models.EventDeviceOffline))
	h.advance(10 * time.Minute)
	off := h.deviceOffline("picups")
	assert.False(t, off.Covered, "[SEC-001] a second offline transition is never covered by an old powered-off row")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), before+1, "[SEC-001] the second transition alerts")
	assert.True(t, h.alerts[len(h.alerts)-1].Notified, "[SEC-001] and notifies")
	_, stillThere := h.repo.PoweredOffRow("picups")
	assert.False(t, stillThere, "[SEC-001] and the row is gone")
	assert.Nil(t, h.activity("picups"), "[SEC-001] the device shows no powered-off label")
}

var sec001Scenarios = []struct {
	name string
	run  func(t *testing.T) *harness
}{
	{"heartbeat return then restart inside the grace", func(t *testing.T) *harness {
		h := newHarness(t)
		shutdownPoweredOff(t, h)
		h.at("10:30:00")
		h.heartbeat("picups")
		h.restart(h.clock.Now().Add(2 * time.Minute))
		return h
	}},
	{"heartbeat return then restart outside the grace", func(t *testing.T) *harness {
		h := newHarness(t)
		shutdownPoweredOff(t, h)
		h.at("10:30:00")
		h.heartbeat("picups")
		h.restart(time.Time{})
		h.advance(time.Minute)
		return h
	}},
	{"heartbeat return seen by the offline worker", func(t *testing.T) *harness {
		h := newHarness(t)
		shutdownPoweredOff(t, h)
		h.at("10:30:00")
		h.heartbeat("picups")
		h.observe()
		return h
	}},
	{"delete failure then reconcile", func(t *testing.T) *harness {
		h := newHarness(t)
		shutdownPoweredOff(t, h)
		h.at("10:30:00")
		h.repo.FailOn("DeletePoweredOff", errors.New("db down"))
		h.register("picups")
		_, stuck := h.repo.PoweredOffRow("picups")
		assert.True(t, stuck, "the failed delete left the durable row behind")
		h.repo.FailOn("DeletePoweredOff", nil)
		h.advance(31 * time.Second) // reconcile purges and never resurrects it
		return h
	}},
	{"registration while the database says online", func(t *testing.T) *harness {
		h := newHarness(t)
		shutdownPoweredOff(t, h)
		h.at("10:30:00")
		h.heartbeat("picups")
		h.tr.NoteRegistration("picups", h.clock.Now()) // wasOnline: no NoteOnline follows
		return h
	}},
	{"online note while the tracker is not ready", func(t *testing.T) *harness {
		h := newHarness(t, withoutLoad())
		h.repo.SeedPoweredOff(models.PoweredOffState{DeviceID: "picups", CommandID: "c", ActionLabel: "Shutdown", ActorPhrase: "by ryan",
			ShutdownAt: baseTime, OfflineAt: baseTime.Add(time.Minute)})
		h.devices.Devices["picups"].Status = models.DeviceStatusOffline
		h.heartbeat("picups")
		h.tr.NoteOnline(h.ctx(), "picups", h.clock.Now())
		return h
	}},
}

func TestEX_SEC001_HeartbeatReturnThenRestartInsideGrace(t *testing.T) {
	h := sec001Scenarios[0].run(t)
	_, ok := h.repo.PoweredOffRow("picups")
	assert.False(t, ok, "[SEC-001] Load purges a row whose device is live, even inside the grace")
	secondOfflineAlerts(t, h)
}

func TestEX_SEC001_HeartbeatReturnThenRestartOutsideGrace(t *testing.T) {
	h := sec001Scenarios[1].run(t)
	_, ok := h.repo.PoweredOffRow("picups")
	assert.False(t, ok, "[SEC-001] the row is purged at Load")
	secondOfflineAlerts(t, h)
}

func TestEX_SEC001_DeleteFailureThenReconcile(t *testing.T) {
	h := sec001Scenarios[3].run(t)
	list, err := h.repo.ListPoweredOff(h.ctx())
	require.NoError(t, err)
	assert.Empty(t, list, "[SEC-001] ListPoweredOff never returns a row for a live device")
	assert.Nil(t, h.activity("picups"), "[SEC-001] a reconcile never resurrects the label")
	secondOfflineAlerts(t, h)
}

func TestEX_SEC001_RegistrationWhileOnlineClearsRow(t *testing.T) {
	h := sec001Scenarios[4].run(t)
	_, ok := h.repo.PoweredOffRow("picups")
	assert.False(t, ok, "[SEC-001] a registration is evidence of life: the row is deleted")
	secondOfflineAlerts(t, h)
}

func TestEX_SEC001_NoteOnlineWhileNotReadyDeletes(t *testing.T) {
	h := sec001Scenarios[5].run(t)
	require.Error(t, h.tr.Ready(), "the tracker was never loaded")
	_, ok := h.repo.PoweredOffRow("picups")
	assert.False(t, ok, "[SEC-001] the durable delete happens whether or not the tracker is ready")
}

func TestEX_SEC001_SecondOfflineAlerts(t *testing.T) {
	for _, sc := range sec001Scenarios {
		t.Run(sc.name, func(t *testing.T) {
			h := sc.run(t)
			if h.tr.Ready() != nil {
				require.NoError(t, h.tr.Load(h.ctx()), "load before the second transition")
				h.tick()
			}
			secondOfflineAlerts(t, h)
		})
	}
}

func TestEX_SEC001_RowNeverRestamped(t *testing.T) {
	h := newHarness(t)
	shutdownPoweredOff(t, h)
	row, ok := h.repo.PoweredOffRow("picups")
	require.True(t, ok)
	assert.Equal(t, 1, h.repo.CallCount("UpsertPoweredOff"))

	h.advance(time.Hour)
	off := h.deviceOffline("picups") // a second transition without any return in between

	assert.False(t, off.Covered, "[SEC-001 d] the second transition is not covered by the old row")
	assert.Equal(t, 1, h.repo.CallCount("UpsertPoweredOff"), "[SEC-001 d] no upsert refreshed the row")
	_, ok = h.repo.PoweredOffRow("picups")
	assert.False(t, ok, "[SEC-001 a] the transition ended the powered-off state")
	assert.True(t, row.OfflineAt.Equal(baseTime.Add(3*time.Minute)), "the original row kept its own offline_at")
	require.Len(t, h.alertsOf(models.EventDeviceOffline), 1, "[SEC-001] and it alerted")
}

func TestEX_SEC001_LoopEntryRequiresOfflineInsideWindow(t *testing.T) {
	cases := []struct {
		name      string
		offlineAt time.Time // when the device went offline (heartbeat + 45s)
		now       time.Time
		want      bool
	}{
		{"offline before the shutdown was dispatched", baseTime.Add(-10 * time.Minute), baseTime.Add(5 * time.Minute), false},
		{"offline after the return phase ended", baseTime.Add(30 * time.Minute), baseTime.Add(40 * time.Minute), false},
		{"offline inside the return phase", baseTime.Add(3 * time.Minute), baseTime.Add(5 * time.Minute), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			d := baseTime
			dl := d.Add(20 * time.Minute)
			f := baseTime.Add(2 * time.Minute)
			h.seed(models.Command{ID: "sd-1", DeviceID: "picups", Action: models.ActionShutdown, Status: "succeeded",
				DispatchedAt: &d, DeadlineAt: &dl, FinishedAt: &f, UpdatedAt: f})
			hb := tc.offlineAt.Add(-models.OfflineHeartbeatThreshold)
			h.devices.Devices["picups"].Status = models.DeviceStatusOffline
			h.devices.Devices["picups"].LastHeartbeat = &hb
			h.clock.Set(tc.now)

			h.restart(time.Time{})
			h.tick()

			_, ok := h.repo.PoweredOffRow("picups")
			assert.Equal(t, tc.want, ok, "[SEC-001 a] the loop enters powered-off only when offlineSince is inside [D, closeAt)")
			assert.Empty(t, h.alerts)
			assert.Empty(t, h.escalationsOf(models.EventDeviceOffline), "a shutdown never escalates for being offline (FR-021 does not apply)")
		})
	}
}

func TestEX_SEC001_UnverifiableRowAlerts(t *testing.T) {
	t.Run("upsert fails", func(t *testing.T) {
		h := newHarness(t)
		c := h.dispatch("picups", models.ActionShutdown)
		h.at("10:02:00")
		h.succeed(c)
		h.repo.FailOn("UpsertPoweredOff", errors.New("db down"))
		h.at("10:03:00")

		off := h.deviceOffline("picups")

		assert.False(t, off.Covered, "[SEC-001 c] a powered-off row that cannot be stored means the offline alerts")
		require.Len(t, h.alertsOf(models.EventDeviceOffline), 1)
		h.repo.FailOn("UpsertPoweredOff", nil)
		h.advance(time.Minute)
		_, ok := h.repo.PoweredOffRow("picups")
		assert.False(t, ok, "the loop must not turn an alerted transition into a silent powered-off state afterwards")
	})
	t.Run("delete fails on the second transition", func(t *testing.T) {
		h := newHarness(t)
		shutdownPoweredOff(t, h)
		h.repo.FailOn("DeletePoweredOff", errors.New("db down"))
		h.advance(time.Hour)

		off := h.deviceOffline("picups")

		assert.False(t, off.Covered, "[SEC-001 c] the old row cannot be removed: the transition still alerts")
		require.Len(t, h.alertsOf(models.EventDeviceOffline), 1)
	})
}
