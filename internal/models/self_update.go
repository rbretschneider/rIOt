package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// EXPECT-002: agent self-update as expected activity. This file holds the
// parser and validators of the untrusted start message (AD-011), the constants
// of the bounds (AD-010), the param keys, step and failure-step tokens, the
// value types the repository, the commands service and the tracker share, the
// message renderers (AD-012) and the severity clamp (AD-015).

// Bounds of the self-update path (AD-010, AD-013). Each is a hard limit, not a
// default: the 10-minute bound is also a database CHECK.
const (
	// SelfUpdateAttributionWindow is how long after an agent_update command
	// finished a start is still attributed to it (FR-004), and how far back a
	// completed or failed event looks for its record (FR-014).
	SelfUpdateAttributionWindow = 10 * time.Minute
	// SelfUpdateRateCap is the most records one device may create in any
	// SelfUpdateRateWindow (FR-007). It bounds the abuse budget (about 57 minutes
	// of unalerted offline time per device per day).
	SelfUpdateRateCap    = 6
	SelfUpdateRateWindow = 24 * time.Hour
	// SelfUpdateStuckAfter is how long after its bound a still-unresolved record
	// stays before it is made visible (C-4) and may be retired by the next start.
	SelfUpdateStuckAfter = time.Hour
	// SelfUpdateExecDeadlineS is exec_deadline_s of every record (the CHECK
	// allows at most 600).
	SelfUpdateExecDeadlineS = 600
	// MaxSelfUpdateMessageBytes and MaxSelfUpdateRawBytes bound the parse work
	// (AD-011, X-10).
	MaxSelfUpdateMessageBytes = 256
	MaxSelfUpdateRawBytes     = 4096
	// MaxAgentVersionBytes bounds a version string (FRD 7.2).
	MaxAgentVersionBytes = 32
	// SelfUpdateReasonBytes bounds the agent's failure reason in a server event
	// (NFR-010).
	SelfUpdateReasonBytes = 200
	// SelfUpdateResultMsgBytes bounds the server-rendered result_msg (AD-003).
	SelfUpdateResultMsgBytes = 512
)

// Params keys of a self-update record (server-owned).
const (
	SelfUpdateParamFrom = "from_version"
	SelfUpdateParamTo   = "to_version"
)

// Step tokens of a self-update record (all match StepPattern).
const (
	SelfUpdateStepWaitingForRestart      = "waiting_for_restart"
	SelfUpdateStepWaitingForConfirmation = "waiting_for_confirmation"
	SelfUpdateStepConfirmed              = "confirmed"
)

// Failure-step tokens of a self-update record (all match StepPattern).
const (
	SelfUpdateFailureAgentFailed        = "agent_failed"
	SelfUpdateFailureWrongVersion       = "wrong_version"
	SelfUpdateFailureUnconfirmedOnline  = "unconfirmed_online"
	SelfUpdateFailureUnconfirmedOffline = "unconfirmed_offline"
	SelfUpdateFailureUnresolved         = "unresolved"
)

// SelfUpdateActorPhrase is who the dashboard says started a self-update.
const SelfUpdateActorPhrase = "by agent auto-update"

const selfUpdateStartPrefix = "Agent update started: "
const selfUpdateArrow = " → "

var agentVersionPattern = regexp.MustCompile(`^v?[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}([-+][A-Za-z0-9.-]{1,16})?$`)

// ValidAgentVersion reports whether v is a version in the only accepted form
// (FRD 7.2): 1 to 32 bytes, an optional leading "v", three dot-separated groups
// of 1 to 5 digits, and an optional "-" or "+" suffix of 1 to 16 letters, digits,
// dots or hyphens.
func ValidAgentVersion(v string) bool {
	return len(v) >= 1 && len(v) <= MaxAgentVersionBytes && agentVersionPattern.MatchString(v)
}

// AgentVersionsMatch reports whether two versions are equal after removing one
// leading "v" from each.
func AgentVersionsMatch(a, b string) bool {
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}

