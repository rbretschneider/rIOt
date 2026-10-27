package models

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// CommandState is a canonical command lifecycle state (CMD-LIFECYCLE-001,
// AD-001). The database stores these values in commands.status. Rows written
// before migration 000023 may still hold the legacy values "pending", "sent",
// "success" and "error"; CanonicalStatus maps them.
type CommandState string

// The ten canonical states (FR-071).
const (
	CommandQueued       CommandState = "queued"
	CommandDispatched   CommandState = "dispatched"
	CommandAcknowledged CommandState = "acknowledged"
	CommandRunning      CommandState = "running"
	CommandVerifying    CommandState = "verifying"
	CommandSucceeded    CommandState = "succeeded"
	CommandFailed       CommandState = "failed"
	CommandTimedOut     CommandState = "timed_out"
	CommandCancelled    CommandState = "cancelled"
	CommandExpired      CommandState = "expired"
)

// Typed legacy aliases that can still be stored in commands.status. They exist
// so guard lists (SQLStatusList) can name them without a string conversion.
// "success" and "error" are never part of a guard list: both are terminal and
// therefore never a valid "from" state.
const (
	LegacyPending CommandState = "pending" // alias of CommandQueued
	LegacySent    CommandState = "sent"    // alias of CommandDispatched
)

// Legacy and wire status strings. CommandResult.Status always uses
// ResultStatusSuccess / ResultStatusError (FR-081).
const (
	ResultStatusSuccess = "success"
	ResultStatusError   = "error"
)

// CommandActor identifies who requests a state transition.
type CommandActor int

const (
	// ActorServer is rIOt server internals: claim, expiry, sweeper, revert.
	ActorServer CommandActor = iota + 1
	// ActorAgent is a report authenticated as the owning device.
	ActorAgent
	// ActorOperator is the authenticated dashboard user (cancel).
	ActorOperator
	// ActorSelfUpdate is the server resolving an agent self-update record
	// (EXPECT-002 AD-003): the only actor whose UPDATE can match such a row, and
	// it can match nothing else. Its edges are the seven below.
	ActorSelfUpdate
)

func (a CommandActor) String() string {
	switch a {
	case ActorServer:
		return "server"
	case ActorAgent:
		return "agent"
	case ActorOperator:
		return "operator"
	case ActorSelfUpdate:
		return "self_update"
	default:
		return fmt.Sprintf("actor(%d)", int(a))
	}
}

type actorSet uint8

const (
	bitServer actorSet = 1 << iota
	bitAgent
	bitOperator
	bitSelfUpdate
)

func (a CommandActor) bit() actorSet {
	switch a {
	case ActorServer:
		return bitServer
	case ActorAgent:
		return bitAgent
	case ActorOperator:
		return bitOperator
	case ActorSelfUpdate:
		return bitSelfUpdate
	default:
		return 0
	}
}

// canonicalOrder is the lifecycle order. It is the iteration order for every
// derived list, so output is deterministic.
var canonicalOrder = [...]CommandState{
	CommandQueued, CommandDispatched, CommandAcknowledged, CommandRunning,
	CommandVerifying, CommandSucceeded, CommandFailed, CommandTimedOut,
	CommandCancelled, CommandExpired,
}

// AllCommandStates returns the ten canonical states in lifecycle order.
func AllCommandStates() []CommandState {
	out := make([]CommandState, len(canonicalOrder))
	copy(out, canonicalOrder[:])
	return out
}

// transitions encodes the FRD 7.2 table exactly. Any pair that is not listed is
// disallowed. running -> running is allowed; verifying -> verifying is not.
var transitions = buildTransitions()

