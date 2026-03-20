package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ListAllDeviceProbes handles GET /api/v1/device-probes.
// Returns all device probes across all devices, enriched with device hostname.
func (h *Handlers) ListAllDeviceProbes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	probes, err := h.deviceProbeRepo.ListAll(ctx)
	if err != nil {
		slog.Error("list all device probes", "error", err)
		http.Error(w, `{"error":"failed to list all device probes"}`, http.StatusInternalServerError)
		return
	}

	devices, err := h.devices.List(ctx)
	if err != nil {
		slog.Error("list devices for device probes", "error", err)
		http.Error(w, `{"error":"failed to list all device probes"}`, http.StatusInternalServerError)
		return
	}

	// Build device_id -> hostname lookup map. A missing key returns "" (Go zero value),
	// which the frontend handles gracefully by falling back to device_id for display.
	// This covers orphaned probes whose device has been deleted from the database.
	hostnames := make(map[string]string, len(devices))
	for _, d := range devices {
		hostnames[d.ID] = d.Hostname
	}

	results := make([]models.DeviceProbeWithResultEnriched, len(probes))
	for i, p := range probes {
		results[i] = models.DeviceProbeWithResultEnriched{
			DeviceProbeWithResult: models.DeviceProbeWithResult{DeviceProbe: p},
			DeviceHostname:        hostnames[p.DeviceID],
		}
		if lr, err := h.deviceProbeRepo.LatestResult(ctx, p.ID); err == nil {
			results[i].LatestResult = lr
		}
		if rate, total, err := h.deviceProbeRepo.SuccessRate(ctx, p.ID); err == nil && total > 0 {
			results[i].SuccessRate = &rate
			results[i].TotalChecks = total
		}
	}
	writeJSON(w, http.StatusOK, results)
}

// ListDeviceProbes handles GET /api/v1/devices/{id}/device-probes.
func (h *Handlers) ListDeviceProbes(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	probes, err := h.deviceProbeRepo.List(r.Context(), deviceID)
	if err != nil {
		http.Error(w, `{"error":"failed to list device probes"}`, http.StatusInternalServerError)
		return
	}
	if probes == nil {
		probes = []models.DeviceProbe{}
	}

	// Enrich each probe with latest result and success rate
	results := make([]models.DeviceProbeWithResult, len(probes))
	for i, p := range probes {
		results[i] = models.DeviceProbeWithResult{DeviceProbe: p}
		if lr, err := h.deviceProbeRepo.LatestResult(r.Context(), p.ID); err == nil {
			results[i].LatestResult = lr
		}
		if rate, total, err := h.deviceProbeRepo.SuccessRate(r.Context(), p.ID); err == nil && total > 0 {
			results[i].SuccessRate = &rate
			results[i].TotalChecks = total
		}
	}
	writeJSON(w, http.StatusOK, results)
}

