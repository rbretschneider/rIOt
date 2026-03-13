package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/notify"
	"github.com/DesyncTheThird/rIOt/internal/server/probes"
	"github.com/DesyncTheThird/rIOt/internal/server/updates"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// HandlerDeps bundles all dependencies for the Handlers constructor.
type HandlerDeps struct {
	Devices           db.DeviceRepository
	Telemetry         db.TelemetryRepository
	Events            db.EventRepository
	Hub               *websocket.Hub
	EventGen          *events.Generator
	UpdateChecker     *updates.Checker
	AdminRepo         db.AdminRepository
	TerminalRepo      db.TerminalRepository
	AlertRuleRepo     db.AlertRuleRepository
	NotifyRepo        db.NotifyRepository
	Dispatcher        *notify.Dispatcher
	CommandRepo       db.CommandRepository
	ProbeRepo         db.ProbeRepository
	ProbeRunner       *probes.Runner
	LogRepo           db.LogRepository
	DeviceLogRepo     db.DeviceLogRepository
	AutoUpdateRepo       db.AutoUpdateRepository
	ContainerMetricRepo db.ContainerMetricRepository
	JWTSecret            []byte
	AdminPasswordHash string
}

type Handlers struct {
	devices            db.DeviceRepository
	telemetry          db.TelemetryRepository
	events             db.EventRepository
	hub                *websocket.Hub
	eventGen           *events.Generator
	updateChecker      *updates.Checker
	adminRepo          db.AdminRepository
	terminalRepo       db.TerminalRepository
	alertRuleRepo      db.AlertRuleRepository
	notifyRepo         db.NotifyRepository
	dispatcher         *notify.Dispatcher
	commandRepo        db.CommandRepository
	probeRepo          db.ProbeRepository
	probeRunner        *probes.Runner
	logRepo            db.LogRepository
	deviceLogRepo      db.DeviceLogRepository
	autoUpdateRepo      db.AutoUpdateRepository
	containerMetricRepo db.ContainerMetricRepository
	jwtSecret           []byte
	adminPasswordHash  string

	// serverHostID tracks which device is hosting the rIOt server.
	// Detected via Docker container image or loopback connection.
	serverHostID atomic.Value // stores string
}

func New(deps HandlerDeps) *Handlers {
	return &Handlers{
		devices:           deps.Devices,
		telemetry:         deps.Telemetry,
		events:            deps.Events,
		hub:               deps.Hub,
		eventGen:          deps.EventGen,
		updateChecker:     deps.UpdateChecker,
		adminRepo:         deps.AdminRepo,
		terminalRepo:      deps.TerminalRepo,
		alertRuleRepo:     deps.AlertRuleRepo,
		notifyRepo:        deps.NotifyRepo,
		dispatcher:        deps.Dispatcher,
		commandRepo:       deps.CommandRepo,
		probeRepo:         deps.ProbeRepo,
		probeRunner:       deps.ProbeRunner,
		logRepo:           deps.LogRepo,
		deviceLogRepo:     deps.DeviceLogRepo,
		autoUpdateRepo:      deps.AutoUpdateRepo,
		containerMetricRepo: deps.ContainerMetricRepo,
		jwtSecret:           deps.JWTSecret,
		adminPasswordHash: deps.AdminPasswordHash,
	}
}

func (h *Handlers) Health(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		dbOK := database.Healthy(r.Context())
		if !dbOK {
			status = "degraded"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":   status,
			"database": dbOK,
			"time":     time.Now().UTC(),
		})
	}
}