func buildTransitions() map[CommandState]map[CommandState]actorSet {
	t := map[CommandState]map[CommandState]actorSet{}
	add := func(from CommandState, to CommandState, actors actorSet) {
		if t[from] == nil {
			t[from] = map[CommandState]actorSet{}
		}
		t[from][to] |= actors
	}

	// Server edges.
	add(CommandQueued, CommandDispatched, bitServer)
	add(CommandQueued, CommandExpired, bitServer)
	add(CommandDispatched, CommandQueued, bitServer) // FR-085 revert
	for _, from := range []CommandState{CommandDispatched, CommandAcknowledged, CommandRunning, CommandVerifying} {
		add(from, CommandTimedOut, bitServer)
	}

	// Operator edge.
	add(CommandQueued, CommandCancelled, bitOperator)

	// Self-update edges (EXPECT-002 AD-003): a record starts in running, the
	// registration moves it to verifying, and the outcome ends it.
	add(CommandRunning, CommandVerifying, bitSelfUpdate)
	for _, to := range []CommandState{CommandSucceeded, CommandFailed, CommandTimedOut} {
		add(CommandRunning, to, bitSelfUpdate)
		add(CommandVerifying, to, bitSelfUpdate)
	}

	// Agent edges. queued and dispatched accept every agent report: a report
	// is proof of delivery (queued only after an FR-085 revert).
	for _, from := range []CommandState{CommandQueued, CommandDispatched} {
		add(from, CommandAcknowledged, bitAgent)
	}
	for _, from := range []CommandState{CommandQueued, CommandDispatched, CommandAcknowledged, CommandRunning} {
		add(from, CommandRunning, bitAgent)
		add(from, CommandVerifying, bitAgent)
	}
	for _, from := range []CommandState{CommandQueued, CommandDispatched, CommandAcknowledged, CommandRunning, CommandVerifying, CommandTimedOut} {
		add(from, CommandSucceeded, bitAgent)
		add(from, CommandFailed, bitAgent)
	}
	return t
}

// canonicalize maps any known stored or typed value (canonical, legacy alias,
// or legacy terminal) to its canonical state. ok is false for unknown values.
func canonicalize(s string) (CommandState, bool) {
	switch s {
	case string(CommandQueued), string(LegacyPending):
		return CommandQueued, true
	case string(CommandDispatched), string(LegacySent):
		return CommandDispatched, true
	case string(CommandAcknowledged):
		return CommandAcknowledged, true
	case string(CommandRunning):
		return CommandRunning, true
	case string(CommandVerifying):
		return CommandVerifying, true
	case string(CommandSucceeded), ResultStatusSuccess:
		return CommandSucceeded, true
	case string(CommandFailed), ResultStatusError:
		return CommandFailed, true
	case string(CommandTimedOut):
		return CommandTimedOut, true
	case string(CommandCancelled):
		return CommandCancelled, true
	case string(CommandExpired):
		return CommandExpired, true
	}
	return "", false
}

// CanTransition reports whether actor may move a command from one state to
// another. Legacy aliases ("pending", "sent") are accepted for either side and
// treated as their canonical state. Unknown states and unknown actors are never
// allowed.
func CanTransition(from, to CommandState, actor CommandActor) bool {
	f, ok := canonicalize(string(from))
	if !ok {
		return false
	}
	t, ok := canonicalize(string(to))
	if !ok {
		return false
	}
	return transitions[f][t]&actor.bit() != 0
}

// legacyAlias returns the typed legacy alias stored for a canonical state, if
// there is one that may appear in a guard list.
func legacyAlias(s CommandState) (CommandState, bool) {
	switch s {
	case CommandQueued:
		return LegacyPending, true
	case CommandDispatched:
		return LegacySent, true
	}
	return "", false
}

// ExpandAliases returns states with each queued/dispatched followed by its
// legacy alias (pending/sent), de-duplicated, in input order. Use it before
// SQLStatusList when a guard must also match rows stored with legacy values.
func ExpandAliases(states ...CommandState) []CommandState {
	seen := map[CommandState]bool{}
	out := make([]CommandState, 0, len(states)+2)
	push := func(s CommandState) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range states {
		push(s)
		if alias, ok := legacyAlias(s); ok {
			push(alias)
		}
	}
	return out
}

