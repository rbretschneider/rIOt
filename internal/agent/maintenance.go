package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

type maintenanceClient struct {
	ready    func(context.Context) error
	write    func(maintenance.Request) error
	current  func() (maintenance.Record, error)
	terminal func(string) (maintenance.Record, error)
	active   func(context.Context) (bool, error)
	present  func() bool
	wait     func(context.Context) error

	// EXTRA-UPD-001 seams. A nil member reads as "not available": the updater is
	// then not enabled (unsupported_helper or root_side_missing), never run.
	capabilities func(context.Context) ([]byte, error) // riot-maintenance --capabilities, bounded
	rootPolicy   func() ([]byte, error)                // the root-owned opt-in policy file, bounded
	unitShow     func(context.Context) (string, error) // systemctl show of the three fixed unit properties
	signature    func() string                         // stat signature of helper, policy and drop-in; "" disables the cache
}

func (a *Agent) maintenance() *maintenanceClient {
	if a.maintenanceClient != nil {
		return a.maintenanceClient
	}
	return &maintenanceClient{
		ready: func(ctx context.Context) error { return maintenanceReadiness(ctx, a.runCommand) },
		write: maintenance.WriteRequest, current: maintenance.ReadCurrent, terminal: maintenance.ReadTerminal,
		active: func(ctx context.Context) (bool, error) {
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			out, err := a.runCommand(probe, "/usr/bin/systemctl", "show", maintenance.UnitName, "-p", "LoadState", "-p", "ActiveState")
			text := string(out)
			if strings.Contains(text, "LoadState=not-found") {
				return false, nil
			}
			if err != nil {
				return false, systemctlProbeError(text, err)
			}
			for _, line := range strings.Split(text, "\n") {
				if state, ok := strings.CutPrefix(line, "ActiveState="); ok {
					switch state {
					case "activating", "active", "deactivating", "reloading":
						return true, nil
					case "inactive", "failed":
						return false, nil
					default:
						return false, fmt.Errorf("unknown maintenance unit state")
					}
				}
			}
			return false, fmt.Errorf("maintenance unit state unavailable")
		},
		present: func() bool {
			if runtime.GOOS != "linux" {
				return false
			}
			for _, p := range []string{maintenance.UnitPath, maintenance.StateDir} {
				if _, err := os.Lstat(p); !os.IsNotExist(err) {
					return true
				}
			}
			return false
		},
		wait: func(ctx context.Context) error {
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		capabilities: extraCapabilities,
		rootPolicy:   extraRootPolicy,
		unitShow: func(ctx context.Context) (string, error) {
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			out, err := a.runCommand(probe, "/usr/bin/systemctl", append([]string{}, maintenance.UnitShowArgs()...)...)
			return string(out), err
		},
		signature: extraSignature,
	}
}

// systemctlProbeError keeps systemctl's own diagnosis. Unprivileged systemctl
// talks to systemd over the D-Bus system bus, which minimal images (DietPi)
// omit; root uses a private socket, so root-only checks never see this.
func systemctlProbeError(output string, err error) error {
	detail := strings.TrimSpace(output)
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = detail[:i]
	}
	if strings.Contains(detail, "Failed to connect to bus") {
		return fmt.Errorf("D-Bus system bus unavailable (%s); re-run install.sh to install dbus: %w", detail, err)
	}
	if detail != "" {
		return fmt.Errorf("%s: %w", detail, err)
	}
	return err
}

const maintenanceRunning = "OS maintenance is running"

// maintenanceBusy is conservative when a previously installed worker cannot be
// inspected. Monitoring remains available, but host mutations do not race it.
func (a *Agent) maintenanceBusy(ctx context.Context) bool {
	return a.maintenanceBlocker(ctx) != ""
}

// maintenanceBlocker returns why host mutations are blocked, or "" when they
// are not. The reason reaches command results and logs so a blocked device can
// be diagnosed from the dashboard.
func (a *Agent) maintenanceBlocker(ctx context.Context) string {
	return a.maintenanceBlockerFor(ctx, "")
}

