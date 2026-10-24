package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// REBOOT-DPKG-001: every agent-initiated reboot passes one shared gate that
// waits, bounded, for the dpkg lock and mints the single-use permit execReboot
// requires. A skipped reboot is reported and never forced or retried (BR-002).

// dpkgLockPoll is how often a held lock is re-checked. With the 1s probe and the
// guards it bounds the release-to-reboot delay under FR-004's 10 seconds.
const dpkgLockPoll = 2 * time.Second

// Skip kinds of a rebootClearance.
const (
	skipLockTimeout = "lock_timeout"
	skipLockUnknown = "lock_unknown"
	skipShutdown    = "shutdown"
	skipGuard       = "guard"
)

// Guard texts. They are the exact words the reports carry.
const (
	guardShuttingDown   = "agent shutting down"
	guardAllowRebootOff = "commands.allow_reboot is off"
	guardBudgetExpired  = "agent command time budget exhausted"
	guardBusyPrefix     = "host busy: "
)

// causeLockFilesVanished is the cause when the lock files disappear after the
// gate saw them held (C-1 c): the state is no longer trustworthy.
const causeLockFilesVanished = "dpkg lock files disappeared during the wait"

// errRebootNotCleared is returned by execReboot for a permit the gate did not
// mint for this attempt.
var errRebootNotCleared = errors.New("reboot refused: no dpkg-lock clearance for this attempt")

// rebootPermit is proof that awaitRebootClearance returned Cleared for one attempt.
// The type is unexported, so code outside package agent cannot build one. Inside
// the package it is minted only by mintRebootPermit, which only
// awaitRebootClearance calls; an AST test enforces both (AD-007).
type rebootPermit struct{ id uint64 }

// mintRebootPermit issues the permit of the attempt that was just cleared. A
// newer permit invalidates every older one.
func (a *Agent) mintRebootPermit() rebootPermit {
	id := a.rebootPermitSeq.Add(1)
	a.rebootPermitLive.Store(id)
	return rebootPermit{id: id}
}

// rebootGuards returns "" when every guard of a reboot path passes, else the text
// of the first guard that failed.
type rebootGuards func(ctx context.Context) string

// rebootClearance is the outcome of the gate for one reboot attempt.
type rebootClearance struct {
	Cleared bool
	Permit  rebootPermit  // valid only when Cleared
	Waited  time.Duration // first "held" observation to return; 0 when never held
	Skip    string        // "" | skipLockTimeout | skipLockUnknown | skipShutdown | skipGuard
	Detail  string        // holder text, cause, or guard text
}

// reason is the text of a skip as the result, event and log carry it (AD-005).
func (c rebootClearance) reason() string {
	switch c.Skip {
	case skipLockTimeout:
		return fmt.Sprintf("dpkg lock held past the timeout (%s), %s", models.FormatDurationShort(models.DpkgLockWaitBound), c.Detail)
	case skipLockUnknown:
		return "dpkg lock state unknown: " + c.Detail
	case skipShutdown:
		return guardShuttingDown
	default:
		return c.Detail
	}
}

// commonRebootGuards are the guards every reboot path re-checks right before the
// reboot (FR-011), in order. ownID is the command whose own maintenance run may
// be winding down ("" is strict, for the standalone reboot).
func (a *Agent) commonRebootGuards(ownID string) rebootGuards {
	return func(ctx context.Context) string {
		if a.shuttingDown.Load() {
			return guardShuttingDown
		}
		if !a.config.Commands.AllowReboot {
			return guardAllowRebootOff
		}
		if blocker := a.maintenanceBlockerFor(ctx, ownID); blocker != "" {
			return guardBusyPrefix + blocker
		}
		return ""
	}
}

// awaitRebootClearance waits, for at most models.DpkgLockWaitBound from the first
// check that finds the dpkg lock held, until a reboot may start. It runs the
// path's guards once the lock looks free and finishes with a fresh lock check, so
// the last thing before the reboot is always a lock check (FR-012). path and
// commandID only label the logs. It returns once: no caller loops, so one attempt
// has at most one wait (FR-009).
func (a *Agent) awaitRebootClearance(ctx context.Context, path, commandID string, guards rebootGuards) rebootClearance {
	var (
		firstHeld     time.Time
		holder        string
		waiting       bool
		sawHeld       bool
		guardsPassed  bool
		waitedSoFar   = func() time.Duration { return a.clock().Now().Sub(firstHeld) }
		endOfWaitInfo = func(c rebootClearance) {
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
	skip := func(kind, detail string) rebootClearance {
		c := rebootClearance{Skip: kind, Detail: detail}
		if waiting {
			c.Waited = waitedSoFar()
		}
		endOfWaitInfo(c)
		return c
	}
	cleared := func() rebootClearance {
		c := rebootClearance{Cleared: true, Permit: a.mintRebootPermit()}
		if waiting {
			c.Waited = waitedSoFar()
		}
		endOfWaitInfo(c)
		return c
	}

	for {
		if a.shuttingDown.Load() {
			return skip(skipShutdown, guardShuttingDown)
		}
		st := a.probeDpkg()
		switch st.State {
		case dpkgLockUnknown:
			return skip(skipLockUnknown, st.Cause)
		case dpkgLockAbsent, dpkgLockFree:
			if st.State == dpkgLockAbsent && sawHeld {
				return skip(skipLockUnknown, causeLockFilesVanished)
			}
			if !guardsPassed {
				if guards != nil {
					if g := guards(ctx); g != "" {
						if g == guardShuttingDown {
							return skip(skipShutdown, g)
						}
						return skip(skipGuard, g)
					}
				}
				guardsPassed = true
				continue // the final check is a fresh probe
			}
			return cleared()
		case dpkgLockHeld:
			sawHeld, guardsPassed = true, false
			holder = st.holderText()
			now := a.clock().Now()
			if !waiting {
				waiting, firstHeld = true, now
				slog.Info("dpkg lock held, waiting before reboot", "path", path, "command_id", commandID, "holder", holder)
			}
			elapsed := now.Sub(firstHeld)
			if elapsed >= models.DpkgLockWaitBound {
				return skip(skipLockTimeout, holder)
			}
			if !a.sleep(ctx, min(dpkgLockPoll, models.DpkgLockWaitBound-elapsed)) {
				if a.shuttingDown.Load() {
					return skip(skipShutdown, guardShuttingDown)
				}
				return skip(skipGuard, guardBudgetExpired)
			}
		default:
			return skip(skipLockUnknown, "unexpected lock state")
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
