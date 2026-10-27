package expect

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-008: the expected-activity window of an installer reinstall record
// (ADD AD-006 to AD-008, AD-010). A record is a commands row with origin
// installer; this file holds its coverage function and the routine that ends its
// window. The rest of the Tracker (hooks, reconcile, coverage lookups, activity)
// is shared with the commands and the self-update records, and the end-of-window
// routine reuses the EXPECT-002 detector gate, stuck-record handling and
// exactly-once event recorder.
//
// Bounds (AD-012): at most one reinstall window per device (one cache entry,
// the trimmed row); there is no per-notice state; a record writes at most three
// marker rows (device, outcome, stuck), each exactly once; the closed window ID
// stays in closedRecent for 5 minutes only.

// ReinstallResolver ends a record at its bound (AD-008 step 2). *commands.Service
// implements it. All of its writes are ActorReinstall transitions.
type ReinstallResolver interface {
	ResolveReinstall(ctx context.Context, commandID, deviceID string, outcome models.ReinstallOutcome, message string, at time.Time) (*models.Command, bool, error)
}

var (
	errNoReinstallResolver = errors.New("no reinstall resolver is wired")
	errReinstallOpen       = errors.New("reinstall record is still not resolved")
)

// SetReinstallResolver wires the resolver. Call it before Load and Run; it is
// never changed afterwards. Never pass a typed nil (a nil *commands.Service).
func (t *Tracker) SetReinstallResolver(r ReinstallResolver) { t.reinstallResolver = r }

// evaluateReinstall is the pure coverage function of a reinstall window (AD-006).
//
//	D = dispatched_at (the receive time of the notice), hard = min(deadline_at,
//	D+10m), F = finished_at.
//
// Coverage ends at the registration that closed the record (F) or at the bound,
// whichever is first. It is strict at its end and never reaches the bound or
// beyond (FR-015, NFR-004). There is no settle period: an outage after the close
// alerts at once (FR-016).
func evaluateReinstall(w *window, now time.Time) timeline {
	c := &w.cmd
	state := c.State()
	d := *c.DispatchedAt
	hard := d.Add(models.ExpectReinstallBound)
	if c.DeadlineAt != nil && c.DeadlineAt.Before(hard) {
		hard = *c.DeadlineAt
	}
	tl := timeline{opened: true, d: d, hard: hard}

	var end time.Time
	switch {
	case !state.IsTerminal():
		tl.phase = models.PhaseInProgress
		tl.offlineEnd = hard
		end, tl.reason = hard, models.EndHardBound
		b := hard
		tl.backBy = &b
	case state == models.CommandSucceeded:
		f := finishedAt(c)
		tl.phase = models.PhaseClosed
		tl.offlineEnd = earlierOf(f, hard)
		end, tl.reason = f, models.EndSettled
	default: // timed_out (the CHECK allows no other terminal state)
		f := finishedAt(c)
		tl.phase = models.PhaseClosed
		tl.offlineEnd = earlierOf(f, hard)
		end, tl.reason = f, models.EndNonSuccessSettle
	}

	tl.closeAt = end
	tl.due = !now.Before(end)
	if tl.due && now.Sub(end) >= models.ExpectDeferredRecheckBound {
		tl.reason = models.EndLapsed
	}
	return tl
}

// reinstallResolution decides how a record that is still open at its bound ends
// (AD-008 step 2): a device that registered would have closed the record, so a
// still-running record means the agent did not restart.
func reinstallResolution(dv devSnap) (models.ReinstallOutcome, string) {
	host := hostnameOf(dv)
	if !dv.online {
		return models.ReinstallUnconfirmedOffline, models.ReinstallUnconfirmedOfflineMessage(host)
	}
	return models.ReinstallUnconfirmedOnline, models.ReinstallNotConfirmedMessage(host)
}

// finishReinstall is finishWindow for a reinstall record (AD-008). It keeps the
// EXPECT-001 contract: it is the only exit, every step is idempotent, a failing
// step leaves the entry for the next tick, and nothing runs before the restart
// grace is over. Order:
//
//  0. the detector gate (bound processing only);
//  1. the device backstop: one escalation when the suppressed offline is still
//     ongoing (FR-021), before anything that can fail;
//  2. resolution of a record that is still open (ActorReinstall only);
//  3. the one outcome event of a not-confirmed-online record (FR-022);
//  4. the guarded close and the drop from the cache.
//
// A record that cannot be resolved is made visible one hour after its bound
// (NFR-004): one marker-guarded warning event, no push.
func (t *Tracker) finishReinstall(ctx context.Context, devID, winID string, w window, dv devSnap, reason models.WindowEndReason) bool {
	now := t.now()
	tl := evaluateReinstall(&w, now)
	active := w.cmd.State().IsActive()
	atBound := reason == models.EndHardBound || reason == models.EndLapsed

	// 0. Detector gate.
	if (atBound || active) && !t.detectorGateOpen(now) {
		return false
	}

	// 1. Device backstop. Runs before resolution, so a failing resolution never
	// delays the page.
	escalated := false
	if t.graceOver(now) && !dv.online && !dv.offlineSince.IsZero() && tl.coversAt(tl.offlineEnd, dv.offlineSince) {
		inserted, err := t.escalateDevice(ctx, w, dv, models.EscDeviceNotBack)
		if err != nil {
			return t.reinstallFailed(ctx, devID, winID, &w, dv, tl, err)
		}
		escalated = inserted
	}

	// 2. Resolution at the bound.
	if active {
		if err := t.resolveReinstall(ctx, &w, dv); err != nil {
			return t.reinstallFailed(ctx, devID, winID, &w, dv, tl, err)
		}
	}

	// 3. The outcome event.
	if err := t.reinstallOutcomeEvent(ctx, &w, dv); err != nil {
		return t.reinstallFailed(ctx, devID, winID, &w, dv, tl, err)
	}

	// 4. Close.
	outcome := models.WindowOutcomeClosed
	switch reason {
	case models.EndLapsed:
		outcome = models.WindowOutcomeLapsed
	case models.EndNotOpened:
		outcome = models.WindowOutcomeNotOpened
	}
	if escalated && outcome == models.WindowOutcomeClosed {
		outcome = models.WindowOutcomeEscalated
	}
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	_, err := t.opt.Repo.CloseWindow(cctx, winID, now, outcome)
	cancel()
	if err != nil {
		return t.reinstallFailed(ctx, devID, winID, &w, dv, tl, err)
	}
	t.dropWindow(devID, winID, true)
	return true
}

