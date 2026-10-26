package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
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
	AllowReboot     bool `yaml:"allow_reboot"`      // opt-in for remote reboot
	AllowShutdown   bool `yaml:"allow_shutdown"`    // opt-in for remote shutdown
	AllowPatching   bool `yaml:"allow_patching"`    // opt-in for remote OS patching
	AllowProbes     bool `yaml:"allow_probes"`      // opt-in for device probe execution
	HoldRebootClass bool `yaml:"hold_reboot_class"` // opt-in: hold GPU-driver/kernel packages outside maintenance windows (PATCH-GATE)

	// AllowExtraUpdaters lists the platform updaters outside the package manager
	// (today only "dietpi") that rIOt may apply during OS patching
	// (EXTRA-UPD-001 FR-017). Empty by default. This is the agent-side half of the
	// opt-in: the root-side half is written only by install.sh, and an updater
	// runs only when both agree.
	AllowExtraUpdaters ExtraUpdaterIDs `yaml:"allow_extra_updaters,omitempty"`
}

// ExtraUpdaterIDs is a list of updater identifiers read leniently from YAML. A
// value that is not a list of plain strings does not stop the agent from
// starting: it decodes to the marker "<invalid>", which is never an implemented
// updater and is reported once by the load warning.
type ExtraUpdaterIDs []string

// invalidExtraUpdaterID stands for anything in the list that is not a string.
const invalidExtraUpdaterID = "<invalid>"

// UnmarshalYAML never fails.
func (l *ExtraUpdaterIDs) UnmarshalYAML(n *yaml.Node) error {
	*l = nil
	if n.Kind != yaml.SequenceNode {
		*l = ExtraUpdaterIDs{invalidExtraUpdaterID}
		return nil
	}
	for _, item := range n.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			*l = append(*l, invalidExtraUpdaterID)
			continue
		}
		*l = append(*l, item.Value)
	}
	return nil
}

// EnabledExtraUpdaters returns the listed identifiers this agent implements, in
// registry order, without duplicates. Anything else is ignored (FR-017).
func (c CommandsConfig) EnabledExtraUpdaters() []string {
	listed := make(map[string]bool, len(c.AllowExtraUpdaters))
	for _, id := range c.AllowExtraUpdaters {
		listed[id] = true
	}
	var out []string
	for _, u := range extraupd.Registry() {
		if listed[u.ID()] {
			out = append(out, u.ID())
		}
	}
	return out
}

// ignoredExtraUpdaters returns the listed entries that are not implemented,
// deduplicated, each cut to the identifier bound, at most eight of them.
func (c CommandsConfig) ignoredExtraUpdaters() []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range c.AllowExtraUpdaters {
		if extraupd.IsImplemented(id) || seen[id] {
			continue
		}
		seen[id] = true
		if len(out) < 8 {
			out = append(out, truncateConfigValue(id, 40))
		}
	}
	return out
}

