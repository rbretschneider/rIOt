package models

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-002 model tests (ADD section 8, "M").

func TestEX2_AC016_ParseRejectsMalformed(t *testing.T) {
	// The seven inputs of AC-016 plus the grammar edges of AD-011.
	cases := []struct {
		name string
		raw  string
	}{
		{"ASCII arrow", "Agent update started: 2.78.0 -> 2.80.0"},
		{"dev version", "Agent update started: dev → 2.80.0"},
		{"same version", "Agent update started: 2.80.0 → 2.80.0"},
		{"same version after v", "Agent update started: v2.80.0 → 2.80.0"},
		{"NUL byte", "Agent update started: 2.78.0 → 2.80.0\u0000x"},
		{"lowercase prefix", "agent update started: 2.78.0 → 2.80.0"},
		{"300 byte version", "Agent update started: 2.78.0 → " + strings.Repeat("9", 300)},
		{"10 KB message", "Agent update started: 2.78.0 → 2.80.0" + strings.Repeat(" ", 10<<10)},
		{"empty", ""},
		{"prefix only", "Agent update started: "},
		{"no to version", "Agent update started: 2.78.0 → "},
		{"two arrows", "Agent update started: 2.78.0 → 2.79.0 → 2.80.0"},
		{"bidi override", "Agent update started: 2.78.0 → 2.80.0‮"},
		{"zero width space inside", "Agent update started: 2.78.0 → 2.8​0.0"},
		{"line separator", "Agent update started: 2.78.0 → 2.80.0 x"},
		{"embedded newline", "Agent update started: 2.78.0\n → 2.80.0"},
		{"invalid UTF-8", "Agent update started: 2.78.0 → 2.80.0\xff"},
		{"full-width digits", "Agent update started: 2.78.0 → ２.80.0"},
		{"look-alike arrow", "Agent update started: 2.78.0 ⟶ 2.80.0"},
		{"two groups only", "Agent update started: 2.78 → 2.80.0"},
		{"six digit group", "Agent update started: 2.78.0 → 2.800000.0"},
		{"suffix too long", "Agent update started: 2.78.0 → 2.80.0-" + strings.Repeat("a", 17)},
		{"suffix bad char", "Agent update started: 2.78.0 → 2.80.0-rc 1"},
		{"missing space before arrow", "Agent update started: 2.78.0→ 2.80.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, ok := ParseSelfUpdateStart(tc.raw)
			assert.False(t, ok, "[AC-016] %q must not be accepted", tc.name)
			assert.Empty(t, from+to, "[AC-016] a rejected message yields no versions")
		})
	}
}

func TestEX2_AC016_ParseAcceptsTheAgentForm(t *testing.T) {
	cases := []struct {
		raw, from, to string
	}{
		{"Agent update started: 2.78.0 → 2.80.0", "2.78.0", "2.80.0"},
		{"Agent update started: v2.78.0 → v2.80.0", "v2.78.0", "v2.80.0"},
		{"  Agent update started: 2.40.0 → 2.80.0\n", "2.40.0", "2.80.0"},
		{"Agent update started: 2.80.0-rc.1 → 2.80.0", "2.80.0-rc.1", "2.80.0"},
		{"Agent update started: 2.80.0+build.5 → 2.81.0", "2.80.0+build.5", "2.81.0"},
		{"Agent update started: 12345.1.1 → 1.2.3", "12345.1.1", "1.2.3"},
	}
	for _, tc := range cases {
		from, to, ok := ParseSelfUpdateStart(tc.raw)
		require.True(t, ok, "[AC-016] %q is the form the agent sends", tc.raw)
		assert.Equal(t, tc.from, from, "from version of %q", tc.raw)
		assert.Equal(t, tc.to, to, "to version of %q", tc.raw)
	}
}

