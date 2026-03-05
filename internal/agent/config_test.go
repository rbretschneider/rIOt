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
	assert.Len(t, cfg.Collector.Enabled, 11)
	assert.Contains(t, cfg.Collector.Enabled, "cpu")
	assert.Contains(t, cfg.Collector.Enabled, "docker")
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
	cfg, err := LoadConfig("/nonexistent/path.yaml")
	require.Error(t, err)
	// Should still return defaults
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