func truncateConfigValue(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// warnExtraUpdaters logs one warning per config load when the list names
// something this agent does not implement.
func (c CommandsConfig) warnExtraUpdaters() {
	if ignored := c.ignoredExtraUpdaters(); len(ignored) > 0 {
		slog.Warn("commands.allow_extra_updaters lists updaters this agent does not implement; they are ignored",
			"ignored", strings.Join(ignored, ","))
	}
}

// HostTerminalConfig controls host-level terminal access.
type HostTerminalConfig struct {
	Enabled bool   `yaml:"enabled"` // opt-in for host shell access
	Shell   string `yaml:"shell"`   // override default shell (e.g., /bin/bash)
}

// DockerConfig controls Docker collection behavior.
type DockerConfig struct {
	Enabled         string `yaml:"enabled"`                 // "auto" (default), "true", "false"
	SocketPath      string `yaml:"socket_path"`             // override auto-detection
	CollectStats    bool   `yaml:"collect_stats"`           // CPU/mem per container
	TerminalEnabled bool   `yaml:"terminal_enabled"`        // opt-in for remote exec
	CheckUpdates    *bool  `yaml:"check_updates,omitempty"` // nil = true (default enabled)

	// ComposeRoots limits where compose updates may run (AUTOUPD-HARDEN-001):
	// absolute directories. Empty means not enforced (required on Windows).
	ComposeTrustedRoots   []string `yaml:"compose_trusted_roots,omitempty"`
	PruneSupersededImages bool     `yaml:"prune_superseded_images"`
	ComposeRoots          []string `yaml:"compose_roots,omitempty"`
	// rootsErr is set by normalizeComposeRoots when an entry is invalid. Every
	// compose check then fails closed while the rest of the agent keeps running.
	rootsErr error
}

type ServerConfig struct {
	URL           string `yaml:"url"`
	APIKey        string `yaml:"api_key"`
	TLSVerify     bool   `yaml:"tls_verify"`
	CACertFile    string `yaml:"ca_cert_file"`    // custom CA certificate for TLS verification
	ClientCert    string `yaml:"client_cert"`     // mTLS client certificate path
	ClientKey     string `yaml:"client_key"`      // mTLS client key path
	BootstrapKey  string `yaml:"bootstrap_key"`   // single-use enrollment key
	ServerCertPin string `yaml:"server_cert_pin"` // SHA256 fingerprint for TOFU verification
}

type AgentConfig struct {
	DeviceName        string   `yaml:"device_name"`
	DeviceID          string   `yaml:"device_id"`
	Tags              []string `yaml:"tags"`
	PollInterval      int      `yaml:"poll_interval"`
	HeartbeatInterval int      `yaml:"heartbeat_interval"`
	AutoUpdate        *bool    `yaml:"auto_update,omitempty"` // nil = true (default enabled)
	// BufferMaxMB caps the offline telemetry buffer in MiB (default 50, range 5-64).
	BufferMaxMB LimitInt `yaml:"buffer_max_mb,omitempty"`
}

type CollectorConfig struct {
	Enabled       []string            `yaml:"enabled"`
	SMARTInterval int                 `yaml:"smart_interval"` // SMART scan interval in seconds (default 14400 = 4h)
	Webservers    WebServersConfig    `yaml:"webservers"`
	ContainerLogs ContainerLogsConfig `yaml:"container_logs,omitempty"`
	Logs          LogsConfig          `yaml:"logs,omitempty"`
}

// ContainerLogsConfig bounds the container_logs collector (GROWTH-001 AD-017).
type ContainerLogsConfig struct {
	MaxLines     LimitInt `yaml:"max_lines,omitempty"`      // lines kept per container per pass
	MaxBytes     LimitInt `yaml:"max_bytes,omitempty"`      // bytes kept per container per pass
	MaxLineBytes LimitInt `yaml:"max_line_bytes,omitempty"` // longer lines are truncated
}

// LogsConfig bounds the journal logs collector (GROWTH-001 AD-017).
type LogsConfig struct {
	MaxEntriesPerPass LimitInt `yaml:"max_entries_per_pass,omitempty"` // journal entries kept per pass
	MaxLineBytes      LimitInt `yaml:"max_line_bytes,omitempty"`       // longer messages are truncated
}

// WebServersConfig holds per-collector configuration for the webservers collector.
type WebServersConfig struct {
	Nginx NginxConfig `yaml:"nginx"`
}

// NginxConfig holds nginx-specific collector configuration.
type NginxConfig struct {
	AccessLog string `yaml:"access_log"`
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
				"docker", "security", "logs", "ups",
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
		if os.IsNotExist(err) {
			// Generate default config on first run so the user can edit it
			if writeErr := writeDefaultConfig(path); writeErr != nil {
				slog.Warn("could not write default config", "path", path, "error", writeErr)
			} else {
				slog.Info("generated default config", "path", path)
			}
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}
	cfg.Docker.normalizeComposeRoots()
	cfg.Validate()
	cfg.Commands.warnExtraUpdaters()

	return cfg, nil
}

// normalizeComposeRoots cleans docker.compose_roots. An entry that is empty or
// not absolute does not fail the load: it is logged and remembered, and every
// compose check then refuses, so a typo cannot silently widen where compose runs.
func (d *DockerConfig) normalizeComposeRoots() {
	d.rootsErr = nil
	for i, r := range d.ComposeRoots {
		if r == "" || !filepath.IsAbs(r) {
			d.rootsErr = fmt.Errorf("docker.compose_roots entry %d is not an absolute path", i+1)
			slog.Error("invalid docker.compose_roots entry; compose updates are refused until it is fixed", "entry", i+1)
			continue
		}
		d.ComposeRoots[i] = filepath.Clean(r)
	}
}

// writeDefaultConfig creates the config directory and writes a commented
// default agent.yaml so users can discover and edit settings.
func writeDefaultConfig(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfigTemplate), 0600)
}

