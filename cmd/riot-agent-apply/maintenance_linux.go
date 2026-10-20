//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

func (in *installer) maintenanceExclusion() (func(), error) {
	if in.acquireMaintenance == nil { // Filesystem-only test fixture.
		return func() {}, nil
	}
	lock, err := in.acquireMaintenance()
	if err != nil {
		return nil, err
	}
	if in.maintenanceIdle != nil {
		if err := in.maintenanceIdle(); err != nil {
			lock.Close()
			return nil, err
		}
	}
	return func() {
		if err := lock.Close(); err != nil {
			in.logger.Error("cannot close maintenance exclusion", "error", err)
		}
	}, nil
}

// A free flock does not prove systemd has finished cleaning up worker children.
func requireMaintenanceIdle() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", maintenance.UnitName, "-p", "LoadState", "-p", "ActiveState").Output()
	if err != nil {
		return fmt.Errorf("cannot verify maintenance unit state: %w", err)
	}
	state := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			state[key] = value
		}
	}
	if state["LoadState"] != "loaded" && state["LoadState"] != "not-found" {
		return errors.New("maintenance unit state is unsafe or unknown")
	}
	if state["ActiveState"] != "inactive" && state["ActiveState"] != "failed" {
		return maintenance.ErrBusy
	}
	record, err := maintenance.ReadCurrent()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot verify maintenance record: %w", err)
	}
	if !record.Terminal() || record.HoldsReleased || record.HoldCleanupFailed || record.HoldRestoreError != "" {
		return errors.New("maintenance outcome or hold restoration needs recovery")
	}
	return nil
}

func (in *installer) updateStatus(state, digest, message string) error {
	if in.writeUpdateStatus == nil {
		return nil
	}
	if digest == "" {
		digest = in.stagedSHA256
	}
	if digest == "" {
		data, err := readStagedFile(in.staged.binary, in.maxBinarySize)
		if err != nil {
			return fmt.Errorf("cannot identify staged update: %w", err)
		}
		sum := sha256.Sum256(data)
		digest = hex.EncodeToString(sum[:])
		in.stagedSHA256 = digest
	}
	if len(message) > 4096 {
		message = message[:4096]
	}
	return in.writeUpdateStatus(maintenance.UpdateStatus{
		Version: maintenance.ProtocolVersion, State: state, SHA256: digest,
		Message: message, UpdatedAt: time.Now().UTC(),
	})
}

func (in *installer) noteUpdateStatus(state, digest, message string) {
	if err := in.updateStatus(state, digest, message); err != nil {
		in.logger.Error("cannot persist updater outcome", "state", state, "error", err)
	}
}