// maintenanceBlockerFor is maintenanceBlocker for a reboot that follows command
// ownID. The helper unit of that same command can still be active or
// deactivating just after it wrote its terminal record; that is the command
// winding down, not other maintenance work, so it does not block (PATCH-FLOW-001
// C-3). An empty ownID keeps the strict behavior.
func (a *Agent) maintenanceBlockerFor(ctx context.Context, ownID string) string {
	m := a.maintenance()
	if !m.present() {
		return ""
	}
	active, err := m.active(ctx)
	if err != nil {
		return "cannot inspect " + maintenance.UnitName + ": " + err.Error()
	}
	if active && ownID == "" {
		return maintenanceRunning
	}
	record, err := m.current()
	if active && !finishedRunOf(ownID, record, err) {
		return maintenanceRunning
	}
	switch {
	case os.IsNotExist(err):
		return ""
	case err != nil:
		return "cannot read maintenance state: " + err.Error()
	case !record.Terminal():
		return "previous OS maintenance did not finish; run os_update to recover"
	case record.HoldsReleased || record.HoldCleanupFailed || record.HoldRestoreError != "":
		return "package holds were not restored after OS maintenance; run os_update to recover"
	}
	return ""
}

// finishedRunOf reports whether record is the terminal record of command id.
func finishedRunOf(id string, record maintenance.Record, err error) bool {
	return id != "" && err == nil && record.CommandID == id && record.Terminal()
}

func (a *Agent) runDetachedMaintenance(ctx context.Context, payload models.CommandPayload, mode string, include bool) commandExecResult {
	return a.runDetachedMaintenanceWith(ctx, payload, mode, include, nil, nil)
}

// runDetachedMaintenanceWith is runDetachedMaintenance with the extra-updater
// plan and the command's run context. A nil plan, or one that enables nothing,
// sends the version 1 request exactly as before (FR-066, AC-044).
func (a *Agent) runDetachedMaintenanceWith(ctx context.Context, payload models.CommandPayload, mode string, include bool, plan *extraPlan, cr *cmdRun) commandExecResult {
	req := maintenance.Request{Version: maintenance.ProtocolVersion, CommandID: payload.CommandID, Mode: mode, IncludeRebootClass: include}
	if plan.sendsV2() {
		req.Version, req.ExtraUpdaters = maintenance.RequestVersionExtra, append([]string(nil), plan.request...)
	}
	if err := maintenance.ValidateRequest(req); err != nil {
		return precheckFailure(err.Error())
	}
	m := a.maintenance()
	if err := m.ready(ctx); err != nil {
		return precheckFailure(fmt.Sprintf("safe apt maintenance unavailable: %v; run the verified root installer and riot-agent doctor", err))
	}
	if old, err := m.terminal(req.CommandID); err == nil {
		r := maintenanceResult(old)
		r.RebootClassApplied = nil
		return r
	} else if !os.IsNotExist(err) {
		return precheckFailure("cannot validate previous maintenance result: " + err.Error())
	}
	if active, err := m.active(ctx); err != nil || active {
		return precheckFailure("maintenance unit is busy or unavailable")
	}
	if a.holdMgr != nil {
		if err := a.holdMgr.MaintenanceBarrier(ctx); err != nil {
			return precheckFailure("hold reconciliation did not drain: " + err.Error())
		}
	}
	if err := m.write(req); err != nil {
		return precheckFailure("cannot persist maintenance request: " + err.Error())
	}
	if _, err := a.runCommand(ctx, "sudo", "-n", "/usr/bin/systemctl", "start", "--no-block", maintenance.UnitName); err != nil {
		return precheckFailure("cannot start fixed maintenance service: " + err.Error())
	}
	return a.observeMaintenanceWith(ctx, req.CommandID, false, cr)
}

