package agent

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// captureSlog replaces the default logger for one test and returns the lines it
// wrote.
func captureSlog(t *testing.T) *lockedLog {
	t.Helper()
	out := &lockedLog{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return out
}

type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func loadConfigText(t *testing.T, text string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	cfg, err := LoadConfig(path)
	require.NoError(t, err, "the config must load whatever shape allow_extra_updaters has")
	return cfg
}

func TestLoadConfig_EU_AC014_UnknownIDOneWarning(t *testing.T) {
	logs := captureSlog(t)
	cfg := loadConfigText(t, "commands:\n  allow_extra_updaters: [dietpi, snap]\n")
	assert.Equal(t, []string{"dietpi"}, cfg.Commands.EnabledExtraUpdaters(), "[AC-014] snap is ignored, dietpi stays")
	var warnings []string
	for _, line := range logs.lines() {
		if strings.Contains(line, "allow_extra_updaters") {
			warnings = append(warnings, line)
		}
	}
	require.Len(t, warnings, 1, "[AC-014] one warning per config load, not one per unknown identifier")
	assert.Contains(t, warnings[0], "level=WARN")
	assert.Contains(t, warnings[0], "snap")
	assert.NotContains(t, warnings[0], "dietpi", "the implemented identifier is not named as ignored")
}

func TestLoadConfig_EU_AC014_NoWarningWhenAllKnownOrAbsent(t *testing.T) {
	for name, text := range map[string]string{
		"absent":         "commands:\n  allow_patching: true\n",
		"known":          "commands:\n  allow_extra_updaters: [dietpi]\n",
		"empty":          "commands:\n  allow_extra_updaters: []\n",
		"known and dups": "commands:\n  allow_extra_updaters: [dietpi, dietpi]\n",
	} {
		t.Run("[AC-014] "+name, func(t *testing.T) {
			logs := captureSlog(t)
			loadConfigText(t, text)
			for _, line := range logs.lines() {
				assert.NotContains(t, line, "allow_extra_updaters", "unexpected warning: %s", line)
			}
		})
	}
}

func TestLoadConfig_EU_AC014_ManyUnknownIDsStillOneBoundedWarning(t *testing.T) {
	logs := captureSlog(t)
	var ids []string
	for i := 0; i < 40; i++ {
		ids = append(ids, "updater"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	cfg := loadConfigText(t, "commands:\n  allow_extra_updaters: ["+strings.Join(ids, ", ")+", dietpi]\n")
	assert.Equal(t, []string{"dietpi"}, cfg.Commands.EnabledExtraUpdaters())
	n := 0
	for _, line := range logs.lines() {
		if strings.Contains(line, "allow_extra_updaters") {
			n++
			assert.Less(t, len(line), 600, "[NFR-002] the warning line is bounded")
		}
	}
	assert.Equal(t, 1, n)
	assert.LessOrEqual(t, len(cfg.Commands.ignoredExtraUpdaters()), 8)
}

func TestLoadConfig_EU_AC014_WrongShapesNeverStopTheAgent(t *testing.T) {
	for name, text := range map[string]string{
		"a scalar":           "commands:\n  allow_extra_updaters: dietpi\n",
		"a mapping":          "commands:\n  allow_extra_updaters: {dietpi: true}\n",
		"a number":           "commands:\n  allow_extra_updaters: 7\n",
		"a list of numbers":  "commands:\n  allow_extra_updaters: [1, 2]\n",
		"a nested list":      "commands:\n  allow_extra_updaters: [[dietpi]]\n",
		"null":               "commands:\n  allow_extra_updaters:\n",
		"quoted items":       "commands:\n  allow_extra_updaters: [\"dietpi\"]\n",
		"a block list":       "commands:\n  allow_extra_updaters:\n    - dietpi\n",
		"mixed good and bad": "commands:\n  allow_extra_updaters: [dietpi, 5]\n",
		"an overlong id":     "commands:\n  allow_extra_updaters: [" + strings.Repeat("x", 500) + "]\n",
	} {
		t.Run("[AC-014] "+name, func(t *testing.T) {
			captureSlog(t)
			cfg := loadConfigText(t, text)
			enabled := cfg.Commands.EnabledExtraUpdaters()
			switch name {
			case "quoted items", "a block list", "mixed good and bad":
				assert.Equal(t, []string{"dietpi"}, enabled)
			default:
				assert.Empty(t, enabled, "a bad shape enables nothing")
			}
		})
	}
}

func TestConfig_EU_AC014_DefaultsAndSaveRoundTrip(t *testing.T) {
	assert.Empty(t, DefaultConfig().Commands.AllowExtraUpdaters, "[FR-017] the default is an empty list")
	out, err := yaml.Marshal(DefaultConfig())
	require.NoError(t, err)
	assert.NotContains(t, string(out), "allow_extra_updaters", "an empty list is omitted, so a saved config is unchanged")
	cfg := DefaultConfig()
	cfg.Commands.AllowExtraUpdaters = ExtraUpdaterIDs{"dietpi"}
	out, err = yaml.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(out), "allow_extra_updaters:\n        - dietpi")
	var back Config
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, []string{"dietpi"}, back.Commands.EnabledExtraUpdaters())
	assert.Contains(t, defaultConfigTemplate, "# allow_extra_updaters: []", "the commented key is discoverable in the generated config")
}
