package scripts

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// EXTRA-UPD-001 documentation checks (ADD section 8, AC-043 and SEC-008). They read
// the README, docs/ops/os-maintenance.md and the CHANGELOG only.
//
// The negative check (SEC-008) holds today and always. The positive checks (every
// FR-063 topic, the FR-064 steps, the FR-065 entry) can only pass once the technical
// writer has written the docs; until the README names commands.allow_extra_updaters
// they skip with a message. The writer must keep them, not delete them: once the
// docs land they run on every build.

const extraUpdatersDocsMarker = "commands.allow_extra_updaters"

func extraDocs(t *testing.T) (readme, ops, changelog string) {
	t.Helper()
	readme = readPackagingFile(t, "../README.md")
	ops = readPackagingFile(t, "../docs/ops/os-maintenance.md")
	changelog = readPackagingFile(t, "../CHANGELOG.md")
	return readme, ops, changelog
}

// docsLanded reports whether the story's documentation has been written.
func docsLanded(readme, ops string) bool {
	return strings.Contains(readme, extraUpdatersDocsMarker) || strings.Contains(ops, extraUpdatersDocsMarker)
}

// manualCopy matches a copy, move or sync command on one line that names
// /var/tmp/dietpi.
var manualCopy = regexp.MustCompile(`(?i)\b(cp|mv|rsync|install|ln|tar)\b[^\n]*/var/tmp/dietpi`)

func TestDocs_EU_SEC008_NoManualCopyOfVarTmpDietpi(t *testing.T) {
	readme, ops, changelog := extraDocs(t)
	for _, doc := range []struct{ name, text string }{{"README", readme}, {"docs/ops/os-maintenance.md", ops}, {"CHANGELOG", changelog}} {
		if m := manualCopy.FindString(doc.text); m != "" {
			t.Errorf("[SEC-008] %s tells the operator to copy /var/tmp/dietpi content by hand: %q (run as root, it can import attacker-placed symlinks)", doc.name, m)
		}
		if strings.Contains(doc.text, "cp -an /var/tmp/dietpi") {
			t.Errorf("[SEC-008] %s still carries the removed 'cp -an /var/tmp/dietpi' step", doc.name)
		}
	}
}

// Every FR-063 topic, matched by a phrase the writer must use. The phrases are
// deliberately plain words so the writer is free in how a sentence is built.
var fr063Topics = []struct{ topic, phrase string }{
	{"the extra-updater step and its order after apt", "after apt"},
	{"the modes it runs in", "repair"},
	{"the opt-in on the agent side", "allow_extra_updaters"},
	{"the opt-in on the root side", "/etc/riot-maintenance/extra-updaters.json"},
	{"how to revoke the opt-in", "revoke"},
	{"the time limit", "40 minutes"},
	{"the output cap", "128 KiB"},
	{"truthful outcomes", "timed_out"},
	{"the reboot rule", "reboot"},
	{"the meaning of unknown", "unknown"},
	{"DietPi's service stop and restart", "stops and restarts"},
	{"autopurge", "autopurge"},
	{"the survey", "survey"},
	{"the CONFIG_CHECK_APT_UPDATES=2 caveat", "CONFIG_CHECK_APT_UPDATES=2"},
	{"the group K badge rule: Needs you only when work is waiting", "Needs you"},
	{"the Manual badge", "Manual"},
}

func TestDocs_EU_AC043_OpsDocCoversEveryTopic(t *testing.T) {
	readme, ops, _ := extraDocs(t)
	if !docsLanded(readme, ops) {
		t.Skip("AC-043: the EXTRA-UPD-001 documentation has not been written yet (technical writer). This test runs once README or os-maintenance.md names " + extraUpdatersDocsMarker + ".")
	}
	for _, topic := range fr063Topics {
		if !strings.Contains(ops, topic.phrase) {
			t.Errorf("[AC-043] docs/ops/os-maintenance.md does not cover %s (expected the words %q)", topic.topic, topic.phrase)
		}
	}
}

