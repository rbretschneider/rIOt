package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedLog is one captured slog record with its attributes flattened.
type recordedLog struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

// logRecorder is a slog.Handler that keeps every record, so a test can count
// warnings and read what was logged without touching slog.Default.
type logRecorder struct {
	mu      sync.Mutex
	records []recordedLog
}

func (l *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (l *logRecorder) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *logRecorder) WithGroup(string) slog.Handler            { return l }
func (l *logRecorder) Handle(_ context.Context, r slog.Record) error {
	rec := recordedLog{Level: r.Level, Message: r.Message, Attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	l.records = append(l.records, rec)
	l.mu.Unlock()
	return nil
}

func (l *logRecorder) at(level slog.Level) []recordedLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []recordedLog
	for _, r := range l.records {
		if r.Level == level {
			out = append(out, r)
		}
	}
	return out
}

func resolve(displayTZ, tz string) (DisplayZone, *logRecorder) {
	rec := &logRecorder{}
	return ResolveDisplayZone(displayTZ, tz, slog.New(rec)), rec
}

// assertNoLoadLocationText fails when any captured record carries the error
// text time.LoadLocation produces, which echoes the raw input (C-4).
func assertNoLoadLocationText(t *testing.T, rec *logRecorder) {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, r := range rec.records {
		all := r.Message
		for k, v := range r.Attrs {
			all += " " + k + "=" + v
		}
		assert.NotContains(t, all, "unknown time zone", "[C-4] the LoadLocation error must never be logged: %q", all)
		assert.NotContains(t, all, "invalid location name", "[C-4] the LoadLocation error must never be logged: %q", all)
	}
}

func TestTIME12H_AC042_RiotDisplayTZUsed(t *testing.T) {
	zone, rec := resolve("America/New_York", "")

	assert.Equal(t, "America/New_York", zone.Name)
	assert.Equal(t, DisplayZoneSourceEnv, zone.Source)
	assert.Equal(t, "America/New_York", zone.Location.String())
	assert.Empty(t, rec.at(slog.LevelWarn), "no warning for a valid value")
	info := rec.at(slog.LevelInfo)
	require.Len(t, info, 1, "exactly one informational line")
	assert.Equal(t, "America/New_York", info[0].Attrs["zone"])
	assert.Equal(t, "RIOT_DISPLAY_TZ", info[0].Attrs["source"])
}

func TestTIME12H_AC042_RiotDisplayTZWinsOverValidTZ(t *testing.T) {
	zone, _ := resolve("America/New_York", "Europe/Berlin")
	assert.Equal(t, "America/New_York", zone.Name, "RIOT_DISPLAY_TZ is tried before TZ")
	assert.Equal(t, DisplayZoneSourceEnv, zone.Source)
}

func TestTIME12H_AC042_LoadConfigReadsBothValuesVerbatim(t *testing.T) {
	t.Setenv("RIOT_DISPLAY_TZ", "  America/New_York  ")
	t.Setenv("TZ", "Europe/Berlin ")

	cfg := LoadConfig()

	assert.Equal(t, "  America/New_York  ", cfg.DisplayTZRaw, "LoadConfig keeps the raw value; Start validates it")
	assert.Equal(t, "Europe/Berlin ", cfg.TZRaw)
}

func TestTIME12H_AC043_ValueTrimmed(t *testing.T) {
	zone, rec := resolve("  America/New_York  ", "")

	assert.Equal(t, "America/New_York", zone.Name, "the effective name is exactly the trimmed value")
	assert.Equal(t, "America/New_York", zone.Location.String())
	assert.Empty(t, rec.at(slog.LevelWarn))
}

func TestTIME12H_AC044_TZFallback(t *testing.T) {
	zone, rec := resolve("", "America/Chicago")

	assert.Equal(t, "America/Chicago", zone.Name)
	assert.Equal(t, DisplayZoneSourceTZ, zone.Source)
	assert.Empty(t, rec.at(slog.LevelWarn))
	info := rec.at(slog.LevelInfo)
	require.Len(t, info, 1)
	assert.Equal(t, "TZ", info[0].Attrs["source"])
}

