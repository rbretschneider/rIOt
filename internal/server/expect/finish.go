package expect

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// snapshot copies a window and its device state under the lock so the finish
// logic can run without it. ok is false when the window is gone.
func (t *Tracker) snapshot(devID, winID string) (window, devSnap, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	dev := t.devs[devID]
	if dev == nil {
		return window{}, devSnap{}, false
	}
	w := dev.windows[winID]
	if w == nil {
		return window{}, devSnap{}, false
	}
	return *w, dev.snap(), true
}

// update runs f on the cached window under the lock (processing-state flags).
func (t *Tracker) update(devID, winID string, f func(w *window)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if dev := t.devs[devID]; dev != nil {
		if w := dev.windows[winID]; w != nil {
			f(w)
		}
	}
}

// dropWindow removes a window from the cache. closed marks it as closed by this
// process, so a late command hook or a lagging reconcile cannot bring it back
// (the id is forgotten after closedRecentTTL: NFR-004).
func (t *Tracker) dropWindow(devID, winID string, closed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if dev := t.devs[devID]; dev != nil {
		delete(dev.windows, winID)
	}
	if closed {
		t.closedRecent[winID] = t.now()
	}
}

// noteFailure records that finishing a window failed. The window stays and the
// next tick retries; after evictAfter without success it leaves the cache only
// (the row stays pending in the database, so it is reloaded and retried: the
// backstop is never skipped, AD-009).
func (t *Tracker) noteFailure(devID, winID string, err error) {
	now := t.now()
	evict := false
	t.mu.Lock()
	if dev := t.devs[devID]; dev != nil {
		if w := dev.windows[winID]; w != nil {
			if w.failingSince.IsZero() {
				w.failingSince = now
			}
			if now.Sub(w.failingSince) >= evictAfter {
				delete(dev.windows, winID)
				evict = true
			}
		}
	}
	t.mu.Unlock()
	t.warnThrottled("finish_window", "expected-activity window could not be finished; it is retried",
		"device_id", devID, "command_id", winID, "error", err.Error())
	if evict {
		t.warnThrottled("evict_window", "expected-activity window evicted from the cache after repeated failures; it stays pending and is reloaded",
			"device_id", devID, "command_id", winID)
	}
}

// deferWindow parks a window whose container check cannot run because its device
// is offline (AD-011): it stops covering anything, its row stays pending, and the
// check runs once the device has been back for ExpectHostSettle, or lapses 24 h
// after base.
func (t *Tracker) deferWindow(devID, winID string, base time.Time) {
	t.update(devID, winID, func(w *window) {
		if !w.deferred {
			w.deferred = true
			w.deferredBase = base
		}
	})
}

// escalationSince is the "<p> after" of a container escalation text: how long
// the container had to come back.
func escalationSince(w *window, reason models.WindowEndReason, resumed bool) time.Duration {
	if resumed {
		return models.ExpectHostSettle
	}
	switch reason {
	case models.EndSettled:
		return w.policy.Settle
	case models.EndNonSuccessSettle:
		return models.ExpectNonSuccessSettle
	}
	return w.policy.PostTerminal
}

// offlineWasCovered reports whether the device's current offline period started
// while the window covered offline: that is the offline the window suppressed.
func offlineWasCovered(w *window, dv devSnap) bool {
	if dv.offlineSince.IsZero() || w.cmd.DispatchedAt == nil || dv.offlineSince.Before(*w.cmd.DispatchedAt) {
		return false
	}
	if s := w.cmd.State(); s == models.CommandFailed || s == models.CommandTimedOut {
		return dv.offlineSince.Before(finishedAt(&w.cmd))
	}
	return true
}