func TestEX2_AC016_ParseBounds(t *testing.T) {
	// Whitespace is trimmed before the 256 byte check, but the 4096 byte pre-check
	// looks at the raw input first.
	prefix := "Agent update started: 2.78.0 → 2.80.0"
	_, _, ok := ParseSelfUpdateStart(prefix + strings.Repeat(" ", 4000))
	assert.True(t, ok, "whitespace is trimmed before the 256 byte check, within the 4096 byte pre-check")
	_, _, ok = ParseSelfUpdateStart(prefix + strings.Repeat(" ", MaxSelfUpdateRawBytes))
	assert.False(t, ok, "over the 4096 byte pre-check is rejected before any trimming")
	assert.Equal(t, 32, MaxAgentVersionBytes)
	long := "1.2.3-" + strings.Repeat("a", 16)
	assert.True(t, ValidAgentVersion(long), "a suffix of 16 characters is valid")
	assert.False(t, ValidAgentVersion("v"+long+strings.Repeat("a", 20)), "more than 32 bytes is not a version")
	assert.False(t, ValidAgentVersion(""), "empty is not a version")
}

func TestEX2_AgentVersionsMatch(t *testing.T) {
	assert.True(t, AgentVersionsMatch("v2.80.0", "2.80.0"), "one leading v is ignored")
	assert.True(t, AgentVersionsMatch("2.80.0", "2.80.0"))
	assert.False(t, AgentVersionsMatch("2.80.0", "2.80.1"))
	assert.False(t, AgentVersionsMatch("vv2.80.0", "2.80.0"), "only one v is removed")
}

func TestEX2_AC019_ReasonSanitizedAndBounded(t *testing.T) {
	raw := "bad\x00reason ‮evil​  line\nbreak \xff\xfe" + strings.Repeat("x", 2048)
	got := SafeText(raw, SelfUpdateReasonBytes)
	assert.LessOrEqual(t, len(got), SelfUpdateReasonBytes, "[AC-019] the quoted reason is at most 200 bytes")
	assert.True(t, utf8.ValidString(got), "[AC-019] valid UTF-8 only")
	for _, r := range got {
		assert.False(t, unsafeNameRune(r), "[AC-019] no control, format or separator rune (found %U)", r)
	}
	assert.NotContains(t, got, "‮", "bidi override removed")
	assert.NotContains(t, got, "\x00", "NUL removed")

	msg := SelfUpdateFailedMessage("vermont", "2.78.0", "2.80.0", raw)
	assert.LessOrEqual(t, len(msg), SelfUpdateResultMsgBytes, "the server's text is at most 512 bytes")
	assert.True(t, utf8.ValidString(msg))
	assert.True(t, strings.HasPrefix(msg, "Agent update 2.78.0 → 2.80.0 by agent auto-update failed on vermont: bad"),
		"[AC-019] the reason is quoted after the fixed prefix, got %q", msg)

	// A rune that straddles the cut is dropped whole, never split.
	multi := strings.Repeat("é", 150) // 300 bytes
	cut := SafeText(multi, SelfUpdateReasonBytes)
	assert.True(t, utf8.ValidString(cut), "a cut on a rune boundary stays valid UTF-8")
	assert.Equal(t, 200, len(cut), "100 two-byte runes fit exactly")
}

func TestEX2_SafeNameUnchangedBySafeText(t *testing.T) {
	assert.Equal(t, SafeText("plex‮\x00", ExpectNameMaxBytes), SafeName("plex‮\x00"), "SafeName is SafeText at 128 bytes")
	assert.Equal(t, ExpectNameMaxBytes, len(SafeName(strings.Repeat("a", 500))), "SafeName still cuts at 128 bytes")
}

