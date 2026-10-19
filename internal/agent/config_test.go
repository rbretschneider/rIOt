package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "http://localhost:7331", cfg.Server.URL)
	assert.True(t, cfg.Server.TLSVerify)
	assert.Equal(t, 60, cfg.Agent.PollInterval)
	assert.Equal(t, 15, cfg.Agent.HeartbeatInterval)
	assert.Equal(t, "auto", cfg.Docker.Enabled)
	assert.True(t, cfg.Docker.CollectStats)
	assert.Len(t, cfg.Collector.Enabled, 13)
	assert.Contains(t, cfg.Collector.Enabled, "cpu")
	assert.Contains(t, cfg.Collector.Enabled, "docker")
	assert.Contains(t, cfg.Collector.Enabled, "logs")
	assert.Contains(t, cfg.Collector.Enabled, "ups")
}

func TestLoadConfig_FromYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")

	yaml := `
server:
  url: "https://myserver:7331"
  api_key: "riot_abc123"
  tls_verify: false
agent:
  device_name: "test-device"
  poll_interval: 120
  heartbeat_interval: 30
collectors:
  enabled:
    - cpu
    - memory
docker:
  enabled: "false"
`
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0644))

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	assert.Equal(t, "https://myserver:7331", cfg.Server.URL)
	assert.Equal(t, "riot_abc123", cfg.Server.APIKey)
	assert.False(t, cfg.Server.TLSVerify)
	assert.Equal(t, "test-device", cfg.Agent.DeviceName)
	assert.Equal(t, 120, cfg.Agent.PollInterval)
	assert.Equal(t, 30, cfg.Agent.HeartbeatInterval)
	assert.Equal(t, []string{"cpu", "memory"}, cfg.Collector.Enabled)
	assert.Equal(t, "false", cfg.Docker.Enabled)
}

func TestLoadConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(filepath.Join(dir, "subdir", "deep", "agent.yaml"))
	// On first run with a missing file, the agent generates a default config.
	// If the directory is writable the file is created and loaded (no error).
	// If it's not writable we get an error but still get defaults.
	if err != nil {
		assert.True(t, os.IsNotExist(err) || os.IsPermission(err))
	}
	// Either way, defaults should be returned
	assert.Equal(t, "http://localhost:7331", cfg.Server.URL)
}

func TestConfigSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	cfg := DefaultConfig()
	cfg.Server.URL = "https://saved:7331"

	require.NoError(t, cfg.Save(path))

	loaded, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "https://saved:7331", loaded.Server.URL)
}

func TestConfigPath(t *testing.T) {
	path := ConfigPath()
	if runtime.GOOS == "windows" {
		assert.Contains(t, path, "riot\\agent.yaml")
	} else {
		assert.Equal(t, "/etc/riot/agent.yaml", path)
	}
}

func TestIDPath(t *testing.T) {
	path := IDPath()
	if runtime.GOOS == "windows" {
		assert.Contains(t, path, "riot\\id")
	} else {
		assert.Equal(t, "/etc/riot/id", path)
	}
}

func TestBufferPath(t *testing.T) {
	path := BufferPath()
	if runtime.GOOS == "windows" {
		assert.Contains(t, path, "riot\\buffer.db")
	} else {
		assert.Equal(t, "/var/lib/riot/buffer.db", path)
	}
}

// AUTOUPD-HARDEN-001 AC-008: docker.compose_roots entries are stored cleaned.
func TestLoadConfig_AC008_ComposeRootsParsedAndCleaned(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "stacks")
	b := filepath.Join(dir, "other")
	path := filepath.Join(dir, "agent.yaml")
	// single-quoted YAML strings keep Windows backslashes literal
	require.NoError(t, os.WriteFile(path, []byte("docker:\n  compose_roots:\n    - '"+a+string(filepath.Separator)+"'\n    - '"+b+string(filepath.Separator)+".'\n"), 0600))

	cfg, err := LoadConfig(path)

	require.NoError(t, err)
	assert.Equal(t, []string{a, b}, cfg.Docker.ComposeRoots, "[AC-008] roots are stored cleaned")
	assert.NoError(t, cfg.Docker.rootsErr, "[AC-008] valid roots set no error")
}

// AC-008: an invalid root does not fail the load; it is remembered so every
// compose check fails closed while the agent keeps running.
func TestLoadConfig_AC008_RelativeRootLoadsButSetsRootsErr(t *testing.T) {
	captureLogs(t)
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte("docker:\n  compose_roots:\n    - stacks/relative\n    - ''\n"), 0600))

	cfg, err := LoadConfig(path)

	require.NoError(t, err, "[AC-008] the load itself succeeds")
	assert.Error(t, cfg.Docker.rootsErr, "[AC-008] a relative or empty root is remembered as an error")
	ct := (&Agent{config: cfg}).composeTrust()
	assert.Error(t, ct.rootsErr, "[AC-008] and reaches the validator")
}

// The sample config documents the new key as a commented example.
func TestDefaultConfigTemplate_AC008_DocumentsComposeRoots(t *testing.T) {
	assert.Contains(t, defaultConfigTemplate, "# compose_roots: []", "[AC-008] the generated agent.yaml shows the setting")
}

// loadYAML writes body to a temp agent.yaml and loads it, returning the config
// and everything slog emitted while loading.
func loadYAML(t *testing.T, body string) (*Config, string) {
	t.Helper()
	logs := captureLogs(t)
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	cfg, err := LoadConfig(path)
	require.NoError(t, err, "a bad limit value must never stop the agent loading its config")
	return cfg, logs.String()
}

