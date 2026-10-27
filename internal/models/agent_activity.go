package models

import "time"

// EXPECT-007: agent-side activity reporting. This file holds the step token the
// agent reports after a power action started, the validators of the untrusted
// correlation references and structured versions that agent_update_* events
// carry (AD-004, AD-006, AD-007) and the bounds of the agent's bounded retry
// (AD-010). Everything an agent sends is untrusted input.

// StepPowerAction is the step a standalone reboot or shutdown reports after the
// power action has started, when it had waited for the dpkg lock (AD-002). It
// matches StepPattern.
const StepPowerAction = "power_action"

// MaxAgentUpdateRefBytes bounds a correlation reference (FRD 7.2).
const MaxAgentUpdateRefBytes = 64

// Bounds of the agent's retry of an update event (AD-010, FR-031, FR-032).
// Each is a hard limit, not a default.
const (
	// UpdateEventMaxAttempts is the most sending attempts per update event.
	UpdateEventMaxAttempts = 5
	// UpdateEventRetryBudget is the longest after the first attempt that an
	// attempt may start.
	UpdateEventRetryBudget = 90 * time.Second
	// UpdateEventAttemptTimeout bounds one attempt, so the budget does not
	// depend on the HTTP client's own timeout (Q-7).
	UpdateEventAttemptTimeout = 10 * time.Second
)

// UpdateEventRetrySchedule is the earliest start of each attempt, measured from
// the first. An attempt starts at the later of its offset and the end of the
// previous attempt, provided that is within UpdateEventRetryBudget.
var UpdateEventRetrySchedule = [UpdateEventMaxAttempts]time.Duration{
	0, 5 * time.Second, 15 * time.Second, 35 * time.Second, 75 * time.Second,
}

// AgentUpdateRefKind is which kind of correlation reference an update event
// carries. The kind is decided by the field the value arrived in, never by the
// value's format (AD-004).
type AgentUpdateRefKind uint8

const (
	// RefNone: no usable reference. The event is processed as EXPECT-002 does.
	RefNone AgentUpdateRefKind = iota
	// RefCommand: the value names an agent_update command (still to be checked
	// against the store).
	RefCommand
	// RefAttempt: the value is an agent-generated attempt ID.
	RefAttempt
)

// AgentUpdateRef is a parsed correlation reference.
type AgentUpdateRef struct {
	Kind  AgentUpdateRefKind
	Value string
}

// ValidRefToken reports whether s is 1 to MaxAgentUpdateRefBytes bytes of ASCII
// letters, digits and hyphens (FRD 7.2).
func ValidRefToken(s string) bool {
	if len(s) < 1 || len(s) > MaxAgentUpdateRefBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}

// ParseAgentUpdateRef classifies the two raw reference fields of an update
// event. Both non-empty means no reference, whatever their content, and is
// decided before either value is validated (FR-027). A command reference must
// also be a canonical UUID, which every command ID is.
func ParseAgentUpdateRef(commandID, attemptID string) AgentUpdateRef {
	if commandID != "" && attemptID != "" {
		return AgentUpdateRef{}
	}
	if commandID != "" {
		if ValidRefToken(commandID) && IsCanonicalUUID(commandID) {
			return AgentUpdateRef{Kind: RefCommand, Value: commandID}
		}
		return AgentUpdateRef{}
	}
	if attemptID != "" && ValidRefToken(attemptID) {
		return AgentUpdateRef{Kind: RefAttempt, Value: attemptID}
	}
	return AgentUpdateRef{}
}

// ValidAgentUpdateCommandRef reports whether c, read device-scoped by the
// caller, is a valid target of a command reference received at time at (FRD
// Section 4 definition, AD-006 step 2):
//   - action agent_update;
//   - an origin an operator or a schedule can create (IsValidOrigin). It is an
//     allow-list on purpose: every server-created origin, present or future,
//     is invalid by default (security condition C-2);
//   - dispatched;
//   - not terminal, or terminal with a finished_at at most
//     SelfUpdateAttributionWindow before at. A terminal row without finished_at
//     is invalid (C-3 c).
//
// It is a pure function and nil-safe.
func ValidAgentUpdateCommandRef(c *Command, at time.Time) bool {
	if c == nil || c.Action != ActionAgentUpdate || !IsValidOrigin(c.Origin) || c.DispatchedAt == nil {
		return false
	}
	if !CommandState(c.Status).IsTerminal() {
		return true
	}
	if c.FinishedAt == nil {
		return false
	}
	return !c.FinishedAt.Before(at.Add(-SelfUpdateAttributionWindow))
}

// StructuredSelfUpdateVersions validates the structured versions of an
// agent_update_started event (AD-007, FRD 7.2): both must be valid versions and
// must differ. ok is false otherwise, and the caller falls back to the message.
func StructuredSelfUpdateVersions(from, to string) (string, string, bool) {
	if !ValidAgentVersion(from) || !ValidAgentVersion(to) || AgentVersionsMatch(from, to) {
		return "", "", false
	}
	return from, to, true
}