func TestEX2_AC024_ActorSelfUpdateEdges(t *testing.T) {
	want := map[[2]CommandState]bool{
		{CommandRunning, CommandVerifying}:   true,
		{CommandRunning, CommandSucceeded}:   true,
		{CommandRunning, CommandFailed}:      true,
		{CommandRunning, CommandTimedOut}:    true,
		{CommandVerifying, CommandSucceeded}: true,
		{CommandVerifying, CommandFailed}:    true,
		{CommandVerifying, CommandTimedOut}:  true,
	}
	n := 0
	for _, from := range AllCommandStates() {
		for _, to := range AllCommandStates() {
			got := CanTransition(from, to, ActorSelfUpdate)
			assert.Equal(t, want[[2]CommandState{from, to}], got, "[AC-024] ActorSelfUpdate %s -> %s", from, to)
			if got {
				n++
			}
		}
	}
	assert.Equal(t, 7, n, "[AC-024] ActorSelfUpdate has exactly seven edges")
	assert.Equal(t, []CommandState{CommandRunning, CommandVerifying}, AllowedFrom(CommandTimedOut, ActorSelfUpdate),
		"[C-2] the bound's timed_out edge belongs to ActorSelfUpdate")
	assert.Equal(t, []CommandState{CommandRunning}, AllowedFrom(CommandVerifying, ActorSelfUpdate))
	assert.Nil(t, AllowedFrom(CommandQueued, ActorSelfUpdate), "a self-update row can never go back to queued")
	assert.Nil(t, AllowedFrom(CommandDispatched, ActorSelfUpdate), "nor be claimed")
	assert.Nil(t, AllowedFrom(CommandCancelled, ActorSelfUpdate), "nor be cancelled")
	assert.Equal(t, "self_update", ActorSelfUpdate.String())
}

func TestEX2_ExistingActorEdgesUnchanged(t *testing.T) {
	want := map[edge]bool{}
	for _, e := range frdTransitions() {
		want[e] = true
	}
	for _, from := range AllCommandStates() {
		for _, to := range AllCommandStates() {
			for _, actor := range []CommandActor{ActorServer, ActorAgent, ActorOperator} {
				assert.Equal(t, want[edge{from, to, actor}], CanTransition(from, to, actor),
					"[AC-028] %s -> %s for %s must be exactly the CMD-LIFECYCLE-001 table", from, to, actor)
			}
		}
	}
}

func TestEX2_IsValidOriginExcludesSelfUpdate(t *testing.T) {
	assert.False(t, IsValidOrigin(OriginAgentSelfUpdate), "[AD-001] Service.Dispatch can never create a self-update row")
	assert.True(t, IsValidOrigin(OriginUser) && IsValidOrigin(OriginAutoUpdate) && IsValidOrigin(OriginAutoPatch))
	assert.Equal(t, "agent_self_update", OriginAgentSelfUpdate)
}

func TestEX2_PolicyForSelfUpdate(t *testing.T) {
	p, ok := ExpectPolicyFor(Command{Action: ActionAgentUpdate, Origin: OriginAgentSelfUpdate})
	require.True(t, ok)
	assert.True(t, p.SelfUpdate && p.CoversOffline && p.EscalateNotBack && !p.CheckContainers && !p.RequireObservedReturn,
		"[AD-005] self-update policy flags: %+v", p)
	assert.Equal(t, CoverNoContainers, p.Containers, "[FR-010] no container coverage")
	assert.Zero(t, p.Settle, "no settle period")
	assert.Zero(t, p.PostTerminal, "no post-terminal period")
	assert.Equal(t, WindowAgent, p.Class)

	cmd, ok := ExpectPolicyFor(Command{Action: ActionAgentUpdate, Origin: OriginUser})
	require.True(t, ok)
	table, _ := ExpectPolicy(ActionAgentUpdate)
	assert.Equal(t, table, cmd, "[AC-028] a command keeps the EXPECT-001 agent_update policy")
	assert.False(t, table.SelfUpdate, "the 12-row table is unchanged")
	_, ok = ExpectPolicyFor(Command{Action: ActionAgentUninstall, Origin: OriginAgentSelfUpdate})
	assert.False(t, ok, "the self-update policy needs action agent_update")
	assert.Equal(t, 12, len(ExpectActions()), "ExpectActions is unchanged")
	assert.Equal(t, 10*time.Minute, ExpectSelfUpdateBound)
}

func TestEX2_ActorPhraseSelfUpdate(t *testing.T) {
	assert.Equal(t, "by agent auto-update", ActorPhrase(Command{Origin: OriginAgentSelfUpdate}, ""), "[AD-012] actor phrase")
}