// CreateDeviceProbe handles POST /api/v1/devices/{id}/device-probes.
func (h *Handlers) CreateDeviceProbe(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var probe models.DeviceProbe
	if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if probe.Name == "" || probe.Type == "" {
		http.Error(w, `{"error":"name and type are required"}`, http.StatusBadRequest)
		return
	}
	probe.DeviceID = deviceID
	if probe.Config == nil {
		probe.Config = make(map[string]interface{})
	}
	if probe.Assertions == nil {
		probe.Assertions = []models.ProbeAssertion{}
	}
	if probe.IntervalSeconds == 0 {
		probe.IntervalSeconds = 60
	}
	if probe.TimeoutSeconds == 0 {
		probe.TimeoutSeconds = 10
	}
	if err := h.deviceProbeRepo.Create(r.Context(), &probe); err != nil {
		slog.Error("create device probe", "error", err)
		http.Error(w, `{"error":"failed to create device probe"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, probe)
}

// UpdateDeviceProbe handles PUT /api/v1/devices/{id}/device-probes/{pid}.
func (h *Handlers) UpdateDeviceProbe(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	pid, err := strconv.ParseInt(chi.URLParam(r, "pid"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid probe id"}`, http.StatusBadRequest)
		return
	}
	var probe models.DeviceProbe
	if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	probe.ID = pid
	probe.DeviceID = deviceID
	if probe.Config == nil {
		probe.Config = make(map[string]interface{})
	}
	if probe.Assertions == nil {
		probe.Assertions = []models.ProbeAssertion{}
	}
	if err := h.deviceProbeRepo.Update(r.Context(), &probe); err != nil {
		slog.Error("update device probe", "error", err)
		http.Error(w, `{"error":"failed to update device probe"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, probe)
}

// DeleteDeviceProbe handles DELETE /api/v1/devices/{id}/device-probes/{pid}.
func (h *Handlers) DeleteDeviceProbe(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(chi.URLParam(r, "pid"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid probe id"}`, http.StatusBadRequest)
		return
	}
	if err := h.deviceProbeRepo.Delete(r.Context(), pid); err != nil {
		slog.Error("delete device probe", "error", err)
		http.Error(w, `{"error":"failed to delete device probe"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RunDeviceProbe handles POST /api/v1/devices/{id}/device-probes/{pid}/run.
// Sends a run_device_probe command to the agent.
func (h *Handlers) RunDeviceProbe(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	pid, err := strconv.ParseInt(chi.URLParam(r, "pid"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid probe id"}`, http.StatusBadRequest)
		return
	}
	probe, err := h.deviceProbeRepo.GetByID(r.Context(), pid)
	if err != nil {
		http.Error(w, `{"error":"device probe not found"}`, http.StatusNotFound)
		return
	}

	// Build command params
	params := map[string]interface{}{
		"probe_id":        probe.ID,
		"type":            probe.Type,
		"config":          probe.Config,
		"assertions":      probe.Assertions,
		"timeout_seconds": probe.TimeoutSeconds,
	}

	cmd := &models.Command{
		ID:       uuid.New().String(),
		DeviceID: deviceID,
		Action:   "run_device_probe",
		Params:   params,
		Status:   "pending",
	}
	if err := h.commandRepo.Create(r.Context(), cmd); err != nil {
		slog.Error("create run_device_probe command", "error", err)
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}

	// Try to send via WS if agent is connected
	agentConnections.RLock()
	ac := agentConnections.m[deviceID]
	agentConnections.RUnlock()

	if ac != nil {
		payload := models.CommandPayload{
			CommandID: cmd.ID,
			Action:    cmd.Action,
			Params:    cmd.Params,
		}
		payloadJSON, _ := json.Marshal(payload)
		if err := ac.Send(agentWSMessage{
			Type: "command",
			Data: payloadJSON,
		}); err != nil {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "ws send failed, queued for heartbeat delivery")
			cmd.Status = "queued"
		} else {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "")
			cmd.Status = "sent"
		}
	} else {
		h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "agent not connected, queued for heartbeat delivery")
		cmd.Status = "queued"
	}

	writeJSON(w, http.StatusCreated, cmd)
}

// GetDeviceProbeResults handles GET /api/v1/devices/{id}/device-probes/{pid}/results.
func (h *Handlers) GetDeviceProbeResults(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(chi.URLParam(r, "pid"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid probe id"}`, http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 100
	}
	results, err := h.deviceProbeRepo.ListResults(r.Context(), pid, limit)
	if err != nil {
		http.Error(w, `{"error":"failed to list results"}`, http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []models.DeviceProbeResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

// ReceiveDeviceProbeResults handles POST /api/v1/devices/{id}/probe-results.
// Accepts probe results pushed from agents.
func (h *Handlers) ReceiveDeviceProbeResults(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var results []models.DeviceProbeResult
	if err := json.NewDecoder(r.Body).Decode(&results); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	stored := 0
	for i := range results {
		results[i].DeviceID = deviceID
		if err := h.deviceProbeRepo.StoreResult(r.Context(), &results[i]); err != nil {
			slog.Error("store device probe result", "probe_id", results[i].ProbeID, "error", err)
			continue
		}
		stored++

		// Generate event on failure
		if !results[i].Success {
			probe, err := h.deviceProbeRepo.GetByID(r.Context(), results[i].ProbeID)
			probeName := "unknown"
			if err == nil && probe != nil {
				probeName = probe.Name
			}
			msg := "Device probe failed: " + probeName
			if results[i].ErrorMsg != "" {
				msg += " — " + results[i].ErrorMsg
			}
			evt := &models.Event{
				DeviceID:  deviceID,
				Type:      "device_probe_failed",
				Severity:  "warning",
				Message:   msg,
				CreatedAt: time.Now().UTC(),
			}
			if err := h.events.Create(r.Context(), evt); err != nil {
				slog.Error("create device probe event", "error", err)
			} else {
				h.hub.BroadcastEvent(evt)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"stored": stored,
	})
}
