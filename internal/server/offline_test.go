package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The offline detector and the restart series (EXPECT-001 ADD section 8, "O":
// AC-035, SEC-004, SEC-007). Clocks, tickers and timers are injected: nothing
// here sleeps.

var offlineBase = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

type offlineRig struct {
	devices *testutil.MockDeviceRepo
	events  *testutil.MockEventRepo
	gen     *events.Generator
	obs     *recordingObserver
	det     *offlineDetector
	mu      sync.Mutex // guards now: the run tests read it from the detector goroutine
	now     time.Time
}

type recordingObserver struct {
	mu    sync.Mutex
	calls [][]models.Device
}

func (r *recordingObserver) ObserveDevices(devices []models.Device, _ time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]models.Device(nil), devices...))
}

func (r *recordingObserver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newOfflineRig(t *testing.T, graceEnd time.Time) *offlineRig {
	t.Helper()
	r := &offlineRig{
		devices: testutil.NewMockDeviceRepo(), events: testutil.NewMockEventRepo(),
		obs: &recordingObserver{}, now: offlineBase,
	}
	rules := testutil.NewMockAlertRuleRepo()
	rules.Rules = []models.AlertRule{{ID: 1, Enabled: true, Metric: "device_offline", Operator: ">", Severity: "critical", Notify: true}}
	hub := websocket.NewHub()
	go hub.Run()
	r.gen = events.NewGenerator(r.events, hub, rules, testutil.NewMockDispatcher(), nil)
	r.gen.SetClock(r.clock)
	r.det = &offlineDetector{
		devices: r.devices, events: r.gen, observer: r.obs, graceEnd: graceEnd, now: r.clock,
	}
	return r
}

// device adds a device whose last heartbeat is age old.
func (r *offlineRig) device(id string, status models.DeviceStatus, age time.Duration) {
	hb := r.now.Add(-age)
	r.devices.Devices[id] = &models.Device{ID: id, Hostname: id, Status: status, LastHeartbeat: &hb}
}

func (r *offlineRig) status(id string) models.DeviceStatus { return r.devices.Devices[id].Status }

func (r *offlineRig) offlineEvents() []models.Event {
	var out []models.Event
	for _, e := range r.events.Events {
		if e.Type == models.EventDeviceOffline {
			out = append(out, e)
		}
	}
	return out
}

func TestEX_AC035_RestartGraceNoOffline(t *testing.T) {
	r := newOfflineRig(t, offlineBase.Add(2*time.Minute))
	r.device("hawaii", models.DeviceStatusOnline, 10*time.Minute) // silent: the server was down

	r.det.check(context.Background())
	r.now = r.now.Add(119 * time.Second)
	r.det.check(context.Background())

	assert.Equal(t, models.DeviceStatusOnline, r.status("hawaii"), "[AC-035] no device goes offline during the restart grace")
	assert.Empty(t, r.offlineEvents(), "[AC-035] and no offline event is stored")
	assert.Zero(t, r.obs.count(), "[AC-035] the tracker is told nothing: no online-state decision exists yet")
}

func TestEX_AC035_OfflineAfterGrace(t *testing.T) {
	graceEnd := offlineBase.Add(2 * time.Minute)
	r := newOfflineRig(t, graceEnd)
	r.now = graceEnd
	r.device("hawaii", models.DeviceStatusOnline, 10*time.Minute)
	r.device("vermont", models.DeviceStatusOnline, 10*time.Second)

	r.det.check(context.Background())

	assert.Equal(t, models.DeviceStatusOffline, r.status("hawaii"), "[AC-035] at the end of the grace the stale device goes offline")
	assert.Equal(t, models.DeviceStatusOnline, r.status("vermont"), "a fresh heartbeat stays online")
	evts := r.offlineEvents()
	require.Len(t, evts, 1, "[AC-035] one offline event")
	assert.Equal(t, "Device hawaii went offline", evts[0].Message)
	assert.Equal(t, models.EventSeverity("critical"), evts[0].Severity, "the offline rule applies as before")

	r.det.check(context.Background())
	assert.Len(t, r.offlineEvents(), 1, "an already offline device makes no second transition")
	require.Equal(t, 2, r.obs.count(), "the tracker is told after every pass")
	var seen models.DeviceStatus
	for _, d := range r.obs.calls[0] {
		if d.ID == "hawaii" {
			seen = d.Status
		}
	}
	assert.Equal(t, models.DeviceStatusOffline, seen, "with the updated statuses")
}

func TestEX_AC035_OfflineIsDatedByTheLastHeartbeat(t *testing.T) {
	// The transition is dated at heartbeat + 45 s, not at the detection time:
	// the generator receives the heartbeat (checked through the stored event).
	r := newOfflineRig(t, offlineBase)
	r.device("hawaii", models.DeviceStatusOnline, 100*time.Second)
	rec := &recordingEvents{}
	r.det.events = rec

	r.det.check(context.Background())

	require.Len(t, rec.calls, 1)
	assert.Equal(t, "hawaii", rec.calls[0].deviceID)
	require.NotNil(t, rec.calls[0].heartbeat)
	assert.Equal(t, r.now.Add(-100*time.Second), *rec.calls[0].heartbeat, "the last heartbeat is passed on")
}

type recordingEvents struct {
	calls []struct {
		deviceID, host string
		heartbeat      *time.Time
	}
}

func (r *recordingEvents) DeviceOfflineAt(_ context.Context, id, host string, hb *time.Time) {
	r.calls = append(r.calls, struct {
		deviceID, host string
		heartbeat      *time.Time
	}{id, host, hb})
}

func TestEX_AC035_OneShotIsArmedForTheGraceEnd(t *testing.T) {
	graceEnd := offlineBase.Add(2 * time.Minute)
	r := newOfflineRig(t, graceEnd)
	r.device("hawaii", models.DeviceStatusOnline, 10*time.Minute)
	tick := make(chan time.Time)
	oneShot := make(chan time.Time)
	var waited time.Duration
	r.det.tick = tick
	r.det.after = func(d time.Duration) <-chan time.Time { waited = d; return oneShot }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.det.run(ctx) }()

	// The tick during the grace changes nothing.
	tick <- r.now
	oneShot <- r.now // blocks until run receives it
	cancel()
	<-done

	assert.Equal(t, 2*time.Minute, waited, "[AC-035] the one-shot timer is set for exactly the end of the grace")
}

