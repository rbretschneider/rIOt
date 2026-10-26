package scripts

import (
	"strings"
	"testing"
)

// SHUTDOWN-SUDO-001 documentation checks (ADD §8, AC-027 and AC-028). They read
// the README and CHANGELOG only.

// unreleasedSection returns the CHANGELOG text from "## [Unreleased]" up to the
// next "## [" release heading.
func unreleasedSection(t *testing.T, changelog string) string {
	t.Helper()
	start := strings.Index(changelog, "## [Unreleased]")
	if start < 0 {
		t.Fatal("CHANGELOG.md has no [Unreleased] section")
	}
	rest := changelog[start+len("## [Unreleased]"):]
	if end := strings.Index(rest, "\n## ["); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestDocs_SS_AC027_NoStaleShutdownStatements(t *testing.T) {
	readme := readPackagingFile(t, "../README.md")
	for _, stale := range []string{
		"until SHUTDOWN-SUDO-001",
		"grants the `riot` user no sudo rule for `systemctl poweroff`",
	} {
		if strings.Contains(readme, stale) {
			t.Errorf("[AC-027] README still says %q", stale)
		}
	}
	if !strings.Contains(readme, "Allow remote shutdown from dashboard?") {
		t.Error("[AC-027] README does not document the installer question")
	}
}

func TestDocs_SS_AC028_FleetActionNamed(t *testing.T) {
	readme := readPackagingFile(t, "../README.md")
	unreleased := unreleasedSection(t, readPackagingFile(t, "../CHANGELOG.md"))
	for _, doc := range []struct{ name, text string }{
		{"README", readme},
		{"CHANGELOG [Unreleased]", unreleased},
	} {
		for _, want := range []string{"hawaii", "vermont", "PiVPN", "PiCUPS", "allow_shutdown: true"} {
			if !strings.Contains(doc.text, want) {
				t.Errorf("[AC-028] %s does not contain %q", doc.name, want)
			}
		}
	}
	found := false
	for _, line := range strings.Split(unreleased, "\n") {
		if strings.Contains(line, "riot ALL=(root) NOPASSWD:") && strings.Contains(line, "poweroff") {
			found = true
			break
		}
	}
	if !found {
		t.Error("[AC-028] CHANGELOG [Unreleased] does not show the poweroff sudoers line")
	}
}
