package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-007 model tests (ADD section 8, "M").

func TestEX7_AC016_StructuredVersionsValidation(t *testing.T) {
	cases := []struct {
		name     string
		from, to string
		ok       bool
	}{
		{"two valid different versions", "2.86.0", "2.87.0", true},
		{"leading v accepted", "v2.86.0", "2.87.0", true},
		{"suffix accepted", "2.86.0-rc1", "2.87.0+build.5", true},
		{"latest is not a version", "latest", "2.87.0", false},
		{"matching versions", "2.87.0", "2.87.0", false},
		{"matching after leading v", "v2.87.0", "2.87.0", false},
		{"to-version of 300 bytes", "2.86.0", strings.Repeat("9", 300), false},
		{"empty from", "", "2.87.0", false},
		{"empty to", "2.86.0", "", false},
		{"dev build", "dev", "2.87.0", false},
		{"control character", "2.86.0\n", "2.87.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, ok := StructuredSelfUpdateVersions(tc.from, tc.to)
			assert.Equal(t, tc.ok, ok, "validity of (%q, %q)", tc.from, tc.to)
			if tc.ok {
				assert.Equal(t, tc.from, from, "from is returned unchanged")
				assert.Equal(t, tc.to, to, "to is returned unchanged")
			} else {
				assert.Empty(t, from, "an invalid pair returns empty from")
				assert.Empty(t, to, "an invalid pair returns empty to")
			}
		})
	}
}

func TestEX7_AC023_RefTokenValidation(t *testing.T) {
	t.Run("ValidRefToken accepts 1 to 64 bytes of letters digits hyphens", func(t *testing.T) {
		for _, ok := range []string{"a", "A", "0", "-", "9f1c-AB", strings.Repeat("a", 64), "0123456789abcdef0123456789abcdef"} {
			assert.True(t, ValidRefToken(ok), "%q must be valid", ok)
		}
	})
	t.Run("ValidRefToken rejects everything else", func(t *testing.T) {
		for name, bad := range map[string]string{
			"empty":            "",
			"65 bytes":         strings.Repeat("a", 65),
			"300 bytes":        strings.Repeat("a", 300),
			"control char":     "ab\x01cd",
			"newline":          "ab\ncd",
			"space":            "ab cd",
			"underscore":       "ab_cd",
			"slash":            "ab/cd",
			"non-ASCII":        "abé",
			"NUL":              "ab\x00",
			"percent":          "ab%20",
			"unicode hyphen":   "ab‐cd",
			"fullwidth letter": "ａb",
		} {
			assert.False(t, ValidRefToken(bad), "%s must be invalid", name)
		}
	})
	t.Run("a command reference must also be a canonical UUID", func(t *testing.T) {
		id := "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c4d"
		assert.Equal(t, AgentUpdateRef{Kind: RefCommand, Value: id}, ParseAgentUpdateRef(id, ""))
		assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef("not-a-uuid", ""), "a valid token that is no UUID is no reference")
		assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef(strings.ToUpper(id), ""), "upper case is not canonical")
		assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef(strings.Repeat("a", 300), ""), "300 bytes is no reference")
		assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef("0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c\x00d", ""), "control characters are no reference")
	})
	t.Run("an attempt reference is a token whatever its shape", func(t *testing.T) {
		assert.Equal(t, AgentUpdateRef{Kind: RefAttempt, Value: "9f1c0123456789abcdef0123456789ab"}, ParseAgentUpdateRef("", "9f1c0123456789abcdef0123456789ab"))
		assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef("", "has space"))
		assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef("", strings.Repeat("a", 65)))
		assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef("", ""))
	})
}

func TestEX7_AC023_CommandRefPredicate(t *testing.T) {
	at := time.Date(2026, 10, 27, 10, 5, 0, 0, time.UTC)
	dispatched := at.Add(-time.Minute)
	mk := func(mut func(c *Command)) *Command {
		c := &Command{Action: ActionAgentUpdate, Origin: OriginUser, Status: string(CommandRunning), DispatchedAt: &dispatched}
		if mut != nil {
			mut(c)
		}
		return c
	}
	finished := func(d time.Duration) func(c *Command) {
		return func(c *Command) {
			f := at.Add(-d)
			c.Status = string(CommandSucceeded)
			c.FinishedAt = &f
		}
	}
	cases := []struct {
		name string
		c    *Command
		ok   bool
	}{
		{"nil command", nil, false},
		{"running agent_update by user", mk(nil), true},
		{"origin auto_update", mk(func(c *Command) { c.Origin = OriginAutoUpdate }), true},
		{"origin auto_patch", mk(func(c *Command) { c.Origin = OriginAutoPatch }), true},
		{"another action", mk(func(c *Command) { c.Action = ActionDockerUpdate }), false},
		{"self-update record origin", mk(func(c *Command) { c.Origin = OriginAgentSelfUpdate }), false},
		{"EXPECT-008 installer origin (SEC-002)", mk(func(c *Command) { c.Origin = OriginInstaller }), false},
		{"empty origin", mk(func(c *Command) { c.Origin = "" }), false},
		{"never dispatched", mk(func(c *Command) { c.DispatchedAt = nil }), false},
		{"finished 9m59s ago", mk(finished(9*time.Minute + 59*time.Second)), true},
		{"finished exactly 10m ago", mk(finished(10 * time.Minute)), true},
		{"finished 10m1s ago", mk(finished(10*time.Minute + time.Second)), false},
		{"finished 11m ago", mk(finished(11 * time.Minute)), false},
		{"terminal with finished_at nil", mk(func(c *Command) { c.Status = string(CommandFailed) }), false},
		{"timed_out with finished_at nil", mk(func(c *Command) { c.Status = string(CommandTimedOut) }), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() { assert.Equal(t, tc.ok, ValidAgentUpdateCommandRef(tc.c, at), "validity of %q", tc.name) })
		})
	}
}