const defaultConfigTemplate = `# rIOt Agent Configuration
# Generated on first run — edit and restart the agent to apply changes.

server:
  url: "http://localhost:7331"
  tls_verify: true
  # api_key: ""            # Set during enrollment
  # bootstrap_key: ""      # Single-use enrollment key
  # ca_cert_file: ""       # Custom CA certificate for TLS
  # server_cert_pin: ""    # SHA256 fingerprint for TOFU verification

agent:
  poll_interval: 60        # Telemetry push interval (seconds)
  heartbeat_interval: 15   # Heartbeat interval (seconds)
  # device_name: ""        # Override auto-detected hostname
  # auto_update: true      # Auto-update agent binary when server offers a new version
  # tags: []               # Device tags for grouping/filtering
  # buffer_max_mb: 50      # Offline telemetry buffer size cap in MiB (5-64)

collectors:
  enabled:
    - system
    - cpu
    - memory
    - disk
    - network
    - os_info
    - updates
    - services
    - processes
    - docker
    - security
    - logs
    - ups
  # smart_interval: 14400  # SMART scan interval in seconds (default 14400 = 4 hours)
  # container_logs:
  #   max_lines: 50       # newest lines kept per container per pass (1-1000)
  #   max_bytes: 16384     # bytes kept per container per pass (1024-262144)
  #   max_line_bytes: 16384 # longer lines are truncated (1024-16384)
  # logs:
  #   max_entries_per_pass: 5000  # newest journal entries kept per pass (500-20000)
  #   max_line_bytes: 4096        # longer messages are truncated (1024-16384)
  # webservers:
  #   nginx:
  #     access_log: /var/log/nginx/access.log  # Enable nginx access log monitoring

docker:
  enabled: "auto"          # "auto" (detect), "true", or "false"
  collect_stats: true      # Per-container CPU/memory stats
  # check_updates: true    # Check registries for newer images (30min cache)
  # terminal_enabled: false  # Allow remote exec into containers
  # socket_path: ""        # Override Docker socket auto-detection
  # compose_trusted_roots: [] # Host-local reviewed roots containers may write
  # prune_superseded_images: false # Opt-in removal of unused, untagged previous images
  # compose_roots: []      # Directories compose updates may run in, e.g. [/opt/stacks] (required on Windows)

commands:
  allow_reboot: false      # Allow remote reboot commands
  allow_shutdown: false    # Allow remote shutdown commands
  allow_patching: false    # Allow remote OS patching commands
  hold_reboot_class: false # Hold GPU driver + kernel packages at the OS level (release only during rIOt maintenance-window patch runs)
  # allow_extra_updaters: [] # Updaters outside apt that rIOt may apply after it, e.g. [dietpi]; also needs install.sh re-run as root

host_terminal:
  enabled: false           # Allow remote shell access to this host
  # shell: ""              # Override default shell (e.g., /bin/bash)
`

// Save writes the config atomically (temp file, fsync, rename, directory
// fsync) so a full disk or a crash can never leave a truncated agent.yaml.
//
// The temp file is created beside the config, so the agent needs write access
// to the config DIRECTORY, not only to the file. A root-owned directory, a
// symlinked agent.yaml (the rename replaces the link) or a single-file bind
// mount (the rename fails with "busy") no longer works. install.sh chowns
// /etc/riot to the agent user, so installer installs are unaffected.
func (c *Config) Save(path string) error {
	return c.SaveWith(osStateFS{}, path)
}

// SaveWith is Save over an injectable file system. The identity pre-flight and
// the post-registration writes both go through the same atomic helper, so the
// pre-flight proves the path the real write will take (GROWTH-001 SEC-016).
func (c *Config) SaveWith(fsys stateFS, path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return newAtomicJSONFile(path, fsys).SaveBytes(data)
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

// HoldStatePath returns the path for the reboot-class hold bookkeeping file
// (PATCH-GATE AD-003). Linux-only feature; the Windows path exists for symmetry.
func HoldStatePath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\holds.json"
	}
	return "/var/lib/riot/holds.json"
}

