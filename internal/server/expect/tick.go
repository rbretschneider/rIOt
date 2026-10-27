package expect

import (
	"context"
	"sort"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type planKind int

// Plan kinds in processing order: eager escalations and powered-off entries
// first, then due windows by close time, then deferred container re-checks.
const (
	planEager planKind = iota
	planPoweredOff
	planDue
	planRecheck
)

type planItem struct {
	kind    planKind
	devID   string
	winID   string
	reason  models.WindowEndReason
	closeAt time.Time
}

// Tick is one pass of the evaluation loop (AD-009). It ends every window that
// is due through finishWindow, raises FR-020 as soon as a host or agent command
// failed with its device offline, runs the deferred container re-checks whose
// device has returned, flushes held push summaries and broadcasts activity
// changes. No new database call starts after tickBudget of tick time; what is
// left carries over. Before the end of the restart grace it makes no online-state
// decision: it only broadcasts activity and tidies memory.
//
// Tick never publishes: pushes and broadcasts go through the bounded publisher,
// so a stuck notification channel cannot stall it (C-3).
func (t *Tracker) Tick(ctx context.Context) {
	t.tickMu.Lock()
	defer t.tickMu.Unlock()
	start := t.now()
	withinBudget := func() bool { return t.now().Sub(start) < tickBudget }

	t.pub.flushHeld()
	if t.graceOver(start) {
		for _, it := range t.plan(start) {
			if !withinBudget() {
				break
			}
			switch it.kind {
			case planEager:
				t.runEager(ctx, it)
			case planPoweredOff:
				t.runPoweredOff(ctx, it)
			case planDue:
				t.finishWindow(ctx, it.devID, it.winID, it.reason, false)
			case planRecheck:
				t.finishWindow(ctx, it.devID, it.winID, it.reason, true)
			}
		}
	}
	t.housekeep(start)
	t.broadcastActivity()

	t.mu.Lock()
	t.lastTick = t.now()
	t.mu.Unlock()
}

// plan lists what this tick has to do, in processing order.
func (t *Tracker) plan(now time.Time) []planItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	var items []planItem
	for devID, dev := range t.devs {
		dv := dev.view()
		snap := dev.snap()
		for id, w := range dev.windows {
			tl := evaluate(w, dv, now, t.graceEnd)
			if w.deferred {
				switch {
				case now.Sub(w.deferredBase) >= models.ExpectDeferredRecheckBound:
					items = append(items, planItem{kind: planDue, devID: devID, winID: id, reason: models.EndLapsed, closeAt: w.deferredBase})
				case dev.online && now.Sub(dev.onlineSince) >= models.ExpectHostSettle:
					items = append(items, planItem{kind: planRecheck, devID: devID, winID: id, reason: models.EndSettled, closeAt: w.deferredBase})
				}
				continue
			}
			if tl.opened && !w.noPoweredOff && w.policy.Class == models.WindowShutdown && w.cmd.State() == models.CommandSucceeded &&
				!dev.online && !dev.offlineSince.IsZero() && !dev.offlineSince.Before(tl.d) && dev.offlineSince.Before(tl.closeAt) {
				// The device went offline inside the shutdown's return phase while
				// the server was not looking (restart): it is powered off (C-1 a).
				items = append(items, planItem{kind: planPoweredOff, devID: devID, winID: id, closeAt: tl.closeAt})
				continue
			}
			if tl.due {
				items = append(items, planItem{kind: planDue, devID: devID, winID: id, reason: tl.reason, closeAt: tl.closeAt})
				continue
			}
			// A self-update or reinstall record is never escalated eagerly: its failed
			// and timed-out rows are due at F (EXPECT-002 AD-006, EXPECT-008 AD-008).
			if s := w.cmd.State(); (s == models.CommandFailed || s == models.CommandTimedOut) && !w.deviceEscDone &&
				w.policy.CoversOffline && !w.policy.SelfUpdate && !w.policy.Reinstall && !dev.online && offlineWasCovered(w, snap) {
				items = append(items, planItem{kind: planEager, devID: devID, winID: id, closeAt: tl.closeAt})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].kind != items[j].kind {
			return items[i].kind < items[j].kind
		}
		if !items[i].closeAt.Equal(items[j].closeAt) {
			return items[i].closeAt.Before(items[j].closeAt)
		}
		return items[i].winID < items[j].winID
	})
	return items
}

// runEager raises FR-020 for a failed or timed-out command whose device is
// offline, without waiting for the window to end.
func (t *Tracker) runEager(ctx context.Context, it planItem) {
	w, dv, ok := t.snapshot(it.devID, it.winID)
	if !ok {
		return
	}
	kind, need := deviceKind(&w)
	if !need {
		return
	}
	if _, err := t.escalateDevice(ctx, w, dv, kind); err != nil {
		t.warnThrottled("eager_escalation", "expected-activity escalation failed; it is retried", "device_id", it.devID, "error", err.Error())
		return
	}
	t.update(it.devID, it.winID, func(w *window) { w.deviceEscDone = true })
}

// runPoweredOff enters the powered-off state from the loop (the device went
// offline in a succeeded shutdown's return phase before this process looked).
func (t *Tracker) runPoweredOff(ctx context.Context, it planItem) {
	w, dv, ok := t.snapshot(it.devID, it.winID)
	if !ok {
		return
	}
	state := models.PoweredOffState{
		DeviceID: it.devID, CommandID: w.cmd.ID, ActionLabel: models.ExpectActionLabel(w.cmd.Action),
		ActorPhrase: t.actorPhrase(&w), ShutdownAt: finishedAt(&w.cmd), OfflineAt: dv.offlineSince, CreatedAt: dv.offlineSince,
	}
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	stored, err := t.opt.Repo.UpsertPoweredOff(cctx, &state)
	cancel()
	if err != nil {
		t.warnThrottled("upsert_powered_off", "powered-off state could not be stored; it is retried", "device_id", it.devID, "error", err.Error())
		return
	}
	if stored {
		t.mu.Lock()
		if dev := t.devs[it.devID]; dev != nil {
			dev.poweredOff = &state
		}
		t.mu.Unlock()
	}
	t.finishWindow(ctx, it.devID, it.winID, models.EndPoweredOff, false)
}

// housekeep releases memory: closed-window ids after closedRecentTTL (NFR-004)
// and escalated labels after ExpectEscalationDisplayTTL (FR-037).
func (t *Tracker) housekeep(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, at := range t.closedRecent {
		if now.Sub(at) >= closedRecentTTL {
			delete(t.closedRecent, id)
		}
	}
	for _, dev := range t.devs {
		if dev.escalation != nil && now.Sub(dev.escalation.at) >= models.ExpectEscalationDisplayTTL {
			dev.escalation = nil
		}
		if dev.endedPO != nil && now.Sub(dev.endedPOAt) >= endedPOTTL {
			dev.endedPO = nil
		}
	}
}
