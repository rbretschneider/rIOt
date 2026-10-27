package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-008 model tests (ADD section 8, "M8").

func TestEX8_ActorReinstallEdges(t *testing.T) {
	want := map[[2]CommandState]bool{
		{CommandRunning, CommandSucceeded}: true,
		{CommandRunning, CommandTimedOut}:  true,
	}
	n := 0
	for _, from := range AllCommandStates() {
		for _, to := range AllCommandStates() {
			got := CanTransition(from, to, ActorReinstall)
			assert.Equal(t, want[[2]CommandState{from, to}], got, "[C-2] ActorReinstall %s -> %s", from, to)
			if got {
				n++
			}
		}
	}
	assert.Equal(t, 2, n, "[C-2] ActorReinstall has exactly two edges")
	assert.Equal(t, []CommandState{CommandRunning}, AllowedFrom(CommandSucceeded, ActorReinstall))
	assert.Equal(t, []CommandState{CommandRunning}, AllowedFrom(CommandTimedOut, ActorReinstall))
	assert.Nil(t, AllowedFrom(CommandVerifying, ActorReinstall), "a reinstall row never moves to verifying")
	assert.Nil(t, AllowedFrom(CommandFailed, ActorReinstall), "nor to failed")
	assert.Nil(t, AllowedFrom(CommandQueued, ActorReinstall), "nor back to queued")
	assert.Nil(t, AllowedFrom(CommandCancelled, ActorReinstall), "nor be cancelled")
	assert.Equal(t, "reinstall", ActorReinstall.String())
}

func TestEX8_ExistingActorEdgesUnchanged(t *testing.T) {
	want := map[edge]bool{}
	for _, e := range frdTransitions() {
		want[e] = true
	}
	for _, from := range AllCommandStates() {
		for _, to := range AllCommandStates() {
			for _, actor := range []CommandActor{ActorServer, ActorAgent, ActorOperator} {
				assert.Equal(t, want[edge{from, to, actor}], CanTransition(from, to, actor),
					"[AC-025] %s -> %s for %s must be exactly the CMD-LIFECYCLE-001 table", from, to, actor)
			}
		}
	}
	// ActorSelfUpdate keeps its seven edges (TestEX2_AC024_ActorSelfUpdateEdges
	// asserts the list); the reinstall actor must not have changed that count.
	n := 0
	for _, from := range AllCommandStates() {
		for _, to := range AllCommandStates() {
			if CanTransition(from, to, ActorSelfUpdate) {
				n++
			}
		}
	}
	assert.Equal(t, 7, n, "[AC-025] ActorSelfUpdate still has exactly seven edges")
}

func TestEX8_IsValidOriginExcludesInstaller(t *testing.T) {
	assert.False(t, IsValidOrigin(OriginInstaller), "[AD-003] Service.Dispatch can never create a reinstall row")
	assert.True(t, IsValidOrigin(OriginUser) && IsValidOrigin(OriginAutoUpdate) && IsValidOrigin(OriginAutoPatch))
	assert.Equal(t, "installer", OriginInstaller)
}

func TestEX8_PolicyForReinstall(t *testing.T) {
	p, ok := ExpectPolicyFor(Command{Action: ActionAgentUpdate, Origin: OriginInstaller})
	require.True(t, ok)
	assert.True(t, p.Reinstall && p.CoversOffline && p.EscalateNotBack && !p.SelfUpdate && !p.CheckContainers && !p.RequireObservedReturn,
		"[AD-006] reinstall policy flags: %+v", p)
	assert.Equal(t, CoverNoContainers, p.Containers, "[FR-014] a reinstall covers no container")
	assert.Zero(t, p.Settle, "[AD-006] no settle period")
	assert.Zero(t, p.PostTerminal, "[AD-006] no post-terminal period")

	sp, ok := ExpectPolicyFor(Command{Action: ActionAgentUpdate, Origin: OriginAgentSelfUpdate})
	require.True(t, ok)
	assert.True(t, sp.SelfUpdate && !sp.Reinstall, "[FR-027] the self-update policy is unchanged and not a reinstall: %+v", sp)

	up, ok := ExpectPolicyFor(Command{Action: ActionAgentUpdate, Origin: OriginUser})
	require.True(t, ok)
	want, _ := ExpectPolicy(ActionAgentUpdate)
	assert.Equal(t, want, up, "[FR-027] a user agent_update keeps the EXPECT-001 policy")
	assert.False(t, up.Reinstall)

	_, ok = ExpectPolicyFor(Command{Action: ActionReboot, Origin: OriginInstaller})
	want2, _ := ExpectPolicy(ActionReboot)
	got2, _ := ExpectPolicyFor(Command{Action: ActionReboot, Origin: OriginInstaller})
	assert.True(t, ok)
	assert.Equal(t, want2, got2, "the installer origin only changes agent_update rows")
}