// reinstallFailed records a failing step. One hour after the record's bound it
// also makes the stuck record visible (NFR-004); the entry keeps retrying.
func (t *Tracker) reinstallFailed(ctx context.Context, devID, winID string, w *window, dv devSnap, tl timeline, err error) bool {
	if !tl.hard.IsZero() && !t.now().Before(tl.hard.Add(models.ReinstallStuckAfter)) {
		t.writeReinstallStuck(ctx, w, dv)
	}
	t.noteFailure(devID, winID, err)
	return false
}

// resolveReinstall ends a record that is still open. A transition that does not
// apply (another writer got there first) is followed by a re-read: the step
// succeeds only when the row is no longer open.
func (t *Tracker) resolveReinstall(ctx context.Context, w *window, dv devSnap) error {
	if t.reinstallResolver == nil {
		return errNoReinstallResolver
	}
	outcome, msg := reinstallResolution(dv)
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	_, applied, err := t.reinstallResolver.ResolveReinstall(cctx, w.cmd.ID, w.cmd.DeviceID, outcome, msg, t.now())
	cancel()
	if err != nil {
		return fmt.Errorf("resolve reinstall record: %w", err)
	}
	if applied {
		return nil
	}
	if t.opt.Commands == nil {
		return errNoCommandReader
	}
	cctx, cancel = context.WithTimeout(ctx, dbTimeout)
	cur, err := t.opt.Commands.GetByID(cctx, w.cmd.ID)
	cancel()
	if err != nil {
		return fmt.Errorf("re-read reinstall record: %w", err)
	}
	if cur.State().IsActive() {
		return errReinstallOpen
	}
	return nil
}

// reinstallOutcomeEvent writes the one outcome event of a record (FR-022):
// agent_reinstall_unconfirmed for a record not confirmed while the device was
// online. Nothing else produces an event of its own: a not-back device
// escalates, a success is silent. Exactly once per record through the marker,
// and broadcast only (no rule, no push, no ceiling).
func (t *Tracker) reinstallOutcomeEvent(ctx context.Context, w *window, dv devSnap) error {
	if t.opt.Commands == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	row, err := t.opt.Commands.GetByID(cctx, w.cmd.ID)
	cancel()
	if err != nil {
		return fmt.Errorf("read reinstall record: %w", err)
	}
	if row.FailureStep != models.ReinstallFailureUnconfirmedOnline || !row.State().IsTerminal() {
		return nil
	}
	msg := row.ResultMsg
	if msg == "" {
		msg = models.ReinstallNotConfirmedMessage(hostnameOf(dv))
	}
	_, err = t.recordSelfUpdateEvent(ctx, w, dv, models.EscalationTargetOutcome, models.Event{
		DeviceID: w.cmd.DeviceID, Type: models.EventAgentReinstallUnconfirmed, Severity: models.SeverityWarning, Message: msg,
		CommandID: w.cmd.ID, CommandAttr: models.CommandAttrOutcome, CreatedAt: t.now(),
	})
	return err
}

// writeReinstallStuck makes a record that could not be completed visible, one
// hour after its bound (NFR-004): exactly one agent_reinstall_paused warning
// event per record (marker "stuck"), no push, and one Warn. The next accepted
// notice on the device retires the row, so it never blocks new records.
func (t *Tracker) writeReinstallStuck(ctx context.Context, w *window, dv devSnap) {
	msg := models.ReinstallStuckMessage(hostnameOf(dv), models.ShortCommandID(w.cmd.ID))
	inserted, err := t.recordSelfUpdateEvent(ctx, w, dv, models.EscalationTargetStuck, models.Event{
		DeviceID: w.cmd.DeviceID, Type: models.EventAgentReinstallPaused, Severity: models.SeverityWarning, Message: msg,
		CommandID: w.cmd.ID, CommandAttr: models.CommandAttrOutcome, CreatedAt: t.now(),
	})
	if err != nil {
		t.warnThrottled("reinstall_stuck_event", "agent reinstall stuck-record event could not be stored; it is retried",
			"device_id", w.cmd.DeviceID, "command_id", w.cmd.ID, "error", err.Error())
		return
	}
	if inserted {
		t.warnThrottled("reinstall_stuck", "agent reinstall record could not be completed one hour after its bound; tracking is paused for the device until it is replaced",
			"device_id", w.cmd.DeviceID, "command_id", w.cmd.ID)
	}
}
