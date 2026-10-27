package expect

import (
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// window is one tracked command (EXPECT-001 AD-001). Everything that decides
// whether it covers an occurrence is derived from the command row, the device's
// observed state and the clock (evaluate); the fields below the line are
// processing state that lives in memory only and is re-derived after a restart.
type window struct {
	cmd    models.Command // trimmed copy: no params, steps or output
	policy models.WindowPolicy
	// addedAt is when the command hook created this entry; reconcile never
	// treats a window it did not see in the database as deleted before then.
	addedAt time.Time
	// fromVersion is the device's agent version when an agent_update was first
	// seen (the "from" of "Updating agent from -> to").
	fromVersion string
	// selfFrom and selfTo are the validated versions of an agent self-update
	// record (EXPECT-002 AD-009), read from the row's params before it is trimmed;
	// both are "" for a command window or when the params are not valid versions.
	selfFrom, selfTo string

	deferred      bool      // the container check waits for the device to return (AD-011)
	deferredBase  time.Time // the close time at which the check was deferred
	deviceEscDone bool      // FR-020 already raised for this window in this process
	// noPoweredOff: the offline transition of this shutdown alerted (the
	// powered-off row could not be stored); the loop must not turn it into a
	// silent powered-off state afterwards.
	noPoweredOff bool
	failingSince time.Time // first failure of finishWindow, for the 5 min eviction
}

// deviceView is the part of a device's observed state that evaluate needs.
type deviceView struct {
	online      bool
	onlineSince time.Time
	returnedAt  time.Time
}

// timeline is the evaluation of one window at one instant.
type timeline struct {
	opened bool
	d      time.Time // dispatch time
	hard   time.Time // hard bound: D + execution deadline + post-terminal period
	phase  models.ActivityPhase

	// offlineEnd and containerEnd are the exclusive ends of the periods in which
	// the window covers a device_offline / container_died occurrence. A zero
	// value means the window covers none of that kind.
	offlineEnd   time.Time
	containerEnd time.Time

	// closeAt is when the window must be finished; reason says why. due is true
	// when now has reached it.
	closeAt time.Time
	reason  models.WindowEndReason
	due     bool
	// backBy is the return bound of a succeeded host or agent command.
	backBy *time.Time
}

func finishedAt(c *models.Command) time.Time {
	if c.FinishedAt != nil {
		return *c.FinishedAt
	}
	return c.UpdatedAt
}

// execBudget is the execution deadline E of a dispatched command: deadline_at
// minus dispatched_at, falling back to exec_deadline_s and then to the policy
// default (AD-003). It is never zero.
func execBudget(c *models.Command) time.Duration {
	if c.DispatchedAt != nil && c.DeadlineAt != nil {
		if e := c.DeadlineAt.Sub(*c.DispatchedAt); e > 0 {
			return e
		}
	}
	if c.ExecDeadlineS > 0 {
		return time.Duration(c.ExecDeadlineS) * time.Second
	}
	return models.ExecDeadline(c.Action, len(c.TargetKeys))
}

func laterOf(ts ...time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if t.After(out) {
			out = t
		}
	}
	return out
}

func earlierOf(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// evaluate is the pure window function of AD-003: phase, coverage ends and
// close time of w at now, given the device's observed state and the end of the
// restart grace. Coverage is strict at the end (AC-007): an occurrence at
// closeAt is not covered. No rule ever covers at or after the hard bound
// (FR-005, NFR-002).
func evaluate(w *window, dv deviceView, now, graceEnd time.Time) timeline {
	c := &w.cmd
	p := w.policy
	state := c.State()
	var tl timeline

	if c.DispatchedAt == nil {
		if state.IsTerminal() {
			// Never dispatched (expired or cancelled while queued): nothing to cover.
			tl.phase = models.PhaseClosed
			tl.closeAt = finishedAt(c)
			tl.reason = models.EndNotOpened
			tl.due = true
			return tl
		}
		tl.phase = models.PhaseQueued
		return tl
	}

	if p.SelfUpdate {
		return evaluateSelf(w, dv, now)
	}

	tl.opened = true
	tl.d = *c.DispatchedAt
	tl.hard = tl.d.Add(execBudget(c) + p.PostTerminal)
	if w.deferred {
		// The container check waits for the device; the window covers nothing.
		tl.phase = models.PhaseAwaitingReturn
		return tl
	}

	var end time.Time
	switch {
	case !state.IsTerminal():
		tl.phase = models.PhaseInProgress
		end = tl.hard
		tl.reason = models.EndHardBound
		if p.CoversOffline {
			tl.offlineEnd = end
		}
		if p.Containers != models.CoverNoContainers {
			tl.containerEnd = end
		}

	case state == models.CommandSucceeded:
		f := finishedAt(c)
		bound := f.Add(p.PostTerminal)
		end = earlierOf(bound, tl.hard)
		tl.reason = models.EndReturnBound
		if !bound.Before(tl.hard) {
			tl.reason = models.EndHardBound
		}
		switch p.Class {
		case models.WindowContainer, models.WindowContainerDown:
			tl.phase = models.PhaseSettling
			tl.reason = models.EndSettled
			if !bound.Before(tl.hard) {
				tl.reason = models.EndHardBound
			}
		case models.WindowHost, models.WindowAgent:
			tl.phase = models.PhaseReturn
			b := end
			tl.backBy = &b
			observed := !p.RequireObservedReturn || dv.returnedAt.After(f)
			if p.Class == models.WindowAgent && dv.returnedAt.After(tl.d) {
				// A signed agent update completes from the restarted agent, so its
				// return (re-registration) comes before F, not after it.
				observed = true
			}
			if dv.online && observed {
				anchor := laterOf(f, dv.onlineSince, graceEnd)
				if p.RequireObservedReturn {
					anchor = laterOf(anchor, dv.returnedAt)
				}
				if early := anchor.Add(p.Settle); early.Before(end) {
					end = early
					tl.reason = models.EndSettled
				}
			}
		default: // shutdown
			tl.phase = models.PhaseReturn
		}
		if p.CoversOffline {
			tl.offlineEnd = end
		}
		if p.Containers != models.CoverNoContainers {
			tl.containerEnd = end
		}

	default:
		// failed, timed_out (cancelled and expired never get here: they are
		// terminal without dispatch). Offline coverage stops at F (FR-004); a
		// rollback or a Docker restart may still be observed for 2 minutes.
		f := finishedAt(c)
		tl.phase = models.PhaseSettling
		tl.reason = models.EndNonSuccessSettle
		if p.Containers == models.CoverNoContainers {
			end = earlierOf(f, tl.hard)
		} else {
			end = earlierOf(f.Add(models.ExpectNonSuccessSettle), tl.hard)
			tl.containerEnd = end
		}
		if p.CoversOffline {
			tl.offlineEnd = earlierOf(f, tl.hard)
		}
		if end.Equal(tl.hard) && tl.hard.Before(f.Add(models.ExpectNonSuccessSettle)) {
			tl.reason = models.EndHardBound
		}
	}

	tl.closeAt = end
	tl.due = !now.Before(end)
	// An unprocessed window older than the deferral bound lapses (AD-009).
	if tl.due && now.Sub(end) >= models.ExpectDeferredRecheckBound {
		tl.reason = models.EndLapsed
	}
	return tl
}

// coversAt reports whether the end e covers an occurrence at t.
func (tl timeline) coversAt(end, t time.Time) bool {
	return tl.opened && !end.IsZero() && !t.Before(tl.d) && t.Before(end)
}

// intersects reports whether two key lists share an element.
func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
