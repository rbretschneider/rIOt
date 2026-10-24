package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Authorized-reboot timing (PATCH-FLOW-001 AD-005).
const (
	// defaultRebootReportWait bounds how long the reboot waits for the result to
	// be delivered and acknowledged. After it the reboot proceeds with the result
	// still durable in the outbox, so a down server cannot hold a required reboot.
	defaultRebootReportWait = 30 * time.Second
	rebootDeliveryPoll      = 500 * time.Millisecond
	defaultRebootDelay      = 2 * time.Second
)

// Package managers an os_update can use (the values of UpdateInfo.PackageManager).
const (
	pmAPT = "apt"
	pmDNF = "dnf"
)

// Reasons an authorized run did not start a reboot (FR-023). They are the
// exact words of the result message.
const (
	rebootReasonNotRequired     = "not required"
	rebootReasonAllowRebootOff  = "allow_reboot off"
	rebootReasonRunNotSuccess   = "run not successful"
	rebootReasonSuppressed      = "suppressed"
	suppressedAlreadyClaimed    = "reboot already claimed for this command"
	suppressedIntentNotRecorded = "reboot intent could not be recorded"
	suppressedIntentStoreFull   = "reboot intent store full"
	suppressedResultRecovered   = "result recovered after agent restart"
)

// rebootAuthOutcome records, for one authorized os_update, whether a reboot was
// required after the run and whether one was started (FR-023).
type rebootAuthOutcome struct {
	Required bool
	Started  bool
	Reason   string // why no reboot was started; empty when Started
	Detail   string // extra words for rebootReasonSuppressed
}

// suffix renders the single message suffix that carries the FR-023 facts. It is
// appended last, so the tail-preserving truncation keeps it.
func (o rebootAuthOutcome) suffix() string {
	required := "no"
	if o.Required {
		required = "yes"
	}
	head := "Reboot authorized; reboot required after run: " + required
	if o.Started {
		return head + "; reboot started: yes"
	}
	reason := o.Reason
	if o.Detail != "" {
		reason += " (" + o.Detail + ")"
	}
	return head + "; reboot not started: " + reason
}

func appendRebootSuffix(message string, outcome rebootAuthOutcome) string {
	if message == "" {
		return outcome.suffix()
	}
	return message + " | " + outcome.suffix()
}

// rebootAuthorization reads os_update's reboot_if_required param. Absent is
// false; a value that is present but not a boolean is an error.
func rebootAuthorization(params map[string]interface{}) (bool, error) {
	value, present := params[models.ParamRebootIfRequired]
	if !present {
		return false, nil
	}
	authorized, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", models.ParamRebootIfRequired)
	}
	return authorized, nil
}

// rebootAuthorized reports whether payload carries reboot_if_required: true. A
// malformed value is not an authorization.
func rebootAuthorized(payload models.CommandPayload) bool {
	authorized, err := rebootAuthorization(payload.Params)
	return err == nil && authorized
}

// executeOSUpdate runs an os_update and attaches its post-result reboot
// behavior: the unchanged PATCH-GATE rule for unauthorized runs, or the
// authorized decision of AD-005.
func (a *Agent) executeOSUpdate(cr *cmdRun) commandExecResult {
	authorized, err := rebootAuthorization(cr.payload.Params)
	if err != nil {
		return precheckFailure(err.Error())
	}
	return a.attachRebootDecision(cr, authorized, a.handleOSUpdateWithOutput(cr.ctx, cr.payload))
}

// attachRebootDecision wires the reboot behavior of a finished os_update.
// Phase A (the decision and the claim) runs here, before the result is queued,
// so the facts the result states are true when it is sent. Phase B runs from
// completeCommand through postResult.
func (a *Agent) attachRebootDecision(cr *cmdRun, authorized bool, r commandExecResult) commandExecResult {
	r.RebootCommandID = cr.id
	switch {
	case !authorized:
		r.postResult = func() { a.maybeRebootAfterPatch(r) }
	case !r.Detached:
		a.decideAuthorizedReboot(cr.ctx, cr.id, &r)
		r.postResult = func() { a.runAuthorizedReboot(r) }
	}
	return r
}

// decideAuthorizedReboot is phase A. It evaluates FR-002 after the run, applies
// the agent-side veto, claims the single reboot, and states the outcome in the
// result message.
func (a *Agent) decideAuthorizedReboot(ctx context.Context, id string, r *commandExecResult) {
	outcome := rebootAuthOutcome{Required: a.rebootRequired(ctx, r.PackageManager)}
	switch {
	case r.Status != models.ResultStatusSuccess:
		outcome.Reason = rebootReasonRunNotSuccess
	case !outcome.Required:
		outcome.Reason = rebootReasonNotRequired
	case !a.config.Commands.AllowReboot:
		outcome.Reason = rebootReasonAllowRebootOff
	default:
		a.claimForAuthorizedReboot(id, &outcome)
	}
	r.RebootAuth = &outcome
	r.Message = appendRebootSuffix(r.Message, outcome)
}

// noteRecoveredAuthorization states the authorization on a result recovered
// after an agent restart. A recovered result never reboots (the original
// decision point is gone), so the outcome is always "suppressed".
func (a *Agent) noteRecoveredAuthorization(ctx context.Context, r *commandExecResult) {
	outcome := rebootAuthOutcome{
		Required: a.rebootRequired(ctx, pmAPT),
		Reason:   rebootReasonSuppressed,
		Detail:   suppressedResultRecovered,
	}
	r.RebootAuth = &outcome
	r.Message = appendRebootSuffix(r.Message, outcome)
}

