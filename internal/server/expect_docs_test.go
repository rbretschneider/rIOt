package server

import (
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exDocsSection returns the text of the markdown section that starts at
// heading, up to the next heading of the same level. Line endings are
// normalised so the check does not depend on the checkout's autocrlf setting.
func exDocsSection(t *testing.T, doc, heading string) string {
	t.Helper()
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	start := strings.Index(doc, "\n"+heading+"\n")
	require.GreaterOrEqual(t, start, 0, "missing section %q", heading)
	rest := doc[start+1+len(heading):]
	level := heading[:strings.Index(heading, " ")+1]
	if end := strings.Index(rest, "\n"+level); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// exUnreleased returns the CHANGELOG text from "## [Unreleased]" up to the
// next release heading.
func exUnreleased(t *testing.T, changelog string) string {
	t.Helper()
	changelog = strings.ReplaceAll(changelog, "\r\n", "\n")
	start := strings.Index(changelog, "## [Unreleased]")
	require.GreaterOrEqual(t, start, 0, "CHANGELOG.md has no [Unreleased] section")
	rest := changelog[start+len("## [Unreleased]"):]
	if end := strings.Index(rest, "\n## ["); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// [EXPECT-001][AC-048] (FR-045) The README describes expected-activity
// windows: every action of Table 4.1 with its settle periods and bounds, what
// is suppressed and what still alerts, the powered-off state, the restart
// grace and the accepted residuals.
func TestEX_AC048_ReadmeDescribesExpectedActivity(t *testing.T) {
	readme := readRepoFile(t, readmePath)
	section := exDocsSection(t, readme, "## Expected Activity")

	// Every action that opens a window is named. The list comes from the policy
	// table, so a thirteenth action cannot be added without a README change.
	for _, action := range models.ExpectActions() {
		assert.Contains(t, section, "`"+action+"`", "the README must name the action %s", action)
	}

	groups := map[string][]string{
		"settle periods and return bounds": {
			"3 minutes", "15 minutes", "1 minute", "10 minutes", "2 minutes after the command ends",
			"35 minutes", "55 minutes", "25 minutes", "37 minutes", "6 hours 2 minutes",
		},
		"what is suppressed and recorded": {
			"`info` event", "linked to the command", "sends no notification", "Nothing is dropped",
			"by auto-update policy for", "by the auto-patch schedule",
		},
		"what still alerts": {
			"`container_oom` is never suppressed", "Containers the command does not target",
			"excluded from a rule", "Anything after the window closes",
		},
		"escalation": {
			"one **escalation**", "at most one escalation", "at most 20 container escalations",
			"5 per device per rolling hour", "operator's display name",
		},
		"the powered-off state": {
			"powered-off state", "no time limit", "never hides a later outage",
		},
		"the restart grace": {
			"2 minutes after the server starts", "45 seconds", "4 minutes from the first start",
			"restart grace shortened", "Fail open",
		},
		"the dashboard": {
			"Expected offline", "Offline (expected)", "GET /api/v1/activity",
		},
		"the residuals (NFR-011, ADD R-2 to R-6)": {
			"stays hidden until the window ends", "dies inside a window and comes back raises nothing",
			"is not pushed", "Work outside rIOt still alerts", "105 seconds", "never happens",
			"A push can be lost", "A stuck notification backend", "own device only",
		},
		"behaviour changes (QA-8)": {
			"status `warning`", "raises no `came online` event", "2 minutes after a server start",
		},
		"the upgrade": {
			"No agent upgrade", "no `install.sh` re-run", "000033", "second time",
		},
	}
	for name, terms := range groups {
		for _, term := range terms {
			assert.Contains(t, section, term, "the README section must cover %s: missing %q", name, term)
		}
	}

	// The API and WebSocket reference name the new endpoint and message.
	assert.Contains(t, readme, "| `GET` | `/api/v1/activity` |", "the API table must list the activity endpoint")
	assert.Contains(t, readme, "### Activity endpoint", "the activity endpoint needs its own reference")
	assert.Contains(t, readme, "| `activity_update` |", "the WebSocket table must list activity_update")
	assert.Contains(t, readme, "(#expected-activity)", "the rest of the README must link to the new section")
}

// [EXPECT-001][AC-048] (FR-046) The CHANGELOG entry says that no agent
// upgrade and no install.sh re-run is needed.
func TestEX_AC048_ChangelogSaysNoAgentUpgrade(t *testing.T) {
	unreleased := exUnreleased(t, readRepoFile(t, changelogPath))

	var lines []string
	for _, line := range strings.Split(unreleased, "\n") {
		if strings.Contains(line, "[EXPECT-001]") {
			lines = append(lines, line)
		}
	}
	require.NotEmpty(t, lines, "[Unreleased] must have EXPECT-001 entries")

	found := false
	for _, line := range lines {
		if strings.Contains(line, "no agent upgrade") && strings.Contains(line, "no `install.sh` re-run") {
			found = true
			break
		}
	}
	assert.True(t, found, "an [EXPECT-001] entry must say that no agent upgrade and no `install.sh` re-run is required")

	all := strings.Join(lines, "\n")
	for _, term := range []string{
		"`000033_expected_activity`", "GET /api/v1/activity", "activity_update", "restart grace",
		"**Escalation messages contain the operator's display name**", "status is `warning`",
		"TestCheckDockerEvent_DieDowngradedDuringUpdate",
	} {
		assert.Contains(t, all, term, "the [EXPECT-001] entries must cover %q", term)
	}
}

// [EXPECT-001][AC-048] The CHANGELOG puts the story under the headings the
// format requires.
func TestEX_AC048_ChangelogEntriesUseTheExpectedHeadings(t *testing.T) {
	unreleased := exUnreleased(t, readRepoFile(t, changelogPath))
	var heading string
	seen := map[string]bool{}
	for _, line := range strings.Split(unreleased, "\n") {
		if strings.HasPrefix(line, "### ") {
			heading = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			continue
		}
		if strings.Contains(line, "[EXPECT-001]") {
			seen[heading] = true
		}
	}
	for _, want := range []string{"Added", "Changed", "Upgrade notes"} {
		assert.True(t, seen[want], "[Unreleased] must have an [EXPECT-001] entry under %q", want)
	}
}
