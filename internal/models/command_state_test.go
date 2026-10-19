package models

import (
	"bytes"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// edge is one row of the FRD 7.2 table, written out independently of the
// implementation's builder so a drift in either is caught.
type edge struct {
	from, to CommandState
	actor    CommandActor
}

// frdTransitions is FRD 7.2 verbatim (the "(new) -> queued" row has no from).
func frdTransitions() []edge {
	var e []edge
	add := func(from CommandState, actor CommandActor, tos ...CommandState) {
		for _, to := range tos {
			e = append(e, edge{from, to, actor})
		}
	}
	add(CommandQueued, ActorServer, CommandDispatched, CommandExpired)
	add(CommandQueued, ActorOperator, CommandCancelled)
	add(CommandQueued, ActorAgent, CommandAcknowledged, CommandRunning, CommandVerifying, CommandSucceeded, CommandFailed)
	add(CommandDispatched, ActorServer, CommandQueued, CommandTimedOut)
	add(CommandDispatched, ActorAgent, CommandAcknowledged, CommandRunning, CommandVerifying, CommandSucceeded, CommandFailed)
	add(CommandAcknowledged, ActorAgent, CommandRunning, CommandVerifying, CommandSucceeded, CommandFailed)
	add(CommandAcknowledged, ActorServer, CommandTimedOut)
	add(CommandRunning, ActorAgent, CommandRunning, CommandVerifying, CommandSucceeded, CommandFailed)
	add(CommandRunning, ActorServer, CommandTimedOut)
	add(CommandVerifying, ActorAgent, CommandSucceeded, CommandFailed)
	add(CommandVerifying, ActorServer, CommandTimedOut)
	add(CommandTimedOut, ActorAgent, CommandSucceeded, CommandFailed)
	return e
}

var allActors = []CommandActor{ActorServer, ActorAgent, ActorOperator}

func TestCL_AC085_TransitionTableAllPairs(t *testing.T) {
	want := map[edge]bool{}
	for _, e := range frdTransitions() {
		want[e] = true
	}
	for _, from := range AllCommandStates() {
		for _, to := range AllCommandStates() {
			for _, actor := range allActors {
				got := CanTransition(from, to, actor)
				if got != want[edge{from, to, actor}] {
					t.Errorf("CanTransition(%s -> %s, %s) = %v, FRD 7.2 says %v", from, to, actor, got, !got)
				}
			}
		}
	}
}

func TestCL_AC085_AllowedFromAgreesWithCanTransition(t *testing.T) {
	for _, to := range AllCommandStates() {
		for _, actor := range allActors {
			got := AllowedFrom(to, actor)
			inList := map[CommandState]bool{}
			for _, s := range got {
				inList[s] = true
			}
			for _, from := range AllCommandStates() {
				if inList[from] != CanTransition(from, to, actor) {
					t.Errorf("AllowedFrom(%s, %s) membership of %s = %v, CanTransition says %v",
						to, actor, from, inList[from], CanTransition(from, to, actor))
				}
			}
			// Legacy aliases ride along with their canonical state, never alone.
			if inList[CommandQueued] != inList[LegacyPending] {
				t.Errorf("AllowedFrom(%s, %s): queued=%v but pending=%v", to, actor, inList[CommandQueued], inList[LegacyPending])
			}
			if inList[CommandDispatched] != inList[LegacySent] {
				t.Errorf("AllowedFrom(%s, %s): dispatched=%v but sent=%v", to, actor, inList[CommandDispatched], inList[LegacySent])
			}
		}
	}
}

func TestCL_AC085_AllowedFromExactLists(t *testing.T) {
	cases := []struct {
		name  string
		to    CommandState
		actor CommandActor
		want  []CommandState
	}{
		{"agent result", CommandSucceeded, ActorAgent,
			[]CommandState{CommandQueued, LegacyPending, CommandDispatched, LegacySent, CommandAcknowledged, CommandRunning, CommandVerifying, CommandTimedOut}},
		{"agent ack", CommandAcknowledged, ActorAgent,
			[]CommandState{CommandQueued, LegacyPending, CommandDispatched, LegacySent}},
		{"agent running", CommandRunning, ActorAgent,
			[]CommandState{CommandQueued, LegacyPending, CommandDispatched, LegacySent, CommandAcknowledged, CommandRunning}},
		{"agent verifying", CommandVerifying, ActorAgent,
			[]CommandState{CommandQueued, LegacyPending, CommandDispatched, LegacySent, CommandAcknowledged, CommandRunning}},
		{"server claim", CommandDispatched, ActorServer, []CommandState{CommandQueued, LegacyPending}},
		{"server revert", CommandQueued, ActorServer, []CommandState{CommandDispatched, LegacySent}},
		{"server expire", CommandExpired, ActorServer, []CommandState{CommandQueued, LegacyPending}},
		{"server sweep", CommandTimedOut, ActorServer,
			[]CommandState{CommandDispatched, LegacySent, CommandAcknowledged, CommandRunning, CommandVerifying}},
		{"operator cancel", CommandCancelled, ActorOperator, []CommandState{CommandQueued, LegacyPending}},
		{"agent cannot cancel", CommandCancelled, ActorAgent, nil},
		{"unknown target", CommandState("bogus"), ActorAgent, nil},
		{"unknown actor", CommandSucceeded, CommandActor(99), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AllowedFrom(tc.to, tc.actor)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("AllowedFrom(%s, %s) = %v, want %v", tc.to, tc.actor, got, tc.want)
			}
		})
	}
}