func (h *Handlers) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	// Check registration key: if server has one configured, require it
	regKey, _ := h.adminRepo.GetConfig(r.Context(), "registration_key")
	if regKey != "" {
		apiKey := r.Header.Get("X-rIOt-Key")
		if apiKey != regKey {
			http.Error(w, `{"error":"invalid registration key"}`, http.StatusUnauthorized)
			return
		}
	}

	var reg models.DeviceRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	var device *models.Device

	// Check for re-registration
	if reg.DeviceID != "" {
		existing, err := h.devices.FindByDeviceUUID(r.Context(), reg.DeviceID)
		if err == nil && existing != nil {
			// Update existing device
			existing.Hostname = reg.Hostname
			existing.Arch = reg.Arch
			existing.AgentVersion = reg.AgentVersion
			existing.HardwareProfile = reg.HardwareProfile
			existing.Status = models.DeviceStatusOnline
			if reg.Tags != nil {
				existing.Tags = reg.Tags
			}
			if err := h.devices.Update(r.Context(), existing); err != nil {
				slog.Error("update device", "error", err)
				http.Error(w, `{"error":"failed to update device"}`, http.StatusInternalServerError)
				return
			}
			device = existing

			// If the agent doesn't have an API key (e.g. device was created by
			// mTLS enrollment which doesn't generate one), issue a new key now.
			apiKey := ""
			agentKey := r.Header.Get("X-rIOt-Key")
			if agentKey == "" {
				apiKey = generateAPIKey()
				if err := h.devices.StoreAPIKey(r.Context(), apiKey, device.ID); err != nil {
					slog.Error("store API key for enrolled device", "error", err)
					http.Error(w, `{"error":"failed to store API key"}`, http.StatusInternalServerError)
					return
				}
				slog.Info("generated API key for enrolled device", "device_id", device.ID)
			}

			writeJSON(w, http.StatusOK, models.DeviceRegistrationResponse{
				DeviceID: device.ID,
				ShortID:  device.ShortID,
				APIKey:   apiKey,
			})
			h.hub.BroadcastDeviceUpdate(device)
			h.eventGen.DeviceOnline(r.Context(), device.ID, device.Hostname)
			return
		}
	}

	// New registration
	deviceID := uuid.New().String()
	shortID := deviceID[:8]
	deviceKey := generateAPIKey()

	device = &models.Device{
		ID:              deviceID,
		ShortID:         shortID,
		Hostname:        reg.Hostname,
		Arch:            reg.Arch,
		AgentVersion:    reg.AgentVersion,
		Status:          models.DeviceStatusOnline,
		Tags:            reg.Tags,
		HardwareProfile: reg.HardwareProfile,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if device.Tags == nil {
		device.Tags = []string{}
	}

	if err := h.devices.Create(r.Context(), device); err != nil {
		slog.Error("create device", "error", err)
		http.Error(w, `{"error":"failed to create device"}`, http.StatusInternalServerError)
		return
	}
	if err := h.devices.StoreAPIKey(r.Context(), deviceKey, deviceID); err != nil {
		slog.Error("store api key", "error", err)
		http.Error(w, `{"error":"failed to store api key"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("device registered", "id", deviceID, "hostname", reg.Hostname)
	h.hub.BroadcastDeviceUpdate(device)
	h.eventGen.DeviceOnline(r.Context(), deviceID, reg.Hostname)

	writeJSON(w, http.StatusCreated, models.DeviceRegistrationResponse{
		DeviceID: deviceID,
		ShortID:  shortID,
		APIKey:   deviceKey,
	})
}

func (h *Handlers) Heartbeat(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var hb models.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	hb.DeviceID = deviceID
	if hb.Timestamp.IsZero() {
		hb.Timestamp = time.Now().UTC()
	}

	if err := h.telemetry.StoreHeartbeat(r.Context(), &hb); err != nil {
		slog.Error("store heartbeat", "error", err)
		http.Error(w, `{"error":"failed to store heartbeat"}`, http.StatusInternalServerError)
		return
	}
	h.devices.UpdateHeartbeatTime(r.Context(), deviceID, hb.Data.AgentVersion)

	// Loopback fallback: if agent connects from localhost, it's likely the server host
	if h.serverHostID.Load() == nil {
		if isLoopback(r.RemoteAddr) {
			h.serverHostID.Store(deviceID)
		}
	}

	// Check thresholds and generate events
	h.eventGen.CheckHeartbeatThresholds(r.Context(), deviceID, &hb.Data)

	// Broadcast heartbeat via WebSocket
	h.hub.BroadcastHeartbeat(deviceID, &hb.Data)

	// Include pending commands in the heartbeat response for agents without WS
	resp := map[string]interface{}{"status": "ok"}
	if h.commandRepo != nil {
		pending, err := h.commandRepo.ListPending(r.Context(), deviceID)
		if err == nil && len(pending) > 0 {
			var payloads []models.CommandPayload
			for _, cmd := range pending {
				payloads = append(payloads, models.CommandPayload{
					CommandID: cmd.ID,
					Action:    cmd.Action,
					Params:    cmd.Params,
				})
				h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "delivered via heartbeat")
			}
			resp["pending_commands"] = payloads
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) Telemetry(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var snap models.TelemetrySnapshot
	if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	snap.DeviceID = deviceID
	if snap.Timestamp.IsZero() {
		snap.Timestamp = time.Now().UTC()
	}

	if err := h.telemetry.StoreSnapshot(r.Context(), &snap); err != nil {
		slog.Error("store telemetry", "error", err)
		http.Error(w, `{"error":"failed to store telemetry"}`, http.StatusInternalServerError)
		return
	}
	h.devices.UpdateTelemetryTime(r.Context(), deviceID)

	// Extract and store primary IP from network telemetry
	if ip := extractPrimaryIP(&snap.Data); ip != "" {
		h.devices.UpdatePrimaryIP(r.Context(), deviceID, ip)
	}

	// Track whether Docker is available on this device
	dockerAvail := snap.Data.Docker != nil && snap.Data.Docker.Available
	h.devices.UpdateDockerAvailable(r.Context(), deviceID, dockerAvail)

	// Detect if this device hosts the rIOt server (check docker containers)
	if hasRiotServerContainer(&snap.Data) {
		h.serverHostID.Store(deviceID)
	}

	// Extract and store device logs
	if len(snap.Data.Logs) > 0 && h.deviceLogRepo != nil {
		if err := h.deviceLogRepo.InsertBatch(r.Context(), deviceID, snap.Data.Logs); err != nil {
			slog.Error("store device logs", "error", err)
		}
		snap.Data.Logs = nil // Don't persist logs in the telemetry snapshot
	}

	// Extract and store per-container metrics
	if snap.Data.Docker != nil && snap.Data.Docker.Available && h.containerMetricRepo != nil {
		var metrics []models.ContainerMetric
		for _, c := range snap.Data.Docker.Containers {
			if c.State != "running" {
				continue
			}
			metrics = append(metrics, models.ContainerMetric{
				ContainerName: c.Name,
				ContainerID:   c.ID,
				Timestamp:     snap.Timestamp,
				CPUPercent:    c.CPUPercent,
				MemUsage:      c.MemUsage,
				MemLimit:      c.MemLimit,
				CPULimit:      c.CPULimit,
			})
		}
		if len(metrics) > 0 {
			if err := h.containerMetricRepo.StoreBatch(r.Context(), deviceID, metrics); err != nil {
				slog.Error("store container metrics", "error", err)
			}
		}
	}

	// Check thresholds
	h.eventGen.CheckTelemetryThresholds(r.Context(), deviceID, &snap.Data)

	// Set device status to "warning" if UPS is on battery power
	if snap.Data.UPS != nil && snap.Data.UPS.OnBattery {
		h.devices.SetStatus(r.Context(), deviceID, models.DeviceStatusWarning)
	}

	// Check auto-update policies (Docker containers)
	h.checkAutoUpdates(r.Context(), deviceID, &snap.Data)

	// Check auto-patch (OS updates)
	h.checkAutoPatch(r.Context(), deviceID, &snap.Data)

	// Broadcast via WebSocket
	h.hub.BroadcastTelemetry(deviceID, &snap.Data)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) ListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.devices.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list devices"}`, http.StatusInternalServerError)
		return
	}
	if devices == nil {
		devices = []models.Device{}
	}
	type deviceWithConn struct {
		models.Device
		AgentConnected bool `json:"agent_connected"`
	}
	result := make([]deviceWithConn, len(devices))
	for i, d := range devices {
		result[i] = deviceWithConn{Device: d, AgentConnected: IsAgentConnected(d.ID)}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) GetDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	device, err := h.devices.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"device not found"}`, http.StatusNotFound)
		return
	}

	// Include latest telemetry
	latest, _ := h.telemetry.GetLatestSnapshot(r.Context(), id)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"device":           device,
		"latest_telemetry": latest,
		"agent_connected":  IsAgentConnected(id),
	})
}

func (h *Handlers) GetDeviceHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit == 0 {
		limit = 50
	}

	snapshots, err := h.telemetry.GetHistory(r.Context(), id, limit, offset)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch history"}`, http.StatusInternalServerError)
		return
	}
	if snapshots == nil {
		snapshots = []models.TelemetrySnapshot{}
	}
	writeJSON(w, http.StatusOK, snapshots)
}

