package expect

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-002: the expected-activity window of an agent self-update record
// (ADD AD-005, AD-006, AD-009). A record is a commands row with origin
// agent_self_update; this file holds its coverage function, the routine that
// ends its window and the pieces that routine needs. The rest of the Tracker
// (hooks, reconcile, coverage lookups, activity) is shared with the commands.
//
// Bounds (AD-013): at most one self-update window per device (one cache entry
// through running and verifying); the entry holds the trimmed row and two
// strings of at most 32 bytes; there is no per-event state; a record writes at
// most three marker rows (device, outcome, stuck), each exactly once.

// SelfUpdateResolver ends a record at its bound (AD-006 step 2).
// *commands.Service implements it. All of its writes are ActorSelfUpdate
// transitions (C-2).
type SelfUpdateResolver interface {
	ResolveSelfUpdate(ctx context.Context, commandID, deviceID string, outcome models.SelfUpdateOutcome, message string, at time.Time) (*models.Command, bool, error)
}

var (
	errNoSelfUpdateResolver = errors.New("no self-update resolver is wired")
	errSelfUpdateOpen       = errors.New("self-update record is still not resolved")
	errNoCommandReader      = errors.New("self-update record state cannot be read: no command reader is wired")
)

// selfUpdateGateCap is how long past graceEnd the detector gate waits for the
// offline detector's pass at graceEnd (AD-006, INFO-4).
const selfUpdateGateCap = 30 * time.Second

// SetSelfUpdateResolver wires the resolver. Call it before Load and Run; it is
// never changed afterwards. Never pass a typed nil (a nil *commands.Service).
func (t *Tracker) SetSelfUpdateResolver(r SelfUpdateResolver) { t.selfResolver = r }

// windowActionLabel is the action label of a window in messages: "Agent
// reinstall" for an installer record (EXPECT-008), "Agent update <from> -> <to>"
// for a self-update record with valid versions, the plain action label otherwise.
func windowActionLabel(w *window) string {
	if w.policy.Reinstall {
		return models.ReinstallActionLabel
	}
	if w.policy.SelfUpdate && w.selfFrom != "" && w.selfTo != "" {
		return models.SelfUpdateActionLabel(w.selfFrom, w.selfTo)
	}
	return models.ExpectActionLabel(w.cmd.Action)
}

// selfUpdateVersionsOf reads the validated versions of a row before it is
// trimmed (implementation note 6). Both are "" when the row is not a
// self-update record or its params are not valid versions.
func selfUpdateVersionsOf(c models.Command) (from, to string) {
	if c.Origin != models.OriginAgentSelfUpdate {
		return "", ""
	}
	from, to, _ = models.SelfUpdateVersions(c)
	return from, to
}

func verifyingAtOf(c *models.Command) time.Time {
	if c.VerifyingAt != nil {
		return *c.VerifyingAt
	}
	return c.UpdatedAt
}