func TestEX_AC035_OneShotRunsTheCheckOnceTheGraceIsOver(t *testing.T) {
	graceEnd := offlineBase.Add(2 * time.Minute)
	r := newOfflineRig(t, graceEnd)
	r.device("hawaii", models.DeviceStatusOnline, 10*time.Minute)
	tick := make(chan time.Time)
	oneShot := make(chan time.Time)
	r.det.tick = tick
	r.det.after = func(time.Duration) <-chan time.Time { return oneShot }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.det.run(ctx) }()

	tick <- r.now // inside the grace: nothing
	r.setNow(graceEnd)
	oneShot <- r.now // the one-shot fires at the end of the grace
	tick <- r.now    // a later tick is a no-op transition-wise; receiving it also proves the one-shot's check finished
	cancel()
	<-done

	assert.Equal(t, models.DeviceStatusOffline, r.status("hawaii"), "[AC-035] the device is reported at the end of the grace")
	assert.Len(t, r.offlineEvents(), 1, "[AC-035] exactly once")
}

func TestEX_AC035_NoGraceMeansNoOneShot(t *testing.T) {
	r := newOfflineRig(t, offlineBase) // grace already over
	called := false
	r.det.tick = make(chan time.Time)
	r.det.after = func(time.Duration) <-chan time.Time { called = true; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.det.run(ctx) }()
	cancel()
	<-done
	assert.False(t, called, "no timer is armed when the grace has ended")
}

