package agent

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// collectorDeps maps collector name → required external tools.
var collectorDeps = map[string][]string{
	"hardware":   {"smartctl"},
	"ups":        {"upsc"},
	"webservers": {"nginx", "caddy", "ferron"}, // any one is sufficient
	"services":   {"systemctl"},
	"logs":       {"journalctl"},
	"security":   {"journalctl"},
	"cron":       {"systemctl"},
}

// Doctor runs diagnostic checks and prints a summary of the agent's health.
func Doctor(configPath string) {
	fmt.Println("rIOt agent doctor")
	fmt.Println(strings.Repeat("─", 50))

	// ── Config ──
	fmt.Println()
	section("Configuration")
	fmt.Printf("  Config file:  %s\n", configPath)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			warn("Config file not found — using defaults")
		} else if os.IsPermission(err) {
			fail("Config file permission denied — try: sudo riot-agent doctor")
			return
		} else {
			fail("Config load error: %v", err)
			return
		}
	} else {
		pass("Config loaded")
	}

	// ── Identity ──
	fmt.Println()
	section("Identity")
	idPath := IDPath()
	if data, err := os.ReadFile(idPath); err == nil {
		fmt.Printf("  Device ID:    %s\n", strings.TrimSpace(string(data)))
	} else {
		warn("No device ID at %s (will be assigned on first connect)", idPath)
	}
	if cfg.Agent.DeviceName != "" {
		fmt.Printf("  Device name:  %s (override)\n", cfg.Agent.DeviceName)
	} else {
		if h, err := os.Hostname(); err == nil {
			fmt.Printf("  Device name:  %s (auto-detected)\n", h)
		}
	}
	if len(cfg.Agent.Tags) > 0 {
		fmt.Printf("  Tags:         %s\n", strings.Join(cfg.Agent.Tags, ", "))
	}
	fmt.Printf("  Platform:     %s/%s\n", runtime.GOOS, runtime.GOARCH)

	// ── Server ──
	fmt.Println()
	section("Server Connection")
	fmt.Printf("  URL:          %s\n", cfg.Server.URL)
	fmt.Printf("  TLS verify:   %v\n", cfg.Server.TLSVerify)
	if cfg.Server.APIKey != "" {
		pass("API key configured")
	} else if cfg.Server.BootstrapKey != "" {
		warn("Using bootstrap key (not yet enrolled)")
	} else {
		fail("No API key or bootstrap key configured")
	}
	checkServer(cfg)

	// ── Intervals ──
	fmt.Println()
	section("Intervals")
	poll := cfg.Agent.PollInterval
	if poll == 0 {
		poll = 60
	}
	hb := cfg.Agent.HeartbeatInterval
	if hb == 0 {
		hb = 15
	}
	smart := cfg.Collector.SMARTInterval
	if smart == 0 {
		smart = 14400
	}
	fmt.Printf("  Telemetry:    %ds\n", poll)
	fmt.Printf("  Heartbeat:    %ds\n", hb)
	fmt.Printf("  SMART:        %s\n", friendlyDuration(smart))

	// ── Collectors ──
	fmt.Println()
	section("Collectors")

	allCollectors := []string{
		"system", "cpu", "memory", "disk", "network", "os_info",
		"updates", "services", "processes", "docker", "container_logs",
		"security", "logs", "ups", "webservers", "usb", "hardware", "cron",
	}
	enabled := make(map[string]bool)
	for _, name := range cfg.Collector.Enabled {
		enabled[name] = true
	}

	for _, name := range allCollectors {
		if enabled[name] {
			fmt.Printf("  %-18s %s\n", name, green("enabled"))
		} else {
			fmt.Printf("  %-18s %s\n", name, dim("disabled"))
		}
	}

	// ── Dependencies ──
	fmt.Println()
	section("Dependencies")
	anyMissing := false
	for _, name := range allCollectors {
		if !enabled[name] {
			continue
		}
		deps, ok := collectorDeps[name]
		if !ok {
			continue
		}
		if name == "webservers" {
			// Any one web server binary is fine
			found := false
			for _, dep := range deps {
				if _, err := exec.LookPath(dep); err == nil {
					found = true
					break
				}
			}
			if !found {
				warn("webservers: none of [%s] found in PATH (collector will return empty data)", strings.Join(deps, ", "))
				anyMissing = true
			}
			continue
		}
		for _, dep := range deps {
			if _, err := exec.LookPath(dep); err != nil {
				warn("%s: %q not found in PATH", name, dep)
				anyMissing = true
			}
		}
	}
	if !anyMissing {
		pass("All dependencies found for enabled collectors")
	}

	// Sudoers check for smartctl
	if enabled["hardware"] {
		// Find smartctl the same way the collector does
		smartctlPath, _ := exec.LookPath("smartctl")
		if smartctlPath == "" {
			for _, p := range []string{"/usr/sbin/smartctl", "/usr/bin/smartctl", "/sbin/smartctl"} {
				if _, err := os.Stat(p); err == nil {
					smartctlPath = p
					break
				}
			}
		}
		if smartctlPath == "" {
			warn("smartctl not found — SMART data will not be collected")
		} else if _, err := exec.Command("sudo", "-n", smartctlPath, "--version").CombinedOutput(); err != nil {
			warn("smartctl sudo check failed — SMART data may not be available")
			warn("  Add: riot ALL=(root) NOPASSWD: %s", smartctlPath)
		} else {
			pass("smartctl sudo access OK (%s)", smartctlPath)
		}
	}

	// ── Docker ──
	if enabled["docker"] || enabled["container_logs"] {
		fmt.Println()
		section("Docker")
		fmt.Printf("  Mode:         %s\n", cfg.Docker.Enabled)
		if cfg.Docker.SocketPath != "" {
			fmt.Printf("  Socket:       %s (override)\n", cfg.Docker.SocketPath)
		} else {
			fmt.Printf("  Socket:       auto-detect\n")
		}
		fmt.Printf("  Stats:        %v\n", cfg.Docker.CollectStats)
		fmt.Printf("  Terminal:     %v\n", cfg.Docker.TerminalEnabled)
	}

	// ── Permissions ──
	fmt.Println()
	section("Permissions")
	fmt.Printf("  Reboot:       %v\n", cfg.Commands.AllowReboot)
	fmt.Printf("  Patching:     %v\n", cfg.Commands.AllowPatching)
	fmt.Printf("  Probes:       %v\n", cfg.Commands.AllowProbes)
	fmt.Printf("  Host shell:   %v\n", cfg.HostTerminal.Enabled)

	// ── Files ──
	fmt.Println()
	section("Files")
	checkFile("Config", configPath)
	checkFile("Device ID", IDPath())
	checkFile("Client cert", CertPath())
	checkFile("Client key", KeyPath())
	checkFile("Server cert", ServerCertPath())
	checkFile("Buffer DB", BufferPath())

	// ── Dead Man ──
	if cfg.DeadMan.Enabled {
		fmt.Println()
		section("Dead Man's Switch")
		fmt.Printf("  URL:          %s\n", cfg.DeadMan.URL)
		interval := cfg.DeadMan.IntervalSeconds
		if interval == 0 {
			interval = 60
		}
		fmt.Printf("  Interval:     %ds\n", interval)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println("Doctor complete.")
}