func maintenanceResult(rec maintenance.Record) commandExecResult {
	code := rec.ExitCode
	r := commandExecResult{Status: models.ResultStatusError, Message: rec.Summary, ExitCode: &code, Output: rec.Output, Step: models.StepVerify}
	if r.Message == "" {
		r.Message = "OS maintenance " + rec.Outcome + " at " + rec.Phase
	}
	if rec.Outcome == "success" && !rec.Interrupted && !rec.HoldsReleased && !rec.HoldCleanupFailed && rec.HoldRestoreError == "" {
		r.Status = models.ResultStatusSuccess
		if rec.IncludeRebootClass && rec.Mode != "repair" {
			for _, change := range rec.VersionChanges {
				if change.Before != "" && change.After != "" && change.Before != change.After {
					r.RebootClassApplied = append(r.RebootClassApplied, change.Package)
				}
			}
			// A scheduled run treats "an updater succeeded and a reboot is now
			// required" as a reboot-class change; the gate rules then apply
			// unchanged (EXTRA-UPD-001 DEC-008, OQ-A5).
			r.RebootClassApplied = append(r.RebootClassApplied, extraRebootEntries(rec)...)
		}
	} else if rec.Interrupted {
		r.Reason = models.ReasonRestarted
	}
	if rec.HoldCleanupFailed || rec.HoldsReleased {
		r.Message += "; managed holds require operator recovery"
	}
	return r
}

func (a *Agent) observeMaintenance(ctx context.Context, id string, recovered bool) commandExecResult {
	return a.observeMaintenanceWith(ctx, id, recovered, nil)
}

// observeMaintenanceWith polls the root record. cr, when set, receives the
// extra-updater progress steps; the events are reported with or without it.
func (a *Agent) observeMaintenanceWith(ctx context.Context, id string, recovered bool, cr *cmdRun) commandExecResult {
	m := a.maintenance()
	// A missing observer is never an instruction to cancel package work. The
	// service's independent budget is authoritative.
	for attempts := 0; ; attempts++ {
		if rec, err := m.terminal(id); err == nil {
			a.reportExtraProgress(cr, id, rec)
			r := maintenanceResult(rec)
			if recovered && len(r.RebootClassApplied) > 0 {
				r.Message += "; reboot required (recovered result; automatic reboot not replayed)"
				r.RebootClassApplied = nil
			}
			return r
		} else if !os.IsNotExist(err) {
			slog.Warn("maintenance result inspection failed", "command_id", id, "error", err)
		}
		rec, err := m.current()
		if err == nil && rec.CommandID == id {
			a.reportExtraProgress(cr, id, rec)
			if rec.Terminal() {
				r := maintenanceResult(rec)
				if recovered {
					r.RebootClassApplied = nil
				}
				return r
			}
			a.notifyProgress(id, models.CommandRunning, extraProgressStep(rec.Phase), nil)
		}
		active, unitErr := m.active(ctx)
		if attempts >= 10 && unitErr == nil && !active {
			return commandExecResult{Status: models.ResultStatusError, Reason: models.ReasonRestarted, Step: models.StepVerify, Message: "maintenance service ended without a validated terminal result; package outcome unknown; inspect root journal and recovery status"}
		}
		if err := m.wait(ctx); err != nil {
			return commandExecResult{Detached: true, Message: "observer detached; independent maintenance continues; result will be recovered"}
		}
	}
}

func (a *Agent) recoverMaintenanceCommand(id string, marker inflightMarker) bool {
	m := a.maintenance()
	if !m.present() {
		return false
	}
	var rec maintenance.Record
	var err error
	rec, err = m.terminal(id)
	if err != nil {
		rec, err = m.current()
	}
	if err != nil || rec.CommandID != id {
		active, unitErr := m.active(a.commandContext())
		if unitErr != nil || !active {
			return false
		}
	}
	if a.out.Has(id) {
		_ = a.inflight.Remove(id)
		return true
	}
	_, _, ok := a.locks.TryLockAll([]string{models.TargetKeyHost}, id, models.ActionOSUpdate)
	if !ok {
		return true
	}
	a.cmdWG.Add(1)
	go func() {
		defer a.cmdWG.Done()
		cr := a.newCmdRun(models.CommandPayload{CommandID: id, Action: models.ActionOSUpdate})
		defer cr.cancel()
		cr.start = marker.StartedAt
		res := a.observeMaintenanceWith(a.commandContext(), id, true, cr)
		if marker.RebootIfRequired && !res.Detached {
			a.noteRecoveredAuthorization(a.commandContext(), &res)
		}
		a.completeCommand(cr, res)
		if !res.Detached {
			a.locks.Unlock([]string{models.TargetKeyHost}, id)
			a.triggerTelemetry()
		}
	}()
	return true
}