func (h *Handlers) GetHeartbeatHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	if hours > 168 {
		hours = 168
	}
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	heartbeats, err := h.telemetry.GetHeartbeatHistory(r.Context(), id, since)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch heartbeat history"}`, http.StatusInternalServerError)
		return
	}
	if heartbeats == nil {
		heartbeats = []models.Heartbeat{}
	}
	writeJSON(w, http.StatusOK, heartbeats)
}

func (h *Handlers) GetDeviceLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	priority, _ := strconv.Atoi(r.URL.Query().Get("priority"))
	if priority <= 0 {
		priority = 7
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	exact := r.URL.Query().Get("exact") == "1"
	entries, err := h.deviceLogRepo.List(r.Context(), id, priority, limit, exact)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch logs"}`, http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []models.LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// ReceiveDeviceLogs accepts log entries pushed by the agent (e.g. via fetch_logs command).
func (h *Handlers) ReceiveDeviceLogs(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var entries []models.LogEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if len(entries) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if h.deviceLogRepo == nil {
		http.Error(w, `{"error":"device log storage not available"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.deviceLogRepo.InsertBatch(r.Context(), deviceID, entries); err != nil {
		slog.Error("receive device logs", "device", deviceID, "error", err)
		http.Error(w, `{"error":"failed to store logs"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("received device logs", "device", deviceID, "count", len(entries))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "count": strconv.Itoa(len(entries))})
}

func (h *Handlers) GetDeviceAlertRules(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	device, err := h.devices.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"device not found"}`, http.StatusNotFound)
		return
	}
	rules, err := h.alertRuleRepo.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to fetch alert rules"}`, http.StatusInternalServerError)
		return
	}
	var matching []models.AlertRule
	for _, rule := range rules {
		if events.MatchesDeviceFilter(rule.DeviceFilter, id, device.Tags) {
			matching = append(matching, rule)
		}
	}
	if matching == nil {
		matching = []models.AlertRule{}
	}
	writeJSON(w, http.StatusOK, matching)
}