// ParseSelfUpdateStart parses the message of an agent_update_started event as
// untrusted input (AD-011). ok is false for anything that is not exactly
// "Agent update started: <from> → <to>" with two valid, different versions.
func ParseSelfUpdateStart(raw string) (from, to string, ok bool) {
	if len(raw) > MaxSelfUpdateRawBytes {
		return "", "", false
	}
	s := strings.TrimSpace(raw)
	if len(s) > MaxSelfUpdateMessageBytes || !utf8.ValidString(s) {
		return "", "", false
	}
	for _, r := range s {
		if unsafeNameRune(r) {
			return "", "", false
		}
	}
	rest, found := strings.CutPrefix(s, selfUpdateStartPrefix)
	if !found || strings.Count(rest, selfUpdateArrow) != 1 {
		return "", "", false
	}
	from, to, _ = strings.Cut(rest, selfUpdateArrow)
	if !ValidAgentVersion(from) || !ValidAgentVersion(to) || AgentVersionsMatch(from, to) {
		return "", "", false
	}
	return from, to, true
}

// SelfUpdateParams is the params map of a new record.
func SelfUpdateParams(from, to string) map[string]interface{} {
	return map[string]interface{}{SelfUpdateParamFrom: from, SelfUpdateParamTo: to}
}

// SelfUpdateVersions reads the versions of a self-update row and re-validates
// them (implementation note 9): a value that is not a valid version yields
// ok=false, so nothing unvalidated reaches a label or a message.
func SelfUpdateVersions(c Command) (from, to string, ok bool) {
	from, _ = c.Params[SelfUpdateParamFrom].(string)
	to, _ = c.Params[SelfUpdateParamTo].(string)
	if !ValidAgentVersion(from) || !ValidAgentVersion(to) {
		return "", "", false
	}
	return from, to, true
}

// SelfUpdateActionLabel is "Agent update <from> → <to>".
func SelfUpdateActionLabel(from, to string) string {
	return fmt.Sprintf("Agent update %s%s%s", from, selfUpdateArrow, to)
}

// SafeText makes untrusted text safe for an event message: SafeName's algorithm
// with a byte limit. SafeName is SafeText with ExpectNameMaxBytes.
func SafeText(s string, max int) string {
	s = SanitizeText(s)
	clean := true
	for _, r := range s {
		if unsafeNameRune(r) {
			clean = false
			break
		}
	}
	if !clean {
		var b strings.Builder
		b.Grow(len(s))
		for _, r := range s {
			if !unsafeNameRune(r) {
				b.WriteRune(r)
			}
		}
		s = b.String()
	}
	return TruncateHead(s, max)
}

