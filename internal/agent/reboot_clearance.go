package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// REBOOT-DPKG-001 / SHUTDOWN-DPKG-001: every agent-initiated reboot and every
// shutdown command passes one shared gate that waits, bounded, for the dpkg lock
// and mints the single-use permit execReboot or execShutdown requires. A skipped
// power action is reported and never forced or retried (BR-002). The gate is
// parameterized by a powerGate spec, so the two actions share one loop and keep
// separate bounds and messages (NFR-001, FR-017).

// dpkgLockPoll is how often a held lock is re-checked. With the 1s probe and the
// guards it bounds the release-to-poweroff delay under FR-004's 10 seconds.
const dpkgLockPoll = 2 * time.Second

// Skip kinds of a powerClearance.
const (
	skipLockTimeout = "lock_timeout"
	skipLockUnknown = "lock_unknown"
	skipShutdown    = "shutdown"
	skipGuard       = "guard"
)

// Guard texts. They are the exact words the reports carry.
const (
	guardShuttingDown     = "agent shutting down"
	guardAllowRebootOff   = "commands.allow_reboot is off"
	guardAllowShutdownOff = "commands.allow_shutdown is off"
	guardBudgetExpired    = "agent command time budget exhausted"
	guardBusyPrefix       = "host busy: "
)

// causeLockFilesVanished is the cause when the lock files disappear after the
// gate saw them held (C-1 c): the state is no longer trustworthy.
const causeLockFilesVanished = "dpkg lock files disappeared during the wait"

// causeStateNotRecognized is the cause when a status carries no cause of its own,
// for example a zero or hand-built dpkgLockStatus.
const causeStateNotRecognized = "lock state not recognized"

// powerAction names the power action a gate attempt or a permit is for.
type powerAction uint8

const (
	powerActionNone powerAction = iota // zero value: never valid for any exec
	powerActionReboot
	powerActionShutdown
)

// powerGate is the per-action spec of the shared gate. Only the entry points
// build it (rebootGate, shutdownGate), so no call site can pair one action with
// the other's bound or flag. allowed is the action's config flag: the gate checks
// it itself, so a permit is never minted without it, whatever guards the caller
// passes (SHUTDOWN-DPKG-001 C-3).
type powerGate struct {
	action       powerAction
	waitBound    time.Duration
	waitMsg      string              // Info message when a wait starts
	allowed      func(a *Agent) bool // the action's config flag; a nil config is not allowed
	allowOffText string              // guard text when allowed() is false
}

func rebootGate() powerGate {
	return powerGate{
		action:       powerActionReboot,
		waitBound:    models.DpkgLockWaitBound,
		waitMsg:      "dpkg lock held, waiting before reboot",
		allowed:      func(a *Agent) bool { return a.config != nil && a.config.Commands.AllowReboot },
		allowOffText: guardAllowRebootOff,
	}
}

func shutdownGate() powerGate {
	return powerGate{
		action:       powerActionShutdown,
		waitBound:    models.ShutdownDpkgLockWaitBound,
		waitMsg:      "dpkg lock held, waiting before shutdown",
		allowed:      func(a *Agent) bool { return a.config != nil && a.config.Commands.AllowShutdown },
		allowOffText: guardAllowShutdownOff,
	}
}

// errRebootNotCleared and errShutdownNotCleared are returned by the exec
// functions for a permit the gate did not mint for this attempt and action.
var (
	errRebootNotCleared   = errors.New("reboot refused: no dpkg-lock clearance for this attempt")
	errShutdownNotCleared = errors.New("shutdown refused: no dpkg-lock clearance for this attempt")
)

// powerPermit is proof that awaitPowerClearance returned Cleared for one attempt
// of one action. The type is unexported, so code outside package agent cannot
// build one. Inside the package it is minted only by mintPowerPermit, which only
// awaitPowerClearance calls, and its fields are read only by consumePowerPermit;
// AST tests enforce both (AD-006).
type powerPermit struct {
	permitID     uint64
	permitAction powerAction
}

// mintPowerPermit issues the permit of the attempt that was just cleared. A newer
// permit invalidates every older one, of either action.
func (a *Agent) mintPowerPermit(action powerAction) powerPermit {
	id := a.powerPermitSeq.Add(1)
	a.powerPermitLive.Store(id)
	return powerPermit{permitID: id, permitAction: action}
}

