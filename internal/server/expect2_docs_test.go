package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// [EXPECT-002][AC-030] (FR-032, FR-033, C-5) The README describes agent
// self-updates: how they are recognised, the 10-minute bound, what is
// suppressed, how the window ends, the outcomes and their events, the rate cap,
// the label, the fallback, the residuals and the accepted abuse budget. The
// CHANGELOG entry says that no agent upgrade and no install.sh re-run is needed
// and that already-deployed agents are covered.
func TestEX2_AC030_ReadmeAndChangelog(t *testing.T) {
	readme := readRepoFile(t, readmePath)
	section := exDocsSection(t, readme, "### Agent self-updates")

	groups := map[string][]string{
		"how a self-update is recognised": {
			"`agent_update_started`", "Agent update started: 2.78.0 → 2.80.0", "exactly that form",
			"at most 256 bytes", "never read", "the server's time, never the agent's",
		},
		"the record": {
			"self-update record", "`by agent auto-update`", "read-only", "never sent to an agent",
		},
		"attribution to a command (FR-004)": {
			"finished at most 10 minutes earlier", "belongs to that command", "creates no record",
		},
		"the bound and the scope": {
			"at most 10 minutes", "`device_offline` for that device only", "A container that dies still alerts",
			"There is no settle period",
		},
		"how the window ends": {
			"registers again reporting the new version", "`agent_update_failed`", "registers on any other version",
			"the 10-minute bound",
		},
		"the outcomes and their events (FR-018 to FR-021)": {
			"`succeeded`", "`failed`", "`timed_out`", "`agent_self_update_failed`", "`agent_self_update_unconfirmed`",
			"came back on agent 2.78.0", "was not confirmed within 10m", "still offline 10m after Agent update 2.78.0 → 2.80.0 by agent auto-update",
			"cleaned and cut to 200 bytes", "5 escalation pushes per device per hour",
		},
		"the rate cap (FR-007)": {
			"At most **6 records per device in any 24 hours**", "`agent_self_update_paused`",
		},
		"the label": {
			"`Updating agent 2.78.0 → 2.80.0`", "**Expected offline**", "The label clears",
		},
		"the fallback (FR-003)": {
			"Anything else is treated as before",
		},
		"the abuse budget (C-5, SEC-005)": {
			"about 57 minutes per rolling 24 hours", "at most 6 outages of under about 10 minutes each",
			"`info` event that names the update", "leaves a `warning` event but no push", "The cap of 6 stays",
		},
		"the residuals (NFR-011)": {
			"hidden for up to 10 minutes", "start event is lost", "without its connection dropping for 45 seconds",
			"more than about 30 seconds late", "no push", "takes over that offline's attribution",
		},
		"restarts and stuck records": {
			"rebuilt from the database", "first offline-detector pass", "for an hour after its bound",
		},
		"compatibility and upgrade": {
			"No agent upgrade, no agent configuration change and no `install.sh` re-run", "000034_agent_self_update",
			"deletes the self-update records",
		},
	}
	for name, terms := range groups {
		for _, term := range terms {
			assert.Contains(t, section, term, "the README section must cover %s: missing %q", name, term)
		}
	}

	// The API reference names the new endpoint, field and origin.
	assert.Contains(t, readme, "### Command events", "the events endpoint needs its own reference")
	assert.Contains(t, readme, "| `GET` | `/api/v1/devices/:id/commands/:cmdId/events` |", "the API table must list the events endpoint")
	assert.Contains(t, readme, "| `agent_to_version` |", "the activity reference must list agent_to_version")
	assert.Contains(t, readme, "`agent_self_update`", "the command origin list must name agent_self_update")
	assert.Contains(t, readme, "(#agent-self-updates)", "the rest of the README must link to the new section")

	unreleased := exUnreleased(t, readRepoFile(t, changelogPath))
	var lines []string
	for _, line := range strings.Split(unreleased, "\n") {
		if strings.Contains(line, "[EXPECT-002]") {
			lines = append(lines, line)
		}
	}
	require.NotEmpty(t, lines, "[Unreleased] must have EXPECT-002 entries")

	found := false
	for _, line := range lines {
		if strings.Contains(line, "no agent upgrade") && strings.Contains(line, "no `install.sh` re-run") && strings.Contains(line, "already deployed") {
			found = true
		}
	}
	assert.True(t, found, "an [EXPECT-002] entry must say that no agent upgrade and no `install.sh` re-run is required and that already-deployed agents are covered")

	all := strings.Join(lines, "\n")
	for _, term := range []string{
		"`000034_agent_self_update`", "agent_self_update_failed", "agent_self_update_unconfirmed", "agent_self_update_paused",
		"GET /api/v1/devices/:id/commands/:commandId/events", "agent_to_version",
		"about 57 minutes per rolling 24 hours", "at most 6 outages of under about 10 minutes each",
		"`last_heartbeat`", "limited to `info`, `warning` and `critical`",
	} {
		assert.Contains(t, all, term, "the [EXPECT-002] entries must cover %q", term)
	}
}
