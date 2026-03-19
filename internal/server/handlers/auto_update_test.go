package handlers

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

func TestGetAutomationConfig_Default(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})

	req := httptest.NewRequest("GET", "/api/v1/settings/automation", nil)
	w := httptest.NewRecorder()
	h.GetAutomationConfig(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cfg models.AutomationConfig
	json.NewDecoder(w.Body).Decode(&cfg)

	if cfg.OSPatch.Mode != "anytime" {
		t.Errorf("expected os_patch mode 'anytime', got %q", cfg.OSPatch.Mode)
	}
	if cfg.OSPatch.CooldownMinutes != 360 {
		t.Errorf("expected os_patch cooldown 360, got %d", cfg.OSPatch.CooldownMinutes)
	}
	if cfg.DockerUpdate.Mode != "anytime" {
		t.Errorf("expected docker_update mode 'anytime', got %q", cfg.DockerUpdate.Mode)
	}
	if cfg.DockerUpdate.CooldownMinutes != 30 {
		t.Errorf("expected docker_update cooldown 30, got %d", cfg.DockerUpdate.CooldownMinutes)
	}
}

func TestSetAutomationConfig_SaveAndRetrieve(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})

	cfg := models.AutomationConfig{
		OSPatch: models.MaintenanceWindow{
			Mode:            "window",
			StartTime:       "23:00",
			EndTime:         "05:00",
			CooldownMinutes: 120,
		},
		DockerUpdate: models.MaintenanceWindow{
			Mode:            "disabled",
			StartTime:       "00:00",
			EndTime:         "06:00",
			CooldownMinutes: 60,
		},
	}
	body, _ := json.Marshal(cfg)

	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Retrieve and verify
	req2 := httptest.NewRequest("GET", "/api/v1/settings/automation", nil)
	w2 := httptest.NewRecorder()
	h.GetAutomationConfig(w2, req2)

	var got models.AutomationConfig
	json.NewDecoder(w2.Body).Decode(&got)

	if got.OSPatch.Mode != "window" {
		t.Errorf("expected os_patch mode 'window', got %q", got.OSPatch.Mode)
	}
	if got.OSPatch.CooldownMinutes != 120 {
		t.Errorf("expected os_patch cooldown 120, got %d", got.OSPatch.CooldownMinutes)
	}
	if got.DockerUpdate.Mode != "disabled" {
		t.Errorf("expected docker_update mode 'disabled', got %q", got.DockerUpdate.Mode)
	}
}

func TestSetAutomationConfig_InvalidMode(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})

	body := `{"os_patch":{"mode":"invalid","start_time":"23:00","end_time":"05:00","cooldown_minutes":60},"docker_update":{"mode":"anytime","start_time":"00:00","end_time":"06:00","cooldown_minutes":30}}`
	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid mode, got %d", w.Code)
	}
}

func TestSetAutomationConfig_InvalidTime(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})

	body := `{"os_patch":{"mode":"window","start_time":"25:00","end_time":"05:00","cooldown_minutes":60},"docker_update":{"mode":"anytime","start_time":"00:00","end_time":"06:00","cooldown_minutes":30}}`
	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid time, got %d", w.Code)
	}
}

func TestSetAutomationConfig_ZeroCooldown(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})

	body := `{"os_patch":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":0},"docker_update":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":30}}`
	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for zero cooldown, got %d", w.Code)
	}
}

func TestInMaintenanceWindow(t *testing.T) {
	tests := []struct {
		name   string
		window models.MaintenanceWindow
		want   bool
	}{
		{
			name:   "anytime always true",
			window: models.MaintenanceWindow{Mode: "anytime"},
			want:   true,
		},
		{
			name:   "disabled always false",
			window: models.MaintenanceWindow{Mode: "disabled"},
			want:   false,
		},
		{
			name: "24h window covers everything",
			window: models.MaintenanceWindow{
				Mode:      "window",
				StartTime: "00:00",
				EndTime:   "00:00",
			},
			want: false, // start == end means zero-width window
		},
		{
			name:   "unknown mode defaults to true",
			window: models.MaintenanceWindow{Mode: ""},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inMaintenanceWindow(tt.window)
			if got != tt.want {
				t.Errorf("inMaintenanceWindow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTimeStr(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"00:00", 0},
		{"23:59", 23*60 + 59},
		{"12:30", 12*60 + 30},
		{"05:00", 5 * 60},
		{"invalid", 0},
	}
	for _, tt := range tests {
		got := parseTimeStr(tt.input)
		if got != tt.want {
			t.Errorf("parseTimeStr(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestValidTimeStr(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"00:00", true},
		{"23:59", true},
		{"12:30", true},
		{"24:00", false},
		{"23:60", false},
		{"-1:00", false},
		{"ab:cd", false},
		{"1200", false},
		{"", false},
	}
	for _, tt := range tests {
		got := validTimeStr(tt.input)
		if got != tt.want {
			t.Errorf("validTimeStr(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestAutomationConfigRoundTrip(t *testing.T) {
	cfg := models.DefaultAutomationConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got models.AutomationConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.OSPatch.Mode != cfg.OSPatch.Mode {
		t.Errorf("os_patch mode mismatch: %q vs %q", got.OSPatch.Mode, cfg.OSPatch.Mode)
	}
	if got.DockerUpdate.CooldownMinutes != cfg.DockerUpdate.CooldownMinutes {
		t.Errorf("docker_update cooldown mismatch: %d vs %d", got.DockerUpdate.CooldownMinutes, cfg.DockerUpdate.CooldownMinutes)
	}
}
