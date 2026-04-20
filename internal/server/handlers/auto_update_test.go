package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

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

	if cfg.OSPatch.Mode != "disabled" {
		t.Errorf("expected os_patch mode 'disabled', got %q", cfg.OSPatch.Mode)
	}
	if cfg.OSPatch.CooldownMinutes != 360 {
		t.Errorf("expected os_patch cooldown 360, got %d", cfg.OSPatch.CooldownMinutes)
	}
	if cfg.DockerUpdate.Mode != "disabled" {
		t.Errorf("expected docker_update mode 'disabled', got %q", cfg.DockerUpdate.Mode)
	}
	if cfg.DockerUpdate.CooldownMinutes != 10080 {
		t.Errorf("expected docker_update cooldown 10080 (weekly), got %d", cfg.DockerUpdate.CooldownMinutes)
	}
	if cfg.DockerUpdate.StaggerSeconds != models.DefaultDockerStaggerSeconds {
		t.Errorf("expected docker_update stagger %d, got %d",
			models.DefaultDockerStaggerSeconds, cfg.DockerUpdate.StaggerSeconds)
	}
}

func TestSetAutomationConfig_NegativeStagger(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})

	body := `{"os_patch":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":60,"stagger_seconds":0},"docker_update":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":30,"stagger_seconds":-5}}`
	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for negative stagger, got %d", w.Code)
	}
}

func TestLoadAutomationConfig_BackfillsLegacyStagger(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	// Seed a pre-stagger-field config: active docker_update window with no stagger key.
	legacy := `{"os_patch":{"mode":"disabled","start_time":"23:00","end_time":"05:00","cooldown_minutes":360},"docker_update":{"mode":"anytime","start_time":"23:00","end_time":"05:00","cooldown_minutes":30}}`
	_ = adminRepo.SetConfig(nil, "automation_config", legacy)

	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})
	got := h.loadAutomationConfig(nil)
	if got.DockerUpdate.StaggerSeconds != models.DefaultDockerStaggerSeconds {
		t.Errorf("expected legacy config to backfill stagger to %d, got %d",
			models.DefaultDockerStaggerSeconds, got.DockerUpdate.StaggerSeconds)
	}
}

func TestLoadAutomationConfig_PreservesExplicitZeroStaggerWhenDisabled(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	// Disabled docker_update should not be backfilled — user isn't using auto-update.
	saved := `{"os_patch":{"mode":"disabled","start_time":"23:00","end_time":"05:00","cooldown_minutes":360,"stagger_seconds":0},"docker_update":{"mode":"disabled","start_time":"23:00","end_time":"05:00","cooldown_minutes":30,"stagger_seconds":0}}`
	_ = adminRepo.SetConfig(nil, "automation_config", saved)

	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})
	got := h.loadAutomationConfig(nil)
	if got.DockerUpdate.StaggerSeconds != 0 {
		t.Errorf("expected stagger to remain 0 on disabled docker_update, got %d",
			got.DockerUpdate.StaggerSeconds)
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
			name:   "unknown mode defaults to false",
			window: models.MaintenanceWindow{Mode: ""},
			want:   false,
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
	if got.DockerUpdate.StaggerSeconds != cfg.DockerUpdate.StaggerSeconds {
		t.Errorf("docker_update stagger mismatch: %d vs %d", got.DockerUpdate.StaggerSeconds, cfg.DockerUpdate.StaggerSeconds)
	}
}

// staggerTestHandler builds a Handlers instance wired for checkAutoUpdates tests.
func staggerTestHandler(t *testing.T, staggerSeconds int) (*Handlers, *testutil.MockCommandRepo, *testutil.MockAutoUpdateRepo) {
	t.Helper()
	adminRepo := testutil.NewMockAdminRepo("hash")
	cfg := models.AutomationConfig{
		OSPatch: models.MaintenanceWindow{
			Mode: "disabled", StartTime: "00:00", EndTime: "00:00", CooldownMinutes: 60,
		},
		DockerUpdate: models.MaintenanceWindow{
			Mode: "anytime", StartTime: "00:00", EndTime: "00:00",
			CooldownMinutes: 60, StaggerSeconds: staggerSeconds,
		},
	}
	cfgJSON, _ := json.Marshal(cfg)
	_ = adminRepo.SetConfig(context.Background(), "automation_config", string(cfgJSON))

	cmdRepo := testutil.NewMockCommandRepo()
	autoRepo := testutil.NewMockAutoUpdateRepo()

	h := New(HandlerDeps{
		Devices:        &testutil.MockDeviceRepo{},
		Telemetry:      &testutil.MockTelemetryRepo{},
		Events:         &testutil.MockEventRepo{},
		AdminRepo:      adminRepo,
		CommandRepo:    cmdRepo,
		AutoUpdateRepo: autoRepo,
	})
	return h, cmdRepo, autoRepo
}

