package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

var validActions = map[string]bool{
	"docker_stop":          true,
	"docker_restart":       true,
	"docker_start":         true,
	"docker_update":        true,
	"docker_check_updates": true,
	"reboot":               true,
	"shutdown":             true,
	"agent_update":         true,
	"os_update":            true,
	"fetch_logs":           true,
	"enable_auto_updates":  true,
	"docker_bulk_update":   true,
	"run_device_probe":     true,
}

// SendCommand handles POST /api/v1/devices/{id}/commands.
func (h *Handlers) SendCommand(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	deviceID := chi.URLParam(r, "id")

	var req struct {
		ConfirmHost string                 `json:"confirm_host"`
		Action      string                 `json:"action"`
		Params      map[string]interface{} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if !validActions[req.Action] {
		http.Error(w, `{"error":"invalid action"}`, http.StatusBadRequest)
		return
	}
	if req.Params == nil {
		req.Params = make(map[string]interface{})
	}
	// Manual dispatches never carry include_reboot_class (PATCH-GATE FR-023,
	// BR-005): the only release path for reboot-class holds is the in-window
	// automated run dispatched by checkAutoPatch. Enforced at the API
	// boundary rather than trusting UI callers.
	if req.Action == "os_update" {
		delete(req.Params, "include_reboot_class")
	}

	if req.Action == "docker_stop" {
		if snap, ok := h.dockerSnapshot(r, deviceID); ok {
			name, _ := req.Params["container"].(string)
			id, _ := req.Params["container_id"].(string)
			self := dockerSelf(snap.Data.Docker.Containers)
			for _, c := range snap.Data.Docker.Containers {
				if (c.Name == name || c.ID == id) && self[c.Name] != "" {
					d, e := h.devices.GetByID(r.Context(), deviceID)
					if e != nil || req.ConfirmHost != d.Hostname {
						dockerError(w, 400, "confirmation_required")
						return
					}
				}
			}
		}
	}
	res, err := h.cmdSvc().Dispatch(r.Context(), dockerDispatchRequest(r, deviceID, req.Action, req.Params))
	if err != nil {
		if errors.Is(err, commands.ErrInvalidCommandParams) {
			dockerError(w, 400, "invalid_request")
			return
		}
		if errors.Is(err, commands.ErrCreationRateLimited) {
			dockerError(w, 429, "rate_limited")
			return
		}
		if errors.Is(err, commands.ErrDeviceNotFound) {
			dockerError(w, 404, "device_not_found")
			return
		}
		slog.Error("dispatch command", "device_id", deviceID, "action", req.Action, "error", err.Error())
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}
	if res.Conflict != nil {
		writeCommandConflict(w, res.Conflict, res.ConflictKey)
		return
	}
	cmd := res.Command

	// Emit informational event for all commands
	if device, err := h.devices.GetByID(r.Context(), deviceID); err == nil && h.eventGen != nil {
		h.eventGen.CommandSent(r.Context(), deviceID, device.Hostname, req.Action, req.Params)
	}

	slog.Info("command created", "id", cmd.ID, "device", deviceID, "action", req.Action, "status", cmd.Status)
	writeJSON(w, http.StatusCreated, cmd)
}

// writeCommandConflict answers 409 for a create that hit an in-flight command
// on the same target (BR-008).
func writeCommandConflict(w http.ResponseWriter, existing *models.Command, targetKey string) {
	writeJSON(w, http.StatusConflict, models.CommandConflictResponse{
		Error:             models.ConflictMessage,
		Code:              models.CodeConflict,
		ExistingCommandID: existing.ID,
		ExistingStatus:    existing.Status,
		TargetKey:         targetKey,
	})
}

// BulkDockerUpdate handles POST /api/v1/devices/{id}/docker/bulk-update.
// Targets that conflict with an in-flight command are skipped and reported; the
// rest get one docker_bulk_update. When every target conflicts the answer is 409.
func (h *Handlers) BulkDockerUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	deviceID := chi.URLParam(r, "id")

	var req struct {
		ContainerIDs []string `json:"container_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if len(req.ContainerIDs) > 100 {
		dockerError(w, 400, "too_many_targets")
		return
	}
	if len(req.ContainerIDs) == 0 {
		http.Error(w, `{"error":"container_ids is required"}`, http.StatusBadRequest)
		return
	}

	res, err := h.cmdSvc().DispatchBulkDocker(r.Context(), deviceID, req.ContainerIDs)
	if err != nil {
		slog.Error("dispatch bulk update", "device_id", deviceID, "error", err.Error())
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}
	conflicts := res.Conflicts
	if conflicts == nil {
		conflicts = []models.BulkConflict{}
	}
	if res.Command == nil && len(conflicts) == 0 {
		http.Error(w, `{"error":"container_ids is required"}`, http.StatusBadRequest)
		return
	}
	if res.Command == nil {
		first := conflicts[0]
		writeJSON(w, http.StatusConflict, models.CommandConflictResponse{
			Error:             models.ConflictMessage,
			Code:              models.CodeConflict,
			ExistingCommandID: first.ExistingCommandID,
			TargetKey:         first.TargetKey,
			Conflicts:         conflicts,
		})
		return
	}
	writeJSON(w, http.StatusCreated, models.BulkCommandResponse{Command: *res.Command, Conflicts: conflicts})
}

// ListDeviceCommands handles GET /api/v1/devices/{id}/commands.
//
// Query: limit, offset, action, status (comma-separated canonical or legacy
// values), active=true (every non-terminal command) and target_key. Request
// values reach SQL only as bind parameters, and an unknown status value or an
// over-long target_key is a 400 invalid_filter (SEC C-6).
func (h *Handlers) ListDeviceCommands(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit == 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	statuses, ok := models.ParseStatusFilter(q.Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, models.ReportResponse{Error: "invalid status filter", Code: models.CodeInvalidFilter})
		return
	}
	targetKey := q.Get("target_key")
	if len(targetKey) > models.MaxTargetKeyFilterBytes {
		writeJSON(w, http.StatusBadRequest, models.ReportResponse{Error: "invalid target_key filter", Code: models.CodeInvalidFilter})
		return
	}
	action := q.Get("action")

	var list []models.Command
	var err error
	switch {
	case q.Get("active") == "true":
		list, err = h.commandRepo.ListActive(r.Context(), deviceID, targetKey)
	case len(statuses) > 0 || action != "" || offset > 0 || targetKey != "":
		list, err = h.commandRepo.ListByDeviceFiltered(r.Context(), deviceID, limit, offset, statuses, action, targetKey)
	default:
		list, err = h.commandRepo.ListByDevice(r.Context(), deviceID, limit)
	}
	if err != nil {
		slog.Error("list commands", "device_id", deviceID, "error", err.Error())
		http.Error(w, `{"error":"failed to list commands"}`, http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []models.Command{}
	}
	writeJSON(w, http.StatusOK, list)
}

// commandNotFound writes the one 404 body used for a missing command, a foreign
// command and a malformed id alike, so they cannot be told apart.
func commandNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, models.ReportResponse{Error: "command not found", Code: models.CodeCommandNotFound})
}

// GetCommand handles GET /api/v1/devices/{id}/commands/{commandId}.
func (h *Handlers) GetCommand(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	commandID := chi.URLParam(r, "commandId")
	if !models.IsCanonicalUUID(commandID) {
		commandNotFound(w)
		return
	}
	cmd, err := h.commandRepo.GetForDevice(r.Context(), commandID, deviceID)
	if errors.Is(err, db.ErrCommandNotFound) {
		commandNotFound(w)
		return
	}
	if err != nil {
		slog.Error("get command", "device_id", deviceID, "command_id", commandID, "error", err.Error())
		http.Error(w, `{"error":"failed to get command"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, cmd)
}

// CancelCommand handles POST /api/v1/devices/{id}/commands/{commandId}/cancel.
// Only a queued command can be cancelled (FR-086); cancel and delivery claim are
// mutually exclusive, so the loser of a race gets 409 (FR-087, FR-088).
func (h *Handlers) CancelCommand(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	commandID := chi.URLParam(r, "commandId")
	cmd, outcome, err := h.cmdSvc().Cancel(r.Context(), deviceID, commandID)
	if err != nil {
		slog.Error("cancel command", "device_id", deviceID, "command_id", commandID, "error", err.Error())
		http.Error(w, `{"error":"failed to cancel command"}`, http.StatusInternalServerError)
		return
	}
	switch outcome {
	case commands.CancelApplied:
		writeJSON(w, http.StatusOK, cmd)
	case commands.CancelNotCancellable:
		writeJSON(w, http.StatusConflict, models.ReportResponse{
			Error:  "command cannot be cancelled after it has been dispatched",
			Code:   models.CodeNotCancellable,
			Status: cmd.Status,
		})
	default:
		commandNotFound(w)
	}
}

// AgentCommandReport handles POST /api/v1/devices/{id}/commands/{commandId}/report,
// the agent's HTTP path for ack, progress and result reports (device-key auth).
//
// Processing order (SEC C-1): per-device rate limit, path id, body size,
// decode, body id and kind, then the service (sanitize and guarded
// transition). The device scope of every write is the authenticated device.
func (h *Handlers) AgentCommandReport(w http.ResponseWriter, r *http.Request) {
	deviceID := authenticatedDeviceID(r)
	if deviceID == "" {
		writeJSON(w, http.StatusUnauthorized, models.ReportResponse{Error: "unauthorized"})
		return
	}
	svc := h.cmdSvc()
	if !svc.AllowReport(deviceID) {
		writeJSON(w, http.StatusTooManyRequests, models.ReportResponse{Error: "too many requests", Code: models.CodeRateLimited})
		return
	}
	commandID := chi.URLParam(r, "commandId")
	if !models.IsCanonicalUUID(commandID) {
		writeJSON(w, http.StatusBadRequest, models.ReportResponse{Error: "invalid command id", Code: models.CodeInvalidReport})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, models.MaxReportBodyBytes)
	var report models.CommandReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, models.ReportResponse{Error: "report too large", Code: models.CodeInvalidReport})
			return
		}
		writeJSON(w, http.StatusBadRequest, models.ReportResponse{Error: "invalid report body", Code: models.CodeInvalidReport})
		return
	}
	if err := commands.ValidateReport(commandID, report); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ReportResponse{Error: "invalid report", Code: models.CodeInvalidReport})
		return
	}
	report.CommandID = commandID

	outcome, err := svc.HandleAgentReport(r.Context(), deviceID, report)
	switch {
	case errors.Is(err, commands.ErrMalformedCommandID), errors.Is(err, commands.ErrInvalidReport):
		writeJSON(w, http.StatusBadRequest, models.ReportResponse{Error: "invalid report", Code: models.CodeInvalidReport})
		return
	case err != nil:
		slog.Error("agent command report", "device_id", deviceID, "command_id", commandID, "error", err.Error())
		http.Error(w, `{"error":"failed to process report"}`, http.StatusInternalServerError)
		return
	}

	switch outcome.Disposition {
	case models.ReportAccepted, models.ReportDuplicate, models.ReportCoalesced:
		writeJSON(w, http.StatusOK, models.ReportResponse{CommandID: commandID, Result: outcome.Disposition, Status: outcome.Status})
	case models.ReportAlreadyTerminal:
		writeJSON(w, http.StatusConflict, models.ReportResponse{
			Error: "command is already terminal", Code: models.CodeAlreadyTerminal, Status: outcome.Status})
	case models.ReportInvalidTransition:
		writeJSON(w, http.StatusConflict, models.ReportResponse{
			Error: "transition not allowed from the current state", Code: models.CodeInvalidTransition, Status: outcome.Status})
	default:
		commandNotFound(w)
	}
}

