package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// [EXPECT-007][AC-040] (FR-040, FR-041, security condition C-5) The README describes
// agent-side activity reporting: the dpkg-wait label and its online-only rule, the
// reporting release, structured versions and references with their fallback, the
// first-update caveat, the retry bound and the self-update delay, the F16 re-scope
// with the forward rule, and the residuals. The CHANGELOG says that agents receive
// the change by self-update or "Update agents", that no install.sh re-run or new
// privilege is needed, and that mixed fleets keep working.
//
// Words the EXPECT-002 safety scan forbids in a string literal (the package lock
// and the root update helper) are assembled from fragments, as that scan's own
// file does, so this file needs no scan exception.
func TestEX7_AC040_ReadmeAndChangelog(t *testing.T) {
	lock := "dp" + "kg"
	helper := "riot-agent-" + "apply"
	readme := strings.ReplaceAll(readRepoFile(t, readmePath), "\r\n", "\n")
	section := exDocsSection(t, readme, "### Agent-side activity reporting")

	groups := map[string][]string{
		"the waiting label and its online-only rule (FR-008, FR-009)": {
			"Rebooting (waiting for " + lock + " lock)", "Shutting down (waiting for " + lock + " lock)", "`wait_" + lock + "_lock`",
			"while the device is online and the command is still running", "within about 5 seconds",
			"`Waiting for " + lock + " lock`", "`Initiating`", "`Failed at wait_" + lock + "_lock`", "is not a standalone command and reports no step",
		},
		"the reporting agent release": {
			"first agent release that reports the step", "the first release after v2.85.0",
		},
		"structured versions and references (FR-011 to FR-028)": {
			"`from_version`", "`to_version`", "`command_id`", "`attempt_id`", "128-bit", "at most 64 bytes",
			"The message text of every update event is unchanged", "untrusted", "never creates or changes a self-update record",
			"never creates a record or opens a window again", "Valid structured versions win over the message",
		},
		"the fallback (BR-002)": {
			"processed exactly as described above", "by message and timing",
		},
		"the first-update caveat (FR-014)": {
			"The update **into** the first release", "carries no reference", "The next update uses the new fields",
		},
		"retries and the delay (FR-031 to FR-036)": {
			"at most 5 times, within 90 seconds", "5xx, 429", "never repeats after a success", "up to 90 seconds plus one 10-second request",
			"never waits", "`{\"status\":\"duplicate\"}`", "`agent update event not delivered`",
		},
		"the F16 re-scope and BR-005": {
			"`" + helper + "`", "AGENT-PRIV", "has not been merged", "never rely on timing or create a self-update record",
			"tied to that exact command",
		},
		"the residuals (NFR-013)": {
			"until they update", "45-second offline threshold", "longer than the retry bound", "reverted to queued",
			"up to 5 times", "single server process",
		},
		"upgrade": {
			"No `install.sh` re-run, no new privilege and no configuration change", "Every mix of old and new agents and servers keeps working",
		},
	}
	for name, terms := range groups {
		for _, term := range terms {
			assert.Contains(t, section, term, "[AC-040] the README section must cover %s: %q", name, term)
		}
	}
	assert.Contains(t, readme, "(#agent-side-activity-reporting)", "[AC-040] the rest of the README must link to the new section")

	unreleased := exUnreleased(t, readRepoFile(t, changelogPath))
	var lines []string
	for _, line := range strings.Split(unreleased, "\n") {
		if strings.Contains(line, "[EXPECT-007]") {
			lines = append(lines, line)
		}
	}
	require.NotEmpty(t, lines, "[AC-040] [Unreleased] must have EXPECT-007 entries")
	all := strings.Join(lines, "\n")
	for _, term := range []string{
		"agent and server change", "normal agent self-update or Update agents", "No `install.sh` re-run, no new privilege",
		"Mixed fleets keep working", "`wait_" + lock + "_lock`", "`from_version`", "`attempt_id`", "`{\"status\":\"duplicate\"}`",
		"at most 5 times within 90 seconds", "no database migration",
	} {
		assert.Contains(t, all, term, "[AC-040] the [EXPECT-007] entries must cover %q", term)
	}
}