// makeTelemetryWithUpdates builds a telemetry snapshot with N running containers
// all flagged as having an update available, for stagger testing.
func makeTelemetryWithUpdates(names ...string) *models.FullTelemetryData {
	updateAvail := true
	var containers []models.ContainerInfo
	for _, n := range names {
		containers = append(containers, models.ContainerInfo{
			ID:              "id-" + n,
			Name:            n,
			Image:           "img/" + n + ":latest",
			State:           "running",
			UpdateAvailable: &updateAvail,
		})
	}
	return &models.FullTelemetryData{
		Docker: &models.DockerInfo{Containers: containers},
	}
}

func TestCheckAutoUpdates_StaggerBlocksRecentDispatch(t *testing.T) {
	h, cmdRepo, autoRepo := staggerTestHandler(t, 600)
	deviceID := "dev-1"
	ctx := context.Background()

	// Seed: one enabled policy and a docker_update command created 60s ago.
	_ = autoRepo.Upsert(ctx, &models.AutoUpdatePolicy{
		DeviceID: deviceID, Target: "alpha", Enabled: true,
	})
	cmdRepo.Commands["prev"] = &models.Command{
		ID: "prev", DeviceID: deviceID, Action: "docker_update", Status: "sent",
		CreatedAt: time.Now().Add(-60 * time.Second),
	}

	before := len(cmdRepo.Commands)
	h.checkAutoUpdates(ctx, deviceID, makeTelemetryWithUpdates("alpha"))
	if got := len(cmdRepo.Commands); got != before {
		t.Errorf("stagger should have blocked dispatch: expected %d commands, got %d", before, got)
	}
}

func TestCheckAutoUpdates_StaggerAllowsOnePerPass(t *testing.T) {
	h, cmdRepo, autoRepo := staggerTestHandler(t, 600)
	deviceID := "dev-1"
	ctx := context.Background()

	// Three eligible policies, no prior dispatch → stagger lets exactly one fire.
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		_ = autoRepo.Upsert(ctx, &models.AutoUpdatePolicy{
			DeviceID: deviceID, Target: name, Enabled: true,
		})
	}

	h.checkAutoUpdates(ctx, deviceID, makeTelemetryWithUpdates("alpha", "bravo", "charlie"))

	dispatched := 0
	for _, cmd := range cmdRepo.Commands {
		if cmd.Action == "docker_update" {
			dispatched++
		}
	}
	if dispatched != 1 {
		t.Errorf("with stagger enabled, expected exactly 1 dispatch per pass, got %d", dispatched)
	}
}

// TestCheckAutoUpdates_ZeroStaggerIsBackfilled verifies that an active window
// saved with stagger_seconds=0 still enforces the default stagger at runtime,
// since loadAutomationConfig backfills zero to DefaultDockerStaggerSeconds. A
// user who never configured stagger should still be protected from Docker Hub
// rate limits.
func TestCheckAutoUpdates_ZeroStaggerIsBackfilled(t *testing.T) {
	h, cmdRepo, autoRepo := staggerTestHandler(t, 0)
	deviceID := "dev-1"
	ctx := context.Background()

	for _, name := range []string{"alpha", "bravo", "charlie"} {
		_ = autoRepo.Upsert(ctx, &models.AutoUpdatePolicy{
			DeviceID: deviceID, Target: name, Enabled: true,
		})
	}

	h.checkAutoUpdates(ctx, deviceID, makeTelemetryWithUpdates("alpha", "bravo", "charlie"))

	dispatched := 0
	for _, cmd := range cmdRepo.Commands {
		if cmd.Action == "docker_update" {
			dispatched++
		}
	}
	if dispatched != 1 {
		t.Errorf("backfilled stagger should still limit dispatches to 1 per pass, got %d", dispatched)
	}
}