func TestEX8_ActorPhraseInstaller(t *testing.T) {
	assert.Equal(t, "by installer", ActorPhrase(Command{Origin: OriginInstaller, ActorName: "ignored"}, ""),
		"[DEC-3] the installer actor phrase never reads actor_name")
}

func TestEX8_ReinstallOutcomeMapping(t *testing.T) {
	for _, o := range []ReinstallOutcome{ReinstallUnconfirmedOnline, ReinstallUnconfirmedOffline} {
		s, ok := o.State()
		assert.True(t, ok)
		assert.Equal(t, CommandTimedOut, s, "%s ends timed_out", o)
		assert.Equal(t, string(o), o.FailureStep(), "the failure step carries the outcome token")
	}
	_, ok := ReinstallOutcome("bogus").State()
	assert.False(t, ok, "an unknown outcome has no state")
	assert.Empty(t, ReinstallOutcome("bogus").FailureStep())
}

func TestEX8_MessagesMatchFRD(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"not back", ReinstallNotBackMessage("PiVPN", "1a2b3c4d"),
			"PiVPN still offline 10m after Agent reinstall by installer (record 1a2b3c4d)"},
		{"not confirmed", ReinstallNotConfirmedMessage("PiCUPS"),
			"Agent reinstall by installer on PiCUPS was not confirmed within 10m (agent did not restart)"},
		{"unconfirmed offline result", ReinstallUnconfirmedOfflineMessage("PiVPN"),
			"not confirmed: PiVPN was offline 10m after the start"},
		{"rate cap", ReinstallRateCapMessage("hawaii"),
			"Agent reinstall tracking paused on hawaii: more than 6 reinstalls in 24h; offline alerts during reinstalls are not suppressed"},
		{"stuck", ReinstallStuckMessage("hawaii", "1a2b3c4d"),
			"Agent reinstall tracking paused on hawaii: record 1a2b3c4d (Agent reinstall) could not be completed 1h after its bound; the next reinstall replaces it"},
		{"no hostname", ReinstallRateCapMessage("  "),
			"Agent reinstall tracking paused on device: more than 6 reinstalls in 24h; offline alerts during reinstalls are not suppressed"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.got, "[AD-011] %s", tc.name)
		assert.LessOrEqual(t, len(tc.got), ReinstallResultMsgBytes, "%s is at most 512 bytes", tc.name)
	}

	// EXPECT-001 C-6: a hostile hostname is sanitised in every template.
	hostile := "pi\x00vpn‮" + strings.Repeat("h", 500)
	for name, msg := range map[string]string{
		"not back":       ReinstallNotBackMessage(hostile, "1a2b3c4d"),
		"not confirmed":  ReinstallNotConfirmedMessage(hostile),
		"offline result": ReinstallUnconfirmedOfflineMessage(hostile),
		"rate cap":       ReinstallRateCapMessage(hostile),
		"stuck":          ReinstallStuckMessage(hostile, "1a2b3c4d"),
	} {
		assert.NotContains(t, msg, "\x00", "[C-6] %s carries no NUL", name)
		assert.NotContains(t, msg, "‮", "[C-6] %s carries no bidi override", name)
		assert.LessOrEqual(t, len(msg), ReinstallResultMsgBytes, "[C-6] %s stays within 512 bytes, got %d", name, len(msg))
	}

	// The window text goes through the shared EXPECT-001 templates (FR-017, FR-018).
	off := OfflineCoverage{Kind: OfflineKindWindow, ActionLabel: ReinstallActionLabel, ActorPhrase: ReinstallActorPhrase}
	assert.Equal(t, "Device PiVPN went offline during Agent reinstall by installer (expected)", off.Message("PiVPN"))
	note := OnlineNote{ActionLabel: ReinstallActionLabel, ActorPhrase: ReinstallActorPhrase, Offline: 20 * 1e9}
	assert.Equal(t, "Device PiVPN came online after Agent reinstall by installer (offline 20s)", note.Message("PiVPN"))
}

func TestEX8_BoundsAreTheFRDNumbers(t *testing.T) {
	assert.Equal(t, 10*60, int(ExpectReinstallBound.Seconds()), "[OQ-001] 10-minute bound")
	assert.Equal(t, 6, ReinstallRateCap, "[FR-010] 6 per 24h")
	assert.Equal(t, 24*3600, int(ReinstallRateWindow.Seconds()))
	assert.Equal(t, 3600, int(ReinstallStuckAfter.Seconds()), "[NFR-004] 1h")
	assert.Equal(t, 600, ReinstallExecDeadlineS, "matches the CHECK exec_deadline_s <= 600")
}
