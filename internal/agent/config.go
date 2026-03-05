package agent

import (
	"os"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Agent        AgentConfig        `yaml:"agent"`
	Collector    CollectorConfig    `yaml:"collectors"`
	Docker       DockerConfig       `yaml:"docker"`
	Commands     CommandsConfig     `yaml:"commands"`
	HostTerminal HostTerminalConfig `yaml:"host_terminal"`
}

// CommandsConfig controls remote command execution.
type CommandsConfig struct {
	AllowReboot bool `yaml:"allow_reboot"` // opt-in for remote reboot
}

// HostTerminalConfig controls host-level terminal access.
type HostTerminalConfig struct {
	Enabled bool   `yaml:"enabled"` // opt-in for host shell access
	Shell   string `yaml:"shell"`   // override default shell (e.g., /bin/bash)
}

// DockerConfig controls Docker collection behavior.
type DockerConfig struct {
	Enabled         string `yaml:"enabled"`          // "auto" (default), "true", "false"
	SocketPath      string `yaml:"socket_path"`      // override auto-detection
	CollectStats    bool   `yaml:"collect_stats"`    // CPU/mem per container
	TerminalEnabled bool   `yaml:"terminal_enabled"` // opt-in for remote exec
}

type ServerConfig struct {
	URL        string `yaml:"url"`
	APIKey     string `yaml:"api_key"`
	TLSVerify  bool   `yaml:"tls_verify"`
	CACertFile string `yaml:"ca_cert_file"` // custom CA certificate for TLS verification
}

type AgentConfig struct {
	DeviceName        string   `yaml:"device_name"`
	DeviceID          string   `yaml:"device_id"`
	Tags              []string `yaml:"tags"`
	PollInterval      int      `yaml:"poll_interval"`
	HeartbeatInterval int      `yaml:"heartbeat_interval"`
}

type CollectorConfig struct {
	Enabled []string `yaml:"enabled"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			URL:       "http://localhost:7331",
			TLSVerify: true,
		},
		Agent: AgentConfig{
			PollInterval:      60,
			HeartbeatInterval: 15,
		},
		Collector: CollectorConfig{
			Enabled: []string{
				"system", "cpu", "memory", "disk", "network",
				"os_info", "updates", "services", "processes",
				"docker", "security",
			},
		},
		Docker: DockerConfig{
			Enabled:      "auto",
			CollectStats: true,
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// ConfigPath returns the default config file path for the current OS.
func ConfigPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\agent.yaml"
	}
	return "/etc/riot/agent.yaml"
}

// IDPath returns the path for the persistent device ID file.
func IDPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\id"
	}
	return "/etc/riot/id"
}

// BufferPath returns the path for the offline buffer database.
func BufferPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\buffer.db"
	}
	return "/var/lib/riot/buffer.db"
}
