package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Offline detection (EXPECT-001 AD-006, AD-008, C-4, C-7). A device whose
// heartbeat stopped goes offline; the detector pauses for a grace period after
// each server start, because the agents' heartbeats are not flowing yet.

// offlineDevices is what the detector needs from the device repository.
type offlineDevices interface {
	List(ctx context.Context) ([]models.Device, error)
	SetStatus(ctx context.Context, id string, status models.DeviceStatus) error
}

// offlineEvents records an offline transition. *events.Generator implements it.
type offlineEvents interface {
	DeviceOfflineAt(ctx context.Context, deviceID, hostname string, lastHeartbeat *time.Time)
}

// offlineObserver is told the devices' statuses after each pass
// (*expect.Tracker.ObserveDevices).
type offlineObserver interface {
	ObserveDevices(devices []models.Device, at time.Time)
}

// offlineDetector moves devices with a stale heartbeat to offline.
type offlineDetector struct {
	devices  offlineDevices
	events   offlineEvents
	observer offlineObserver // nil when the tracker is not wired
	// graceEnd is the end of the restart grace: no device goes offline before it.
	graceEnd time.Time
	now      func() time.Time
	// tick and after replace the 30 s ticker and the one-shot timer in tests.
	tick  <-chan time.Time
	after func(time.Duration) <-chan time.Time
}

// offlineCheckInterval is how often the detector looks at the heartbeats.
const offlineCheckInterval = 30 * time.Second

// check runs one detector pass. Before graceEnd it does nothing: an agent that
// has not yet had a chance to heartbeat must not be reported offline because the
// server was down (AC-035). After it, every device that is live by
// models.DeviceIsLive (online or warning, C-4) and whose heartbeat is older than
// models.OfflineHeartbeatThreshold goes offline; a device that is already
// offline makes no transition.
func (d *offlineDetector) check(ctx context.Context) {
	now := d.now()
	if now.Before(d.graceEnd) {
		return
	}
	devices, err := d.devices.List(ctx)
	if err != nil {
		return
	}
	threshold := now.Add(-models.OfflineHeartbeatThreshold) // 3x the 15 s heartbeat interval
	for i := range devices {
		dev := &devices[i]
		if !models.DeviceIsLive(dev.Status) || dev.LastHeartbeat == nil || !dev.LastHeartbeat.Before(threshold) {
			continue
		}
		if err := d.devices.SetStatus(ctx, dev.ID, models.DeviceStatusOffline); err != nil {
			slog.Warn("offline detection: set status", "device_id", dev.ID, "error", err.Error())
			continue
		}
		dev.Status = models.DeviceStatusOffline
		d.events.DeviceOfflineAt(ctx, dev.ID, dev.Hostname, dev.LastHeartbeat)
	}
	if d.observer != nil {
		d.observer.ObserveDevices(devices, now)
	}
}

// run checks every 30 s and once more at exactly graceEnd, so a device that went
// silent during the grace is reported at its end rather than up to 30 s later.
func (d *offlineDetector) run(ctx context.Context) {
	tick := d.tick
	if tick == nil {
		ticker := time.NewTicker(offlineCheckInterval)
		defer ticker.Stop()
		tick = ticker.C
	}
	after := d.after
	if after == nil {
		after = time.After
	}
	var oneShot <-chan time.Time
	if wait := d.graceEnd.Sub(d.now()); wait > 0 {
		oneShot = after(wait)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			d.check(ctx)
		case <-oneShot:
			oneShot = nil
			d.check(ctx)
		}
	}
}

// restartSeriesKey is the admin_config row of the restart series (C-7). One row,
// at most 128 bytes, overwritten at each start: the bound on its growth.
const restartSeriesKey = "expect_restart_series"

// restartSeriesConfig is where the series is kept (db.AdminRepository).
type restartSeriesConfig interface {
	GetConfigMap(ctx context.Context, keys []string) (map[string]string, error)
	SetConfig(ctx context.Context, key, value string) error
}

type restartSeries struct {
	SeriesStart time.Time `json:"series_start"`
	LastStart   time.Time `json:"last_start"`
}

// restartSeriesGrace returns the end of the offline-detection grace of the start
// at now (EXPECT-001 AD-008, C-7). The grace is models.OfflineDetectionGrace
// (2 min) after each start. A start less than that after the previous one
// continues a series of rapid restarts, and the series' grace never extends past
// models.RestartSeriesGraceCap (4 min) after the series' first start: a restart
// loop cannot keep detection off indefinitely. A gap of 2 min or more begins a
// new series.
//
// When the series cannot be read or written, or the stored value is unusable,
// there is no grace (the result is now): when in doubt, alert.
func restartSeriesGrace(ctx context.Context, cfg restartSeriesConfig, now time.Time, log *slog.Logger) time.Time {
	now = now.UTC()
	seriesStart, prevStart := now, time.Time{}
	continues := false
	ok := true

	stored, err := cfg.GetConfigMap(ctx, []string{restartSeriesKey})
	switch {
	case err != nil:
		log.Warn("restart grace unavailable: cannot read the restart series", "error", err.Error())
		ok = false
	case stored[restartSeriesKey] != "":
		var prev restartSeries
		if err := json.Unmarshal([]byte(stored[restartSeriesKey]), &prev); err != nil || prev.LastStart.IsZero() || prev.SeriesStart.IsZero() {
			log.Warn("restart grace unavailable: the restart series value is unusable")
			ok = false
		} else if now.Sub(prev.LastStart) < models.OfflineDetectionGrace {
			continues, prevStart = true, prev.LastStart.UTC()
			// A series start in the future (the clock went back) must not stretch the cap.
			if prev.SeriesStart.Before(now) {
				seriesStart = prev.SeriesStart.UTC()
			}
		}
	}

	next, err := json.Marshal(restartSeries{SeriesStart: seriesStart, LastStart: now})
	if err == nil {
		err = cfg.SetConfig(ctx, restartSeriesKey, string(next))
	}
	if err != nil {
		log.Warn("restart grace unavailable: cannot store the restart series", "error", err.Error())
		ok = false
	}
	if !ok {
		return now
	}

	graceEnd := now.Add(models.OfflineDetectionGrace)
	if capEnd := seriesStart.Add(models.RestartSeriesGraceCap); capEnd.Before(graceEnd) {
		graceEnd = capEnd
	}
	if graceEnd.Before(now) {
		graceEnd = now
	}
	if continues {
		log.Warn("restart grace shortened",
			"series_start", seriesStart, "previous_start", prevStart, "grace", graceEnd.Sub(now))
	}
	return graceEnd
}