// consumePowerPermit reports whether p is the live permit for want, and uses it
// up. The action is compared before the slot, so a permit for the other action is
// refused and leaves the live permit intact.
func (a *Agent) consumePowerPermit(p powerPermit, want powerAction) bool {
	return p.permitID != 0 && p.permitAction == want && a.powerPermitLive.CompareAndSwap(p.permitID, 0)
}

// powerGuards returns "" when every guard of a power path passes, else the text
// of the first guard that failed.
type powerGuards func(ctx context.Context) string

// powerClearance is the outcome of the gate for one power attempt.
type powerClearance struct {
	Cleared bool
	Permit  powerPermit   // valid only when Cleared
	Waited  time.Duration // first "held" observation to return; 0 when never held
	Skip    string        // "" | skipLockTimeout | skipLockUnknown | skipShutdown | skipGuard
	Detail  string        // holder text, cause, or guard text
	Bound   time.Duration // the gate's wait bound; set on every return
}

// reason is the text of a skip as the result, event and log carry it (AD-005).
func (c powerClearance) reason() string {
	switch c.Skip {
	case skipLockTimeout:
		return fmt.Sprintf("dpkg lock held past the timeout (%s), %s", models.FormatDurationShort(c.Bound), c.Detail)
	case skipLockUnknown:
		return "dpkg lock state unknown: " + c.Detail
	case skipShutdown:
		return guardShuttingDown
	default:
		return c.Detail
	}
}

// commonPowerGuards are the guards every power path re-checks right before the
// poweroff (FR-011), in order. allowed and offText are the action's config flag
// and its guard text; ownID is the command whose own maintenance run may be
// winding down ("" is strict, for the standalone reboot and for shutdown).
func (a *Agent) commonPowerGuards(allowed func() bool, offText, ownID string) powerGuards {
	return func(ctx context.Context) string {
		if a.shuttingDown.Load() {
			return guardShuttingDown
		}
		if !allowed() {
			return offText
		}
		if blocker := a.maintenanceBlockerFor(ctx, ownID); blocker != "" {
			return guardBusyPrefix + blocker
		}
		return ""
	}
}

// commonRebootGuards are the guards every reboot path re-checks right before the
// reboot (FR-011). ownID is the command whose own maintenance run may be winding
// down ("" is strict, for the standalone reboot).
func (a *Agent) commonRebootGuards(ownID string) powerGuards {
	return a.commonPowerGuards(func() bool { return a.config.Commands.AllowReboot }, guardAllowRebootOff, ownID)
}

// commonShutdownGuards are the guards of the shutdown command. The maintenance
// check is strict: no run is exempt (OQ-005).
func (a *Agent) commonShutdownGuards() powerGuards {
	return a.commonPowerGuards(func() bool { return a.config.Commands.AllowShutdown }, guardAllowShutdownOff, "")
}

// awaitRebootClearance is the reboot entry point of the gate. Its signature and
// behavior are those of REBOOT-DPKG-001.
func (a *Agent) awaitRebootClearance(ctx context.Context, path, commandID string, guards powerGuards) powerClearance {
	return a.awaitPowerClearance(ctx, rebootGate(), path, commandID, guards)
}

// awaitShutdownClearance is the shutdown entry point. It binds its own guards, so
// a caller cannot weaken them.
func (a *Agent) awaitShutdownClearance(ctx context.Context, commandID string) powerClearance {
	return a.awaitPowerClearance(ctx, shutdownGate(), "shutdown", commandID, a.commonShutdownGuards())
}