// AllowedFrom returns every state (canonical plus legacy alias) from which
// actor may move a command to `to`, in lifecycle order. The result is the
// WHERE-guard of the repository's Transition. An empty result means the target
// is unreachable for that actor.
func AllowedFrom(to CommandState, actor CommandActor) []CommandState {
	t, ok := canonicalize(string(to))
	if !ok {
		return nil
	}
	var canon []CommandState
	for _, from := range canonicalOrder {
		if transitions[from][t]&actor.bit() != 0 {
			canon = append(canon, from)
		}
	}
	if len(canon) == 0 {
		return nil
	}
	return ExpandAliases(canon...)
}

var (
	unknownStatusMu     sync.Mutex
	unknownStatusLogged = map[string]struct{}{}
)

// maxUnknownStatusLogged bounds the dedupe set; past it unknown values map to
// failed silently.
const maxUnknownStatusLogged = 64

// CanonicalStatus maps a stored status string to its canonical state per
// FR-083: pending->queued, sent->dispatched, success->succeeded,
// error->failed. Canonical values map to themselves. An unknown value maps to
// CommandFailed and logs one Warn per distinct value.
func CanonicalStatus(stored string) CommandState {
	if s, ok := canonicalize(stored); ok {
		return s
	}
	warnUnknownStatus(stored)
	return CommandFailed
}

func warnUnknownStatus(stored string) {
	unknownStatusMu.Lock()
	defer unknownStatusMu.Unlock()
	if _, dup := unknownStatusLogged[stored]; dup || len(unknownStatusLogged) >= maxUnknownStatusLogged {
		return
	}
	unknownStatusLogged[stored] = struct{}{}
	shown := stored
	if len(shown) > 64 {
		shown = TruncateHead(shown, 64)
	}
	slog.Warn("unknown command status mapped to failed", "status", SanitizeText(shown))
}

// LegacyStatus returns the value a pre-lifecycle server understands for a
// state (used by the down migration and any legacy-facing surface):
// succeeded->success; failed, timed_out, cancelled, expired->error;
// queued->queued; dispatched, acknowledged, running, verifying->sent.
// Legacy aliases are canonicalized first. An unknown state yields "error".
func LegacyStatus(s CommandState) string {
	c, ok := canonicalize(string(s))
	if !ok {
		return ResultStatusError
	}
	switch c {
	case CommandSucceeded:
		return ResultStatusSuccess
	case CommandQueued:
		return string(CommandQueued)
	case CommandDispatched, CommandAcknowledged, CommandRunning, CommandVerifying:
		return string(LegacySent)
	default:
		return ResultStatusError
	}
}

// IsTerminal reports succeeded, failed, cancelled, expired and timed_out.
// timed_out is terminal for conflicts, FR-007 and the UI, but is not immutable.
func (s CommandState) IsTerminal() bool {
	c, ok := canonicalize(string(s))
	if !ok {
		return false
	}
	switch c {
	case CommandSucceeded, CommandFailed, CommandCancelled, CommandExpired, CommandTimedOut:
		return true
	}
	return false
}

// IsImmutable reports succeeded, failed, cancelled and expired: no actor can
// ever change them (FR-073). timed_out accepts one late agent result.
func (s CommandState) IsImmutable() bool {
	c, ok := canonicalize(string(s))
	if !ok {
		return false
	}
	switch c {
	case CommandSucceeded, CommandFailed, CommandCancelled, CommandExpired:
		return true
	}
	return false
}

// IsActive reports queued, dispatched, acknowledged, running and verifying:
// the non-terminal states that hold a target conflict key (BR-008).
func (s CommandState) IsActive() bool {
	c, ok := canonicalize(string(s))
	if !ok {
		return false
	}
	return !c.IsTerminal()
}

// Canonical returns the canonical form of a (possibly legacy) typed state, or
// "" when the value is not a known state. It never logs.
func (s CommandState) Canonical() CommandState {
	c, _ := canonicalize(string(s))
	return c
}

// ActiveStates returns the five non-terminal canonical states.
func ActiveStates() []CommandState {
	return []CommandState{CommandQueued, CommandDispatched, CommandAcknowledged, CommandRunning, CommandVerifying}
}

