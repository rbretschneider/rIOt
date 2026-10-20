package agent

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

type updateCommandIDKey struct{}
type updateHandoff struct {
	SHA256      string    `json:"sha256"`
	RequestedAt time.Time `json:"requested_at"`
	CommandID   string    `json:"command_id,omitempty"`
	Reported    bool      `json:"reported,omitempty"`
}

func (a *Agent) signedHandoffFile() *atomicJSONFile {
	path := filepath.Join(filepath.Dir(OutboxPath()), "agent-update-handoff.json")
	var fs stateFS = osStateFS{}
	if a.inflight != nil {
		path = filepath.Join(filepath.Dir(a.inflight.file.path), "agent-update-handoff.json")
		fs = a.inflight.file.fs
	}
	return newAtomicJSONFile(path, fs)
}
func (a *Agent) persistSignedHandoff(h updateHandoff) error {
	raw, err := hex.DecodeString(h.SHA256)
	if err != nil || len(raw) != 32 || h.RequestedAt.IsZero() {
		return fmt.Errorf("invalid signed update handoff")
	}
	if h.CommandID != "" {
		if err := a.inflight.SetUpdateHandoff(h.CommandID, h.SHA256, h.RequestedAt); err != nil {
			return err
		}
	}
	return a.signedHandoffFile().Save(h)
}
func (a *Agent) signedUpdateStatus() (maintenance.UpdateStatus, error) {
	if a.updateStatusReader != nil {
		return a.updateStatusReader()
	}
	return maintenance.ReadUpdateStatus()
}
func (a *Agent) signedRunningDigest() (string, error) {
	if a.updateDigestReader != nil {
		return a.updateDigestReader()
	}
	return runningAgentDigest()
}
func matchingSignedStatus(h updateHandoff, s maintenance.UpdateStatus) bool {
	return h.SHA256 != "" && s.SHA256 == h.SHA256 && !s.UpdatedAt.Before(h.RequestedAt)
}

// accepted is an acknowledgement, not completion. The independent helper may
// still fail before stopping us, so keep watching without restarting ourselves.
func (a *Agent) watchSignedHandoff(h updateHandoff) {
	a.cmdWG.Add(1)
	go func() {
		defer a.cmdWG.Done()
		ctx := a.commandContext()
		for {
			s, err := a.signedUpdateStatus()
			if err == nil && matchingSignedStatus(h, s) {
				if s.State == "error" || s.State == "deferred" {
					a.finishSignedHandoff(h, false, "signed agent update "+s.State+": "+s.Message)
					return
				}
				if s.State == "installed" {
					digest, e := a.signedRunningDigest()
					verified := e == nil && digest == h.SHA256
					message := "installed digest does not match the running executable; completion remains unconfirmed"
					if verified {
						message = "signed installation completion verified against running executable"
					}
					a.finishSignedHandoff(h, verified, message)
					return
				}
			}
			// A terminated apply unit cannot produce a later acknowledgement. Unknown
			// unit state remains conservative rather than releasing an active handoff.
			{
				run := a.runner
				if run == nil {
					run = execRunner
				}
				out, e := run(ctx, "/usr/bin/systemctl", "show", updateUnit, "--property=ActiveState", "--value")
				state := strings.TrimSpace(string(out))
				if e == nil && (state == "inactive" || state == "failed") {
					a.finishSignedHandoff(h, false, "signed update helper stopped without verified completion")
					return
				}
			}
			if a.maintenance().wait(ctx) != nil {
				return
			}
		}
	}()
}
func (a *Agent) finishSignedHandoff(h updateHandoff, success bool, message string) {
	h.Reported = true
	if err := a.signedHandoffFile().Save(h); err != nil {
		return
	}
	id := h.CommandID
	if id != "" {
		marker, ok := a.inflight.Get(id)
		if !ok {
			return
		}
		if !a.out.Has(id) {
			cr := a.newCmdRun(models.CommandPayload{CommandID: id, Action: models.ActionAgentUpdate})
			defer cr.cancel()
			cr.start = marker.StartedAt
			r := commandExecResult{Status: models.ResultStatusError, Step: models.StepVerify, Message: message}
			if success {
				r.Status = models.ResultStatusSuccess
			}
			a.completeCommand(cr, r)
		} else {
			_ = a.inflight.Remove(id)
		}
	} else {
		id = "automatic-agent-update"
		event, severity := models.EventAgentUpdateFailed, models.SeverityWarning
		if success {
			event, severity = models.EventAgentUpdateCompleted, models.SeverityInfo
		}
		a.reportUpdateEvent(a.commandContext(), event, severity, message)
	}
	if a.locks != nil {
		a.locks.Unlock([]string{models.TargetKeyHost}, id)
	}
}
func (a *Agent) recoverAutomaticSignedHandoff() {
	var h updateHandoff
	found, err := a.signedHandoffFile().Load(&h)
	if err != nil || !found || h.Reported || h.CommandID != "" {
		return
	}
	if a.locks != nil {
		if _, _, ok := a.locks.TryLockAll([]string{models.TargetKeyHost}, "automatic-agent-update", models.ActionAgentUpdate); !ok {
			return
		}
	}
	a.watchSignedHandoff(h)
}
