//go:build linux

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

func trustedMaintenanceFile(path string, executable bool) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		st, err := os.Lstat(current)
		if err != nil {
			return err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Uid != 0 || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("untrusted root-owned maintenance path %s", current)
		}
		if current == path && (!st.Mode().IsRegular() || (executable && st.Mode().Perm()&0111 == 0)) {
			return fmt.Errorf("invalid maintenance file %s", path)
		}
		if current == "/" {
			break
		}
	}
	return nil
}

// systemBusSocket is where unprivileged systemctl reaches systemd. Root uses
// systemd's private socket instead, so a root-run probe cannot prove this.
const systemBusSocket = "/run/dbus/system_bus_socket"

func systemBusReady(path string) error {
	st, err := os.Stat(path)
	if err != nil || st.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("D-Bus system bus missing at %s; the riot user cannot query systemd; re-run install.sh to install dbus", path)
	}
	return nil
}

func maintenanceReadiness(ctx context.Context, run collectors.CommandRunner) error {
	if err := systemBusReady(systemBusSocket); err != nil {
		return err
	}
	for _, path := range []string{maintenance.HelperPath, applyBinPath, "/usr/bin/systemctl", "/usr/bin/sudo", "/usr/bin/apt-get", "/usr/bin/dpkg", "/usr/bin/dpkg-query", "/usr/bin/apt-mark"} {
		if err := trustedMaintenanceFile(path, true); err != nil {
			return err
		}
	}
	if err := trustedMaintenanceFile(maintenance.UnitPath, false); err != nil {
		return err
	}
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	protocol, err := run(probe, applyBinPath, "--maintenance-protocol")
	if err != nil || strings.TrimSpace(string(protocol)) != "1" {
		return fmt.Errorf("signed apply helper lacks maintenance exclusion protocol")
	}
	sudoVersion, err := run(probe, "/usr/bin/sudo", "--version")
	if err != nil {
		return fmt.Errorf("cannot verify sudo argument-regex support: %w", err)
	}
	if err := maintenanceSudoVersion(string(sudoVersion)); err != nil {
		return err
	}
	apt, err := run(probe, "/usr/bin/apt-get", "--version")
	fields := strings.Fields(string(apt))
	if err != nil || len(fields) < 2 {
		return fmt.Errorf("cannot verify apt version")
	}
	major, err := strconv.Atoi(strings.Split(fields[1], ".")[0])
	if err != nil || major < 2 {
		return fmt.Errorf("apt 2.x or newer required")
	}
	systemd, err := run(probe, "/usr/bin/systemctl", "--version")
	fields = strings.Fields(string(systemd))
	if err != nil || len(fields) < 2 {
		return fmt.Errorf("cannot verify systemd version")
	}
	version, err := strconv.Atoi(fields[1])
	if err != nil || version < 241 {
		return fmt.Errorf("systemd 241 or newer required")
	}
	if _, err = run(probe, "sudo", "-n", "-l", "/usr/bin/systemctl", "start", "--no-block", maintenance.UnitName); err != nil {
		return fmt.Errorf("exact maintenance start sudo rule missing: %w", err)
	}
	policy, err := run(probe, "sudo", "-n", "-l")
	if err != nil {
		return fmt.Errorf("cannot inspect effective maintenance sudo policy: %w", err)
	}
	if err := maintenanceSudoPolicy(string(policy)); err != nil {
		return err
	}
	if err := maintenanceDeniedPrivileges(probe, run); err != nil {
		return err
	}
	unit, err := run(probe, "/usr/bin/systemctl", "show", maintenance.UnitName, "-p", "FragmentPath", "-p", "User", "-p", "ExecStart", "-p", "PartOf", "-p", "BindsTo", "-p", "Requires", "-p", "StandardInput", "-p", "StandardOutput", "-p", "StandardError", "-p", "Restart", "-p", "KillMode", "-p", "RuntimeMaxUSec", "-p", "TimeoutStopUSec", "-p", "Type", "-p", "WorkingDirectory", "-p", "PrivateTmp", "-p", "MountFlags")
	if err != nil {
		return fmt.Errorf("cannot inspect fixed maintenance unit: %w", err)
	}
	if err := maintenanceUnitPolicy(string(unit)); err != nil {
		return err
	}
	agent, err := run(probe, "/usr/bin/systemctl", "show", "riot-agent.service", "-p", "ExecStartPre", "-p", "User")
	if err != nil {
		return fmt.Errorf("cannot inspect agent boot policy: %w", err)
	}
	return maintenanceAgentPolicy(string(agent))
}

func maintenanceDeniedPrivileges(ctx context.Context, run collectors.CommandRunner) error {
	probes := [][]string{{"/bin/sh", "-c", "true"}, {"/usr/bin/systemd-run", "--unit=riot-agent-update", "sh", "-c", "true"}, {"/usr/bin/apt-get", "-o", "APT::Update::Pre-Invoke::=/bin/true", "update"}, {"/usr/bin/apt-mark", "hold", "-oDir::Bin::dpkg=/tmp/riot-maintenance-denied-probe", "riot-policy-probe"}, {"/usr/bin/apt-mark", "unhold", "-oDir::Bin::dpkg=/tmp/riot-maintenance-denied-probe", "riot-policy-probe"}}
	for _, args := range probes {
		if _, err := run(ctx, "sudo", append([]string{"-n", "-l"}, args...)...); err == nil {
			return fmt.Errorf("unsafe legacy privilege remains: %s", args[0])
		}
	}
	return nil
}