func TestCL_AC085_RunningToRunningAllowedVerifyingToVerifyingNot(t *testing.T) {
	if !CanTransition(CommandRunning, CommandRunning, ActorAgent) {
		t.Error("running -> running must be allowed for the agent (step/progress change)")
	}
	if CanTransition(CommandVerifying, CommandVerifying, ActorAgent) {
		t.Error("verifying -> verifying must not be allowed")
	}
}

func TestCL_AC085_LegacyAliasesAcceptedAsEndpoints(t *testing.T) {
	if !CanTransition(LegacyPending, CommandDispatched, ActorServer) {
		t.Error("pending -> dispatched must behave like queued -> dispatched")
	}
	if !CanTransition(LegacySent, CommandSucceeded, ActorAgent) {
		t.Error("sent -> succeeded must behave like dispatched -> succeeded")
	}
	if CanTransition(CommandState("bogus"), CommandSucceeded, ActorAgent) {
		t.Error("an unknown from-state must never transition")
	}
}

// TestCL_SEC_NoResurrectionEdges: across all actors no edge leaves an immutable
// state, no agent edge leaves timed_out for a non-terminal state, and only the
// server may enter queued or dispatched.
func TestCL_SEC_NoResurrectionEdges(t *testing.T) {
	for _, from := range []CommandState{CommandSucceeded, CommandFailed, CommandCancelled, CommandExpired} {
		for _, to := range AllCommandStates() {
			for _, actor := range allActors {
				if CanTransition(from, to, actor) {
					t.Errorf("immutable state %s must have no outgoing edge, found %s -> %s by %s", from, from, to, actor)
				}
			}
		}
		if got := AllowedFrom(CommandQueued, ActorServer); containsState(got, from) {
			t.Errorf("immutable state %s must not appear in the revert guard: %v", from, got)
		}
	}
	for _, to := range AllCommandStates() {
		if !to.IsTerminal() && CanTransition(CommandTimedOut, to, ActorAgent) {
			t.Errorf("agent edge timed_out -> %s resurrects a timed-out command", to)
		}
		if !to.IsTerminal() && CanTransition(CommandTimedOut, to, ActorServer) {
			t.Errorf("server edge timed_out -> %s resurrects a timed-out command", to)
		}
	}
	for _, from := range AllCommandStates() {
		for _, to := range []CommandState{CommandQueued, CommandDispatched} {
			for _, actor := range []CommandActor{ActorAgent, ActorOperator} {
				if CanTransition(from, to, actor) {
					t.Errorf("%s must not move %s -> %s: only the server may enter queued/dispatched", actor, from, to)
				}
			}
		}
	}
	// Terminal exits: timed_out is the only terminal state with an exit.
	for _, from := range TerminalStates() {
		exits := 0
		for _, to := range AllCommandStates() {
			for _, actor := range allActors {
				if CanTransition(from, to, actor) {
					exits++
				}
			}
		}
		if (from == CommandTimedOut) != (exits > 0) {
			t.Errorf("terminal state %s has %d exits; only timed_out may have any", from, exits)
		}
	}
}

