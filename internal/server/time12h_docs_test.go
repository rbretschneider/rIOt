package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Paths are relative to internal/server, where go test runs.
const (
	envExamplePath    = "../../.env.example"
	composePath       = "../../docker-compose.yml"
	uiReadmePath      = "../../web/src/components/ui/README.md"
	timeDisplayAnchor = "## Time Display"
)

// normalised returns doc with LF line endings so a check does not depend on the
// checkout's autocrlf setting.
func normalised(doc string) string { return strings.ReplaceAll(doc, "\r\n", "\n") }

// mentions and omits assert on a document without printing it: assert.Contains
// would dump a whole README into the failure message.
func mentions(t *testing.T, doc, want, why string) {
	t.Helper()
	assert.True(t, strings.Contains(doc, want), "%s: missing %q", why, want)
}

func omits(t *testing.T, doc, unwanted, why string) {
	t.Helper()
	assert.False(t, strings.Contains(doc, unwanted), "%s: still contains %q", why, unwanted)
}

// [TIME-12H-001][AC-068] The README "Time Display" section, the environment
// table and the API section cover FR-070, and the stale statements are gone.
func TestTIME12H_AC068_ReadmeTimeDisplay(t *testing.T) {
	readme := normalised(readRepoFile(t, readmePath))
	section := exDocsSection(t, readme, timeDisplayAnchor)

	for _, want := range []string{
		"RIOT_DISPLAY_TZ", "TZ", "riot-time-zone", "riot-hour-cycle",
		"**Server**", "**Local**", "**UTC**", "12-hour", "24-hour",
		"`12h`", "`24h`", "`localStorage`",
		"/api/v1/server/time-display",
		"`2026-10-27 1:05:09 PM EDT`",
	} {
		mentions(t, section, want, "[AC-068] the Time Display section")
	}

	t.Run("legacy values", func(t *testing.T) {
		mentions(t, section, "An old `UTC` becomes **Server**", "[AC-068] the legacy UTC mapping (FR-049)")
		mentions(t, section, "An old `Local` stays **Local**", "[AC-068] the legacy Local mapping (FR-049)")
	})

	t.Run("RIOT_DISPLAY_TZ is presented as preferred over TZ", func(t *testing.T) {
		mentions(t, section, "It is the preferred setting", "[INFO-3] RIOT_DISPLAY_TZ must be named as preferred")
		mentions(t, section, "`RIOT_DISPLAY_TZ`, then `TZ`, then UTC", "[AC-068] the fallback chain (FR-001)")
		mentions(t, section, "also changes the server process's own zone", "[INFO-3] TZ also changes the process zone and log offsets")
		mentions(t, section, "WARN", "[AC-068] the validation behaviour")
	})

	t.Run("server-written text", func(t *testing.T) {
		for _, want := range []string{"patch badge", "`Generated` line", "event messages", "UTC whatever the display zone is"} {
			mentions(t, section, want, "[AC-068] server-written text and UTC data")
		}
	})

	t.Run("only the held summary is said to be pushed (AD-016)", func(t *testing.T) {
		mentions(t, section, "only the held-escalation summary is sent to notification channels", "[AD-016]")
		mentions(t, section, "are not pushed", "[AD-016]")
		omits(t, section, "powered-off and expected-offline messages are pushed", "[AD-016]")
	})

	t.Run("environment table", func(t *testing.T) {
		mentions(t, readme, "| `RIOT_DISPLAY_TZ` |", "[FR-070a] the environment variable table")
		mentions(t, readme, "| `TZ` |", "the TZ fallback in the environment variable table")
	})

	t.Run("API section", func(t *testing.T) {
		mentions(t, readme, "| `GET` | `/api/v1/server/time-display` |", "[AC-068] the API section")
	})

	t.Run("stale statements are gone", func(t *testing.T) {
		omits(t, readme, "always show UTC", "[AC-068] the 'always show UTC' sentence must be replaced")
		omits(t, readme, "use the server's local time zone", "[R-4] the event message sentence must be replaced")
		omits(t, readme, "back by 14:32", "[R-4] the 24-hour event message example must be replaced")
	})
}

// [TIME-12H-001][AC-069] The configuration examples carry a commented
// RIOT_DISPLAY_TZ with an explanation.
func TestTIME12H_AC069_ConfigExamplesAndUIReadme(t *testing.T) {
	for _, tc := range []struct{ name, path, line string }{
		{".env.example", envExamplePath, "# RIOT_DISPLAY_TZ=America/New_York"},
		{"docker-compose.yml", composePath, "# - RIOT_DISPLAY_TZ=America/New_York"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := normalised(readRepoFile(t, tc.path))
			idx := strings.Index(doc, tc.line)
			require.GreaterOrEqual(t, idx, 0, "[AC-069] %s must carry the commented line %q", tc.name, tc.line)
			before := doc[:idx]
			lines := strings.Split(strings.TrimRight(before, " \n"), "\n")
			require.NotEmpty(t, lines)
			assert.Contains(t, lines[len(lines)-1], "#", "[AC-069] the line before the example must be an explanatory comment")
			mentions(t, before[max(0, len(before)-600):], "display", "[AC-069] the explanation must say what the setting is for")
		})
	}
}

// [TIME-12H-001][AC-069] The UI README documents the 12-hour forms, the clock
// form and the server-zone validation. The README is owned by the web half of
// the story.
func TestTIME12H_AC069_UIReadmeDocumentsTwelveHourForms(t *testing.T) {
	doc := normalised(readRepoFile(t, uiReadmePath))
	for _, want := range []string{"12-hour", "clock", "formatDateOnly", "acceptServerZone", "riot-hour-cycle"} {
		mentions(t, doc, want, "[AC-069] the UI README")
	}
}