func TestTIME12H_AC045_UnsetAndBlankDefaultToUTCWithoutWarning(t *testing.T) {
	for name, in := range map[string][2]string{
		"both unset":          {"", ""},
		"both whitespace":     {"   ", "   "},
		"tab and newline":     {"\t\n", " \r\n "},
		"blank then unset TZ": {"  ", ""},
	} {
		t.Run(name, func(t *testing.T) {
			zone, rec := resolve(in[0], in[1])

			assert.Equal(t, "UTC", zone.Name)
			assert.Equal(t, DisplayZoneSourceDefault, zone.Source)
			assert.Same(t, time.UTC, zone.Location)
			assert.Empty(t, rec.at(slog.LevelWarn), "[FR-003] a blank source is unset and silent")
			info := rec.at(slog.LevelInfo)
			require.Len(t, info, 1, "exactly one informational line")
			assert.Equal(t, "default", info[0].Attrs["source"])
		})
	}
}

func TestTIME12H_AC046_InvalidValuesRejectedSanitizedOneWarning(t *testing.T) {
	cases := []struct {
		name  string
		value string
		// logged is the exact expected logged value; "" skips the exact check.
		logged string
	}{
		{"unknown zone", "Mars/Olympus", "Mars/Olympus"},
		{"Local", "Local", "Local"},
		{"relative path", "../../etc/passwd", "../../etc/passwd"},
		{"shell metacharacters", "America/New_York;rm -rf /", "America/New_York;rm -rf /"},
		{"posix localtime form", ":/etc/localtime", ":/etc/localtime"},
		{"65 letters", strings.Repeat("a", 65), strings.Repeat("a", 64)},
		{"newline and ANSI escape", "America/New\nYork\x1b[31m", "America/NewYork[31m"},
		{"too many segments", "A/B/C/D", "A/B/C/D"},
		{"segment starting with a digit", "America/1abc", "America/1abc"},
		{"absolute path", "/etc/passwd", "/etc/passwd"},
		{"windows path", `C:\Windows`, `C:\Windows`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				zone, rec := resolve(tc.value, "")

				assert.Equal(t, "UTC", zone.Name, "an invalid value falls back to UTC")
				assert.Equal(t, DisplayZoneSourceDefault, zone.Source)
				warns := rec.at(slog.LevelWarn)
				require.Len(t, warns, 1, "exactly one warning")
				assert.Equal(t, "RIOT_DISPLAY_TZ", warns[0].Attrs["source"], "the warning names the source variable")
				assert.Equal(t, tc.logged, warns[0].Attrs["value"])
				assert.LessOrEqual(t, utf8.RuneCountInString(warns[0].Attrs["value"]), 64, "the logged value is cut to 64 runes")
				assertNoLoadLocationText(t, rec)
			})
		})
	}

	// C-2 / SEC-002: Cf runes (bidi override and isolate, zero-width space) are
	// removed along with Cc, and invalid UTF-8 never reaches the log raw.
	t.Run("format characters", func(t *testing.T) {
		zone, rec := resolve("America/New_York\u202e\u2066\u200b\ufeff", "")

		assert.Equal(t, "UTC", zone.Name)
		warns := rec.at(slog.LevelWarn)
		require.Len(t, warns, 1)
		logged := warns[0].Attrs["value"]
		assert.Equal(t, "America/New_York", logged)
		for _, r := range []rune{'\u202e', '\u2066', '\u200b', '\ufeff'} {
			assert.NotContains(t, logged, string(r), "[C-2] U+%04X must not survive in the logged value", r)
		}
		assertNoLoadLocationText(t, rec)
	})

	t.Run("invalid UTF-8", func(t *testing.T) {
		zone, rec := resolve("America/\xff\xfeNew_York", "")

		assert.Equal(t, "UTC", zone.Name)
		warns := rec.at(slog.LevelWarn)
		require.Len(t, warns, 1)
		logged := warns[0].Attrs["value"]
		assert.True(t, utf8.ValidString(logged), "[C-2] the logged value must be valid UTF-8, got %q", logged)
		assert.Equal(t, "America/\ufffdNew_York", logged)
		assertNoLoadLocationText(t, rec)
	})

	t.Run("no control or format rune survives in any case", func(t *testing.T) {
		hostile := "A\x00B\x07C\u0085D\u200fE\u2028F\u202aG\u2069H\u00adI"
		_, rec := resolve(hostile, "")
		warns := rec.at(slog.LevelWarn)
		require.Len(t, warns, 1)
		for _, r := range warns[0].Attrs["value"] {
			assert.False(t, unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r), "[C-2] rune U+%04X is Cc/Cf", r)
		}
	})
}

func TestTIME12H_AC046_LongHostileValueIsCutAtSixtyFourRunes(t *testing.T) {
	_, rec := resolve(strings.Repeat("\u00e9", 500), "")

	warns := rec.at(slog.LevelWarn)
	require.Len(t, warns, 1)
	assert.Equal(t, 64, utf8.RuneCountInString(warns[0].Attrs["value"]), "bounded growth: at most 64 runes per logged value")
}