func containsState(list []CommandState, s CommandState) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestCL_AC101_LegacyStatusesCanonical(t *testing.T) {
	cases := []struct {
		stored string
		want   CommandState
	}{
		{"pending", CommandQueued},
		{"sent", CommandDispatched},
		{"success", CommandSucceeded},
		{"error", CommandFailed},
		{"queued", CommandQueued},
		{"dispatched", CommandDispatched},
		{"acknowledged", CommandAcknowledged},
		{"running", CommandRunning},
		{"verifying", CommandVerifying},
		{"succeeded", CommandSucceeded},
		{"failed", CommandFailed},
		{"timed_out", CommandTimedOut},
		{"cancelled", CommandCancelled},
		{"expired", CommandExpired},
	}
	for _, tc := range cases {
		t.Run(tc.stored, func(t *testing.T) {
			if got := CanonicalStatus(tc.stored); got != tc.want {
				t.Errorf("CanonicalStatus(%q) = %q, want %q", tc.stored, got, tc.want)
			}
		})
	}
}

// swapDefaultLogger captures slog.Default output for the duration of a test.
func swapDefaultLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// resetUnknownStatusDedupe empties the package-level once-per-value dedupe set
// for the duration of a test and restores it afterwards. Added by QA Engineer:
// without it the test passes only on its first run in a process and fails under
// -count=N or -shuffle (the set survives between runs).
func resetUnknownStatusDedupe(t *testing.T) {
	t.Helper()
	unknownStatusMu.Lock()
	prev := unknownStatusLogged
	unknownStatusLogged = map[string]struct{}{}
	unknownStatusMu.Unlock()
	t.Cleanup(func() {
		unknownStatusMu.Lock()
		unknownStatusLogged = prev
		unknownStatusMu.Unlock()
	})
}

func TestCL_AC101_UnknownStatusMapsToFailedWithOneWarnPerValue(t *testing.T) {
	resetUnknownStatusDedupe(t)
	buf := swapDefaultLogger(t)
	value := "weird_status_ac101_unique"
	for i := 0; i < 3; i++ {
		if got := CanonicalStatus(value); got != CommandFailed {
			t.Fatalf("CanonicalStatus(%q) = %q, want failed", value, got)
		}
	}
	if n := strings.Count(buf.String(), value); n != 1 {
		t.Errorf("want exactly one Warn for a repeated unknown value, got %d:\n%s", n, buf.String())
	}
	other := "weird_status_ac101_other"
	CanonicalStatus(other)
	if !strings.Contains(buf.String(), other) {
		t.Errorf("a second distinct unknown value must log its own Warn:\n%s", buf.String())
	}
}

func TestCL_AC101_KnownStatusNeverWarns(t *testing.T) {
	buf := swapDefaultLogger(t)
	for _, s := range []string{"pending", "sent", "success", "error", "queued", "expired"} {
		CanonicalStatus(s)
	}
	if buf.Len() != 0 {
		t.Errorf("known statuses must not log, got: %s", buf.String())
	}
}

