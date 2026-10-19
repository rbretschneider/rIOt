package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
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
	"gpu":        {"nvidia-smi"},
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

	// ── Collection Limits (GROWTH-001) ──
	// The effective, clamped values the agent uses, so a typo in agent.yaml is
	// visible here rather than silently replaced by a default.
	lim := cfg.Limits()
	fmt.Println()
	section("Collection Limits")
	fmt.Printf("  Container logs: %d lines, %d bytes per container; lines cut at %d bytes\n",
		lim.ContainerLogMaxLines, lim.ContainerLogMaxBytes, lim.ContainerLogMaxLineBytes)
	fmt.Printf("  Journal:        %d entries per pass; messages cut at %d bytes\n",
		lim.JournalMaxEntries, lim.JournalMaxLineBytes)
	fmt.Printf("  Offline buffer: %d MiB (at most %d items)\n", lim.BufferMaxMB, maxBuffered)

	// ── Collectors ──
	fmt.Println()
	section("Collectors")

	allCollectors := []string{
		"system", "cpu", "memory", "disk", "network", "os_info",
		"updates", "services", "processes", "docker", "container_logs",
		"security", "logs", "ups", "webservers", "usb", "hardware", "cron",
		"gpu",
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

	// ── Journal Read Access ──
	// SEC-003 mitigation (AD-010): detect cases where journalctl is available
	// but returns empty because the agent user lacks systemd-journal group
	// membership. Without this check, LogsCollector and the auth-failure
	// detector silently report zero with no visible error.
	if runtime.GOOS == "linux" && (enabled["logs"] || enabled["security"]) {
		checkJournalAccess()
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
		checkComposeTrust(cfg)
	}

	// ── Permissions ──
	fmt.Println()
	section("Permissions")
	fmt.Printf("  Reboot:       %v\n", cfg.Commands.AllowReboot)
	fmt.Printf("  Shutdown:     %v\n", cfg.Commands.AllowShutdown)
	fmt.Printf("  Patching:     %v\n", cfg.Commands.AllowPatching)
	fmt.Printf("  Probes:       %v\n", cfg.Commands.AllowProbes)
	fmt.Printf("  Hold pkgs:    %v\n", cfg.Commands.HoldRebootClass)
	fmt.Printf("  Host shell:   %v\n", cfg.HostTerminal.Enabled)

	// ── Hold Enforcement (PATCH-GATE, SEC-PATCH-GATE-004) ──
	// Runs the same sudo -n -l probes the agent's runtime preflight uses so
	// operators discover missing sudoers rules here instead of during an outage.
	if runtime.GOOS == "linux" && cfg.Commands.HoldRebootClass {
		checkHoldEnforcement()
	}

	// ── Agent Update (AGENT-HARDEN-001, AD-007) ──
	// Probes the fixed `systemd-run <verifier>` sudoers rule and the root-owned
	// verifier binary that installs verified updates.
	if runtime.GOOS == "linux" && (cfg.Agent.AutoUpdate == nil || *cfg.Agent.AutoUpdate) {
		checkUpdatePrivilege()
	}

	// ── Memory Limit (GROWTH-001 AD-019) ──
	// The unit written by install.sh caps the agent's memory. A host whose unit
	// predates that has no cap until the installer is re-run.
	if runtime.GOOS == "linux" {
		checkMemoryLimit()
	}

	// ── OS Update Self-Heal (interrupted-dpkg recovery) ──
	// Probes the sudoers rule the agent uses to run `dpkg --configure -a`
	// before an apt upgrade. Without it the self-heal is silently skipped and
	// an interrupted dpkg state keeps failing every os_update.
	if runtime.GOOS == "linux" && cfg.Commands.AllowPatching && commandExists("apt-get") {
		checkDpkgRepair()
	}

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

// checkHoldEnforcement probes whether the reboot-class hold-enforcement
// sudoers rules exist (PATCH-GATE AD-015 preflight, same probes as the
// agent runtime) and whether the dnf fragment mechanism is supported.
func checkHoldEnforcement() {
	fmt.Println()
	section("Hold Enforcement")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hm := collectors.NewHoldManager(true, HoldStatePath(), DNFHoldsStagedPath())

	switch {
	case commandExists("apt-mark"):
		if err := hm.VerifyPrivileges(ctx, "apt"); err != nil {
			fail("apt-mark hold/unhold sudoers rules missing — hold enforcement will report no_privilege")
			warn("  Re-run the install script (curl … | sudo bash) to rewrite /etc/sudoers.d/riot-agent")
		} else {
			pass("apt-mark hold/unhold sudo rules present")
		}
	case commandExists("dnf"):
		if err := hm.VerifyPrivileges(ctx, "dnf"); err != nil {
			fail("dnf fragment install/rm sudoers rules missing — hold enforcement will report no_privilege")
			warn("  Re-run the install script (curl … | sudo bash) to rewrite /etc/sudoers.d/riot-agent")
		} else {
			pass("dnf fragment install/rm sudo rules present")
		}
		if hm.DNF5Supported(ctx) {
			pass("dnf5 detected — excludepkgs drop-in fragment supported")
		} else {
			warn("dnf5 not detected — OS-level dnf holds require dnf5; enforcement will report unsupported")
		}
	default:
		warn("no supported package manager (apt/dnf) found — hold enforcement inactive")
	}
}

// updatePrivilegeProbeTimeout bounds the non-interactive sudo probe.
const updatePrivilegeProbeTimeout = 15 * time.Second

// updatePrivilegeProbeArgs is the `sudo` argv that asks whether the fixed
// update rule is authorized, without running anything. It is built from the
// same constants the agent invokes, so the probe cannot drift from the real
// invocation (V-005).
func updatePrivilegeProbeArgs(systemdRunPath string) []string {
	return []string{"-n", "-l", systemdRunPath, "--unit=" + updateUnit, applyBinPath}
}

// checkUpdatePrivilege probes the update-apply sudoers rule and the root-owned
// verifier binary, telling the operator to re-run the installer if either is
// missing (same pattern as checkHoldEnforcement).
func checkUpdatePrivilege() {
	systemdRunPath, _ := exec.LookPath("systemd-run")
	if systemdRunPath == "" {
		systemdRunPath = "/usr/bin/systemd-run"
	}
	reportUpdatePrivilege(execRunner, os.Stat, systemdRunPath)
}

// reportUpdatePrivilege is checkUpdatePrivilege with the sudo runner and file
// stat injected for tests.
func reportUpdatePrivilege(run collectors.CommandRunner, stat func(string) (os.FileInfo, error), systemdRunPath string) {
	fmt.Println()
	section("Agent Update")

	ctx, cancel := context.WithTimeout(context.Background(), updatePrivilegeProbeTimeout)
	defer cancel()

	if _, err := run(ctx, "sudo", updatePrivilegeProbeArgs(systemdRunPath)...); err != nil {
		fail("update-apply sudo rule missing — auto-update cannot swap the binary")
		warn("  Re-run the install script (curl … | sudo bash) to rewrite /etc/sudoers.d/riot-agent and install the apply helper")
	} else {
		pass("update-apply sudo rule present (%s --unit=%s %s)", systemdRunPath, updateUnit, applyBinPath)
	}

	info, err := stat(applyBinPath)
	switch {
	case err != nil:
		fail("apply helper missing at %s — re-run the install script", applyBinPath)
	case info.Mode().Perm() != 0o755:
		warn("apply helper at %s is not mode 0755", applyBinPath)
	default:
		pass("apply helper present (%s)", applyBinPath)
	}
}

// agentUnit is the systemd unit install.sh writes.
const agentUnit = "riot-agent"

// memoryLimitProbeTimeout bounds the systemctl query.
const memoryLimitProbeTimeout = 10 * time.Second

// unlimitedBytes is the threshold above which systemd's numeric MemoryMax
// means "no limit" (older systemd prints 2^64-1 instead of "infinity").
const unlimitedBytes = uint64(1) << 62

// checkMemoryLimit asks systemd for the agent unit's memory properties.
func checkMemoryLimit() {
	ctx, cancel := context.WithTimeout(context.Background(), memoryLimitProbeTimeout)
	defer cancel()
	out, err := execRunner(ctx, "systemctl", "show", agentUnit,
		"-p", "MemoryMax", "-p", "OOMPolicy", "-p", "KillMode", "-p", "Environment")
	reportMemoryLimit(string(out), err)
}

// reportMemoryLimit prints the memory-limit section from `systemctl show`
// output. It is split from checkMemoryLimit so tests can feed it text.
func reportMemoryLimit(showOutput string, queryErr error) {
	fmt.Println()
	section("Memory Limit")
	for _, f := range memoryLimitFindings(showOutput, queryErr) {
		switch f.level {
		case findingPass:
			pass("%s", f.text)
		case findingWarn:
			warn("%s", f.text)
		case findingFail:
			fail("%s", f.text)
		default:
			fmt.Printf("  %s\n", f.text)
		}
	}
}

type findingLevel int

const (
	findingInfo findingLevel = iota
	findingPass
	findingWarn
	findingFail
)

type finding struct {
	level findingLevel
	text  string
}

const reinstallHint = "  Re-run the install script (curl … | sudo bash) to rewrite the service unit"

// memoryLimitFindings turns `systemctl show` output into doctor findings:
// MemoryMax in bytes (infinity fails), OOMPolicy, KillMode and whether
// GOMEMLIMIT is set.
func memoryLimitFindings(showOutput string, queryErr error) []finding {
	if queryErr != nil {
		return []finding{{findingWarn, fmt.Sprintf("could not query systemd for the %s unit (not installed as a service?): %v", agentUnit, queryErr)}}
	}
	props := map[string]string{}
	for _, line := range strings.Split(showOutput, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[key] = value
		}
	}

	var out []finding

	maxBytes, parseErr := strconv.ParseUint(props["MemoryMax"], 10, 64)
	switch {
	case parseErr != nil || maxBytes >= unlimitedBytes:
		out = append(out,
			finding{findingFail, "no memory limit on the agent unit (MemoryMax=infinity) — a runaway collector can exhaust the host"},
			finding{findingInfo, reinstallHint})
	default:
		out = append(out, finding{findingPass, fmt.Sprintf("MemoryMax = %d bytes (%d MiB)", maxBytes, maxBytes>>20)})
	}

	if policy := props["OOMPolicy"]; policy == "continue" {
		out = append(out, finding{findingPass, "OOMPolicy = continue"})
	} else {
		out = append(out,
			finding{findingWarn, fmt.Sprintf("OOMPolicy = %q (expected continue); one OOM-killed process would stop the whole unit", policy)},
			finding{findingInfo, reinstallHint})
	}

	out = append(out, finding{findingInfo, fmt.Sprintf("KillMode = %s", props["KillMode"])})

	if limit, ok := environmentValue(props["Environment"], "GOMEMLIMIT"); ok {
		out = append(out, finding{findingPass, "GOMEMLIMIT = " + limit})
	} else {
		out = append(out,
			finding{findingWarn, "GOMEMLIMIT is not set in the unit; the Go runtime will not collect garbage early under memory pressure"},
			finding{findingInfo, reinstallHint})
	}
	return out
}

