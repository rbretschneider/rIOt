package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/go-chi/chi/v5"
)

// --- Alert Rules ---

// ListAlertRules handles GET /api/v1/settings/alert-rules.
func (h *Handlers) ListAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.alertRuleRepo.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list alert rules"}`, http.StatusInternalServerError)
		return
	}
	if rules == nil {
		rules = []models.AlertRule{}
	}
	writeJSON(w, http.StatusOK, rules)
}

// CreateAlertRule handles POST /api/v1/settings/alert-rules.
func (h *Handlers) CreateAlertRule(w http.ResponseWriter, r *http.Request) {
	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if rule.Name == "" || rule.Metric == "" {
		http.Error(w, `{"error":"name and metric are required"}`, http.StatusBadRequest)
		return
	}
	if rule.Operator == "" {
		rule.Operator = ">"
	}
	if rule.Severity == "" {
		rule.Severity = "warning"
	}
	if rule.CooldownSeconds == 0 {
		rule.CooldownSeconds = 900
	}
	if err := h.alertRuleRepo.Create(r.Context(), &rule); err != nil {
		http.Error(w, `{"error":"failed to create alert rule"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

// UpdateAlertRule handles PUT /api/v1/settings/alert-rules/{id}.
func (h *Handlers) UpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	rule.ID = id
	if err := h.alertRuleRepo.Update(r.Context(), &rule); err != nil {
		http.Error(w, `{"error":"failed to update alert rule"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// DeleteAlertRule handles DELETE /api/v1/settings/alert-rules/{id}.
func (h *Handlers) DeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	if err := h.alertRuleRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"failed to delete alert rule"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Notification Channels ---

// ListNotificationChannels handles GET /api/v1/settings/notification-channels.
func (h *Handlers) ListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.notifyRepo.ListChannels(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list channels"}`, http.StatusInternalServerError)
		return
	}
	if channels == nil {
		channels = []models.NotificationChannel{}
	}
	writeJSON(w, http.StatusOK, channels)
}

// CreateNotificationChannel handles POST /api/v1/settings/notification-channels.
func (h *Handlers) CreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var ch models.NotificationChannel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if ch.Name == "" || ch.Type == "" {
		http.Error(w, `{"error":"name and type are required"}`, http.StatusBadRequest)
		return
	}
	if ch.Config == nil {
		ch.Config = make(map[string]interface{})
	}
	if err := h.notifyRepo.CreateChannel(r.Context(), &ch); err != nil {
		http.Error(w, `{"error":"failed to create channel"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

// UpdateNotificationChannel handles PUT /api/v1/settings/notification-channels/{id}.
func (h *Handlers) UpdateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	var ch models.NotificationChannel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	ch.ID = id
	if ch.Config == nil {
		ch.Config = make(map[string]interface{})
	}
	if err := h.notifyRepo.UpdateChannel(r.Context(), &ch); err != nil {
		http.Error(w, `{"error":"failed to update channel"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

// DeleteNotificationChannel handles DELETE /api/v1/settings/notification-channels/{id}.
func (h *Handlers) DeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	if err := h.notifyRepo.DeleteChannel(r.Context(), id); err != nil {
		http.Error(w, `{"error":"failed to delete channel"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TestNotificationChannel handles POST /api/v1/settings/notification-channels/{id}/test.
func (h *Handlers) TestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	ch, err := h.notifyRepo.GetChannel(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"channel not found"}`, http.StatusNotFound)
		return
	}
	if err := h.dispatcher.TestChannel(r.Context(), *ch); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ListNotificationLog handles GET /api/v1/settings/notifications/log.
func (h *Handlers) ListNotificationLog(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit == 0 {
		limit = 50
	}
	logs, err := h.notifyRepo.ListNotificationLog(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, `{"error":"failed to list notification log"}`, http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []models.NotificationLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// --- Server Logs ---

// GetServerLogs handles GET /api/v1/settings/logs.
func (h *Handlers) GetServerLogs(w http.ResponseWriter, r *http.Request) {
	if h.logRepo == nil {
		writeJSON(w, http.StatusOK, []models.ServerLog{})
		return
	}

	level := r.URL.Query().Get("level")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 100
	}

	var before *time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		if t, err := time.Parse(time.RFC3339Nano, b); err == nil {
			before = &t
		}
	}

	logs, err := h.logRepo.List(r.Context(), level, limit, before)
	if err != nil {
		http.Error(w, `{"error":"failed to list server logs"}`, http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []models.ServerLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// --- Alert Templates ---

// ListAlertTemplates handles GET /api/v1/settings/alert-templates.
func (h *Handlers) ListAlertTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, events.AlertTemplates())
}

// --- Feature Toggles ---

// GetFeatureToggles handles GET /api/v1/settings/features.
func (h *Handlers) GetFeatureToggles(w http.ResponseWriter, r *http.Request) {
	val, err := h.adminRepo.GetConfig(r.Context(), "feature_toggles")
	if err != nil || val == "" {
		// Return default (all enabled)
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	var toggles map[string]interface{}
	if err := json.Unmarshal([]byte(val), &toggles); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, toggles)
}

// SetFeatureToggles handles PUT /api/v1/settings/features.
func (h *Handlers) SetFeatureToggles(w http.ResponseWriter, r *http.Request) {
	var toggles map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&toggles); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	data, err := json.Marshal(toggles)
	if err != nil {
		http.Error(w, `{"error":"failed to encode toggles"}`, http.StatusInternalServerError)
		return
	}
	if err := h.adminRepo.SetConfig(r.Context(), "feature_toggles", string(data)); err != nil {
		http.Error(w, `{"error":"failed to save feature toggles"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, toggles)
}

// --- Event Acknowledgement ---

// UnreadEventCount handles GET /api/v1/events/unread-count.
func (h *Handlers) UnreadEventCount(w http.ResponseWriter, r *http.Request) {
	count, err := h.events.CountUnacknowledged(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to count unread events"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

// AcknowledgeEvent handles POST /api/v1/events/{id}/acknowledge.
func (h *Handlers) AcknowledgeEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	if err := h.events.Acknowledge(r.Context(), id); err != nil {
		http.Error(w, `{"error":"failed to acknowledge event"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

// AcknowledgeAllEvents handles POST /api/v1/events/acknowledge-all.
func (h *Handlers) AcknowledgeAllEvents(w http.ResponseWriter, r *http.Request) {
	if err := h.events.AcknowledgeAll(r.Context()); err != nil {
		http.Error(w, `{"error":"failed to acknowledge events"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}