// DNFHoldsStagedPath returns the riot-owned staging path for the dnf
// excludes fragment. This is the fixed SOURCE path in the sudoers `install`
// rule written by scripts/install.sh — it must match byte-for-byte
// (PATCH-GATE AD-015, Implementation Note #15).
func DNFHoldsStagedPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\dnf-holds.staged"
	}
	return "/var/lib/riot/dnf-holds.staged"
}

// NginxAccessLogOffsetPath returns the path for the nginx access log byte offset file.
func NginxAccessLogOffsetPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\nginx-access-log.offset"
	}
	return "/etc/riot/nginx-access-log.offset"
}

// FreshnessCachePath returns the path for the Docker image freshness cache file.
func FreshnessCachePath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\docker-freshness-cache.json"
	}
	return "/etc/riot/docker-freshness-cache.json"
}

// OutboxPath returns the path for the durable command-result outbox
// (CMD-LIFECYCLE-001 AD-010). It lives in the agent-writable state directory.
func OutboxPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\command-outbox.json"
	}
	return "/var/lib/riot/command-outbox.json"
}

// SeenCommandsPath returns the path for the persisted seen-command-ID store
// (FR-057).
func SeenCommandsPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\seen-commands.json"
	}
	return "/var/lib/riot/seen-commands.json"
}

// InflightPath returns the path for the in-flight command markers used for
// restart recovery (FR-019, SEC C-4).
func InflightPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PROGRAMDATA") + "\\riot\\inflight-commands.json"
	}
	return "/var/lib/riot/inflight-commands.json"
}

// ---------------------------------------------------------------------------
// Bounded collector and buffer limits (GROWTH-001 AD-017)
// ---------------------------------------------------------------------------

// LimitInt is an optional integer limit read leniently from YAML. A value that
// is not an integer resolves to the key's default; an integer outside the key's
// range is clamped. Neither stops the agent from starting, because a typo in
// agent.yaml must never take monitoring down (and must never unbound a cap).
type LimitInt struct {
	set     bool
	garbage bool
	value   int64
	raw     string
}

// UnmarshalYAML accepts any scalar and records whether it was an integer.
func (l *LimitInt) UnmarshalYAML(n *yaml.Node) error {
	*l = LimitInt{set: true, raw: n.Value}
	if n.Kind != yaml.ScalarNode {
		l.garbage = true
		l.raw = "<non-scalar>"
		return nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(n.Value), 10, 64)
	switch {
	case err == nil:
		l.value = v
	case errors.Is(err, strconv.ErrRange):
		// A number too large for int64 is still a number: saturate so the
		// range clamp (not the default) applies.
		if strings.HasPrefix(strings.TrimSpace(n.Value), "-") {
			l.value = -1 << 63
		} else {
			l.value = 1<<63 - 1
		}
	default:
		l.garbage = true
	}
	return nil
}

// MarshalYAML writes back what the operator wrote, so Config.Save never
// rewrites a hand-edited limit.
func (l LimitInt) MarshalYAML() (interface{}, error) {
	if l.garbage {
		return l.raw, nil
	}
	return l.value, nil
}

// IsZero lets yaml omit an unset limit (omitempty).
func (l LimitInt) IsZero() bool { return !l.set }

// limitOf builds a set LimitInt; tests and code use it instead of YAML.
func limitOf(v int) LimitInt { return LimitInt{set: true, value: int64(v), raw: strconv.Itoa(v)} }

// limitSpec is the default and the inclusive range of one configurable bound.
type limitSpec struct {
	key           string
	def, min, max int64
}

// resolve returns the value to use and, when the configured value had to be
// changed, why.
func (s limitSpec) resolve(l LimitInt) (value int64, why string) {
	switch {
	case !l.set:
		return s.def, ""
	case l.garbage:
		return s.def, "not an integer; using the default"
	case l.value < s.min:
		return s.min, "below the minimum; using the minimum"
	case l.value > s.max:
		return s.max, "above the maximum; using the maximum"
	}
	return l.value, ""
}