// yamlForKey renders one limit key into a config document.
func yamlForKey(key, value string) string {
	switch key {
	case keyContainerLogMaxLines:
		return "collectors:\n  container_logs:\n    max_lines: " + value + "\n"
	case keyContainerLogMaxBytes:
		return "collectors:\n  container_logs:\n    max_bytes: " + value + "\n"
	case keyContainerLogMaxLineBytes:
		return "collectors:\n  container_logs:\n    max_line_bytes: " + value + "\n"
	case keyLogsMaxEntries:
		return "collectors:\n  logs:\n    max_entries_per_pass: " + value + "\n"
	case keyLogsMaxLineBytes:
		return "collectors:\n  logs:\n    max_line_bytes: " + value + "\n"
	default:
		return "agent:\n  buffer_max_mb: " + value + "\n"
	}
}

func resolvedFor(l Limits, key string) int64 {
	switch key {
	case keyContainerLogMaxLines:
		return int64(l.ContainerLogMaxLines)
	case keyContainerLogMaxBytes:
		return int64(l.ContainerLogMaxBytes)
	case keyContainerLogMaxLineBytes:
		return int64(l.ContainerLogMaxLineBytes)
	case keyLogsMaxEntries:
		return int64(l.JournalMaxEntries)
	case keyLogsMaxLineBytes:
		return int64(l.JournalMaxLineBytes)
	default:
		return int64(l.BufferMaxMB)
	}
}

func TestGROWTH_AC068_EveryAgentBoundFiniteInRange(t *testing.T) {
	inputs := []struct {
		name, yaml string
		// want classifies the expected outcome: "min", "max" or "default".
		want string
	}{
		{"zero", "0", "min"},
		{"negative", "-7", "min"},
		{"huge", "999999999999", "max"},
		{"overflowing int64", "99999999999999999999999", "max"},
		{"negative overflow", "-99999999999999999999999", "min"},
		{"garbage word", "lots", "default"},
		{"garbage float", "1.5", "default"},
		{"garbage bool", "true", "default"},
	}
	for key, spec := range agentLimitSpecs {
		for _, in := range inputs {
			t.Run(key+"/"+in.name, func(t *testing.T) {
				cfg, logs := loadYAML(t, yamlForKey(key, in.yaml))
				got := resolvedFor(cfg.Limits(), key)

				want := map[string]int64{"min": spec.min, "max": spec.max, "default": spec.def}[in.want]
				assert.Equal(t, want, got, "[AC-068] %s=%s must resolve to the %s (%d)", key, in.yaml, in.want, want)
				assert.GreaterOrEqual(t, got, spec.min, "[AC-068] resolved value below range")
				assert.LessOrEqual(t, got, spec.max, "[AC-068] resolved value above range")
				assert.Equal(t, 1, strings.Count(logs, "config limit adjusted"),
					"[AC-068] exactly one Warn per adjusted key, got logs: %s", logs)
				assert.Contains(t, logs, key, "[AC-068] the Warn must name the key")
			})
		}
	}
}

func TestGROWTH_AC068_InRangeAndUnsetValuesDoNotWarn(t *testing.T) {
	for key, spec := range agentLimitSpecs {
		t.Run(key, func(t *testing.T) {
			cfg, logs := loadYAML(t, yamlForKey(key, strconv.FormatInt(spec.min, 10)))
			assert.Equal(t, spec.min, resolvedFor(cfg.Limits(), key), "[AC-068] the lower bound itself is valid")
			assert.NotContains(t, logs, "config limit adjusted")

			cfg, logs = loadYAML(t, "agent:\n  poll_interval: 60\n")
			assert.Equal(t, spec.def, resolvedFor(cfg.Limits(), key), "[AC-068] unset keys use the documented default")
			assert.NotContains(t, logs, "config limit adjusted")
		})
	}
}

func TestGROWTH_AC068_DefaultsMatchTheDocumentedValues(t *testing.T) {
	l := DefaultConfig().Limits()
	assert.Equal(t, 1000, l.ContainerLogMaxLines)
	assert.Equal(t, 262144, l.ContainerLogMaxBytes)
	assert.Equal(t, 4096, l.ContainerLogMaxLineBytes)
	assert.Equal(t, 5000, l.JournalMaxEntries)
	assert.Equal(t, 4096, l.JournalMaxLineBytes)
	assert.Equal(t, int64(50<<20), l.BufferMaxBytes())
}

func TestGROWTH_AC068_SaveKeepsConfiguredLimitsAndOmitsUnset(t *testing.T) {
	cfg, _ := loadYAML(t, "collectors:\n  container_logs:\n    max_lines: 200\n")
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, cfg.Save(path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "max_lines: 200", "a configured limit must survive a Save")
	assert.NotContains(t, string(data), "max_bytes", "an unset limit must not be written as a bogus value")
	assert.NotContains(t, string(data), "buffer_max_mb")
}

func TestGROWTH_SEC016_ConfigSaveIsAtomicAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  url: old\n"), 0o600))

	cfg := DefaultConfig()
	cfg.Server.URL = "https://new:7331"
	require.NoError(t, cfg.Save(path))

	loaded, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "https://new:7331", loaded.Server.URL)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "[SEC-016] a successful atomic save must leave only the config file")
}