var errUpdateScheduled = errors.New("signed agent update accepted; privileged installation pending")

func runningAgentDigest() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "linux" {
		path = "/proc/self/exe"
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return "", fmt.Errorf("cannot bound running executable")
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(file, (128<<20)+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *Agent) recoverSignedAgentUpdate(id string, marker inflightMarker) bool {
	if marker.UpdateSHA256 == "" || marker.UpdateRequestedAt.IsZero() {
		return false
	}
	h := updateHandoff{CommandID: id, SHA256: marker.UpdateSHA256, RequestedAt: marker.UpdateRequestedAt}
	if _, _, ok := a.locks.TryLockAll([]string{models.TargetKeyHost}, id, models.ActionAgentUpdate); !ok {
		return true
	}
	status, err := a.signedUpdateStatus()
	if err != nil || !matchingSignedStatus(h, status) || status.State == "accepted" {
		a.watchSignedHandoff(h)
		return true
	}
	r := commandExecResult{Status: models.ResultStatusError, Step: models.StepVerify, Message: "signed agent update " + status.State + ": " + status.Message}
	if status.State == "installed" {
		if digest, err := a.signedRunningDigest(); err == nil && digest == marker.UpdateSHA256 {
			r.Status = models.ResultStatusSuccess
			r.Message = "signed agent installation verified after restart (" + a.version + ")"
		} else {
			r.Message = "installed update digest does not match this running agent; completion not confirmed"
		}
	}
	if !a.out.Has(id) {
		cr := a.newCmdRun(models.CommandPayload{CommandID: id, Action: models.ActionAgentUpdate})
		defer cr.cancel()
		cr.start = marker.StartedAt
		a.completeCommand(cr, r)
	} else {
		_ = a.inflight.Remove(id)
	}
	a.locks.Unlock([]string{models.TargetKeyHost}, id)
	return true
}

func (a *Agent) reserveMaintenanceHost(ctx context.Context) {
	m := a.maintenance()
	if !m.present() {
		return
	}
	active, err := m.active(ctx)
	if err != nil || !active {
		return
	}
	id := "independent-maintenance"
	record, recordErr := a.maintenance().current()
	if recordErr == nil && !record.Terminal() {
		id = record.CommandID
	} else if a.inflight != nil {
		entries := a.inflight.All()
		for i := len(entries) - 1; i >= 0; i-- {
			entry := entries[i]
			if entry.Marker.Action == models.ActionOSUpdate {
				id = entry.ID
				break
			}
		}
	}
	if _, holder, ok := a.locks.TryLockAll([]string{models.TargetKeyHost}, id, models.ActionOSUpdate); !ok {
		slog.Warn("maintenance host reservation conflicts", "holder", holder.CmdID)
		return
	}
	// Even with a lost marker the independent unit must reserve the host only
	// for its lifetime. This observer never invents a command or a success.
	a.cmdWG.Add(1)
	go func() {
		defer a.cmdWG.Done()
		for {
			active, err := m.active(a.commandContext())
			if err == nil && !active {
				a.locks.Unlock([]string{models.TargetKeyHost}, id)
				return
			}
			if m.wait(a.commandContext()) != nil {
				return
			}
		}
	}()
}

func (a *Agent) holdMaintenanceBusy(ctx context.Context) bool {
	if a.locks != nil {
		if holder, held := a.locks.Holder(models.TargetKeyHost); held && holder.Action == models.ActionOSUpdate {
			return true
		}
	}
	return a.maintenanceBusy(ctx)
}

func (a *Agent) canRequestMaintenanceRecovery(ctx context.Context, action string) bool {
	if action != models.ActionOSUpdate {
		return false
	}
	active, err := a.maintenance().active(ctx)
	// The root worker performs only its fixed prior-hold cleanup before
	// refusing this new request. A live or uninspectable worker is never raced.
	return err == nil && !active
}