func TestEX_SEC004_EveryNonOfflineStatusGoesOffline(t *testing.T) {
	for _, status := range []models.DeviceStatus{models.DeviceStatusOnline, models.DeviceStatusWarning} {
		t.Run(string(status), func(t *testing.T) {
			r := newOfflineRig(t, offlineBase)
			r.device("hawaii", status, 5*time.Minute)

			r.det.check(context.Background())

			assert.Equal(t, models.DeviceStatusOffline, r.status("hawaii"), "[SEC-004] a %s device with a dead heartbeat goes offline", status)
			evts := r.offlineEvents()
			require.Len(t, evts, 1, "[SEC-004] and the uncovered transition alerts")
			assert.Equal(t, models.EventSeverity("critical"), evts[0].Severity)
		})
	}
}

func TestEX_SEC004_OfflineDevicesAndMissingHeartbeatsMakeNoTransition(t *testing.T) {
	r := newOfflineRig(t, offlineBase)
	r.device("already", models.DeviceStatusOffline, time.Hour)
	r.devices.Devices["never"] = &models.Device{ID: "never", Hostname: "never", Status: models.DeviceStatusOnline}

	r.det.check(context.Background())

	assert.Empty(t, r.offlineEvents(), "no transition for an offline device or a device with no heartbeat yet")
	assert.Equal(t, models.DeviceStatusOnline, r.status("never"))
}

func TestEX_SEC004_FailedStatusWriteMakesNoEvent(t *testing.T) {
	r := newOfflineRig(t, offlineBase)
	r.device("hawaii", models.DeviceStatusOnline, 5*time.Minute)
	r.det.devices = failingStatus{r.devices}

	r.det.check(context.Background())

	assert.Empty(t, r.offlineEvents(), "an offline event is stored only after the status changed, so the next pass retries cleanly")
}

type failingStatus struct{ *testutil.MockDeviceRepo }

func (failingStatus) SetStatus(context.Context, string, models.DeviceStatus) error {
	return errors.New("db down")
}

func TestEX_SEC004_DetectorWithoutTrackerStillWorks(t *testing.T) {
	r := newOfflineRig(t, offlineBase)
	r.det.observer = nil
	r.device("hawaii", models.DeviceStatusOnline, 5*time.Minute)
	assert.NotPanics(t, func() { r.det.check(context.Background()) })
	assert.Len(t, r.offlineEvents(), 1)
}

// ---------------------------------------------------------------------------
// Restart series (C-7)
// ---------------------------------------------------------------------------

type seriesRig struct {
	cfg *testutil.MockAdminRepo
	buf *bytes.Buffer
	log *slog.Logger
}