// awaitPowerClearance waits, for at most g.waitBound from the first check that
// finds the dpkg lock held, until a power action may start. It runs the guards
// once the lock looks free and finishes with a fresh lock check, so the last thing
// before the poweroff is always a lock check (FR-012). path and commandID only
// label the logs. It returns once: no caller loops, so one attempt has at most one
// wait (FR-009).
func (a *Agent) awaitPowerClearance(ctx context.Context, g powerGate, path, commandID string, guards powerGuards) powerClearance {
	var (
		firstHeld     time.Time
		holder        string
		waiting       bool
		sawHeld       bool
		guardsPassed  bool
		waitedSoFar   = func() time.Duration { return a.clock().Now().Sub(firstHeld) }
		endOfWaitInfo = func(c powerClearance) {
			if !waiting {
				return
			}
			outcome := "cleared"
			if !c.Cleared {
				outcome = c.Skip
			}
			slog.Info("dpkg lock wait ended", "path", path, "command_id", commandID,
				"outcome", outcome, "waited", c.Waited.String(), "holder", holder)
		}
	)
	skip := func(kind, detail string) powerClearance {
		c := powerClearance{Skip: kind, Detail: detail, Bound: g.waitBound}
		if waiting {
			c.Waited = waitedSoFar()
		}
		endOfWaitInfo(c)
		return c
	}
	cleared := func() powerClearance {
		c := powerClearance{Cleared: true, Permit: a.mintPowerPermit(g.action), Bound: g.waitBound}
		if waiting {
			c.Waited = waitedSoFar()
		}
		endOfWaitInfo(c)
		return c
	}

	for {
		// Poll shuttingDown instead of relying on ctx: on SIGTERM the command
		// context is cancelled only after the 20s grace period, and the post-patch
		// goroutines are not tracked by the command wait group (FR-013).
		if a.shuttingDown.Load() {
			return skip(skipShutdown, guardShuttingDown)
		}
		st := a.probeDpkg()
		switch st.State {
		case dpkgLockAbsent, dpkgLockFree:
			// C-1 c: a lock that was held and then vanishes means the lock files were
			// deleted or hidden, not released. "Absent" never clears after a "held".
			if st.State == dpkgLockAbsent && sawHeld {
				return skip(skipLockUnknown, causeLockFilesVanished)
			}
			if !guardsPassed {
				// The action's own allow flag is checked here, whatever guards the caller
				// passes, so a permit is minted only after it evaluated true in this
				// attempt (SHUTDOWN-DPKG-001 C-3). Same order and texts as
				// commonPowerGuards, so the first failure reported is the same.
				if a.shuttingDown.Load() {
					return skip(skipShutdown, guardShuttingDown)
				}
				if !g.allowed(a) {
					return skip(skipGuard, g.allowOffText)
				}
				if guards != nil {
					if text := guards(ctx); text != "" {
						if text == guardShuttingDown {
							return skip(skipShutdown, text)
						}
						return skip(skipGuard, text)
					}
				}
				guardsPassed = true
				continue // the final check is a fresh probe
			}
			return cleared()
		case dpkgLockHeld:
			// A lock taken again after the guards passed (FR-012, AC-014) keeps the same
			// wait deadline and sends the attempt back through the guards, so they
			// are always evaluated against the state just before the poweroff.
			sawHeld, guardsPassed = true, false
			holder = st.holderText()
			now := a.clock().Now()
			if !waiting {
				waiting, firstHeld = true, now
				slog.Info(g.waitMsg, "path", path, "command_id", commandID, "holder", holder)
			}
			elapsed := now.Sub(firstHeld)
			if elapsed >= g.waitBound {
				return skip(skipLockTimeout, holder)
			}
			if !a.sleep(ctx, min(dpkgLockPoll, g.waitBound-elapsed)) {
				if a.shuttingDown.Load() {
					return skip(skipShutdown, guardShuttingDown)
				}
				return skip(skipGuard, guardBudgetExpired)
			}
		default: // dpkgLockUnknown, the zero value, and any value outside the four states
			cause := st.Cause
			if cause == "" {
				cause = causeStateNotRecognized
			}
			return skip(skipLockUnknown, cause)
		}
	}
}

// abortPostPatchReboot reports, once, that the unauthorized post-patch reboot was
// not started, and refreshes telemetry so the still-required reboot stays
// visible. It mirrors abortAuthorizedReboot (AD-005).
func (a *Agent) abortPostPatchReboot(id, reason string) {
	slog.Warn("os_update: post-patch reboot not started", "command_id", id, "reason", reason)
	msg := fmt.Sprintf("post-patch reboot for command %s was not started: %s", id, reason)
	a.notifyEvent(func(ctx context.Context) {
		a.sendAgentEvent(ctx, models.EventCommandCompleted, models.SeverityWarning, msg)
	})
	a.triggerTelemetry()
}