func TestEX2_SelfUpdateVersionsRevalidated(t *testing.T) {
	c := Command{Params: SelfUpdateParams("2.78.0", "2.80.0", "")}
	from, to, ok := SelfUpdateVersions(c)
	assert.True(t, ok)
	assert.Equal(t, "2.78.0", from)
	assert.Equal(t, "2.80.0", to)
	for _, bad := range []map[string]interface{}{
		nil,
		{SelfUpdateParamFrom: "2.78.0"},
		{SelfUpdateParamFrom: "2.78.0", SelfUpdateParamTo: "<script>"},
		{SelfUpdateParamFrom: 2.78, SelfUpdateParamTo: "2.80.0"},
	} {
		_, _, ok := SelfUpdateVersions(Command{Params: bad})
		assert.False(t, ok, "[implementation note 9] params %v are not valid versions", bad)
	}
}

func TestEX2_SelfUpdateMessages(t *testing.T) {
	const (
		from = "2.78.0"
		to   = "2.80.0"
	)
	cases := []struct{ name, got, want string }{
		{"failed", SelfUpdateFailedMessage("PiCUPS", from, to, "Agent update failed: download: connection reset"),
			"Agent update 2.78.0 → 2.80.0 by agent auto-update failed on PiCUPS: Agent update failed: download: connection reset"},
		{"wrong version", SelfUpdateWrongVersionMessage("PiVPN", "2.78.0", from, to),
			"PiVPN came back on agent 2.78.0 after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)"},
		{"wrong version unrecognised", SelfUpdateWrongVersionMessage("PiVPN", "<b>x</b>", from, to),
			"PiVPN came back on agent an unrecognised version after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)"},
		{"not back", SelfUpdateNotBackMessage("vermont", from, to, "1a2b3c4d"),
			"vermont still offline 10m after Agent update 2.78.0 → 2.80.0 by agent auto-update (record 1a2b3c4d)"},
		{"offline after failure", SelfUpdateOfflineAfterFailureMessage("vermont", from, to, "1a2b3c4d"),
			"vermont is offline: Agent update 2.78.0 → 2.80.0 by agent auto-update failed (record 1a2b3c4d)"},
		{"not confirmed", SelfUpdateNotConfirmedMessage("vermont", from, to, "2.78.0"),
			"Agent update 2.78.0 → 2.80.0 by agent auto-update on vermont was not confirmed within 10m (agent reports 2.78.0)"},
		{"not confirmed unrecognised", SelfUpdateNotConfirmedMessage("vermont", from, to, ""),
			"Agent update 2.78.0 → 2.80.0 by agent auto-update on vermont was not confirmed within 10m (agent reports an unrecognised version)"},
		{"rate cap", SelfUpdateRateCapMessage("hawaii"),
			"Agent self-update tracking paused on hawaii: more than 6 agent updates started in 24h; offline alerts during agent updates are not suppressed"},
		{"stuck", SelfUpdateStuckMessage("hawaii", "1a2b3c4d", from, to),
			"Agent self-update tracking paused on hawaii: record 1a2b3c4d (Agent update 2.78.0 → 2.80.0) could not be completed 1h after its bound; the next agent update replaces it"},
		{"no hostname", SelfUpdateRateCapMessage("  "),
			"Agent self-update tracking paused on device: more than 6 agent updates started in 24h; offline alerts during agent updates are not suppressed"},
		{"confirmed by registration", SelfUpdateConfirmedMessage("registered on", to), "confirmed: the agent registered on 2.80.0"},
		{"confirmed by heartbeat", SelfUpdateConfirmedMessage("reports", to), "confirmed: the agent reports 2.80.0"},
		{"unconfirmed offline", SelfUpdateUnconfirmedOfflineMessage("vermont"), "not confirmed: vermont was offline 10m after the start"},
		{"completed", SelfUpdateCompletedMessage("signed installation completion verified against running executable"),
			"completed: signed installation completion verified against running executable"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.got, "[AD-012] %s", tc.name)
		assert.LessOrEqual(t, len(tc.got), SelfUpdateResultMsgBytes, "%s is at most 512 bytes", tc.name)
	}
	// The worst case by construction: a 128 byte host, 32 byte versions, 200 byte reason.
	host := strings.Repeat("h", 500)
	v32 := "v12345.12345.12345-" + strings.Repeat("a", 13)
	require.Equal(t, 32, len(v32))
	worst := SelfUpdateFailedMessage(host, v32, v32+"x", strings.Repeat("r", 500))
	assert.LessOrEqual(t, len(worst), SelfUpdateResultMsgBytes, "the largest template stays within 512 bytes, got %d", len(worst))
}

