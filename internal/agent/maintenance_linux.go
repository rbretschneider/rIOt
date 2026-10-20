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

func maintenanceReadiness(ctx context.Context, run collectors.CommandRunner) error {
	for _, path := range []string{maintenance.HelperPath, applyBinPath, "/usr/bin/systemctl", "/usr/bin/apt-get", "/usr/bin/dpkg", "/usr/bin/dpkg-query", "/usr/bin/apt-mark"} {
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
	unit, err := run(probe, "/usr/bin/systemctl", "show", maintenance.UnitName, "-p", "FragmentPath", "-p", "User", "-p", "ExecStart", "-p", "PartOf", "-p", "BindsTo", "-p", "Requires", "-p", "StandardInput", "-p", "StandardOutput", "-p", "StandardError", "-p", "Restart", "-p", "KillMode", "-p", "RuntimeMaxUSec", "-p", "TimeoutStopUSec", "-p", "Type", "-p", "WorkingDirectory")
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
	probes := [][]string{{"/bin/sh", "-c", "true"}, {"/usr/bin/systemd-run", "--unit=riot-agent-update", "sh", "-c", "true"}, {"/usr/bin/apt-get", "-o", "APT::Update::Pre-Invoke::=/bin/true", "update"}}
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
	if values["FragmentPath"] != maintenance.UnitPath || (values["User"] != "root" && values["User"] != "") || values["PartOf"] != "" || values["BindsTo"] != "" || strings.Contains(values["Requires"], "riot-agent") || values["StandardInput"] != "null" || values["StandardOutput"] != "journal" || values["StandardError"] != "journal" || values["Restart"] != "no" || values["Type"] != "exec" || values["WorkingDirectory"] != "/" || values["KillMode"] != "control-group" || (values["RuntimeMaxUSec"] != "35min" && values["RuntimeMaxUSec"] != "2100000000") || (values["TimeoutStopUSec"] != "1min 30s" && values["TimeoutStopUSec"] != "90s" && values["TimeoutStopUSec"] != "90000000") || !strings.Contains(values["ExecStart"], "argv[]="+maintenance.HelperPath+" ;") || strings.Count(values["ExecStart"], "path=") != 1 || strings.Count(values["ExecStart"], "argv[]=") != 1 {
		return fmt.Errorf("maintenance unit does not match the fixed independent worker policy")
	}
	return nil
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

func maintenanceSudoPolicy(policy string) error {
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