func (h *Handlers) UpdateDeviceLocation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Location string `json:"location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.devices.UpdateLocation(r.Context(), id, body.Location); err != nil {
		http.Error(w, `{"error":"failed to update location"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"location": body.Location})
}

func (h *Handlers) UpdateDeviceTags(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.Tags == nil {
		body.Tags = []string{}
	}
	if err := h.devices.UpdateTags(r.Context(), id, body.Tags); err != nil {
		http.Error(w, `{"error":"failed to update tags"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tags": body.Tags})
}

func (h *Handlers) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uninstall := r.URL.Query().Get("uninstall") == "true"

	// If uninstall requested, try to send uninstall command to connected agent
	if uninstall {
		agentConnections.RLock()
		ac := agentConnections.m[id]
		agentConnections.RUnlock()
		if ac != nil {
			payload := models.CommandPayload{
				CommandID: uuid.New().String(),
				Action:    "agent_uninstall",
			}
			payloadJSON, _ := json.Marshal(payload)
			ac.Send(agentWSMessage{
				Type: "command",
				Data: payloadJSON,
			})
		}
	}

	if err := h.devices.Delete(r.Context(), id); err != nil {
		slog.Error("failed to delete device", "id", id, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"failed to delete device: %s"}`, err), http.StatusInternalServerError)
		return
	}
	h.hub.BroadcastDeviceRemoved(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) Summary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.devices.Summary(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get summary"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit == 0 {
		limit = 100
	}

	evts, err := h.events.ListAll(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, `{"error":"failed to list events"}`, http.StatusInternalServerError)
		return
	}
	if evts == nil {
		evts = []models.Event{}
	}
	writeJSON(w, http.StatusOK, evts)
}

func (h *Handlers) WebSocket(w http.ResponseWriter, r *http.Request) {
	h.hub.HandleWS(w, r)
}