// finishWindow is the one routine through which a window leaves processing
// (AD-009). Whatever the reason, it runs the backstop first:
//
//  1. FR-020 / FR-021 for a host, agent or shutdown window whose device is
//     offline and whose offline the window suppressed;
//  2. the FR-022 container check, or its deferral to the device's return;
//  3. CloseWindow, guarded by window_closed_at IS NULL;
//  4. dropping the window from the cache.
//
// A failing step leaves the window in place for the next tick; markers and the
// guarded close make every retry, crash and restart harmless (FR-024). resumed
// is true for a deferred container check whose device has returned. It returns
// true when the window left the cache.
func (t *Tracker) finishWindow(ctx context.Context, devID, winID string, reason models.WindowEndReason, resumed bool) bool {
	w, dv, ok := t.snapshot(devID, winID)
	if !ok {
		return true
	}
	now := t.now()
	actor := t.actorPhrase(&w)
	host := hostnameOf(dv)

	if reason == models.EndDeleted {
		// The command row is gone: its escalation markers went with it, so no
		// escalation can be recorded. The loss is made visible instead (C-2 c).
		t.dropWindow(devID, winID, false)
		t.warnDeviceExists(ctx, devID, deletedMessage(&w, actor, host))
		return true
	}

	if w.policy.Reinstall {
		// An installer reinstall record has its own end-of-window routine (EXPECT-008
		// AD-008): the same contract, with resolution at the bound.
		return t.finishReinstall(ctx, devID, winID, w, dv, reason)
	}

	if w.policy.SelfUpdate {
		// An agent self-update record has its own end-of-window routine (EXPECT-002
		// AD-006): the same contract, with resolution at the bound.
		return t.finishSelfUpdate(ctx, devID, winID, w, dv, reason)
	}

	outcome := models.WindowOutcomeClosed
	switch reason {
	case models.EndPoweredOff:
		outcome = models.WindowOutcomePoweredOff
	case models.EndNotOpened:
		outcome = models.WindowOutcomeNotOpened
	case models.EndLapsed:
		outcome = models.WindowOutcomeLapsed
	}
	escalated := false
	skippedMsg := ""
	tl := evaluate(&w, deviceView{online: dv.online, onlineSince: dv.onlineSince, returnedAt: dv.returnedAt}, now, t.graceEnd)

	// 1. Device backstop (FR-020, FR-021).
	deviceReason := reason != models.EndPoweredOff && reason != models.EndNotOpened
	if !resumed && deviceReason && w.policy.CoversOffline && t.graceOver(now) && !dv.online && offlineWasCovered(&w, dv) {
		if kind, need := deviceKind(&w); need {
			inserted, err := t.escalateDevice(ctx, w, dv, kind)
			if err != nil {
				t.noteFailure(devID, winID, err)
				return false
			}
			escalated = escalated || inserted
		}
	}

	// 2. Container backstop (FR-022).
	if w.policy.CheckContainers && deviceReason {
		cctx, cancel := context.WithTimeout(ctx, dbTimeout)
		deaths, err := t.opt.Repo.SuppressedDeaths(cctx, devID, winID, 1000)
		cancel()
		if err != nil {
			t.noteFailure(devID, winID, err)
			return false
		}
		if len(deaths) > 0 {
			runNow := dv.online && (resumed || reason == models.EndLapsed || !returnedAfterClose(dv, tl, now))
			switch {
			case runNow:
				cctx, cancel := context.WithTimeout(ctx, dbTimeout)
				n, err := t.containerCheck(cctx, w, dv, deaths, escalationSince(&w, reason, resumed))
				cancel()
				if err != nil {
					t.noteFailure(devID, winID, err)
					return false
				}
				escalated = escalated || n > 0
			case reason == models.EndLapsed:
				skippedMsg = lapsedMessage(&w, actor, host)
			default:
				base := tl.closeAt
				if base.IsZero() {
					base = now
				}
				t.deferWindow(devID, winID, base)
				return false
			}
		}
	}

	// 3. Close.
	if escalated && outcome == models.WindowOutcomeClosed {
		outcome = models.WindowOutcomeEscalated
	}
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	applied, err := t.opt.Repo.CloseWindow(cctx, winID, now, outcome)
	cancel()
	if err != nil {
		t.noteFailure(devID, winID, err)
		return false
	}

	// 4. Drop.
	t.dropWindow(devID, winID, true)
	if skippedMsg != "" && applied {
		t.writeWarning(ctx, devID, skippedMsg)
	}
	return true
}

// returnedAfterClose reports whether the device came back after the window's
// close time and has not been back for the settle period yet: the containers
// have not had time to start, so the check waits (deferred re-check, AD-011).
// A device that was online throughout is checked right away.
func returnedAfterClose(dv devSnap, tl timeline, now time.Time) bool {
	if tl.closeAt.IsZero() || !dv.onlineSince.After(tl.closeAt) {
		return false
	}
	return now.Sub(dv.onlineSince) < models.ExpectHostSettle
}

// warnDeviceExists stores a warning event for a device that still exists.
func (t *Tracker) warnDeviceExists(ctx context.Context, devID, message string) {
	t.mu.Lock()
	dev := t.devs[devID]
	known := dev != nil && dev.known
	t.mu.Unlock()
	if !known {
		return
	}
	t.writeWarning(ctx, devID, message)
}
