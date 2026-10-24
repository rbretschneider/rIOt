//go:build linux

package agent

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

func updateInstallPreflight(run collectors.CommandRunner) func(context.Context) error {
	return func(ctx context.Context) error {
		return probeUpdateInstallation(ctx, run, func() error { return trustedMaintenanceFile(applyBinPath, true) })
	}
}

func probeUpdateInstallation(ctx context.Context, run collectors.CommandRunner, trusted func() error) error {
	if err := trusted(); err != nil {
		return fmt.Errorf("agent update requires a one-time root installer migration: signed apply helper %s is missing or untrusted; preserve existing config and enrollment when running the matching signed release installer: %w", applyBinPath, err)
	}
	systemdRun, err := exec.LookPath("systemd-run")
	if err != nil {
		return fmt.Errorf("agent update requires systemd-run: %w", err)
	}
	probe, cancel := context.WithTimeout(ctx, updatePrivilegeProbeTimeout)
	defer cancel()
	if _, err := run(probe, "sudo", updatePrivilegeProbeArgs(systemdRun)...); err != nil {
		return fmt.Errorf("agent update requires a one-time root installer migration: fixed signed-apply sudo permission is unavailable; repair the installed policy using the matching signed release installer: %w", err)
	}
	return nil
}