func TestEX2_ClampAgentUpdateSeverity(t *testing.T) {
	for _, typ := range []EventType{EventAgentUpdateAvail, EventAgentUpdateStarted, EventAgentUpdateCompleted, EventAgentUpdateFailed} {
		for _, keep := range []EventSeverity{SeverityInfo, SeverityWarning, SeverityCrit} {
			assert.Equal(t, keep, ClampAgentUpdateSeverity(typ, keep), "[AD-015] %s keeps %s", typ, keep)
		}
		want := SeverityInfo
		if typ == EventAgentUpdateFailed {
			want = SeverityWarning
		}
		for _, bad := range []EventSeverity{"", "panic", "WARNING", EventSeverity(strings.Repeat("x", 1024))} {
			assert.Equal(t, want, ClampAgentUpdateSeverity(typ, bad), "[AD-015] %s clamps %.8q to %s", typ, string(bad), want)
		}
	}
	assert.Equal(t, EventSeverity("panic"), ClampAgentUpdateSeverity(EventExtraUpdaterStarted, "panic"),
		"[X-19] other event types are out of scope")
}

func TestEX2_ServerAuthoredClassification(t *testing.T) {
	server := []Event{
		{CommandAttr: CommandAttrExpected},
		{CommandAttr: CommandAttrEscalation},
		{CommandAttr: CommandAttrOutcome},
		{Type: EventAgentSelfUpdateFailed},
		{Type: EventAgentSelfUpdateUnconfirmed},
		{Type: EventAgentSelfUpdatePaused},
	}
	for _, e := range server {
		assert.True(t, IsServerAuthoredEvent(e), "[C-3] %+v is server-authored", e)
	}
	for _, e := range []Event{{CommandAttr: CommandAttrInferred, Type: EventAgentUpdateStarted}, {CommandAttr: "agent"}, {}} {
		assert.False(t, IsServerAuthoredEvent(e), "[C-3] %+v is agent-sourced or unlinked", e)
	}
}

func TestEX2_SelfUpdateOutcomeMapping(t *testing.T) {
	s, ok := SelfUpdateConfirmed.State()
	assert.True(t, ok)
	assert.Equal(t, CommandSucceeded, s)
	assert.Empty(t, SelfUpdateConfirmed.FailureStep())
	s, ok = SelfUpdateUnconfirmedOnline.State()
	assert.True(t, ok)
	assert.Equal(t, CommandTimedOut, s)
	assert.Equal(t, "unconfirmed_online", SelfUpdateUnconfirmedOnline.FailureStep())
	assert.Equal(t, "unconfirmed_offline", SelfUpdateUnconfirmedOffline.FailureStep())
	_, ok = SelfUpdateOutcome("bogus").State()
	assert.False(t, ok, "an unknown outcome maps to nothing")
	for _, tok := range []string{
		SelfUpdateStepWaitingForRestart, SelfUpdateStepWaitingForConfirmation, SelfUpdateStepConfirmed,
		SelfUpdateFailureAgentFailed, SelfUpdateFailureWrongVersion, SelfUpdateFailureUnconfirmedOnline,
		SelfUpdateFailureUnconfirmedOffline, SelfUpdateFailureUnresolved,
	} {
		assert.True(t, StepPattern.MatchString(tok), "[AD-003] %q matches StepPattern", tok)
	}
}
