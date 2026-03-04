package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handlers struct {
	devices      *db.DeviceRepo
	telemetry    *db.TelemetryRepo
	events       *db.EventRepo
	hub          *websocket.Hub
	eventGen     *events.Generator
	masterAPIKey string
}

func New(devices *db.DeviceRepo, telemetry *db.TelemetryRepo, events *db.EventRepo, hub *websocket.Hub, eventGen *events.Generator, masterKey string) *Handlers {
	return &Handlers{
		devices:      devices,
		telemetry:    telemetry,
		events:       events,
		hub:          hub,
		eventGen:     eventGen,
		masterAPIKey: masterKey,
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
	h.devices.UpdateHeartbeatTime(r.Context(), deviceID)

	// Check thresholds and generate events
	h.eventGen.CheckHeartbeatThresholds(r.Context(), deviceID, &hb.Data)

	// Broadcast heartbeat via WebSocket
	h.hub.BroadcastHeartbeat(deviceID, &hb.Data)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	writeJSON(w, http.StatusOK, devices)
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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func generateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "riot_" + hex.EncodeToString(b)
}