func (a *Agent) claimForAuthorizedReboot(id string, outcome *rebootAuthOutcome) {
	claim := a.claimPatchReboot(id)
	if claim == claimOK {
		outcome.Started = true
		return
	}
	outcome.Reason = rebootReasonSuppressed
	outcome.Detail = suppressedDetail(claim)
	a.reportSuppressedReboot(id, claim)
}

func suppressedDetail(claim rebootClaim) string {
	switch claim {
	case claimAlreadyClaimed:
		return suppressedAlreadyClaimed
	case claimStoreFull:
		return suppressedIntentStoreFull
	default:
		return suppressedIntentNotRecorded
	}
}

// rebootRequired is the post-run FR-002 evaluation. The collector uses the same
// function, so the two can never disagree.
func (a *Agent) rebootRequired(ctx context.Context, packageManager string) bool {
	if a.rebootRequiredCheck != nil {
		return a.rebootRequiredCheck(ctx, packageManager)
	}
	return collectors.RebootRequiredNow(ctx, packageManager)
}

// runAuthorizedReboot is phase B. It runs after the result is durable in the
// outbox. When phase A did not start a reboot it only refreshes telemetry.
func (a *Agent) runAuthorizedReboot(r commandExecResult) {
	if r.RebootAuth == nil || !r.RebootAuth.Started {
		a.triggerTelemetry()
		return
	}
	id := r.RebootCommandID
	reservation := "patch-reboot:" + id
	if a.locks == nil {
		a.abortAuthorizedReboot(id, "host locks unavailable")
		return
	}
	if blocker := a.maintenanceBlockerFor(a.commandContext(), id); blocker != "" {
		a.abortAuthorizedReboot(id, "host busy: "+blocker)
		return
	}
	if _, _, ok := a.locks.TryLockAll([]string{models.TargetKeyHost}, reservation, models.ActionReboot); !ok {
		a.abortAuthorizedReboot(id, "host busy")
		return
	}
	go func() {
		defer a.locks.Unlock([]string{models.TargetKeyHost}, reservation)
		a.rebootAfterDelivery(id)
	}()
}

// rebootAfterDelivery waits for the result to be delivered, re-checks every
// guard, and starts the single reboot.
func (a *Agent) rebootAfterDelivery(id string) {
	ctx := a.commandContext()
	if !a.awaitResultDelivery(ctx, id) {
		return
	}
	if a.shuttingDown.Load() {
		a.abortAuthorizedReboot(id, "agent shutting down")
		return
	}
	if blocker := a.maintenanceBlockerFor(ctx, id); blocker != "" {
		a.abortAuthorizedReboot(id, "host busy: "+blocker)
		return
	}
	// The operator may turn allow_reboot off between the decision and now. The
	// agent-side veto is absolute, so it is checked again right before the reboot.
	if !a.config.Commands.AllowReboot {
		a.abortAuthorizedReboot(id, "commands.allow_reboot is off")
		return
	}
	slog.Info("os_update: authorized reboot after a successful run that requires one", "command_id", id)
	if err := a.execReboot(); err != nil {
		slog.Warn("os_update: authorized reboot failed", "command_id", id, "error", err)
		a.abortAuthorizedReboot(id, "reboot command failed: "+err.Error())
	}
}

// awaitResultDelivery waits at least the reboot delay and then, for at most
// rebootReportWait, until the outbox has delivered and dropped the result. It
// returns false when the agent is stopping, in which case no reboot is started.
func (a *Agent) awaitResultDelivery(ctx context.Context, id string) bool {
	delay := a.rebootDelay
	if delay == 0 {
		delay = defaultRebootDelay
	}
	if !a.sleep(ctx, delay) {
		return false
	}
	wait := a.rebootReportWait
	if wait == 0 {
		wait = defaultRebootReportWait
	}
	poll := rebootDeliveryPoll
	if wait < poll {
		poll = wait
	}
	for waited := time.Duration(0); a.out != nil && a.out.Has(id); waited += poll {
		if waited >= wait {
			// The result already says "reboot started: yes". An unreachable server must
			// not turn that into a silent no-op, and the result is durable in the outbox
			// and is delivered after boot.
			slog.Warn("result not yet delivered; rebooting with result queued", "command_id", id)
			return true
		}
		if !a.sleep(ctx, poll) {
			return false
		}
	}
	return true
}

// sleep pauses for d through the pause seam (tests) or a timer. It returns
// false when ctx ends first.
func (a *Agent) sleep(ctx context.Context, d time.Duration) bool {
	if a.pause != nil {
		return a.pause(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// abortAuthorizedReboot reports, once, that a reboot the result announced was
// not started, and refreshes telemetry so the still-required reboot is visible.
func (a *Agent) abortAuthorizedReboot(id, reason string) {
	slog.Warn("os_update: authorized reboot not started", "command_id", id, "reason", reason)
	msg := fmt.Sprintf("authorized reboot for command %s was not started: %s", id, reason)
	a.notifyEvent(func(ctx context.Context) {
		a.sendAgentEvent(ctx, models.EventCommandCompleted, models.SeverityWarning, msg)
	})
	a.triggerTelemetry()
}