func TestTIME12H_AC047_InvalidFirstValidSecond(t *testing.T) {
	zone, rec := resolve("Mars/Olympus", "America/Denver")

	assert.Equal(t, "America/Denver", zone.Name)
	assert.Equal(t, DisplayZoneSourceTZ, zone.Source)
	warns := rec.at(slog.LevelWarn)
	require.Len(t, warns, 1, "exactly one warning")
	assert.Equal(t, "RIOT_DISPLAY_TZ", warns[0].Attrs["source"])
	info := rec.at(slog.LevelInfo)
	require.Len(t, info, 1)
	assert.Equal(t, "TZ", info[0].Attrs["source"])
	assertNoLoadLocationText(t, rec)
}

func TestTIME12H_AC047_ValidFirstInvalidSecondIsNotWarned(t *testing.T) {
	zone, rec := resolve("America/Denver", "Mars/Olympus")

	assert.Equal(t, "America/Denver", zone.Name)
	assert.Empty(t, rec.at(slog.LevelWarn), "a source after the chosen one is never looked at")
}

func TestTIME12H_AC048_BothInvalidTwoWarningsOneInfo(t *testing.T) {
	zone, rec := resolve("Mars/Olympus", ":/etc/localtime")

	assert.Equal(t, "UTC", zone.Name)
	assert.Equal(t, DisplayZoneSourceDefault, zone.Source)
	warns := rec.at(slog.LevelWarn)
	require.Len(t, warns, 2, "exactly two warnings")
	assert.Equal(t, "RIOT_DISPLAY_TZ", warns[0].Attrs["source"])
	assert.Equal(t, "TZ", warns[1].Attrs["source"])
	assert.Len(t, rec.at(slog.LevelInfo), 1, "exactly one informational line")
	assertNoLoadLocationText(t, rec)
}

func TestTIME12H_AC048_InvalidTZWithBlankDisplayTZWarnsOnce(t *testing.T) {
	zone, rec := resolve("  ", "Mars/Olympus")

	assert.Equal(t, "UTC", zone.Name)
	warns := rec.at(slog.LevelWarn)
	require.Len(t, warns, 1)
	assert.Equal(t, "TZ", warns[0].Attrs["source"])
}

func TestTIME12H_AC049_ResolveLeavesTimeLocalUnchanged(t *testing.T) {
	// Not parallel: time.Local is process-wide. It is restored in t.Cleanup.
	old := time.Local
	t.Cleanup(func() { time.Local = old })
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	time.Local = tokyo

	zone, _ := resolve("America/New_York", "")
	_, _ = resolve("Local", "Mars/Olympus")

	assert.Same(t, tokyo, time.Local, "[FR-007] the process-wide local zone is never assigned")
	assert.Equal(t, "America/New_York", zone.Name)
	assert.NotSame(t, time.Local, zone.Location, "the display zone is its own location")
}

func TestTIME12H_AC049_LocalIsNeverAdoptedAsTheProcessZone(t *testing.T) {
	for _, src := range [][2]string{{"Local", ""}, {"", "Local"}} {
		zone, rec := resolve(src[0], src[1])
		assert.Equal(t, "UTC", zone.Name, "Local would silently adopt the process zone, so it is rejected")
		assert.Len(t, rec.at(slog.LevelWarn), 1)
	}
}

func TestTIME12H_AC046_ValidZoneShapesAreAccepted(t *testing.T) {
	for _, name := range []string{"UTC", "America/New_York", "Etc/GMT+5", "America/Argentina/Buenos_Aires", "US/Eastern", "Asia/Kathmandu", "EST5EDT"} {
		t.Run(name, func(t *testing.T) {
			zone, rec := resolve(name, "")
			assert.Equal(t, name, zone.Name, "%s is a valid IANA name", name)
			assert.Empty(t, rec.at(slog.LevelWarn))
		})
	}
}

func TestTIME12H_AC046_SanitizeZoneValue(t *testing.T) {
	cases := map[string]string{
		"plain":                  "plain",
		"a\nb\rc\td":             "abcd",
		"esc\x1b[0m":             "esc[0m",
		"rlo\u202eend":           "rloend",
		"zw\u200bsp":             "zwsp",
		"bad\xffbyte":            "bad\ufffdbyte",
		"":                       "",
		strings.Repeat("x", 100): strings.Repeat("x", 64),
	}
	for in, want := range cases {
		assert.Equal(t, want, sanitizeZoneValue(in), fmt.Sprintf("sanitizeZoneValue(%q)", in))
	}
}