func newSeriesRig() *seriesRig {
	buf := &bytes.Buffer{}
	return &seriesRig{
		cfg: testutil.NewMockAdminRepo(""), buf: buf,
		log: slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
}

func (s *seriesRig) start(at time.Time) time.Time {
	return restartSeriesGrace(context.Background(), s.cfg, at, s.log)
}

func (s *seriesRig) warns(msg string) int { return strings.Count(s.buf.String(), msg) }

func TestEX_SEC007_FirstStartHasTheFullGrace(t *testing.T) {
	s := newSeriesRig()
	assert.Equal(t, offlineBase.Add(2*time.Minute), s.start(offlineBase), "a lone start gets the 2 minute grace")
	assert.Zero(t, s.warns("restart grace shortened"), "and no warning")
}

func TestEX_SEC007_RestartSeriesBoundsGrace(t *testing.T) {
	s := newSeriesRig()
	var graceEnds []time.Time
	for i := 0; i < 5; i++ {
		graceEnds = append(graceEnds, s.start(offlineBase.Add(time.Duration(i)*90*time.Second)))
	}
	seriesCap := offlineBase.Add(models.RestartSeriesGraceCap)
	for i, end := range graceEnds {
		start := offlineBase.Add(time.Duration(i) * 90 * time.Second)
		if start.Before(seriesCap) {
			assert.False(t, end.After(seriesCap), "[SEC-007] start %d: detection stays off at most until 4 minutes after the series began (got %s)", i+1, end.Sub(offlineBase))
		}
	}
	assert.Equal(t, offlineBase.Add(120*time.Second), graceEnds[0])
	assert.Equal(t, offlineBase.Add(210*time.Second), graceEnds[1])
	assert.Equal(t, seriesCap, graceEnds[2], "the third start is cut to the cap")
	for i := 3; i < 5; i++ {
		start := offlineBase.Add(time.Duration(i) * 90 * time.Second)
		assert.Equal(t, start, graceEnds[i], "[SEC-007] start %d: a restart loop gets no grace after the cap", i+1)
	}
}

func TestEX_SEC007_ShortenedGraceWarns(t *testing.T) {
	s := newSeriesRig()
	s.start(offlineBase)
	assert.Zero(t, s.warns("restart grace shortened"))
	s.start(offlineBase.Add(90 * time.Second))
	assert.Equal(t, 1, s.warns("restart grace shortened"), "[SEC-007] a start that continues a series warns once")
	assert.Contains(t, s.buf.String(), "series_start=2026-10-26T10:00:00.000Z", "with the series start")
	assert.Contains(t, s.buf.String(), "previous_start=2026-10-26T10:00:00.000Z", "and the previous start")
	s.start(offlineBase.Add(180 * time.Second))
	assert.Equal(t, 2, s.warns("restart grace shortened"), "one warning per start")
}

func TestEX_SEC007_SeriesResetsAfterGap(t *testing.T) {
	s := newSeriesRig()
	s.start(offlineBase)
	s.start(offlineBase.Add(90 * time.Second))
	later := offlineBase.Add(90*time.Second + models.OfflineDetectionGrace) // exactly 2 minutes after the last start
	assert.Equal(t, later.Add(2*time.Minute), s.start(later), "[SEC-007] a gap of 2 minutes or more begins a new series with the full grace")
	assert.Equal(t, 1, s.warns("restart grace shortened"), "and does not warn")
	assert.Equal(t, later.Add(210*time.Second), s.start(later.Add(90*time.Second)), "the new series is counted from its own first start")
}

func TestEX_SEC007_ConfigErrorMeansNoGrace(t *testing.T) {
	t.Run("read fails", func(t *testing.T) {
		s := newSeriesRig()
		s.cfg.Err = errors.New("db down")
		assert.Equal(t, offlineBase, s.start(offlineBase), "[SEC-007] when in doubt, alert: no grace")
		assert.Contains(t, s.buf.String(), "level=WARN")
	})
	t.Run("write fails", func(t *testing.T) {
		s := newSeriesRig()
		failing := &writeFailure{MockAdminRepo: s.cfg}
		got := restartSeriesGrace(context.Background(), failing, offlineBase, s.log)
		assert.Equal(t, offlineBase, got, "[SEC-007] the series cannot be stored, so there is no grace")
		assert.Contains(t, s.buf.String(), "level=WARN")
	})
	t.Run("unusable value", func(t *testing.T) {
		s := newSeriesRig()
		s.cfg.Config[restartSeriesKey] = "{not json"
		assert.Equal(t, offlineBase, s.start(offlineBase), "[SEC-007] an unusable value gives no grace")
		assert.Equal(t, offlineBase.Add(4*time.Minute), s.start(offlineBase.Add(2*time.Minute)), "and the next start has repaired the row: a new series with the full grace")
	})
}

type writeFailure struct{ *testutil.MockAdminRepo }

func (w *writeFailure) SetConfig(context.Context, string, string) error { return errors.New("db down") }

func TestEX_SEC007_SeriesIsOneBoundedRow(t *testing.T) {
	s := newSeriesRig()
	for i := 0; i < 50; i++ {
		s.start(offlineBase.Add(time.Duration(i) * time.Minute))
	}
	require.Len(t, s.cfg.Config, 1, "[SEC-007] one admin_config row, however many starts")
	assert.LessOrEqual(t, len(s.cfg.Config[restartSeriesKey]), 128, "and at most 128 bytes")
}

func TestEX_SEC007_ClockGoingBackDoesNotStretchTheCap(t *testing.T) {
	s := newSeriesRig()
	s.start(offlineBase.Add(time.Hour)) // a start stamped in the future
	got := s.start(offlineBase)
	assert.False(t, got.After(offlineBase.Add(2*time.Minute)), "a series start in the future is not trusted: the grace is at most 2 minutes, got %s", got.Sub(offlineBase))
}

func (r *offlineRig) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

func (r *offlineRig) setNow(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = t
}