// TerminalStates returns the five terminal canonical states.
func TerminalStates() []CommandState {
	return []CommandState{CommandSucceeded, CommandFailed, CommandTimedOut, CommandCancelled, CommandExpired}
}

// SQLStatusList renders states as a quoted SQL literal list, for example
// 'queued','pending'. It exists so partial indexes can match (a bind parameter
// would defeat them).
//
// SECURITY (SEC C-6): it accepts only the typed CommandState constants. It
// panics on any value that is not one of the 12 known constants (the ten
// canonical states plus LegacyPending and LegacySent). It must never be called
// with request data; request filters go through ParseStatusFilter and are
// bound as parameters.
func SQLStatusList(states ...CommandState) string {
	if len(states) == 0 {
		panic("models.SQLStatusList: empty status list")
	}
	var b strings.Builder
	for i, s := range states {
		if !isKnownLiteral(s) {
			panic(fmt.Sprintf("models.SQLStatusList: refusing unknown status literal (len %d)", len(s)))
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('\'')
		b.WriteString(string(s))
		b.WriteByte('\'')
	}
	return b.String()
}

func isKnownLiteral(s CommandState) bool {
	switch s {
	case CommandQueued, CommandDispatched, CommandAcknowledged, CommandRunning,
		CommandVerifying, CommandSucceeded, CommandFailed, CommandTimedOut,
		CommandCancelled, CommandExpired, LegacyPending, LegacySent:
		return true
	}
	return false
}

// filterExpansion maps every accepted status filter token to the stored
// values it matches. Legacy tokens are accepted as aliases of the canonical
// state they map to under FR-083.
var filterExpansion = map[string][]string{
	"queued":       {"queued", "pending"},
	"pending":      {"queued", "pending"},
	"dispatched":   {"dispatched", "sent"},
	"sent":         {"dispatched", "sent"},
	"acknowledged": {"acknowledged"},
	"running":      {"running"},
	"verifying":    {"verifying"},
	"succeeded":    {"succeeded", "success"},
	"success":      {"succeeded", "success"},
	"failed":       {"failed", "error"},
	"error":        {"failed", "error"},
	"timed_out":    {"timed_out"},
	"cancelled":    {"cancelled"},
	"expired":      {"expired"},
}

// MaxStatusFilterValues bounds how many comma-separated tokens a status filter
// may carry (the allowlist has 14 distinct tokens).
const MaxStatusFilterValues = 14

// ParseStatusFilterValue validates one request status token against the
// canonical-plus-legacy allowlist and returns the stored values it matches
// (alias-expanded). ok is false for anything else, including empty strings,
// different case and surrounding whitespace. The returned slice is a copy and
// is meant to be bound as status = ANY($n::text[]) (SEC C-6).
func ParseStatusFilterValue(v string) (stored []string, ok bool) {
	exp, ok := filterExpansion[v]
	if !ok {
		return nil, false
	}
	out := make([]string, len(exp))
	copy(out, exp)
	return out, true
}

// ParseStatusFilter parses a comma-separated status filter. An empty string
// means "no filter" and returns (nil, true). Otherwise every token must pass
// ParseStatusFilterValue (an empty token, such as a trailing comma, is invalid)
// and the union is returned de-duplicated in first-seen order. ok is false when
// any token is unknown or there are more than MaxStatusFilterValues tokens; the
// handler answers 400 invalid_filter.
func ParseStatusFilter(csv string) (stored []string, ok bool) {
	if csv == "" {
		return nil, true
	}
	tokens := strings.Split(csv, ",")
	if len(tokens) > MaxStatusFilterValues {
		return nil, false
	}
	seen := map[string]bool{}
	for _, tok := range tokens {
		vals, ok := ParseStatusFilterValue(tok)
		if !ok {
			return nil, false
		}
		for _, v := range vals {
			if !seen[v] {
				seen[v] = true
				stored = append(stored, v)
			}
		}
	}
	return stored, true
}