func TestEX7_AC024_BothReferencesMeanNone(t *testing.T) {
	id := "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c4d"
	assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef(id, "9f1c0123456789abcdef0123456789ab"), "a valid command and a valid attempt together are no reference")
	assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef(id, "!!"), "both non-empty is decided before either is validated")
	assert.Equal(t, AgentUpdateRef{}, ParseAgentUpdateRef("!!", "!!"), "two invalid values are no reference")
}

// legacyAgentEvent is a frozen copy of models.AgentEvent at e120031, the shape
// every server since EXPECT-002 decodes with (ADD X-8).
type legacyAgentEvent struct {
	Type      EventType     `json:"type"`
	Severity  EventSeverity `json:"severity"`
	Message   string        `json:"message"`
	CommandID string        `json:"command_id,omitempty"`
	Updater   string        `json:"updater,omitempty"`
	Outcome   string        `json:"outcome,omitempty"`
	Reason    string        `json:"reason,omitempty"`
}

func TestEX7_AC027_NewPayloadDecodesIntoLegacyShape(t *testing.T) {
	msg := "Agent update started: 2.86.0 → 2.87.0"
	payloads := map[string]AgentEvent{
		"self-update start": {Type: EventAgentUpdateStarted, Severity: SeverityInfo, Message: msg,
			FromVersion: "2.86.0", ToVersion: "2.87.0", AttemptID: "9f1c0123456789abcdef0123456789ab"},
		"command-path start": {Type: EventAgentUpdateStarted, Severity: SeverityInfo, Message: msg,
			FromVersion: "2.86.0", ToVersion: "2.87.0", CommandID: "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c4d"},
	}
	for name, evt := range payloads {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(evt)
			require.NoError(t, err)
			var old legacyAgentEvent
			require.NoError(t, json.Unmarshal(raw, &old), "the legacy shape must decode the new payload")
			assert.Equal(t, evt.Type, old.Type)
			assert.Equal(t, msg, old.Message, "the message is byte-for-byte unchanged")
			from, to, ok := ParseSelfUpdateStart(old.Message)
			assert.True(t, ok, "an EXPECT-002 server recognises the start from the message")
			assert.Equal(t, "2.86.0", from)
			assert.Equal(t, "2.87.0", to)
		})
	}
	t.Run("an old agent's payload is byte-identical", func(t *testing.T) {
		raw, err := json.Marshal(AgentEvent{Type: EventAgentUpdateAvail, Severity: SeverityInfo, Message: "Agent update available: 2.87.0"})
		require.NoError(t, err)
		assert.JSONEq(t, `{"type":"agent_update_available","severity":"info","message":"Agent update available: 2.87.0"}`, string(raw),
			"empty new fields must be omitted")
	})
}

func TestEX7_AC027_WaitStepPassesStepPattern(t *testing.T) {
	for _, step := range []string{StepWaitDpkgLock, StepPowerAction} {
		assert.True(t, StepPattern.MatchString(step), "step %q must pass the EXPECT-001 step validation", step)
		assert.Equal(t, step, SanitizeStep(step), "SanitizeStep must keep %q", step)
	}
	assert.Equal(t, "power_action", StepPowerAction)
}

func TestEX7_AC011_SelfUpdateParamsAttempt(t *testing.T) {
	with := Command{Params: SelfUpdateParams("2.86.0", "2.87.0", "9f1c0123456789abcdef0123456789ab")}
	assert.Equal(t, "9f1c0123456789abcdef0123456789ab", SelfUpdateAttemptID(with))
	without := Command{Params: SelfUpdateParams("2.86.0", "2.87.0", "")}
	_, present := without.Params[SelfUpdateParamAttempt]
	assert.False(t, present, "no attempt_id key when the start carried none")
	assert.Equal(t, "", SelfUpdateAttemptID(without))
	bad := Command{Params: map[string]interface{}{SelfUpdateParamAttempt: "has space"}}
	assert.Equal(t, "", SelfUpdateAttemptID(bad), "an attempt ID is re-validated on read")
	assert.Equal(t, "", SelfUpdateAttemptID(Command{}), "a row with no params has no attempt ID")
}