// environmentValue finds NAME=value among the space-separated assignments of a
// `systemctl show -p Environment` line.
func environmentValue(env, name string) (string, bool) {
	for _, field := range strings.Fields(env) {
		field = strings.Trim(field, `"`)
		if value, ok := strings.CutPrefix(field, name+"="); ok {
			return value, true
		}
	}
	return "", false
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// checkDpkgRepair probes whether the sudoers rule for the interrupted-dpkg
// self-heal (`dpkg --configure -a`) exists, using the same `sudo -n -l` probe
// style as hold enforcement so operators find a missing rule here rather than
// during a stuck nightly upgrade.
func checkDpkgRepair() {
	fmt.Println()
	section("OS Update Self-Heal")

	dpkgPath, err := exec.LookPath("dpkg")
	if err != nil {
		dpkgPath = "/usr/bin/dpkg"
	}
	if _, err := exec.Command("sudo", "-n", "-l", dpkgPath, "--configure", "-a").CombinedOutput(); err != nil {
		fail("dpkg --configure -a sudoers rule missing — interrupted-dpkg self-heal will not run")
		warn("  Re-run the install script (curl … | sudo bash) to rewrite /etc/sudoers.d/riot-agent")
	} else {
		pass("dpkg --configure -a sudo rule present (interrupted-dpkg self-heal armed)")
	}
}

// checkJournalAccess verifies the agent can read the system journal (AD-010, SEC-003).
// Called only on Linux when the "logs" or "security" collector is enabled.
func checkJournalAccess() {
	fmt.Println()
	section("Journal Access")

	// Try reading one journal entry with the same flags LogsCollector uses.
	out, err := exec.Command("journalctl", "--priority=0..6", "-n", "1", "--no-pager").Output()
	if err != nil {
		// journalctl binary error — collectorDeps already reported this as missing.
		// Avoid a double-warn; the Dependencies section covers it.
		return
	}

	if len(strings.TrimSpace(string(out))) > 0 {
		pass("Journal read access OK")
		return
	}

	// Empty output — could be permission issue or genuinely empty journal.
	// Check group membership via "id -Gn".
	idOut, idErr := exec.Command("id", "-Gn").Output()
	inGroup := false
	if idErr == nil {
		for _, g := range strings.Fields(string(idOut)) {
			if g == "systemd-journal" {
				inGroup = true
				break
			}
		}
	}

	if !inGroup {
		warn("Journal read returned empty and user is not in 'systemd-journal' group")
		warn("  Auth-failure detector will silently report zero — fix with:")
		warn("  sudo usermod -a -G systemd-journal riot && sudo systemctl restart riot-agent")
	} else {
		warn("Journal read returned empty — journald may be inactive or priority filter matched nothing")
	}
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

func info(format string, args ...interface{}) {
	fmt.Printf("  \033[36mi\033[0m %s\n", fmt.Sprintf(format, args...))
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


// maxDoctorLine bounds one doctor line that carries a label- or
// filesystem-derived value.
const maxDoctorLine = 1100

// checkComposeTrust runs the compose trust check against every compose project
// directory the local Docker daemon knows (AUTOUPD-HARDEN-001 AD-012). It runs
// with the doctor's own privileges. Root can read mount sources and files the
// agent user cannot, so a pass as root can hide a refusal; reportPrivilegeMismatch
// warns when the doctor is not running as the agent's service user (QA-D2).
func checkComposeTrust(cfg *Config) {
	t := &composeTrust{fs: osTrustFS{}, goos: runtime.GOOS, socket: cfg.Docker.SocketPath, roots: cfg.Docker.ComposeRoots, rootsErr: cfg.Docker.rootsErr}
	if runtime.GOOS != "windows" {
		reportPrivilegeMismatch(os.Geteuid(), agentServiceUser(os.ReadFile), lookupUID)
	}
	cli, err := newDockerClient(cfg.Docker.SocketPath)
	if err != nil {
		reportComposeTrust(cfg.Docker, daemonSnapshot{}, err, t)
		return
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snap, err := loadDaemonSnapshot(ctx, sdkDocker{cli})
	reportComposeTrust(cfg.Docker, snap, err, t)
}

// reportComposeTrust prints the compose roots setting and one pass or fail line
// per (project, working directory) pair, with the rule that failed. The state it
// reports on is injected, for tests.
func reportComposeTrust(cfg DockerConfig, snap daemonSnapshot, snapErr error, t *composeTrust) {
	line := func(format string, args ...interface{}) string {
		return models.TruncateHead(fmt.Sprintf(format, args...), maxDoctorLine)
	}
	switch {
	case cfg.rootsErr != nil:
		fmt.Printf("  Compose roots: %s\n", dim("invalid"))
		fail("%s", line("docker.compose_roots has an entry that is not an absolute path; every compose update is refused until it is fixed"))
	case len(cfg.ComposeRoots) == 0:
		fmt.Printf("  Compose roots: %s\n", dim("not set"))
	default:
		fmt.Printf("  Compose roots: %s\n", models.TruncateHead(strings.Join(cfg.ComposeRoots, ", "), maxDoctorLine))
	}
	if t.windows() && len(cfg.ComposeRoots) == 0 && cfg.rootsErr == nil {
		fail("compose updates on Windows require docker.compose_roots (ownership cannot be verified)")
	}
	if snapErr != nil {
		warn("%s", line("cannot read docker state: %s", models.TruncateHead(snapErr.Error(), maxRefusalValueBytes)))
		return
	}

	for _, w := range t.FallbackSources(snap) {
		info("%s", line("mount source %q of container %q was compared through its readable prefix %q (the agent user cannot read below it)",
			models.TruncateHead(w.source, maxRefusalValueBytes), models.TruncateHead(w.container, maxRefusalValueBytes), models.TruncateHead(w.prefix, maxRefusalValueBytes)))
	}

	var ignored []string
	for _, c := range snap.Containers {
		if reason, bad := snap.excluded[c.ID]; bad {
			why := "its image defines the compose working-dir label"
			if reason == excludedImageUnread {
				why = "its image could not be inspected"
			}
			ignored = append(ignored, line("container %q ignored as a compose source: %s", models.TruncateHead(summaryName(c), maxRefusalValueBytes), why))
		}
	}
	sort.Strings(ignored)
	for _, l := range ignored {
		warn("%s", l)
	}

	type pair struct{ project, dir string }
	seen := map[pair]bool{}
	var pairs []pair
	for _, c := range snap.composeSources() {
		p := pair{c.Labels[models.LabelComposeProject], c.Labels[models.LabelComposeWorkingDir]}
		if !seen[p] {
			seen[p] = true
			pairs = append(pairs, p)
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].project != pairs[j].project {
			return pairs[i].project < pairs[j].project
		}
		return pairs[i].dir < pairs[j].dir
	})
	if len(pairs) == 0 {
		fmt.Println("  No compose projects found")
		return
	}
	for _, p := range pairs {
		td, err := t.Check(snap, trustRequest{Dir: p.dir, Project: p.project})
		if err != nil {
			fail("%s", line("project %q: %s", models.TruncateHead(p.project, maxRefusalValueBytes), err.Error()))
			continue
		}
		pass("%s", line("project %q: %q is trusted (%d compose file(s))", models.TruncateHead(p.project, maxRefusalValueBytes), td.Path(), len(td.Files())))
	}
}

// agentServiceUnitPath is where install.sh writes the agent's systemd unit.
const agentServiceUnitPath = "/etc/systemd/system/riot-agent.service"

// agentServiceUser is the user the agent service runs as: the User= line of its
// unit, or "riot", the installer's default.
func agentServiceUser(readFile func(string) ([]byte, error)) string {
	if data, err := readFile(agentServiceUnitPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "User="); ok && strings.TrimSpace(rest) != "" {
				return models.TruncateHead(strings.TrimSpace(rest), maxRefusalValueBytes)
			}
		}
	}
	return "riot"
}

// lookupUID resolves a user name to its UID.
func lookupUID(name string) (int, bool) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, false
	}
	uid, err := strconv.Atoi(u.Uid)
	return uid, err == nil
}

// reportPrivilegeMismatch warns when the doctor runs as a different user than
// the agent service. Run as root, the doctor can read mount sources and files
// the agent user cannot, so a green doctor could hide a refusing agent.
func reportPrivilegeMismatch(euid int, serviceUser string, lookup func(string) (int, bool)) {
	uid, ok := lookup(serviceUser)
	if !ok || uid == euid {
		return
	}
	warn("%s", models.TruncateHead(fmt.Sprintf("the doctor runs as uid %d but the agent runs as user %q (uid %d): the compose checks below can read mount sources and files that the agent user cannot, so the agent may refuse what passes here; run it as that user, for example: sudo -u %s riot-agent doctor",
		euid, serviceUser, uid, serviceUser), maxDoctorLine))
}
