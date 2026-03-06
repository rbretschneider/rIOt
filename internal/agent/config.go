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
	DNSCache     DNSCacheConfig     `yaml:"dns_cache"`
	DeadMan      DeadManConfig      `yaml:"deadman"`
}

// DNSCacheConfig controls resilient DNS caching.
type DNSCacheConfig struct {
	RefreshIntervalSeconds int    `yaml:"refresh_interval_seconds"` // default 1800 (30m)
	StalenessWarningHours  int    `yaml:"staleness_warning_hours"`  // default 24
	CacheFile              string `yaml:"cache_file"`               // default OS-specific
}

// CommandsConfig controls remote command execution.
type CommandsConfig struct {
	AllowReboot   bool `yaml:"allow_reboot"`   // opt-in for remote reboot
	AllowPatching bool `yaml:"allow_patching"` // opt-in for remote OS patching
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
	URL            string `yaml:"url"`
	APIKey         string `yaml:"api_key"`
	TLSVerify      bool   `yaml:"tls_verify"`
	CACertFile     string `yaml:"ca_cert_file"`      // custom CA certificate for TLS verification
	ClientCert     string `yaml:"client_cert"`        // mTLS client certificate path
	ClientKey      string `yaml:"client_key"`         // mTLS client key path
	BootstrapKey   string `yaml:"bootstrap_key"`      // single-use enrollment key
	ServerCertPin  string `yaml:"server_cert_pin"`    // SHA256 fingerprint for TOFU verification
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

// CertPath returns the default path for the mTLS client certificate.
func CertPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\client.crt"
	}
	return "/etc/riot/client.crt"
}

// KeyPath returns the default path for the mTLS client key.
func KeyPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\client.key"
	}
	return "/etc/riot/client.key"
}

// ServerCertPath returns the default path for the TOFU-pinned server certificate.
func ServerCertPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\server.crt"
	}
	return "/etc/riot/server.crt"
}

// CACertPath returns the default path for the CA certificate.
func CACertPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\ca.crt"
	}
	return "/etc/riot/ca.crt"
}

// DNSCachePath returns the default path for the DNS cache file.
func DNSCachePath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\dns-cache.json"
	}
	return "/etc/riot/dns-cache.json"
}

// BufferPath returns the path for the offline buffer database.
func BufferPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\buffer.db"
	}
	return "/var/lib/riot/buffer.db"
}