func unsafeNameRune(r rune) bool {
	return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// ---------------------------------------------------------------------------
// Value types
// ---------------------------------------------------------------------------

// SelfUpdateStart is a validated start the server received (server time At).
type SelfUpdateStart struct {
	DeviceID, Hostname, From, To string
	At                           time.Time
}

// SelfUpdateDisposition is what CreateSelfUpdate did with a start.
type SelfUpdateDisposition string

const (
	// SelfUpdateCreated: a new record was inserted; the event links to it.
	SelfUpdateCreated SelfUpdateDisposition = "created"
	// SelfUpdateAttributedCommand: an agent_update command owns this start (FR-004).
	SelfUpdateAttributedCommand SelfUpdateDisposition = "attributed_command"
	// SelfUpdateOpenRecord: the device already has an open record (FR-006).
	SelfUpdateOpenRecord SelfUpdateDisposition = "open_record"
	// SelfUpdateCapped: the rate cap refused the start (FR-007); no link.
	SelfUpdateCapped SelfUpdateDisposition = "capped"
)

// SelfUpdateCreateResult is the outcome of CreateSelfUpdate.
type SelfUpdateCreateResult struct {
	Disposition SelfUpdateDisposition
	// Command is the created row (SelfUpdateCreated).
	Command *Command
	// Retired is a stale open row retired in the same transaction (C-4); the
	// caller broadcasts it.
	Retired *Command
	// LinkID is the command the start event links to ("" for capped).
	LinkID string
	// CapEvent is the one rate-cap warning event written by this call, if any.
	CapEvent *Event
}

// SelfUpdateOutcome is how the tracker resolves a record at its bound.
type SelfUpdateOutcome string

const (
	// SelfUpdateConfirmed ends the record succeeded.
	SelfUpdateConfirmed SelfUpdateOutcome = "confirmed"
	// SelfUpdateUnconfirmedOnline ends it timed_out, the device online.
	SelfUpdateUnconfirmedOnline SelfUpdateOutcome = "unconfirmed_online"
	// SelfUpdateUnconfirmedOffline ends it timed_out, the device offline.
	SelfUpdateUnconfirmedOffline SelfUpdateOutcome = "unconfirmed_offline"
)

// State is the command state the outcome moves a record to.
func (o SelfUpdateOutcome) State() (CommandState, bool) {
	switch o {
	case SelfUpdateConfirmed:
		return CommandSucceeded, true
	case SelfUpdateUnconfirmedOnline, SelfUpdateUnconfirmedOffline:
		return CommandTimedOut, true
	}
	return "", false
}

// FailureStep is the failure_step the outcome writes ("" for a success).
func (o SelfUpdateOutcome) FailureStep() string {
	switch o {
	case SelfUpdateUnconfirmedOnline:
		return SelfUpdateFailureUnconfirmedOnline
	case SelfUpdateUnconfirmedOffline:
		return SelfUpdateFailureUnconfirmedOffline
	}
	return ""
}

// CommandEvents is the body of GET /api/v1/devices/{id}/commands/{commandId}/events
// (Section 6.3, C-3): server-authored events are never truncated by agent
// volume.
type CommandEvents struct {
	ServerEvents        []Event `json:"server_events"`
	ServerEventsOmitted int     `json:"server_events_omitted"`
	AgentEvents         []Event `json:"agent_events"`
	AgentEventsOmitted  int     `json:"agent_events_omitted"`
}

// Limits of the events endpoint (Section 6.3).
const (
	CommandEventsServerLimit = 500
	CommandEventsAgentLimit  = 50
)

// IsServerAuthoredEvent classifies a linked event (Section 6.3): server-authored
// when its command_attr is expected, escalation or outcome, or its type is one of
// the three agent_self_update_* types.
func IsServerAuthoredEvent(e Event) bool {
	switch e.CommandAttr {
	case CommandAttrExpected, CommandAttrEscalation, CommandAttrOutcome:
		return true
	}
	return IsSelfUpdateServerEventType(e.Type)
}

// IsSelfUpdateServerEventType reports whether t is one of the three server-
// written self-update event types.
func IsSelfUpdateServerEventType(t EventType) bool {
	switch t {
	case EventAgentSelfUpdateFailed, EventAgentSelfUpdateUnconfirmed, EventAgentSelfUpdatePaused:
		return true
	}
	return false
}

// IsAgentUpdateEventType reports whether t is one of the four agent update
// event types whose severity is clamped (AD-015).
func IsAgentUpdateEventType(t EventType) bool {
	switch t {
	case EventAgentUpdateAvail, EventAgentUpdateStarted, EventAgentUpdateCompleted, EventAgentUpdateFailed:
		return true
	}
	return false
}

// ClampAgentUpdateSeverity limits the agent-supplied severity of an
// agent_update_* event to the known values (AD-015, X-19): info, warning and
// critical are kept; anything else becomes info (available, started, completed)
// or warning (failed). Other event types are returned unchanged.
func ClampAgentUpdateSeverity(t EventType, sev EventSeverity) EventSeverity {
	if !IsAgentUpdateEventType(t) {
		return sev
	}
	switch sev {
	case SeverityInfo, SeverityWarning, SeverityCrit:
		return sev
	}
	if t == EventAgentUpdateFailed {
		return SeverityWarning
	}
	return SeverityInfo
}

// ---------------------------------------------------------------------------
// Messages (AD-012). <host> is SafeName(hostname) or "device"; versions are
// validated; every text is at most SelfUpdateResultMsgBytes by construction
// (host 128, versions 32 each, reason 200).
// ---------------------------------------------------------------------------

func selfUpdateHost(hostname string) string {
	if h := SafeName(strings.TrimSpace(hostname)); h != "" {
		return h
	}
	return "device"
}

func selfUpdateLabelPhrase(from, to string) string {
	return SelfUpdateActionLabel(from, to) + " " + SelfUpdateActorPhrase
}

// SelfUpdateFailedMessage is FR-018: the agent reported the update failed.
func SelfUpdateFailedMessage(hostname, from, to, reason string) string {
	return fmt.Sprintf("%s failed on %s: %s", selfUpdateLabelPhrase(from, to), selfUpdateHost(hostname), SafeText(reason, SelfUpdateReasonBytes))
}

// SelfUpdateWrongVersionMessage is FR-019: the device came back on another
// version. reported is shown only when it is a valid version (X-4).
func SelfUpdateWrongVersionMessage(hostname, reported, from, to string) string {
	return fmt.Sprintf("%s came back on agent %s after %s (expected %s)",
		selfUpdateHost(hostname), reportedVersionText(reported), selfUpdateLabelPhrase(from, to), to)
}

// SelfUpdateNotBackMessage is FR-020: still offline at the bound.
func SelfUpdateNotBackMessage(hostname, from, to, id8 string) string {
	return fmt.Sprintf("%s still offline %s after %s (record %s)",
		selfUpdateHost(hostname), FormatDurationShort(ExpectSelfUpdateBound), selfUpdateLabelPhrase(from, to), id8)
}

// SelfUpdateOfflineAfterFailureMessage is X-7: the update failed and the device
// is offline.
func SelfUpdateOfflineAfterFailureMessage(hostname, from, to, id8 string) string {
	return fmt.Sprintf("%s is offline: %s failed (record %s)", selfUpdateHost(hostname), selfUpdateLabelPhrase(from, to), id8)
}

// SelfUpdateNotConfirmedMessage is FR-021: not confirmed within the bound while
// the device is online. stored is the device's stored agent version.
func SelfUpdateNotConfirmedMessage(hostname, from, to, stored string) string {
	return fmt.Sprintf("%s on %s was not confirmed within %s (agent reports %s)",
		selfUpdateLabelPhrase(from, to), selfUpdateHost(hostname), FormatDurationShort(ExpectSelfUpdateBound), reportedVersionText(stored))
}

// SelfUpdateRateCapMessage is FR-007.
func SelfUpdateRateCapMessage(hostname string) string {
	return fmt.Sprintf("Agent self-update tracking paused on %s: more than %d agent updates started in %s; offline alerts during agent updates are not suppressed",
		selfUpdateHost(hostname), SelfUpdateRateCap, FormatDurationShort(SelfUpdateRateWindow))
}

// SelfUpdateStuckMessage is the C-4 text for a record unresolved 1h after its bound.
func SelfUpdateStuckMessage(hostname, id8, from, to string) string {
	return fmt.Sprintf("Agent self-update tracking paused on %s: record %s (%s) could not be completed %s after its bound; the next agent update replaces it",
		selfUpdateHost(hostname), id8, SelfUpdateActionLabel(from, to), FormatDurationShort(SelfUpdateStuckAfter))
}

// SelfUpdateConfirmedMessage is the result_msg of a confirmed record.
func SelfUpdateConfirmedMessage(how, version string) string {
	return fmt.Sprintf("confirmed: the agent %s %s", how, reportedVersionText(version))
}

// SelfUpdateUnconfirmedOfflineMessage is the result_msg of a record whose
// device was offline at the bound.
func SelfUpdateUnconfirmedOfflineMessage(hostname string) string {
	return fmt.Sprintf("not confirmed: %s was offline %s after the start", selfUpdateHost(hostname), FormatDurationShort(ExpectSelfUpdateBound))
}

// SelfUpdateCompletedMessage is the result_msg of a record the agent reported
// completed.
func SelfUpdateCompletedMessage(agentMessage string) string {
	return "completed: " + SafeText(agentMessage, SelfUpdateReasonBytes)
}

// SelfUpdateStaleResultMessage is the result_msg of a record retired as stale.
const SelfUpdateStaleResultMessage = "not resolved within 1h after the bound"

func reportedVersionText(v string) string {
	if ValidAgentVersion(v) {
		return v
	}
	return "an unrecognised version"
}