func TestCL_AC101_LegacyStatusMapping(t *testing.T) {
	cases := map[CommandState]string{
		CommandSucceeded:      "success",
		CommandFailed:         "error",
		CommandTimedOut:       "error",
		CommandCancelled:      "error",
		CommandExpired:        "error",
		CommandQueued:         "queued",
		CommandDispatched:     "sent",
		CommandAcknowledged:   "sent",
		CommandRunning:        "sent",
		CommandVerifying:      "sent",
		LegacyPending:         "queued",
		LegacySent:            "sent",
		CommandState("bogus"): "error",
	}
	for state, want := range cases {
		if got := LegacyStatus(state); got != want {
			t.Errorf("LegacyStatus(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestCL_AC101_LegacyStatusNeverRearms(t *testing.T) {
	// SEC: cancelled/expired/timed_out must never map back to a deliverable
	// status on downgrade.
	for _, s := range []CommandState{CommandCancelled, CommandExpired, CommandTimedOut} {
		if got := LegacyStatus(s); got == "queued" || got == "pending" || got == "sent" {
			t.Errorf("LegacyStatus(%s) = %q would be deliverable by a legacy server", s, got)
		}
	}
}

func TestCL_AC102_LegacySuccessTreatedAsSucceeded(t *testing.T) {
	c := Command{Status: "success"}
	if got := c.State(); got != CommandSucceeded {
		t.Fatalf("legacy success row State() = %q, want succeeded", got)
	}
	if !c.State().IsTerminal() || !c.State().IsImmutable() {
		t.Error("a legacy success row must be terminal and immutable like succeeded")
	}
	e := Command{Status: "error"}
	if e.State() != CommandFailed {
		t.Errorf("legacy error row State() = %q, want failed", e.State())
	}
	if !CanonicalStatus("success").IsTerminal() {
		t.Error("CanonicalStatus(success).IsTerminal() = false")
	}
}

func TestCL_AC102_StatePredicates(t *testing.T) {
	type flags struct{ terminal, immutable, active bool }
	want := map[CommandState]flags{
		CommandQueued:       {false, false, true},
		CommandDispatched:   {false, false, true},
		CommandAcknowledged: {false, false, true},
		CommandRunning:      {false, false, true},
		CommandVerifying:    {false, false, true},
		CommandSucceeded:    {true, true, false},
		CommandFailed:       {true, true, false},
		CommandTimedOut:     {true, false, false},
		CommandCancelled:    {true, true, false},
		CommandExpired:      {true, true, false},
		LegacyPending:       {false, false, true},
		LegacySent:          {false, false, true},
		CommandState("x"):   {false, false, false},
	}
	for s, w := range want {
		got := flags{s.IsTerminal(), s.IsImmutable(), s.IsActive()}
		if got != w {
			t.Errorf("%q: terminal/immutable/active = %v, want %v", s, got, w)
		}
	}
}

func TestCL_AC102_ActiveAndTerminalSetsPartitionTheStates(t *testing.T) {
	seen := map[CommandState]int{}
	for _, s := range ActiveStates() {
		seen[s]++
		if !s.IsActive() {
			t.Errorf("ActiveStates contains %s which is not active", s)
		}
	}
	for _, s := range TerminalStates() {
		seen[s]++
		if !s.IsTerminal() {
			t.Errorf("TerminalStates contains %s which is not terminal", s)
		}
	}
	if len(seen) != 10 {
		t.Errorf("active+terminal should cover the 10 canonical states, covered %d", len(seen))
	}
	for s, n := range seen {
		if n != 1 {
			t.Errorf("state %s appears %d times across active/terminal sets", s, n)
		}
	}
}

func TestCL_SEC006_SQLStatusListRendersTypedConstants(t *testing.T) {
	cases := []struct {
		in   []CommandState
		want string
	}{
		{[]CommandState{CommandQueued}, `'queued'`},
		{[]CommandState{CommandQueued, LegacyPending}, `'queued','pending'`},
		{ExpandAliases(ActiveStates()...), `'queued','pending','dispatched','sent','acknowledged','running','verifying'`},
		{ExpandAliases(CommandDispatched, CommandRunning), `'dispatched','sent','running'`},
	}
	for _, tc := range cases {
		if got := SQLStatusList(tc.in...); got != tc.want {
			t.Errorf("SQLStatusList(%v) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestCL_SEC006_SQLStatusListRejectsUnknown(t *testing.T) {
	bad := []string{
		"",
		"success", // legacy terminal values are not guard literals
		"error",
		"QUEUED",
		" queued",
		"queued') OR 1=1; --",
		"queued'--",
		"bogus",
		"queued\x00",
	}
	for _, b := range bad {
		t.Run(fmt.Sprintf("%q", b), func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("SQLStatusList(%q) must panic", b)
				}
				if msg := fmt.Sprint(r); strings.Contains(msg, "OR 1=1") {
					t.Errorf("panic message must not echo the rejected value: %s", msg)
				}
			}()
			SQLStatusList(CommandQueued, CommandState(b))
		})
	}
}

func TestCL_SEC006_SQLStatusListRejectsEmpty(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an empty list would render invalid SQL (IN ()) and must panic")
		}
	}()
	SQLStatusList()
}

func TestCL_SEC006_SQLStatusListAcceptsAllTwelveConstants(t *testing.T) {
	all := append(AllCommandStates(), LegacyPending, LegacySent)
	if got := strings.Count(SQLStatusList(all...), "'") / 2; got != 12 {
		t.Errorf("want 12 literals rendered, got %d", got)
	}
}

func TestCL_SEC006_StatusFilterRejectsUnknownValues(t *testing.T) {
	for _, bad := range []string{
		"", "Queued", " queued", "queued ", "queued')--", "queued' OR '1'='1", "all",
		"queued;DROP TABLE commands", "%", "queued\x00", "unknown", "timedout",
	} {
		if got, ok := ParseStatusFilterValue(bad); ok || got != nil {
			t.Errorf("ParseStatusFilterValue(%q) = %v, %v; want nil, false", bad, got, ok)
		}
	}
	for _, csv := range []string{"queued,", ",queued", "queued,,running", "queued')--", "queued, running", "a,b,c"} {
		if got, ok := ParseStatusFilter(csv); ok || got != nil {
			t.Errorf("ParseStatusFilter(%q) = %v, %v; want nil, false", csv, got, ok)
		}
	}
}

func TestCL_SEC006_StatusFilterAliasExpansion(t *testing.T) {
	cases := map[string][]string{
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
	for in, want := range cases {
		got, ok := ParseStatusFilterValue(in)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("ParseStatusFilterValue(%q) = %v, %v; want %v, true", in, got, ok, want)
		}
	}
}

func TestCL_SEC006_StatusFilterReturnsACopy(t *testing.T) {
	a, _ := ParseStatusFilterValue("queued")
	a[0] = "tampered"
	b, _ := ParseStatusFilterValue("queued")
	if b[0] != "queued" {
		t.Errorf("mutating a returned slice leaked into the allowlist: %v", b)
	}
}

func TestCL_SEC006_ParseStatusFilterCSV(t *testing.T) {
	if got, ok := ParseStatusFilter(""); !ok || got != nil {
		t.Errorf("empty filter = %v, %v; want nil, true (no filter)", got, ok)
	}
	got, ok := ParseStatusFilter("queued,pending,running")
	want := []string{"queued", "pending", "running"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("duplicate aliases must collapse in first-seen order: got %v, %v want %v", got, ok, want)
	}
	tooMany := strings.Repeat("queued,", MaxStatusFilterValues) + "queued"
	if _, ok := ParseStatusFilter(tooMany); ok {
		t.Errorf("more than %d tokens must be rejected", MaxStatusFilterValues)
	}
}

func TestCL_AC085_ExpandAliases(t *testing.T) {
	got := ExpandAliases(CommandQueued, CommandQueued, LegacyPending, CommandRunning, CommandDispatched)
	want := []CommandState{CommandQueued, LegacyPending, CommandRunning, CommandDispatched, LegacySent}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExpandAliases = %v, want %v", got, want)
	}
}

func TestCL_AC085_ActorString(t *testing.T) {
	if ActorServer.String() != "server" || ActorAgent.String() != "agent" || ActorOperator.String() != "operator" {
		t.Error("actor names are used in log fields and must stay server/agent/operator")
	}
	if CommandActor(0).String() == "" {
		t.Error("the zero actor must render a diagnosable name")
	}
	if CanTransition(CommandQueued, CommandDispatched, CommandActor(0)) {
		t.Error("the zero actor must never be allowed")
	}
}