// evaluateSelf is the pure coverage function of a self-update window (AD-005).
//
//	D = dispatched_at (the receive time of the start), hard = min(deadline_at,
//	D+10m), V = verifying_at, F = finished_at, R = the device's last observed
//	return.
//
// Coverage ends at the registration that reports <to> (V), at an attributed
// failure or a registration on another version (F), and otherwise at the bound.
// A completion that arrives before any return keeps coverage until the first
// return after it, capped at the bound (X-1). Coverage is strict at its end and
// never reaches the bound or beyond (FR-012, NFR-002).
func evaluateSelf(w *window, dv deviceView, now time.Time) timeline {
	c := &w.cmd
	state := c.State()
	d := *c.DispatchedAt
	hard := d.Add(models.ExpectSelfUpdateBound)
	if c.DeadlineAt != nil && c.DeadlineAt.Before(hard) {
		hard = *c.DeadlineAt
	}
	tl := timeline{opened: true, d: d, hard: hard}

	var end time.Time
	switch {
	case state == models.CommandVerifying:
		// Returned and confirming: closed for coverage, resolved at the bound.
		tl.phase = models.PhaseClosed
		tl.offlineEnd = earlierOf(verifyingAtOf(c), hard)
		end, tl.reason = hard, models.EndHardBound

	case !state.IsTerminal():
		tl.phase = models.PhaseInProgress
		tl.offlineEnd = hard
		end, tl.reason = hard, models.EndHardBound
		b := hard
		tl.backBy = &b

	case state == models.CommandSucceeded:
		f := finishedAt(c)
		switch {
		case c.VerifyingAt != nil:
			// The return was seen before the completion (the signed flow).
			tl.phase = models.PhaseClosed
			tl.offlineEnd = earlierOf(*c.VerifyingAt, hard)
			end, tl.reason = f, models.EndSettled
		case dv.returnedAt.After(f):
			// An early completion, and the return after it.
			e := earlierOf(dv.returnedAt, hard)
			tl.phase = models.PhaseClosed
			tl.offlineEnd = e
			end, tl.reason = e, models.EndSettled
		default:
			// A completion before any return (the installed path, older agents): the
			// restart that follows is still covered, up to the bound (X-1).
			tl.phase = models.PhaseInProgress
			tl.offlineEnd = hard
			end, tl.reason = hard, models.EndHardBound
			b := hard
			tl.backBy = &b
		}

	default: // failed, timed_out
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

// detectorGateOpen reports whether bound processing may run (AD-006, INFO-4):
// after the restart grace, once the offline detector has reported its first pass
// at or after graceEnd, or graceEnd + 30 s at the latest. A device that died
// while the server was down is then known to be offline before its record is
// resolved, so it escalates instead of being labelled "not confirmed while
// online".
func (t *Tracker) detectorGateOpen(now time.Time) bool {
	t.mu.Lock()
	seen := t.observedAfterGrace
	t.mu.Unlock()
	return seen || !now.Before(t.graceEnd.Add(selfUpdateGateCap))
}

// selfDeviceKind picks the escalation kind of a self-update window that ends
// with its suppressed offline still ongoing (FR-020, X-7): the failed variant
// for a failed record, the not-back text for every other state.
func selfDeviceKind(w *window) models.EscalationKind {
	if w.cmd.State() == models.CommandFailed {
		return models.EscDeviceNonSuccess
	}
	return models.EscDeviceNotBack
}

// selfEscalationMessage renders the FR-020 / X-7 text of a self-update window.
func selfEscalationMessage(kind models.EscalationKind, host string, w *window) string {
	id8 := models.ShortCommandID(w.cmd.ID)
	if kind == models.EscDeviceNonSuccess {
		return models.SelfUpdateOfflineAfterFailureMessage(host, w.selfFrom, w.selfTo, id8)
	}
	return models.SelfUpdateNotBackMessage(host, w.selfFrom, w.selfTo, id8)
}

func selfEscalationReason(kind models.EscalationKind) string {
	if kind == models.EscDeviceNonSuccess {
		return models.EscalationReasonFailed
	}
	return models.EscalationReasonNotBack
}

// selfUpdateResolution decides how a record that is still open at its bound
// ends (AD-006 step 2).
func selfUpdateResolution(w *window, dv devSnap) (models.SelfUpdateOutcome, string) {
	host := hostnameOf(dv)
	if w.cmd.State() == models.CommandVerifying {
		return models.SelfUpdateConfirmed, models.SelfUpdateConfirmedMessage("registered on", w.selfTo)
	}
	if !dv.online {
		return models.SelfUpdateUnconfirmedOffline, models.SelfUpdateUnconfirmedOfflineMessage(host)
	}
	if models.ValidAgentVersion(w.selfTo) && models.ValidAgentVersion(dv.agentVersion) && models.AgentVersionsMatch(dv.agentVersion, w.selfTo) {
		return models.SelfUpdateConfirmed, models.SelfUpdateConfirmedMessage("reports", w.selfTo)
	}
	return models.SelfUpdateUnconfirmedOnline, models.SelfUpdateNotConfirmedMessage(host, w.selfFrom, w.selfTo, dv.agentVersion)
}

// finishSelfUpdate is finishWindow for a self-update record (AD-006). It keeps
// the EXPECT-001 contract: it is the only exit, every step is idempotent, a
// failing step leaves the entry for the next tick, and nothing runs before the
// restart grace is over. Order:
//
//  0. the detector gate (bound processing only);
//  1. the device backstop: one escalation when the suppressed offline is still
//     ongoing (FR-020, X-7), before anything that can fail;
//  2. resolution of a record that is still open (ActorSelfUpdate only);
//  3. the one outcome event of a failed or not-confirmed-online record;
//  4. the guarded close and the drop from the cache.
//
// A record that cannot be resolved is made visible one hour after its bound
// (C-4): one marker-guarded warning event, no push.
func (t *Tracker) finishSelfUpdate(ctx context.Context, devID, winID string, w window, dv devSnap, reason models.WindowEndReason) bool {
	now := t.now()
	tl := evaluate(&w, deviceView{online: dv.online, onlineSince: dv.onlineSince, returnedAt: dv.returnedAt}, now, t.graceEnd)
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
		inserted, err := t.escalateDevice(ctx, w, dv, selfDeviceKind(&w))
		if err != nil {
			return t.selfUpdateFailed(ctx, devID, winID, &w, dv, tl, err)
		}
		escalated = inserted
	}

	// 2. Resolution at the bound.
	if active {
		if err := t.resolveSelfUpdate(ctx, &w, dv); err != nil {
			return t.selfUpdateFailed(ctx, devID, winID, &w, dv, tl, err)
		}
	}

	// 3. The outcome event.
	if err := t.selfUpdateOutcomeEvent(ctx, &w, dv); err != nil {
		return t.selfUpdateFailed(ctx, devID, winID, &w, dv, tl, err)
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
		return t.selfUpdateFailed(ctx, devID, winID, &w, dv, tl, err)
	}
	t.dropWindow(devID, winID, true)
	return true
}

// selfUpdateFailed records a failing step. One hour after the record's bound it
// also makes the stuck record visible (C-4); the entry keeps retrying.
func (t *Tracker) selfUpdateFailed(ctx context.Context, devID, winID string, w *window, dv devSnap, tl timeline, err error) bool {
	if !tl.hard.IsZero() && !t.now().Before(tl.hard.Add(models.SelfUpdateStuckAfter)) {
		t.writeSelfUpdateStuck(ctx, w, dv)
	}
	t.noteFailure(devID, winID, err)
	return false
}

// resolveSelfUpdate ends a record that is still open. A transition that does not
// apply (another writer got there first) is followed by a re-read: the step
// succeeds only when the row is no longer open.
func (t *Tracker) resolveSelfUpdate(ctx context.Context, w *window, dv devSnap) error {
	if t.selfResolver == nil {
		return errNoSelfUpdateResolver
	}
	outcome, msg := selfUpdateResolution(w, dv)
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	_, applied, err := t.selfResolver.ResolveSelfUpdate(cctx, w.cmd.ID, w.cmd.DeviceID, outcome, msg, t.now())
	cancel()
	if err != nil {
		return fmt.Errorf("resolve self-update record: %w", err)
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
		return fmt.Errorf("re-read self-update record: %w", err)
	}
	if cur.State().IsActive() {
		return errSelfUpdateOpen
	}
	return nil
}

// selfUpdateOutcomeEvent writes the one outcome event of a record (FR-018,
// FR-019, FR-021; AD-006 step 3): agent_self_update_failed for an agent-reported
// failure or a return on another version, agent_self_update_unconfirmed for a
// record not confirmed while the device was online. Nothing else produces an
// event of its own: a not-back device escalates, a success is silent. Exactly
// once per record through the marker, and broadcast only (no rule, no push, no
// ceiling: C-6).
func (t *Tracker) selfUpdateOutcomeEvent(ctx context.Context, w *window, dv devSnap) error {
	if t.opt.Commands == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	row, err := t.opt.Commands.GetByID(cctx, w.cmd.ID)
	cancel()
	if err != nil {
		return fmt.Errorf("read self-update record: %w", err)
	}
	var typ models.EventType
	switch row.FailureStep {
	case models.SelfUpdateFailureAgentFailed, models.SelfUpdateFailureWrongVersion:
		typ = models.EventAgentSelfUpdateFailed
	case models.SelfUpdateFailureUnconfirmedOnline:
		typ = models.EventAgentSelfUpdateUnconfirmed
	default:
		return nil
	}
	if !row.State().IsTerminal() {
		return nil
	}
	msg := row.ResultMsg
	if msg == "" {
		msg = models.SelfUpdateActionLabel(w.selfFrom, w.selfTo) + " " + models.SelfUpdateActorPhrase + " did not complete on " + hostnameOf(dv)
	}
	_, err = t.recordSelfUpdateEvent(ctx, w, dv, models.EscalationTargetOutcome, models.Event{
		DeviceID: w.cmd.DeviceID, Type: typ, Severity: models.SeverityWarning, Message: msg,
		CommandID: w.cmd.ID, CommandAttr: models.CommandAttrOutcome, CreatedAt: t.now(),
	})
	return err
}

// writeSelfUpdateStuck makes a record that could not be completed visible, one
// hour after its bound (C-4): exactly one agent_self_update_paused warning event
// per record (marker "stuck"), no push, and one Warn. The next valid start on the
// device retires the row, so it never blocks new records.
func (t *Tracker) writeSelfUpdateStuck(ctx context.Context, w *window, dv devSnap) {
	msg := models.SelfUpdateStuckMessage(hostnameOf(dv), models.ShortCommandID(w.cmd.ID), w.selfFrom, w.selfTo)
	inserted, err := t.recordSelfUpdateEvent(ctx, w, dv, models.EscalationTargetStuck, models.Event{
		DeviceID: w.cmd.DeviceID, Type: models.EventAgentSelfUpdatePaused, Severity: models.SeverityWarning, Message: msg,
		CommandID: w.cmd.ID, CommandAttr: models.CommandAttrOutcome, CreatedAt: t.now(),
	})
	if err != nil {
		t.warnThrottled("self_update_stuck_event", "agent self-update stuck-record event could not be stored; it is retried",
			"device_id", w.cmd.DeviceID, "command_id", w.cmd.ID, "error", err.Error())
		return
	}
	if inserted {
		t.warnThrottled("self_update_stuck", "agent self-update record could not be completed one hour after its bound; tracking is paused for the device until it is replaced",
			"device_id", w.cmd.DeviceID, "command_id", w.cmd.ID)
	}
}

// recordSelfUpdateEvent stores one exactly-once event of a record: the marker
// and the event in one transaction, then a broadcast-only job. The job has no
// rule, so it is never dispatched to a channel and never consumes the per-device
// push ceiling (C-6).
func (t *Tracker) recordSelfUpdateEvent(ctx context.Context, w *window, dv devSnap, target string, evt models.Event) (bool, error) {
	marker := models.Escalation{CommandID: w.cmd.ID, Target: target, DeviceID: w.cmd.DeviceID, CreatedAt: evt.CreatedAt}
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	inserted, err := t.opt.Repo.RecordEscalation(cctx, &marker, &evt)
	cancel()
	if err != nil {
		return false, fmt.Errorf("record self-update event: %w", err)
	}
	if inserted {
		t.pub.enqueue(job{kind: jobPush, evt: evt, rule: nil, hostname: dv.hostname, deviceID: w.cmd.DeviceID})
	}
	return inserted, nil
}
