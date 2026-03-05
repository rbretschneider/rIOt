package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSettingsTestHandlers(t *testing.T) (*Handlers, *testutil.MockAlertRuleRepo, *testutil.MockNotifyRepo) {
	t.Helper()
	alertRepo := testutil.NewMockAlertRuleRepo()
	notifyRepo := testutil.NewMockNotifyRepo()
	h := &Handlers{
		alertRuleRepo: alertRepo,
		notifyRepo:    notifyRepo,
	}
	return h, alertRepo, notifyRepo
}

// --- Alert Rules ---

func TestListAlertRules_Empty(t *testing.T) {
	h, _, _ := newSettingsTestHandlers(t)

	req := httptest.NewRequest("GET", "/api/v1/settings/alert-rules", nil)
	rec := httptest.NewRecorder()
	h.ListAlertRules(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var rules []models.AlertRule
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&rules))
	assert.Empty(t, rules)
}

func TestCreateAlertRule(t *testing.T) {
	h, alertRepo, _ := newSettingsTestHandlers(t)

	body, _ := json.Marshal(map[string]interface{}{
		"name":    "High Memory",
		"metric":  "mem_percent",
		"enabled": true,
	})
	req := httptest.NewRequest("POST", "/api/v1/settings/alert-rules", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateAlertRule(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, alertRepo.Rules, 1)
	assert.Equal(t, "High Memory", alertRepo.Rules[0].Name)
	assert.Equal(t, ">", alertRepo.Rules[0].Operator, "should default to >")
	assert.Equal(t, 900, alertRepo.Rules[0].CooldownSeconds, "should default cooldown")
}

func TestCreateAlertRule_MissingFields(t *testing.T) {
	h, _, _ := newSettingsTestHandlers(t)

	body, _ := json.Marshal(map[string]interface{}{"name": "test"})
	req := httptest.NewRequest("POST", "/api/v1/settings/alert-rules", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateAlertRule(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateAlertRule(t *testing.T) {
	h, alertRepo, _ := newSettingsTestHandlers(t)
	alertRepo.Rules = []models.AlertRule{{ID: 1, Name: "Old", Metric: "mem_percent"}}

	body, _ := json.Marshal(map[string]interface{}{
		"name":      "Updated",
		"metric":    "disk_percent",
		"threshold": 95,
	})

	r := chi.NewRouter()
	r.Put("/api/v1/settings/alert-rules/{id}", h.UpdateAlertRule)

	req := httptest.NewRequest("PUT", "/api/v1/settings/alert-rules/1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Updated", alertRepo.Rules[0].Name)
}

func TestDeleteAlertRule(t *testing.T) {
	h, alertRepo, _ := newSettingsTestHandlers(t)
	alertRepo.Rules = []models.AlertRule{{ID: 1, Name: "Doomed"}}

	r := chi.NewRouter()
	r.Delete("/api/v1/settings/alert-rules/{id}", h.DeleteAlertRule)

	req := httptest.NewRequest("DELETE", "/api/v1/settings/alert-rules/1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, alertRepo.Rules)
}

// --- Notification Channels ---

func TestListNotificationChannels_Empty(t *testing.T) {
	h, _, _ := newSettingsTestHandlers(t)

	req := httptest.NewRequest("GET", "/api/v1/settings/notification-channels", nil)
	rec := httptest.NewRecorder()
	h.ListNotificationChannels(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var channels []models.NotificationChannel
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&channels))
	assert.Empty(t, channels)
}

func TestCreateNotificationChannel(t *testing.T) {
	h, _, notifyRepo := newSettingsTestHandlers(t)

	body, _ := json.Marshal(map[string]interface{}{
		"name":    "My Ntfy",
		"type":    "ntfy",
		"enabled": true,
		"config":  map[string]interface{}{"topic": "alerts"},
	})
	req := httptest.NewRequest("POST", "/api/v1/settings/notification-channels", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateNotificationChannel(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, notifyRepo.Channels, 1)
	assert.Equal(t, "My Ntfy", notifyRepo.Channels[0].Name)
}

func TestCreateNotificationChannel_MissingFields(t *testing.T) {
	h, _, _ := newSettingsTestHandlers(t)

	body, _ := json.Marshal(map[string]interface{}{"name": "test"})
	req := httptest.NewRequest("POST", "/api/v1/settings/notification-channels", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateNotificationChannel(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeleteNotificationChannel(t *testing.T) {
	h, _, notifyRepo := newSettingsTestHandlers(t)
	notifyRepo.Channels = []models.NotificationChannel{{ID: 1, Name: "Doomed"}}

	r := chi.NewRouter()
	r.Delete("/api/v1/settings/notification-channels/{id}", h.DeleteNotificationChannel)

	req := httptest.NewRequest("DELETE", "/api/v1/settings/notification-channels/1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, notifyRepo.Channels)
}

func TestListNotificationLog(t *testing.T) {
	h, _, notifyRepo := newSettingsTestHandlers(t)
	notifyRepo.Logs = []models.NotificationLog{
		{ID: 1, Status: "sent"},
		{ID: 2, Status: "failed", ErrorMsg: "timeout"},
	}

	req := httptest.NewRequest("GET", "/api/v1/settings/notifications/log", nil)
	rec := httptest.NewRecorder()
	h.ListNotificationLog(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var logs []models.NotificationLog
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&logs))
	assert.Len(t, logs, 2)
}