// GetDeviceContainers returns Docker containers from the latest telemetry.
func (h *Handlers) GetDeviceContainers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	latest, err := h.telemetry.GetLatestSnapshot(r.Context(), id)
	if err != nil || latest == nil {
		writeJSON(w, http.StatusOK, []models.ContainerInfo{})
		return
	}
	if latest.Data.Docker == nil || !latest.Data.Docker.Available {
		writeJSON(w, http.StatusOK, []models.ContainerInfo{})
		return
	}
	containers := latest.Data.Docker.Containers
	if containers == nil {
		containers = []models.ContainerInfo{}
	}
	writeJSON(w, http.StatusOK, containers)
}

// GetContainerDetail returns a single container from the latest telemetry.
func (h *Handlers) GetContainerDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cid := chi.URLParam(r, "cid")
	latest, err := h.telemetry.GetLatestSnapshot(r.Context(), id)
	if err != nil || latest == nil || latest.Data.Docker == nil {
		http.Error(w, `{"error":"container not found"}`, http.StatusNotFound)
		return
	}
	for _, c := range latest.Data.Docker.Containers {
		if c.ID == cid || c.ShortID == cid {
			writeJSON(w, http.StatusOK, c)
			return
		}
	}
	http.Error(w, `{"error":"container not found"}`, http.StatusNotFound)
}

// GetContainerMetricHistory returns historical CPU/memory metrics for a container.
func (h *Handlers) GetContainerMetricHistory(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	containerName := chi.URLParam(r, "cname")
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	if hours > 168 {
		hours = 168
	}
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	metrics, err := h.containerMetricRepo.GetHistory(r.Context(), deviceID, containerName, since)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch container metrics"}`, http.StatusInternalServerError)
		return
	}
	if metrics == nil {
		metrics = []models.ContainerMetric{}
	}
	writeJSON(w, http.StatusOK, metrics)
}