// authenticatedDeviceID is the device the API key authenticated as, or "" when
// the request did not pass through DeviceAuth. It never falls back to the URL
// {id}: a route mounted without device auth must fail closed, not trust the path.
func authenticatedDeviceID(r *http.Request) string {
	if id, ok := r.Context().Value(middleware.DeviceIDKey).(string); ok && id != "" {
		return id
	}
	return ""
}

// GetCommandOutput handles GET /api/v1/devices/{id}/commands/{commandId}/output.
func (h *Handlers) GetCommandOutput(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	commandID := chi.URLParam(r, "commandId")

	cmd, err := h.commandRepo.GetByID(r.Context(), commandID)
	if err != nil {
		http.Error(w, `{"error":"command not found"}`, http.StatusNotFound)
		return
	}
	if cmd.DeviceID != deviceID {
		http.Error(w, `{"error":"command does not belong to this device"}`, http.StatusForbidden)
		return
	}

	outputs, err := h.commandRepo.GetCommandOutput(r.Context(), commandID)
	if err != nil {
		http.Error(w, `{"error":"failed to get command output"}`, http.StatusInternalServerError)
		return
	}
	if outputs == nil {
		outputs = []models.CommandOutput{}
	}
	writeJSON(w, http.StatusOK, outputs)
}
