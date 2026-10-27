package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-007 AD-004, AD-005 and AD-010: the agent_update_started, _completed and
// _failed events carry their structured versions and one correlation reference,
// and are sent with a bounded retry.
//
// Bounds (ADD AD-014):
//   - at most models.UpdateEventMaxAttempts (5) attempts per event, none starting
//     later than models.UpdateEventRetryBudget (90 s) after the first, each under
//     its own models.UpdateEventAttemptTimeout (10 s) context;
//   - one retrying sender per event type and one single-shot sender per type, so
//     at most 3 + 3 = 6 sender goroutines per process, all counted in cmdWG and
//     all ended at once when the agent's own context ends;
//   - an event is held in memory only (about 1 KB); nothing is queued or
//     persisted, and a slot is released on acceptance, a final answer, the end of
//     the agent's context, or after at most about 100 s;
//   - above Debug, a sender writes at most one Warn per abandoned event, with
//     only the event type and the attempt count.

// updateEventTypes is the number of event types that use a sender slot.
const updateEventTypes = 3

// updateEventIndex is the slot of an update event type, or -1 for any other.
func updateEventIndex(t models.EventType) int {
	switch t {
	case models.EventAgentUpdateStarted:
		return 0
	case models.EventAgentUpdateCompleted:
		return 1
	case models.EventAgentUpdateFailed:
		return 2
	}
	return -1
}

// updateEventRetry is the retry policy. The zero value is not used: a nil
// *updateEventRetry on the Agent means one attempt.
type updateEventRetry struct {
	maxAttempts    int
	budget         time.Duration
	attemptTimeout time.Duration
	schedule       [models.UpdateEventMaxAttempts]time.Duration
}

// defaultUpdateEventRetry is the production policy (FR-031, FR-032).
func defaultUpdateEventRetry() *updateEventRetry {
	return &updateEventRetry{
		maxAttempts:    models.UpdateEventMaxAttempts,
		budget:         models.UpdateEventRetryBudget,
		attemptTimeout: models.UpdateEventAttemptTimeout,
		schedule:       models.UpdateEventRetrySchedule,
	}
}

// updateDelivery is how a sender treats its event.
type updateDelivery uint8

const (
	// deliverAwait: every attempt in the caller's goroutine. The self-update
	// start (FR-033).
	deliverAwait updateDelivery = iota
	// deliverFirstSync: the first attempt in the caller's goroutine, as before
	// EXPECT-007; retries in one background goroutine. The self-update path's
	// completed and failed events, which are followed by a restart.
	deliverFirstSync
	// deliverAsync: every attempt in one background goroutine; the caller never
	// waits (FR-034). The command path.
	deliverAsync
)

// updateAttempt identifies one update attempt: its correlation ID (self-update
// path only) and the versions it moves between.
type updateAttempt struct {
	ID       string
	From, To string
}

type updateAttemptKey struct{}

func withUpdateAttempt(ctx context.Context, att updateAttempt) context.Context {
	return context.WithValue(ctx, updateAttemptKey{}, att)
}

func updateAttemptFrom(ctx context.Context) updateAttempt {
	att, _ := ctx.Value(updateAttemptKey{}).(updateAttempt)
	return att
}

// newUpdateAttempt starts a self-update attempt. The ID is 128 bits from
// crypto/rand in hex, so it is unpredictable and unique per attempt on the
// device (FR-013). If the random source fails the attempt carries no ID and the
// server falls back to EXPECT-002; on Go 1.24 crypto/rand.Read cannot fail, so
// that branch is defensive only.
func newUpdateAttempt(from, to string) updateAttempt {
	att := updateAttempt{From: from, To: to}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		slog.Debug("update attempt id unavailable")
		return att
	}
	att.ID = hex.EncodeToString(raw[:])
	return att
}

// buildUpdateEvent assembles an update event. Versions are sent only as a pair.
// The reference is the command ID when one is given (command path), else the
// attempt ID (self-update path); never both (FR-012, FR-013, FR-027). The message
// is passed through untouched (FR-015).
func buildUpdateEvent(t models.EventType, sev models.EventSeverity, message, commandID string, att updateAttempt) *models.AgentEvent {
	evt := &models.AgentEvent{Type: t, Severity: sev, Message: message}
	if att.From != "" && att.To != "" {
		evt.FromVersion, evt.ToVersion = att.From, att.To
	}
	switch {
	case commandID != "":
		evt.CommandID = commandID
	case att.ID != "" && models.ValidRefToken(att.ID):
		evt.AttemptID = att.ID
	}
	return evt
}

type eventVerdict uint8

const (
	evAccepted eventVerdict = iota
	evRetry
	evFinal    // a non-retryable answer, or no client
	evCanceled // the agent's own context ended
)

// classifyEventSendErr decides what one attempt's outcome means (FR-031). The
// parent context is checked first: when the agent's own context is done the
// sender stops whatever the attempt returned, and never retries a
// context.Canceled (security condition C-4 a).
func classifyEventSendErr(parent context.Context, err error) eventVerdict {
	if parent.Err() != nil {
		return evCanceled
	}
	if err == nil {
		return evAccepted
	}
	if errors.Is(err, errNoServerClient) {
		return evFinal
	}
	var se *StatusError
	if errors.As(err, &se) {
		if se.StatusCode >= 500 || se.StatusCode == 429 {
			return evRetry
		}
		return evFinal
	}
	// Transport errors and the per-attempt timeout (the parent is alive).
	return evRetry
}

