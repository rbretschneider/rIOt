package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
	MasterAPIKey      string
	AdminRepo         db.AdminRepository
	TerminalRepo      db.TerminalRepository
	AlertRuleRepo     db.AlertRuleRepository
	NotifyRepo        db.NotifyRepository
	Dispatcher        *notify.Dispatcher
	CommandRepo       db.CommandRepository
	ProbeRepo         db.ProbeRepository
	ProbeRunner       *probes.Runner
	JWTSecret         []byte
	AdminPasswordHash string
}

type Handlers struct {
	devices            db.DeviceRepository
	telemetry          db.TelemetryRepository
	events             db.EventRepository
	hub                *websocket.Hub
	eventGen           *events.Generator
	updateChecker      *updates.Checker
	masterAPIKey       string
	adminRepo          db.AdminRepository
	terminalRepo       db.TerminalRepository
	alertRuleRepo      db.AlertRuleRepository
	notifyRepo         db.NotifyRepository
	dispatcher         *notify.Dispatcher
	commandRepo        db.CommandRepository
	probeRepo          db.ProbeRepository
	probeRunner        *probes.Runner
	jwtSecret          []byte
	adminPasswordHash  string
}

func New(deps HandlerDeps) *Handlers {
	return &Handlers{
		devices:           deps.Devices,
		telemetry:         deps.Telemetry,
		events:            deps.Events,
		hub:               deps.Hub,
		eventGen:          deps.EventGen,
		updateChecker:     deps.UpdateChecker,
		masterAPIKey:      deps.MasterAPIKey,
		adminRepo:         deps.AdminRepo,
		terminalRepo:      deps.TerminalRepo,
		alertRuleRepo:     deps.AlertRuleRepo,
		notifyRepo:        deps.NotifyRepo,
		dispatcher:        deps.Dispatcher,
		commandRepo:       deps.CommandRepo,
		probeRepo:         deps.ProbeRepo,
		probeRunner:       deps.ProbeRunner,
		jwtSecret:         deps.JWTSecret,
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
	// Validate master key or allow open registration
	apiKey := r.Header.Get("X-rIOt-Key")
	if h.masterAPIKey != "" && apiKey != h.masterAPIKey {
		http.Error(w, `{"error":"invalid master api key"}`, http.StatusUnauthorized)
		return
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

			// Return existing API key info (device already has one)
			writeJSON(w, http.StatusOK, models.DeviceRegistrationResponse{
				DeviceID: device.ID,
				ShortID:  device.ShortID,
				APIKey:   "", // Agent should already have its key
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

	// Check thresholds
	h.eventGen.CheckTelemetryThresholds(r.Context(), deviceID, &snap.Data)

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

func (h *Handlers) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.devices.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"failed to delete device"}`, http.StatusInternalServerError)
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
	writeJSON(w, http.StatusOK, info)
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