func checkServer(cfg *Config) {
	// Parse host from URL for a quick TCP dial
	url := cfg.Server.URL
	host := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if !strings.Contains(host, ":") {
		if strings.HasPrefix(url, "https://") {
			host += ":443"
		} else {
			host += ":7331"
		}
	}

	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	if err != nil {
		fail("Cannot reach %s: %v", host, err)
		return
	}
	conn.Close()
	pass("Server reachable at %s", host)

	// Build TLS config using the agent's CA/server certs
	tlsConfig := &tls.Config{}
	if !cfg.Server.TLSVerify {
		tlsConfig.InsecureSkipVerify = true
	} else {
		// Try pinned server cert, then CA cert, then custom CA file
		pool := x509.NewCertPool()
		loaded := false
		for _, path := range []string{ServerCertPath(), CACertPath(), cfg.Server.CACertFile} {
			if path == "" {
				continue
			}
			if pem, err := os.ReadFile(path); err == nil {
				if pool.AppendCertsFromPEM(pem) {
					loaded = true
				}
			}
		}
		if loaded {
			tlsConfig.RootCAs = pool
		}
	}

	// Try /health endpoint
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}
	resp, err := client.Get(url + "/health")
	if err != nil {
		warn("Health check failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		pass("Health endpoint returned 200")
	} else {
		warn("Health endpoint returned %d", resp.StatusCode)
	}
}

func checkFile(label, path string) {
	if info, err := os.Stat(path); err == nil {
		fmt.Printf("  %-14s %s %s\n", label+":", green("found"), dim(fmt.Sprintf("(%d bytes)", info.Size())))
	} else {
		fmt.Printf("  %-14s %s\n", label+":", dim("not found"))
	}
}

func section(name string) {
	fmt.Printf("── %s ──\n", name)
}

func pass(format string, args ...interface{}) {
	fmt.Printf("  \033[32m✓\033[0m %s\n", fmt.Sprintf(format, args...))
}

func warn(format string, args ...interface{}) {
	fmt.Printf("  \033[33m!\033[0m %s\n", fmt.Sprintf(format, args...))
}

func fail(format string, args ...interface{}) {
	fmt.Printf("  \033[31m✗\033[0m %s\n", fmt.Sprintf(format, args...))
}

func green(s string) string  { return "\033[32m" + s + "\033[0m" }
func dim(s string) string    { return "\033[90m" + s + "\033[0m" }

func friendlyDuration(seconds int) string {
	if seconds >= 3600 {
		h := seconds / 3600
		m := (seconds % 3600) / 60
		if m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
	if seconds >= 60 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