// eventErrText is what a Debug line may carry of an error: a StatusError is
// reduced to its status code, never its body.
func eventErrText(err error) string {
	var se *StatusError
	if errors.As(err, &se) {
		return fmt.Sprintf("status %d", se.StatusCode)
	}
	return err.Error()
}

// updateSink returns the function that sends one attempt, or nil when there is
// no server to send to (no attempt is made).
func (a *Agent) updateSink() func(context.Context, *models.AgentEvent) error {
	if a.updateEventSink != nil {
		return a.updateEventSink
	}
	deviceID := a.config.Agent.DeviceID
	if deviceID == "" || a.client == nil {
		return nil
	}
	return func(ctx context.Context, evt *models.AgentEvent) error {
		return a.client.ReportEvent(ctx, deviceID, evt)
	}
}

// tryUpdateEvent makes one attempt under its own timeout (Q-7).
func (a *Agent) tryUpdateEvent(parent context.Context, sink func(context.Context, *models.AgentEvent) error, evt *models.AgentEvent, timeout time.Duration) eventVerdict {
	ctx, cancel := context.WithTimeout(parent, timeout)
	err := sink(ctx, evt)
	cancel()
	v := classifyEventSendErr(parent, err)
	if v != evAccepted {
		slog.Debug("update event attempt failed", "type", evt.Type, "verdict", int(v), "error", errTextOrNone(err))
	}
	return v
}

func errTextOrNone(err error) string {
	if err == nil {
		return ""
	}
	return eventErrText(err)
}

// continueUpdateEvent runs attempts n.. of the policy, the n-th being next.
// first is the start time of the first attempt. It returns when the event is
// accepted, a final answer arrives, the agent's context ends, or the budget is
// exhausted (one Warn, FR-036).
func (a *Agent) continueUpdateEvent(parent context.Context, sink func(context.Context, *models.AgentEvent) error, evt *models.AgentEvent, pol *updateEventRetry, first time.Time, n int) {
	made := n - 1
	for ; n <= pol.maxAttempts; n++ {
		if n > 1 {
			if wait := first.Add(pol.schedule[n-1]).Sub(a.clock().Now()); wait > 0 && !a.sleep(parent, wait) {
				slog.Debug("update event sender stopped: agent context ended", "type", evt.Type)
				return
			}
			if parent.Err() != nil {
				slog.Debug("update event sender stopped: agent context ended", "type", evt.Type)
				return
			}
			if a.clock().Now().Sub(first) > pol.budget {
				break // no attempt may start later than the budget
			}
		}
		made++
		switch a.tryUpdateEvent(parent, sink, evt, pol.attemptTimeout) {
		case evAccepted, evFinal:
			return
		case evCanceled:
			slog.Debug("update event sender stopped: agent context ended", "type", evt.Type)
			return
		}
	}
	slog.Warn("agent update event not delivered", "type", evt.Type, "attempts", made)
}

// sendUpdateEvent sends one update event according to mode (AD-010).
func (a *Agent) sendUpdateEvent(ctx context.Context, evt *models.AgentEvent, mode updateDelivery) {
	sink := a.updateSink()
	if sink == nil {
		return
	}
	pol := a.updateRetry
	idx := updateEventIndex(evt.Type)

	switch mode {
	case deliverAwait:
		if pol == nil || idx < 0 || !a.updateEventSlots[idx].CompareAndSwap(false, true) {
			// One attempt in the caller's goroutine: no policy, or the type's
			// retrying sender is busy.
			timeout := models.UpdateEventAttemptTimeout
			if pol != nil {
				timeout = pol.attemptTimeout
			}
			a.tryUpdateEvent(ctx, sink, evt, timeout)
			return
		}
		defer a.updateEventSlots[idx].Store(false)
		a.continueUpdateEvent(ctx, sink, evt, pol, a.clock().Now(), 1)

	case deliverFirstSync:
		first := a.clock().Now()
		timeout := models.UpdateEventAttemptTimeout
		if pol != nil {
			timeout = pol.attemptTimeout
		}
		if a.tryUpdateEvent(ctx, sink, evt, timeout) != evRetry || pol == nil || idx < 0 {
			return
		}
		if !a.updateEventSlots[idx].CompareAndSwap(false, true) {
			slog.Debug("update event not retried: sender busy", "type", evt.Type)
			return
		}
		a.cmdWG.Add(1)
		go func() {
			defer a.cmdWG.Done()
			defer a.updateEventSlots[idx].Store(false)
			a.continueUpdateEvent(a.commandContext(), sink, evt, pol, first, 2)
		}()

	case deliverAsync:
		retrier := false
		switch {
		case idx >= 0 && a.updateEventSlots[idx].CompareAndSwap(false, true):
			retrier = true
		case idx >= 0 && a.updateEventSingles[idx].CompareAndSwap(false, true):
		default:
			// Only reachable with three concurrent events of one type, which the
			// host lock makes impossible in practice.
			slog.Debug("update event dropped: senders busy", "type", evt.Type)
			return
		}
		a.cmdWG.Add(1)
		go func() {
			defer a.cmdWG.Done()
			parent := a.commandContext()
			if !retrier || pol == nil {
				timeout := models.UpdateEventAttemptTimeout
				if pol != nil {
					timeout = pol.attemptTimeout
				}
				a.tryUpdateEvent(parent, sink, evt, timeout)
			} else {
				a.continueUpdateEvent(parent, sink, evt, pol, a.clock().Now(), 1)
			}
			if retrier {
				a.updateEventSlots[idx].Store(false)
			} else {
				a.updateEventSingles[idx].Store(false)
			}
		}()
	}
}