// ReceiveAgentEvent handles self-reported events from agents (e.g. auto-update status).
func (h *Handlers) ReceiveAgentEvent(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var agentEvt models.AgentEvent
	if err := json.NewDecoder(r.Body).Decode(&agentEvt); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate event type
	switch agentEvt.Type {
	case models.EventAgentUpdateAvail, models.EventAgentUpdateStarted,
		models.EventAgentUpdateCompleted, models.EventAgentUpdateFailed:
		// allowed
	default:
		http.Error(w, `{"error":"unsupported event type"}`, http.StatusBadRequest)
		return
	}

	e := &models.Event{
		DeviceID:  deviceID,
		Type:      agentEvt.Type,
		Severity:  agentEvt.Severity,
		Message:   agentEvt.Message,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.events.Create(r.Context(), e); err != nil {
		slog.Error("create agent event", "error", err)
		http.Error(w, `{"error":"failed to create event"}`, http.StatusInternalServerError)
		return
	}
	h.hub.BroadcastEvent(e)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReceiveDockerEvent handles Docker events pushed from agents.
func (h *Handlers) ReceiveDockerEvent(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var dockerEvt models.DockerEvent
	if err := json.NewDecoder(r.Body).Decode(&dockerEvt); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Generate appropriate event
	h.eventGen.CheckDockerEvent(r.Context(), deviceID, &dockerEvt)

	// Broadcast docker update via WebSocket so dashboards refresh
	h.hub.BroadcastDockerUpdate(deviceID, &dockerEvt)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// AgentUpdateCheck returns update info for an agent.
// Query params: version, os, arch, arm (optional).
func (h *Handlers) AgentUpdateCheck(w http.ResponseWriter, r *http.Request) {
	agentVer := r.URL.Query().Get("version")
	goos := r.URL.Query().Get("os")
	goarch := r.URL.Query().Get("arch")
	goarm := r.URL.Query().Get("arm")

	if agentVer == "" {
		agentVer = "unknown"
	}
	if goos == "" {
		goos = "linux"
	}
	if goarch == "" {
		goarch = "amd64"
	}

	info := h.updateChecker.AgentUpdateInfo(agentVer, goos, goarch, goarm)
	writeJSON(w, http.StatusOK, info)
}

// ServerUpdateCheck returns update info for the server (dashboard use).
func (h *Handlers) ServerUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info := h.updateChecker.ServerUpdateInfo()
	// Wrap with server host device ID
	resp := struct {
		*updates.UpdateInfo
		ServerHostDeviceID string `json:"server_host_device_id,omitempty"`
	}{UpdateInfo: info}
	if v := h.serverHostID.Load(); v != nil {
		resp.ServerHostDeviceID = v.(string)
	}
	writeJSON(w, http.StatusOK, resp)
}

// extractPrimaryIP finds the first non-loopback IPv4 address from telemetry.
func extractPrimaryIP(data *models.FullTelemetryData) string {
	if data.Network == nil {
		return ""
	}
	for _, iface := range data.Network.Interfaces {
		if iface.State != "UP" || iface.Name == "lo" {
			continue
		}
		for _, ip := range iface.IPv4 {
			if ip == "" {
				continue
			}
			// Strip CIDR suffix for comparison and storage
			bare := ip
			if idx := strings.Index(ip, "/"); idx != -1 {
				bare = ip[:idx]
			}
			if bare == "127.0.0.1" {
				continue
			}
			return bare
		}
	}
	return ""
}

// hasRiotServerContainer checks if telemetry includes a Docker container
// running the rIOt server image.
func hasRiotServerContainer(data *models.FullTelemetryData) bool {
	if data.Docker == nil {
		return false
	}
	for _, c := range data.Docker.Containers {
		img := strings.ToLower(c.Image)
		if strings.Contains(img, "riot-server") {
			return true
		}
	}
	return false
}

// isLoopback checks if a remote address is a loopback address.
func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RotateKey handles POST /api/v1/devices/{id}/rotate-key.
func (h *Handlers) RotateKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	device, err := h.devices.GetByID(r.Context(), id)
	if err != nil || device == nil {
		http.Error(w, `{"error":"device not found"}`, http.StatusNotFound)
		return
	}

	// Delete old keys
	if err := h.devices.DeleteAPIKeysByDevice(r.Context(), id); err != nil {
		slog.Error("rotate key: delete old keys", "error", err)
		http.Error(w, `{"error":"failed to rotate key"}`, http.StatusInternalServerError)
		return
	}

	// Generate and store new key
	newKey := generateAPIKey()
	if err := h.devices.StoreAPIKey(r.Context(), newKey, id); err != nil {
		slog.Error("rotate key: store new key", "error", err)
		http.Error(w, `{"error":"failed to store new key"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("api key rotated", "device", id)
	writeJSON(w, http.StatusOK, map[string]string{
		"api_key":   newKey,
		"device_id": id,
	})
}

func generateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "riot_" + hex.EncodeToString(b)
}

// ServerCert returns the server's public TLS certificate and fingerprint.
// Only has content when TLS mode is self-signed.
func (h *Handlers) ServerCert(w http.ResponseWriter, r *http.Request) {
	tlsMode, _ := h.adminRepo.GetConfig(r.Context(), "tls_mode")
	if tlsMode != "self-signed" {
		writeJSON(w, http.StatusOK, map[string]string{
			"cert_pem":    "",
			"fingerprint": "",
		})
		return
	}

	certPEM, _, err := h.adminRepo.GetServerTLSCert(r.Context())
	if err != nil || certPEM == "" {
		writeJSON(w, http.StatusOK, map[string]string{
			"cert_pem":    "",
			"fingerprint": "",
		})
		return
	}

	// Compute SHA256 fingerprint of the DER certificate
	block, _ := pem.Decode([]byte(certPEM))
	fingerprint := ""
	if block != nil {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err == nil {
			hash := sha256.Sum256(cert.Raw)
			fingerprint = fmt.Sprintf("SHA256:%s", hex.EncodeToString(hash[:]))
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"cert_pem":    certPEM,
		"fingerprint": fingerprint,
	})
}

// GetRegistrationKey returns the current registration key setting.
func (h *Handlers) GetRegistrationKey(w http.ResponseWriter, r *http.Request) {
	key, _ := h.adminRepo.GetConfig(r.Context(), "registration_key")
	writeJSON(w, http.StatusOK, map[string]string{
		"registration_key": key,
	})
}

// SetRegistrationKey updates the registration key (empty = open registration).
func (h *Handlers) SetRegistrationKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RegistrationKey string `json:"registration_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if err := h.adminRepo.SetConfig(r.Context(), "registration_key", body.RegistrationKey); err != nil {
		slog.Error("set registration key", "error", err)
		http.Error(w, `{"error":"failed to save registration key"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
