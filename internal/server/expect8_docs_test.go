package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ex8BudgetSentence is the abuse budget in the terms of the security review
// (EXPECT-008 C-8). Ryan's acceptance of it is recorded in the ADD; the README
// must state it word for word so a reader can find what was accepted.
const ex8BudgetSentence = "A holder of a device's key can make offline alerts silent for less than 60 minutes per day " +
	"through reinstall notices, and less than 120 minutes per day together with agent auto-update; " +
	"each silent outage lasts under about 10 minutes, is stored as an info event, and pages if it lasts past the bound."

// [EXPECT-008][AC-027] (FR-029, FR-030, C-8) The README describes installer
// reinstalls: when the notice is sent and when it is not, the one status line
// and its reasons, the skipped trust settings, the window, the outcomes, the
// cap, the label, the residuals, the budget sentence and the rollout. The
// CHANGELOG entry carries the five rollout steps and the rollback order.
func TestEX8_AC027_ReadmeAndChangelog(t *testing.T) {
	readme := normalised(readRepoFile(t, readmePath))
	section := exDocsSection(t, readme, "### Installer reinstalls")

	groups := map[string][]string{
		"when the notice is sent (FR-001, FR-002)": {
			"re-run on a host that already has `/etc/riot/agent.yaml` with a device ID and a device key",
			"A fresh install, a run with `--bootstrap-key` and macOS never send one",
			"loaded and running", "directly before it stops the agent", "A run that ends earlier sends nothing",
			"The server URL on the installer command line is ignored",
		},
		"the one status line and its reasons (FR-006)": {
			"==> Server notified: agent reinstall (offline alerts paused up to 10m)",
			"==> Server not notified (<reason>); a device-offline alert may follow",
			"exactly one line", "its exit status is unchanged",
			"`curl not available`", "`agent.yaml not readable`", "`TLS verification is off in agent.yaml`",
			"`trust settings not supported`", "`reinstall tracking paused on server`",
			"`device already offline on server`", "`server could not record it`",
			"`unexpected server response`", "`server does not support reinstall notices`",
			"`device key rejected`", "`server busy`", "`server error`", "`server name not resolved`",
			"`server unreachable`", "`timed out`", "`TLS verification failed`", "`connection failed`",
			"`internal error`",
		},
		"best effort, bounded, key handling (FR-004, FR-005, NFR-003)": {
			"hard limit of 10 seconds", "never changes the exit status", "asks no question",
			"never appears on a command line", "at most 4096 bytes",
		},
		"the trust settings that are skipped (FR-003)": {
			"Trust settings the installer skips",
			"`tls_verify: false` without a pinned `/etc/riot/server.crt`",
			"`server_cert_pin` without a pinned `/etc/riot/server.crt`",
			"`ca_cert_file` other than `/etc/riot/ca.crt` or `/etc/riot/server.crt`",
			"`/etc/riot/client.crt` and `/etc/riot/client.key`",
			"`tls_verify` value other than `true` or `false`",
			"The host's system certificate store is not consulted",
		},
		"what the server accepts (FR-007 to FR-012)": {
			"current key of the device that the URL names", "the server shows the device online",
			"no open reinstall record", "6 records in any rolling 24 hours",
			"`agent_reinstall_started`", "never stored as an event",
			"`agent_reinstall_unconfirmed` and `agent_reinstall_paused`",
		},
		"the window and its bound (FR-013 to FR-018)": {
			"at most 10 minutes", "`device_offline` for that device only", "A container that dies still alerts",
			"went offline during Agent reinstall by installer (expected, back by", "came online after Agent reinstall by installer (offline 52s)",
			"the device registers again after the notice", "whatever version the agent reports",
			"There is no settle period",
		},
		"the outcomes (FR-020 to FR-023)": {
			"`succeeded`", "`timed_out`", "one escalation", "still offline 10m after Agent reinstall by installer (record 1a2b3c4d)",
			"5 escalation pushes per device per hour",
			"was not confirmed within 10m (agent did not restart)", "`agent_reinstall_unconfirmed` warning",
		},
		"the rate cap (FR-010)": {
			"At most **6 reinstall records per device in any rolling 24 hours**", "counted separately from the cap of agent self-updates",
			"Agent reinstall tracking paused on hawaii: more than 6 reinstalls in 24h; offline alerts during reinstalls are not suppressed",
		},
		"the label and the record (FR-024 to FR-026)": {
			"`Reinstalling agent`", "`by installer`", "**Expected offline**", "`Not back 10m after Agent reinstall`",
			"`Agent reinstall`", "It is read-only", "never sent to an agent",
		},
		"coexistence with self-updates (FR-019)": {
			"the one with the latest start sets the label",
		},
		"the residuals (NFR-010)": {
			"A real outage that coincides with a reinstall is hidden for up to 10 minutes, then escalates once",
			"A run whose notice fails", "pages as before",
			"aborts after the notice and before the stop", "aborts after the stop",
			"No prompt runs between the stop and the start",
		},
		"rollout (FR-029)": {
			"This is a server deploy", "No device needs an agent upgrade, a configuration change or an extra `install.sh` re-run",
			"The first `install.sh` re-run on a registered device after the server deploy is the check",
			"`000035_installer_reinstall`", "**migrate down to schema 34 first, then deploy the previous image.**",
			"deletes every reinstall record",
			"| new | old |", "| old | new |",
		},
	}

	for name, terms := range groups {
		for _, term := range terms {
			mentions(t, section, term, "[AC-027] the README section must cover "+name)
		}
	}

	t.Run("C-8 the budget sentence, word for word", func(t *testing.T) {
		mentions(t, section, ex8BudgetSentence, "[C-8] the README must state the accepted abuse budget in the review's terms")
	})

	t.Run("the rest of the README links to the section", func(t *testing.T) {
		assert.GreaterOrEqual(t, strings.Count(readme, "(#installer-reinstalls)"), 3,
			"[AC-027] the install, alerting and expected-activity sections must link to Installer reinstalls")
		assert.Contains(t, readme, "**Installer reinstalls** need a server deploy and nothing on the devices",
			"[AC-027] the upgrade notes must carry the reinstall rollout")
	})

	unreleased := exUnreleased(t, normalised(readRepoFile(t, changelogPath)))
	var lines []string
	for _, line := range strings.Split(unreleased, "\n") {
		if strings.Contains(line, "[EXPECT-008]") {
			lines = append(lines, line)
		}
	}
	require.NotEmpty(t, lines, "[Unreleased] must have EXPECT-008 entries")
	all := strings.Join(lines, "\n")

	t.Run("CHANGELOG covers the feature", func(t *testing.T) {
		for _, term := range []string{
			"`agent_reinstall_started`", "`installer`", "agent_reinstall_unconfirmed", "agent_reinstall_paused",
			"==> Server notified: agent reinstall (offline alerts paused up to 10m)",
			"==> Server not notified (<reason>); a device-offline alert may follow",
			"at most 10 minutes", "6 per device per rolling 24 hours", "README.md#installer-reinstalls",
			"`origin`", "`riot-no-passphrase`",
		} {
			mentions(t, all, term, "[AC-027] the [EXPECT-008] entries must cover the feature")
		}
	})

	t.Run("CHANGELOG carries the budget", func(t *testing.T) {
		mentions(t, all, ex8BudgetSentence, "[C-8] the [EXPECT-008] entries must carry the accepted budget")
	})

	t.Run("CHANGELOG carries the five rollout steps (FR-029)", func(t *testing.T) {
		for _, term := range []string{
			"**Rollout: a server deploy, nothing on the devices.**",
			"1. Merge the PR.", "2. Tag the next release", "docker compose pull && docker compose up -d",
			"image digest", "schema is at version 35",
			"3. Devices: nothing.", "No agent upgrade, no configuration change and no extra installer re-run",
			"4. Order: deploy the server (step 2) before relying on silence",
			"5. First silent re-run",
		} {
			mentions(t, unreleased, term, "[AC-027] the CHANGELOG rollout steps")
		}
	})

	t.Run("CHANGELOG carries the migration and the rollback order (INFO-6)", func(t *testing.T) {
		for _, term := range []string{
			"`000035_installer_reinstall`",
			"migrate down to schema 34 before rolling the server image back",
			"deletes every reinstall record",
		} {
			mentions(t, unreleased, term, "[AC-027] the CHANGELOG migration and rollback notes")
		}
		assert.Less(t, strings.Index(unreleased, "migrate down to schema 34"), strings.Index(unreleased, "older binary treats"),
			"[INFO-6] the rollback order must come before the reason for it")
	})
}
