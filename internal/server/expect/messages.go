package expect

import (
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Message templates of the escalations and the warning events the tracker
// writes itself (AD-015). The templates of the events the generator stores
// (suppressed death, suppressed offline, powered off, online after a window,
// OOM) live next to their types in models/expect.go. Every agent-sourced part
// (hostname, container key, actor phrase) is already SafeName-clean.

// outcomeWord is the command outcome in an escalation message.
func outcomeWord(s models.CommandState) string {
	switch s {
	case models.CommandSucceeded:
		return "succeeded"
	case models.CommandFailed:
		return "failed"
	case models.CommandTimedOut:
		return "timed out"
	case models.CommandCancelled:
		return "cancelled"
	case models.CommandExpired:
		return "expired"
	}
	return "no result"
}

// deviceEscalationMessage renders the FR-020 / FR-021 texts. total is E + P:
// how long after dispatch the window waited for a result.
func deviceEscalationMessage(kind models.EscalationKind, host string, w *window, actor string, total time.Duration) string {
	label := models.ExpectActionLabel(w.cmd.Action)
	id8 := models.ShortCommandID(w.cmd.ID)
	switch kind {
	case models.EscDeviceNonSuccess:
		return fmt.Sprintf("%s is offline: %s %s %s (command %s)", host, label, actor, outcomeWord(w.cmd.State()), id8)
	case models.EscDeviceNoResult:
		return fmt.Sprintf("%s is offline: %s %s sent no result within %s (command %s)", host, label, actor, models.FormatDurationShort(total), id8)
	}
	return fmt.Sprintf("%s still offline %s after %s %s (command %s)", host, models.FormatDurationShort(w.policy.PostTerminal), label, actor, id8)
}

// containerEscalationMessage renders the FR-022 text; since is the period the
// container had to come back (2m, 3m or 15m).
func containerEscalationMessage(key string, since time.Duration, w *window, actor string) string {
	return fmt.Sprintf("Container %s still not running %s after %s %s (%s, command %s)",
		key, models.FormatDurationShort(since), models.ExpectActionLabel(w.cmd.Action), actor,
		outcomeWord(w.cmd.State()), models.ShortCommandID(w.cmd.ID))
}

// containerSummaryMessage renders the FR-025 summary.
func containerSummaryMessage(n int, since time.Duration, w *window, actor string) string {
	return fmt.Sprintf("%d more containers still not running %s after %s %s (%s, command %s)",
		n, models.FormatDurationShort(since), models.ExpectActionLabel(w.cmd.Action), actor,
		outcomeWord(w.cmd.State()), models.ShortCommandID(w.cmd.ID))
}

// heldSummaryMessage renders the SEC-005 summary push.
func heldSummaryMessage(n int, host string, since time.Time) string {
	return fmt.Sprintf("%d escalation notifications for %s were held since %s; see Alerts", n, host, since.Local().Format("15:04"))
}

// overflowMessage renders the SEC-002 (c) warning event.
func overflowMessage(n int, host string) string {
	return fmt.Sprintf("%d expected-activity windows on %s exceed the tracking limit; their suppression is off and their checks are delayed", n, host)
}

// deletedMessage renders the SEC-002 warning for a command row that vanished.
func deletedMessage(w *window, actor, host string) string {
	return fmt.Sprintf("Checks for %s %s on %s were skipped: the command record was removed (command %s)",
		models.ExpectActionLabel(w.cmd.Action), actor, host, models.ShortCommandID(w.cmd.ID))
}

// lapsedMessage renders the AD-011 warning for a deferred container check that
// never ran.
func lapsedMessage(w *window, actor, host string) string {
	return fmt.Sprintf("Container checks for %s %s on %s were not run: the device did not return within 24h (command %s)",
		models.ExpectActionLabel(w.cmd.Action), actor, host, models.ShortCommandID(w.cmd.ID))
}
