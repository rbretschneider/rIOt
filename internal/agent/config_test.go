package agent

import (
	"os"
	"path/filepath"
	"runtime"
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