const (
	keyContainerLogMaxLines     = "collectors.container_logs.max_lines"
	keyContainerLogMaxBytes     = "collectors.container_logs.max_bytes"
	keyContainerLogMaxLineBytes = "collectors.container_logs.max_line_bytes"
	keyLogsMaxEntries           = "collectors.logs.max_entries_per_pass"
	keyLogsMaxLineBytes         = "collectors.logs.max_line_bytes"
	keyBufferMaxMB              = "agent.buffer_max_mb"
)

// agentLimitSpecs are the six configurable agent bounds (GROWTH-001 AD-017).
var agentLimitSpecs = map[string]limitSpec{
	keyContainerLogMaxLines:     {keyContainerLogMaxLines, 50, 1, 1000},
	keyContainerLogMaxBytes:     {keyContainerLogMaxBytes, 16 << 10, 1 << 10, 256 << 10},
	keyContainerLogMaxLineBytes: {keyContainerLogMaxLineBytes, 16 << 10, 1 << 10, 16 << 10},
	keyLogsMaxEntries:           {keyLogsMaxEntries, 5000, 500, 20000},
	keyLogsMaxLineBytes:         {keyLogsMaxLineBytes, 4 << 10, 1 << 10, 16 << 10},
	keyBufferMaxMB:              {keyBufferMaxMB, 50, 5, 64},
}

type configuredLimit struct {
	spec limitSpec
	val  LimitInt
}

// configuredLimits pairs each key with the value read from the file, in a
// fixed order so Validate's log output is stable.
func (c *Config) configuredLimits() []configuredLimit {
	return []configuredLimit{
		{agentLimitSpecs[keyContainerLogMaxLines], c.Collector.ContainerLogs.MaxLines},
		{agentLimitSpecs[keyContainerLogMaxBytes], c.Collector.ContainerLogs.MaxBytes},
		{agentLimitSpecs[keyContainerLogMaxLineBytes], c.Collector.ContainerLogs.MaxLineBytes},
		{agentLimitSpecs[keyLogsMaxEntries], c.Collector.Logs.MaxEntriesPerPass},
		{agentLimitSpecs[keyLogsMaxLineBytes], c.Collector.Logs.MaxLineBytes},
		{agentLimitSpecs[keyBufferMaxMB], c.Agent.BufferMaxMB},
	}
}

// Validate logs one Warn for each configured limit that had to be adjusted. It
// never fails and never changes the config: Limits() returns the values the
// agent actually uses.
func (c *Config) Validate() {
	for _, e := range c.configuredLimits() {
		if v, why := e.spec.resolve(e.val); why != "" {
			slog.Warn("config limit adjusted",
				"key", e.spec.key,
				"configured", e.val.raw,
				"using", v,
				"min", e.spec.min,
				"max", e.spec.max,
				"reason", why)
		}
	}
}

// Limits is the resolved, always in-range set of agent bounds.
type Limits struct {
	ContainerLogMaxLines     int
	ContainerLogMaxBytes     int
	ContainerLogMaxLineBytes int
	JournalMaxEntries        int
	JournalMaxLineBytes      int
	BufferMaxMB              int
}

// BufferMaxBytes is the offline buffer cap in bytes.
func (l Limits) BufferMaxBytes() int64 { return int64(l.BufferMaxMB) << 20 }

// Limits resolves every configurable bound to a finite in-range value.
func (c *Config) Limits() Limits {
	r := func(key string, v LimitInt) int {
		val, _ := agentLimitSpecs[key].resolve(v)
		return int(val)
	}
	return Limits{
		ContainerLogMaxLines:     r(keyContainerLogMaxLines, c.Collector.ContainerLogs.MaxLines),
		ContainerLogMaxBytes:     r(keyContainerLogMaxBytes, c.Collector.ContainerLogs.MaxBytes),
		ContainerLogMaxLineBytes: r(keyContainerLogMaxLineBytes, c.Collector.ContainerLogs.MaxLineBytes),
		JournalMaxEntries:        r(keyLogsMaxEntries, c.Collector.Logs.MaxEntriesPerPass),
		JournalMaxLineBytes:      r(keyLogsMaxLineBytes, c.Collector.Logs.MaxLineBytes),
		BufferMaxMB:              r(keyBufferMaxMB, c.Agent.BufferMaxMB),
	}
}