func TestDocs_EU_AC043_ReadmeStepsPerServer(t *testing.T) {
	readme, ops, _ := extraDocs(t)
	if !docsLanded(readme, ops) {
		t.Skip("AC-043: the EXTRA-UPD-001 documentation has not been written yet (technical writer).")
	}
	for _, want := range []string{
		"hawaii", "vermont", "PiVPN", "PiCUPS",
		"CONFIG_CHECK_DIETPI_UPDATES=1", "/boot/dietpi.txt", extraUpdatersDocsMarker, "/etc/riot/agent.yaml",
		"install.sh", "doctor",
		"AGENT-HARDEN-001", "PATCH-FLOW-001", "SHUTDOWN-SUDO-001",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("[AC-043] README does not contain %q (FR-064)", want)
		}
	}
	if !regexp.MustCompile(`(?i)reboot[^\n]*(9|nine)[^\n]*10|(9|nine)[^\n]*10[^\n]*reboot`).MatchString(readme) {
		t.Error("[AC-043] README does not say the first DietPi 9 to 10 update needs a reboot (FR-064)")
	}
	if !regexp.MustCompile(`(?m)^\s*1\.\s`).MatchString(readme) {
		t.Error("[AC-043] README has no numbered steps")
	}
}

func TestDocs_EU_AC043_ChangelogEntry(t *testing.T) {
	readme, ops, changelog := extraDocs(t)
	if !docsLanded(readme, ops) {
		t.Skip("AC-043: the EXTRA-UPD-001 documentation has not been written yet (technical writer).")
	}
	unreleased := unreleasedSection(t, changelog)
	for _, want := range []string{"EXTRA-UPD-001", "DietPi", extraUpdatersDocsMarker, "installer", "Manual", "Needs you"} {
		if !strings.Contains(unreleased, want) {
			t.Errorf("[AC-043] CHANGELOG [Unreleased] does not contain %q (FR-065)", want)
		}
	}
	if !strings.Contains(strings.ToLower(unreleased), "privilege") {
		t.Error("[AC-043] CHANGELOG [Unreleased] does not name the new privilege (FR-065); say that no sudoers line is added if that is the case")
	}
}

// SEC-001 residual and the owner rule: once the docs exist they must state both.
func TestDocs_EU_AC043_SecurityStatements(t *testing.T) {
	readme, ops, _ := extraDocs(t)
	if !docsLanded(readme, ops) {
		t.Skip("AC-043: the EXTRA-UPD-001 documentation has not been written yet (technical writer).")
	}
	for _, want := range []string{"MichaIng", "DEV_GITOWNER"} {
		if !strings.Contains(ops, want) {
			t.Errorf("[AC-043] docs/ops/os-maintenance.md does not state the update-source owner rule (%q)", want)
		}
	}
}

func TestDocs_EU_DocsFilesExist(t *testing.T) {
	for _, path := range []string{"../README.md", "../CHANGELOG.md", "../docs/ops/os-maintenance.md"} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s must exist: %v", path, err)
		}
	}
}

// Added by QA Engineer. Covers AC-043 and ADD section 4's documentation row. The
// ADD requires the docs to carry the SEC-001 residual and upstream note, the
// cron advice, the /var/tmp/dietpi trust rule and "DietPi's own mounts take effect
// normally". QA adds the two limits the security review accepted as INFO: the
// single-process event dedupe and the backward clock jump on the 60 minute
// minimum interval. Like the other positive checks, this skips until the docs land.
func TestDocs_EU_AC043_QA_ResidualsAndAcceptedLimits(t *testing.T) {
	readme, ops, _ := extraDocs(t)
	if !docsLanded(readme, ops) {
		t.Skip("AC-043: the EXTRA-UPD-001 documentation has not been written yet (technical writer).")
	}
	lower := strings.ToLower(ops)
	for _, want := range []struct{ topic, phrase string }{
		{"the /var/tmp/dietpi trust rule", "/var/tmp/dietpi"},
		{"the cron.daily advice (avoid 06:25)", "06:25"},
		{"the upstream note on DietPi's own shared /tmp", "upstream"},
		{"the 60 minute minimum between DietPi starts", "60 minutes"},
		{"the clock-jump limit of that minimum", "clock"},
		{"the single-server-process limit of the event dedupe", "single"},
		{"the patch window of at least 90 minutes", "90 minutes"},
		{"DietPi's own mounts take effect normally", "/boot/firmware"},
	} {
		if !strings.Contains(lower, strings.ToLower(want.phrase)) {
			t.Errorf("[AC-043] docs/ops/os-maintenance.md does not cover %s (expected the words %q)", want.topic, want.phrase)
		}
	}
}