func unitProperties(text string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, val, ok := strings.Cut(line, "=")
		if ok {
			values[key] = val
		}
	}
	return values
}

func maintenanceUnitPolicy(text string) error {
	values := unitProperties(text)
	if values["FragmentPath"] != maintenance.UnitPath || (values["User"] != "root" && values["User"] != "") || values["PartOf"] != "" || values["BindsTo"] != "" || strings.Contains(values["Requires"], "riot-agent") || values["StandardInput"] != "null" || values["StandardOutput"] != "journal" || values["StandardError"] != "journal" || values["Restart"] != "no" || values["Type"] != "exec" || values["WorkingDirectory"] != "/" || values["KillMode"] != "control-group" || !maintenanceRuntimeBackstop(values["RuntimeMaxUSec"]) || !maintenanceNoSandbox(values) || (values["TimeoutStopUSec"] != "1min 30s" && values["TimeoutStopUSec"] != "90s" && values["TimeoutStopUSec"] != "90000000") || !strings.Contains(values["ExecStart"], "argv[]="+maintenance.HelperPath+" ;") || strings.Count(values["ExecStart"], "path=") != 1 || strings.Count(values["ExecStart"], "argv[]=") != 1 {
		return fmt.Errorf("maintenance unit does not match the fixed independent worker policy")
	}
	return nil
}

// maintenanceRuntimeBackstop accepts the two backstops the installer can write:
// the signed unit's 35 minutes, or the 85 minutes of a host opted in to an extra
// updater (EXTRA-UPD-001 AD-003). systemd renders each either as words or as
// microseconds. PrivateTmp and MountFlags are checked beside it: a mount
// sandbox would hide the mounts DietPi makes from the host (OPS-001).
func maintenanceRuntimeBackstop(value string) bool {
	switch value {
	case "35min", "2100000000", "1h 25min", "5100000000":
		return true
	}
	return false
}

// maintenanceNoSandbox requires both mount properties to be reported, and to say
// there is no private /tmp and no mount flags. A missing property fails closed.
func maintenanceNoSandbox(values map[string]string) bool {
	tmp, haveTmp := values["PrivateTmp"]
	flags, haveFlags := values["MountFlags"]
	return haveTmp && haveFlags && tmp == "no" && flags == ""
}

func maintenanceAgentPolicy(text string) error {
	values := unitProperties(text)
	if values["User"] != "riot" {
		return fmt.Errorf("agent must use the unprivileged riot service account")
	}
	pre := values["ExecStartPre"]
	if pre != "" && (!strings.Contains(pre, "argv[]="+applyBinPath+" --boot ;") || strings.Count(pre, "path=") != 1 || strings.Count(pre, "argv[]=") != 1) {
		return fmt.Errorf("agent boot policy contains an unsafe or unverified staged update command")
	}
	return nil
}

var broadMaintenanceSudo = regexp.MustCompile(`(?m)^\s*\([^)]*\)\s*(?:NOPASSWD:\s*)?ALL\s*(?:,|$)`)
var maintenanceSudoVersionPattern = regexp.MustCompile(`^Sudo version ([0-9]+)\.([0-9]+)\.([0-9]+)(?:p[0-9]+)?(?:\s|$)`)

func maintenanceSudoVersion(text string) error {
	parts := maintenanceSudoVersionPattern.FindStringSubmatch(text)
	if len(parts) != 4 {
		return fmt.Errorf("cannot verify sudo version; sudo 1.9.10+ required for constrained apt holds")
	}
	version := [3]int{}
	for i := range version {
		value, err := strconv.Atoi(parts[i+1])
		if err != nil {
			return fmt.Errorf("invalid sudo version")
		}
		version[i] = value
	}
	if version[0] > 1 || version[0] == 1 && (version[1] > 9 || version[1] == 9 && version[2] >= 10) {
		return nil
	}
	return fmt.Errorf("sudo 1.9.10+ required for constrained apt holds; upgrade sudo explicitly before root installation")
}

const maintenanceHoldArgs = `[a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$`

func maintenanceAptMarkPolicy(policy string) error {
	// sudo -l may combine multiple commands on one line. Inspect each actual
	// apt-mark argument expression, not the stars inside an approved regex.
	for _, line := range strings.Split(policy, "\n") {
		for _, command := range strings.Split(line, ",") {
			_, args, found := strings.Cut(command, "/usr/bin/apt-mark")
			if !found {
				continue
			}
			args = strings.TrimSpace(args)
			if args != "^hold "+maintenanceHoldArgs && args != "^unhold "+maintenanceHoldArgs && args != "showhold" {
				return fmt.Errorf("effective sudo policy permits unconstrained apt-mark arguments; reinstall constrained hold rules")
			}
		}
	}
	return nil
}

func maintenanceSudoPolicy(policy string) error {
	if err := maintenanceAptMarkPolicy(policy); err != nil {
		return err
	}
	if broadMaintenanceSudo.MatchString(policy) {
		return fmt.Errorf("effective agent sudo policy grants unrestricted root commands")
	}
	for _, line := range strings.Split(policy, "\n") {
		if strings.Contains(line, "/bin/sh") || strings.Contains(line, "/bin/bash") || strings.Contains(line, "/usr/bin/env ") || strings.Contains(line, "apt-get ") || (strings.Contains(line, "systemd-run") && strings.Contains(line, "*")) {
			return fmt.Errorf("effective agent sudo policy contains legacy broad shell/package/update execution")
		}
	}
	return nil
}
