package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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

func TestLoadAutomationConfig_PreservesZeroStagger(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	// User who explicitly sets stagger to 0 (e.g. has a registry pull-through cache)
	// must have that choice respected on load.
	saved := `{"os_patch":{"mode":"disabled","start_time":"23:00","end_time":"05:00","cooldown_minutes":360,"stagger_seconds":0},"docker_update":{"mode":"anytime","start_time":"23:00","end_time":"05:00","cooldown_minutes":30,"stagger_seconds":0}}`
	_ = adminRepo.SetConfig(nil, "automation_config", saved)

	h := New(HandlerDeps{
		Devices:   &testutil.MockDeviceRepo{},
		Telemetry: &testutil.MockTelemetryRepo{},
		Events:    &testutil.MockEventRepo{},
		AdminRepo: adminRepo,
	})
	got := h.loadAutomationConfig(nil)
	if got.DockerUpdate.StaggerSeconds != 0 {
		t.Errorf("expected stagger 0 to be preserved, got %d", got.DockerUpdate.StaggerSeconds)
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
			got := inMaintenanceWindow(tt.window, time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
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

// TestCheckAutoUpdates_ZeroStaggerDispatchesAll verifies that an active window
// with stagger_seconds=0 disables the stagger gate entirely — all eligible
// policies dispatch in a single pass. Intended for operators with a local
// registry pull-through cache where Docker Hub's rate limits don't apply.
func TestCheckAutoUpdates_ZeroStaggerDispatchesAll(t *testing.T) {
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
	if dispatched != 3 {
		t.Errorf("with stagger=0 all eligible targets should dispatch in one pass, got %d", dispatched)
	}
}

func TestFrequencyAllowsDay_DailyAndEmpty(t *testing.T) {
	now := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC) // Wednesday
	for _, freq := range []string{"", "daily"} {
		w := models.MaintenanceWindow{Frequency: freq}
		if !frequencyAllowsDay(w, now) {
			t.Errorf("freq=%q: expected allowed on any day", freq)
		}
	}
}

func TestFrequencyAllowsDay_Weekly(t *testing.T) {
	wed := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC) // Wednesday = 3
	sat := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC) // Saturday = 6

	w := models.MaintenanceWindow{Frequency: "weekly", DaysOfWeek: []int{0, 3, 6}}
	if !frequencyAllowsDay(w, wed) {
		t.Error("expected Wednesday allowed when 3 is in days_of_week")
	}
	if !frequencyAllowsDay(w, sat) {
		t.Error("expected Saturday allowed when 6 is in days_of_week")
	}

	tue := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC) // Tuesday = 2
	if frequencyAllowsDay(w, tue) {
		t.Error("expected Tuesday blocked when 2 not in days_of_week")
	}
}

func TestFrequencyAllowsDay_MonthlyLastDay(t *testing.T) {
	w := models.MaintenanceWindow{Frequency: "monthly"}

	// April 30 is the last day of April (30 days).
	if !frequencyAllowsDay(w, time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)) {
		t.Error("expected April 30 allowed (last day of April)")
	}
	// April 29 is not.
	if frequencyAllowsDay(w, time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)) {
		t.Error("expected April 29 blocked")
	}
	// Feb 28 in a non-leap year is the last day.
	if !frequencyAllowsDay(w, time.Date(2025, 2, 28, 10, 0, 0, 0, time.UTC)) {
		t.Error("expected Feb 28 2025 allowed (non-leap last day)")
	}
	// Feb 29 in a leap year is the last day.
	if !frequencyAllowsDay(w, time.Date(2024, 2, 29, 10, 0, 0, 0, time.UTC)) {
		t.Error("expected Feb 29 2024 allowed (leap year last day)")
	}
	// Dec 31 rollover sanity.
	if !frequencyAllowsDay(w, time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC)) {
		t.Error("expected Dec 31 allowed (year rollover last day)")
	}
}

func TestSetAutomationConfig_RejectsInvalidFrequency(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices: &testutil.MockDeviceRepo{}, Telemetry: &testutil.MockTelemetryRepo{},
		Events: &testutil.MockEventRepo{}, AdminRepo: adminRepo,
	})
	body := `{"os_patch":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":60,"stagger_seconds":0},"docker_update":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":30,"stagger_seconds":0,"frequency":"quarterly"}}`
	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid frequency, got %d", w.Code)
	}
}

func TestSetAutomationConfig_RejectsWeeklyWithoutDays(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices: &testutil.MockDeviceRepo{}, Telemetry: &testutil.MockTelemetryRepo{},
		Events: &testutil.MockEventRepo{}, AdminRepo: adminRepo,
	})
	body := `{"os_patch":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":60,"stagger_seconds":0},"docker_update":{"mode":"window","start_time":"23:00","end_time":"05:00","cooldown_minutes":30,"stagger_seconds":0,"frequency":"weekly","days_of_week":[]}}`
	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for weekly without days, got %d", w.Code)
	}
}

// autoPatchTestHandler wires a Handlers for checkAutoPatch tests with a
// device that has auto-patch enabled and the given OSPatch window config.
func autoPatchTestHandler(t *testing.T, osPatch models.MaintenanceWindow) (*Handlers, *testutil.MockCommandRepo) {
	t.Helper()
	adminRepo := testutil.NewMockAdminRepo("hash")
	cfg := models.AutomationConfig{
		OSPatch: osPatch,
		DockerUpdate: models.MaintenanceWindow{
			Mode: "disabled", StartTime: "23:00", EndTime: "05:00", CooldownMinutes: 60,
		},
	}
	cfgJSON, _ := json.Marshal(cfg)
	_ = adminRepo.SetConfig(context.Background(), "automation_config", string(cfgJSON))

	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "gpu-host", AutoPatch: true}

	cmdRepo := testutil.NewMockCommandRepo()
	h := New(HandlerDeps{
		Devices:     deviceRepo,
		Telemetry:   &testutil.MockTelemetryRepo{},
		Events:      &testutil.MockEventRepo{},
		AdminRepo:   adminRepo,
		CommandRepo: cmdRepo,
	})
	return h, cmdRepo
}

func pendingUpdatesTelemetry() *models.FullTelemetryData {
	return &models.FullTelemetryData{
		Updates: &models.UpdateInfo{PendingUpdates: 3},
	}
}

func osUpdateCommands(cmdRepo *testutil.MockCommandRepo) []*models.Command {
	var out []*models.Command
	for _, c := range cmdRepo.Commands {
		if c.Action == "os_update" {
			out = append(out, c)
		}
	}
	return out
}

// [AC-013] Reboot-class dispatch gating: the param appears only for
// in-window dispatches under the "gated" policy; out-of-window means no
// dispatch at all.
func TestCheckAutoPatch_AC013_RebootClassGating(t *testing.T) {
	t.Run("[AC-013] gated + in-window dispatch carries include_reboot_class", func(t *testing.T) {
		h, cmdRepo := autoPatchTestHandler(t, models.MaintenanceWindow{
			Mode: "anytime", StartTime: "00:00", EndTime: "23:59",
			CooldownMinutes: 60, RebootClass: "gated",
		})
		h.checkAutoPatch(context.Background(), "dev-1", pendingUpdatesTelemetry())

		cmds := osUpdateCommands(cmdRepo)
		if assert.Len(t, cmds, 1) {
			assert.Equal(t, true, cmds[0].Params["include_reboot_class"],
				"[AC-013] gated policy sets the param on in-window dispatch")
		}
	})

	t.Run("[AC-013] gated + out-of-window dispatches nothing", func(t *testing.T) {
		// Zero-width window (start == end) is never "in window".
		h, cmdRepo := autoPatchTestHandler(t, models.MaintenanceWindow{
			Mode: "window", StartTime: "00:00", EndTime: "00:00",
			CooldownMinutes: 60, RebootClass: "gated",
		})
		h.checkAutoPatch(context.Background(), "dev-1", pendingUpdatesTelemetry())

		assert.Empty(t, osUpdateCommands(cmdRepo),
			"[AC-013] out-of-window means no dispatch at all, param or not")
	})

	t.Run("[AC-013] policy off + in-window dispatch omits the param", func(t *testing.T) {
		h, cmdRepo := autoPatchTestHandler(t, models.MaintenanceWindow{
			Mode: "anytime", StartTime: "00:00", EndTime: "23:59",
			CooldownMinutes: 60, RebootClass: "off",
		})
		h.checkAutoPatch(context.Background(), "dev-1", pendingUpdatesTelemetry())

		cmds := osUpdateCommands(cmdRepo)
		if assert.Len(t, cmds, 1) {
			_, present := cmds[0].Params["include_reboot_class"]
			assert.False(t, present, "[AC-013] policy off never sets the param")
		}
	})
}

// [AC-023] Default configuration dispatch params are byte-for-byte
// pre-PATCH-GATE: exactly {"mode":"full"}.
func TestCheckAutoPatch_AC023_DefaultParamsUnchanged(t *testing.T) {
	h, cmdRepo := autoPatchTestHandler(t, models.MaintenanceWindow{
		Mode: "anytime", StartTime: "00:00", EndTime: "23:59",
		CooldownMinutes: 60, // RebootClass empty ≡ off (default)
	})
	h.checkAutoPatch(context.Background(), "dev-1", pendingUpdatesTelemetry())

	cmds := osUpdateCommands(cmdRepo)
	if assert.Len(t, cmds, 1) {
		assert.Equal(t, map[string]interface{}{"mode": "full"}, cmds[0].Params,
			"[AC-023] default dispatch params deep-equal the pre-story shape")
	}
}

// [AC-013 setup] reboot_class validation on PUT /settings/automation.
func TestSetAutomationConfig_RebootClassValidation(t *testing.T) {
	newHandler := func(t *testing.T) *Handlers {
		t.Helper()
		return New(HandlerDeps{
			Devices: &testutil.MockDeviceRepo{}, Telemetry: &testutil.MockTelemetryRepo{},
			Events: &testutil.MockEventRepo{}, AdminRepo: testutil.NewMockAdminRepo("hash"),
		})
	}
	put := func(t *testing.T, h *Handlers, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.SetAutomationConfig(w, req)
		return w
	}
	base := `"start_time":"00:00","end_time":"23:59","cooldown_minutes":60,"stagger_seconds":0`

	t.Run("accepts gated on os_patch", func(t *testing.T) {
		w := put(t, newHandler(t), `{"os_patch":{"mode":"anytime",`+base+`,"reboot_class":"gated"},"docker_update":{"mode":"anytime",`+base+`}}`)
		assert.Equal(t, 200, w.Code)
	})
	t.Run("accepts off and empty", func(t *testing.T) {
		w := put(t, newHandler(t), `{"os_patch":{"mode":"anytime",`+base+`,"reboot_class":"off"},"docker_update":{"mode":"anytime",`+base+`}}`)
		assert.Equal(t, 200, w.Code)
	})
	t.Run("rejects unknown value with 400", func(t *testing.T) {
		w := put(t, newHandler(t), `{"os_patch":{"mode":"anytime",`+base+`,"reboot_class":"always"},"docker_update":{"mode":"anytime",`+base+`}}`)
		assert.Equal(t, 400, w.Code)
	})
	t.Run("rejects reboot_class on docker_update with 400", func(t *testing.T) {
		w := put(t, newHandler(t), `{"os_patch":{"mode":"anytime",`+base+`},"docker_update":{"mode":"anytime",`+base+`,"reboot_class":"gated"}}`)
		assert.Equal(t, 400, w.Code)
	})
}

func TestSetAutomationConfig_RejectsOutOfRangeDayOfWeek(t *testing.T) {
	adminRepo := testutil.NewMockAdminRepo("hash")
	h := New(HandlerDeps{
		Devices: &testutil.MockDeviceRepo{}, Telemetry: &testutil.MockTelemetryRepo{},
		Events: &testutil.MockEventRepo{}, AdminRepo: adminRepo,
	})
	body := `{"os_patch":{"mode":"anytime","start_time":"00:00","end_time":"23:59","cooldown_minutes":60,"stagger_seconds":0},"docker_update":{"mode":"window","start_time":"23:00","end_time":"05:00","cooldown_minutes":30,"stagger_seconds":0,"frequency":"weekly","days_of_week":[7]}}`
	req := httptest.NewRequest("PUT", "/api/v1/settings/automation", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SetAutomationConfig(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for day_of_week=7, got %d", w.Code)
	}
}


// ---------------------------------------------------------------------------
// CMD-LIFECYCLE-001 item 1: targeting, success-only cooldown, failure backoff
// ---------------------------------------------------------------------------

type autoEnv struct {
	h        *Handlers
	repo     *testutil.MockCommandRepo
	policies *testutil.MockAutoUpdateRepo
	clock    *testClock
	events   *testutil.MockEventRepo
}

// newAutoEnv builds Handlers over the faithful mocks with an injected clock
// (12:00 UTC on 2026-10-17) and the given maintenance windows.
func newAutoEnv(t *testing.T, docker, osPatch models.MaintenanceWindow) *autoEnv {
	t.Helper()
	admin := testutil.NewMockAdminRepo("hash")
	cfg, err := json.Marshal(models.AutomationConfig{OSPatch: osPatch, DockerUpdate: docker})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := admin.SetConfig(context.Background(), "automation_config", string(cfg)); err != nil {
		t.Fatalf("set config: %v", err)
	}
	devs := testutil.NewMockDeviceRepo()
	devs.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host", AutoPatch: true}
	e := &autoEnv{repo: testutil.NewMockCommandRepo(), policies: testutil.NewMockAutoUpdateRepo(), clock: newTestClock(), events: testutil.NewMockEventRepo()}
	e.h = New(HandlerDeps{
		Devices: devs, Telemetry: testutil.NewMockTelemetryRepo(), Events: e.events,
		AdminRepo: admin, CommandRepo: e.repo, AutoUpdateRepo: e.policies, Clock: e.clock.Now,
	})
	e.h.agents = newAgentRegistry()
	return e
}

func anytime() models.MaintenanceWindow {
	return models.MaintenanceWindow{Mode: "anytime", StartTime: "00:00", EndTime: "00:00", CooldownMinutes: 60}
}

func (e *autoEnv) policy(t *testing.T, target string, isStack bool, mod func(*models.AutoUpdatePolicy)) *models.AutoUpdatePolicy {
	t.Helper()
	p := &models.AutoUpdatePolicy{DeviceID: "dev-1", Target: target, IsStack: isStack, Enabled: true}
	if err := e.policies.Upsert(context.Background(), p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	stored := &e.policies.Policies["dev-1"][len(e.policies.Policies["dev-1"])-1]
	if mod != nil {
		mod(stored)
	}
	return stored
}

func (e *autoEnv) dockerUpdates() []models.Command {
	var out []models.Command
	for _, c := range e.repo.All() {
		if c.Action == "docker_update" {
			out = append(out, c)
		}
	}
	return out
}

func TestCL_AC001_AutoUpdateDispatchIncludesNameAndID(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "web", false, nil)
	updateAvail := true
	data := &models.FullTelemetryData{Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
		{ID: "abc", Name: "web", State: "running", UpdateAvailable: &updateAvail},
	}}}

	e.h.checkAutoUpdates(context.Background(), "dev-1", data)

	cmds := e.dockerUpdates()
	if assert.Len(t, cmds, 1) {
		assert.Equal(t, "web", cmds[0].Params["container"], "[AC-001] the name is the stable identity")
		assert.Equal(t, "abc", cmds[0].Params["container_id"], "[AC-001] legacy agents need the current id")
		assert.Equal(t, models.OriginAutoUpdate, cmds[0].Origin)
		if assert.NotNil(t, cmds[0].PolicyID) {
			assert.Equal(t, e.policies.Policies["dev-1"][0].ID, *cmds[0].PolicyID)
		}
	}
}

func TestCL_AC008_BackoffGatesDispatch(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	failedAt := e.clock.Now()
	e.policy(t, "alpha", false, func(p *models.AutoUpdatePolicy) {
		p.ConsecutiveFailures = 1
		p.LastFailureAt = &failedAt
	})
	data := makeTelemetryWithUpdates("alpha")

	e.clock.Advance(14 * time.Minute)
	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	assert.Empty(t, e.dockerUpdates(), "[AC-008] no dispatch at T+14m (backoff is 15m for n=1)")

	e.clock.Advance(2 * time.Minute) // T+16m
	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	assert.Len(t, e.dockerUpdates(), 1, "[AC-008] one dispatch at T+16m")
}

func TestBackoffGrowsWithConsecutiveFailures(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	failedAt := e.clock.Now()
	e.policy(t, "alpha", false, func(p *models.AutoUpdatePolicy) {
		p.ConsecutiveFailures = 3 // 60 minutes
		p.LastFailureAt = &failedAt
	})
	data := makeTelemetryWithUpdates("alpha")

	e.clock.Advance(59 * time.Minute)
	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	assert.Empty(t, e.dockerUpdates(), "n=3 backs off for 60 minutes")

	e.clock.Advance(time.Minute)
	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	assert.Len(t, e.dockerUpdates(), 1, "the boundary instant is eligible")
}

func TestCL_AC010_NoDispatchWhilePolicyCommandRunning(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	pol := e.policy(t, "alpha", false, nil)
	now := e.clock.Now()
	dl := now.Add(time.Hour)
	policyID := pol.ID
	e.repo.Commands["running-cmd"] = &models.Command{
		ID: "running-cmd", DeviceID: "dev-1", Action: "docker_update", Params: map[string]interface{}{"container": "alpha"},
		Status: "running", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour), ExecDeadlineS: 2100,
		DeadlineAt: &dl, Origin: models.OriginAutoUpdate, PolicyID: &policyID, TargetKeys: []string{"docker:container:alpha"},
	}

	e.h.checkAutoUpdates(context.Background(), "dev-1", makeTelemetryWithUpdates("alpha"))

	assert.Len(t, e.dockerUpdates(), 1, "[AC-010] no new command while the policy command is running")
}

func TestCL_AC012_OutsideWindowNoDispatch(t *testing.T) {
	night := models.MaintenanceWindow{Mode: "window", StartTime: "02:00", EndTime: "03:00", CooldownMinutes: 60}
	e := newAutoEnv(t, night, anytime())
	failedAt := e.clock.Now().Add(-24 * time.Hour)
	e.policy(t, "alpha", false, func(p *models.AutoUpdatePolicy) {
		p.ConsecutiveFailures = 1
		p.LastFailureAt = &failedAt // backoff long elapsed
	})

	e.h.checkAutoUpdates(context.Background(), "dev-1", makeTelemetryWithUpdates("alpha")) // 12:00 UTC

	assert.Empty(t, e.dockerUpdates(), "[AC-012] backoff must never let a dispatch happen outside the window")

	e.clock.Advance(14*time.Hour + 30*time.Minute) // 02:30 next day
	e.h.checkAutoUpdates(context.Background(), "dev-1", makeTelemetryWithUpdates("alpha"))
	assert.Len(t, e.dockerUpdates(), 1, "inside the window the elapsed backoff allows the retry")
}

func TestCL_AC030_AutoPatchExpiryCappedAtWindowEnd(t *testing.T) {
	window := models.MaintenanceWindow{Mode: "window", StartTime: "08:00", EndTime: "11:00", CooldownMinutes: 60}
	e := newAutoEnv(t, anytime(), window)
	e.clock.Advance(-70 * time.Minute) // 10:50, ten minutes before the window closes
	now := e.clock.Now()

	e.h.checkAutoPatch(context.Background(), "dev-1", &models.FullTelemetryData{Updates: &models.UpdateInfo{PendingUpdates: 2}})

	cmds := osUpdateCommands(e.repo)
	if assert.Len(t, cmds, 1, "now=%s must be inside 08:00-11:00", now) {
		assert.True(t, cmds[0].ExpiresAt.Equal(now.Add(10*time.Minute)),
			"[AC-030] expires_at must be the window end (T+10m), got %s", cmds[0].ExpiresAt)
	}
}

func TestMaintenanceWindowEnd(t *testing.T) {
	day := func(h, m int) time.Time { return time.Date(2026, 10, 17, h, m, 0, 0, time.UTC) }
	cases := []struct {
		name string
		w    models.MaintenanceWindow
		now  time.Time
		want time.Time
	}{
		{"same-day window", models.MaintenanceWindow{Mode: "window", StartTime: "09:00", EndTime: "17:00"}, day(10, 0), day(17, 0)},
		{"overnight, before midnight", models.MaintenanceWindow{Mode: "window", StartTime: "23:00", EndTime: "05:00"}, day(23, 30), day(5, 0).AddDate(0, 0, 1)},
		{"overnight, after midnight", models.MaintenanceWindow{Mode: "window", StartTime: "23:00", EndTime: "05:00"}, day(2, 0), day(5, 0)},
		{"anytime has no end", models.MaintenanceWindow{Mode: "anytime"}, day(10, 0), time.Time{}},
		{"disabled has no end", models.MaintenanceWindow{Mode: "disabled"}, day(10, 0), time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, maintenanceWindowEnd(tc.w, tc.now).Equal(tc.want))
		})
	}
}

func TestCL_AC072_AutoUpdateConflictSkipLogsNoBackoff(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	e := newAutoEnv(t, anytime(), anytime())
	pol := e.policy(t, "alpha", false, nil)
	now := e.clock.Now()
	e.repo.Commands["manual"] = &models.Command{
		ID: "manual", DeviceID: "dev-1", Action: "docker_update", Params: map[string]interface{}{"container": "alpha"},
		Status: "queued", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour), ExecDeadlineS: 2100,
		Origin: models.OriginUser, TargetKeys: []string{"docker:container:alpha"},
	}

	e.h.checkAutoUpdates(context.Background(), "dev-1", makeTelemetryWithUpdates("alpha"))

	assert.Len(t, e.dockerUpdates(), 1, "[AC-072] no new row on a target conflict")
	assert.Contains(t, buf.String(), "auto-dispatch conflict skip", "[AC-072] a log entry is written")
	assert.Contains(t, buf.String(), "existing_command_id=manual")
	assert.Zero(t, pol.ConsecutiveFailures, "[AC-072] a conflict must not count toward the backoff")
}

func TestCheckAutoUpdates_StaggerOnlyConsumedByRealDispatch(t *testing.T) {
	e := newAutoEnv(t, models.MaintenanceWindow{Mode: "anytime", CooldownMinutes: 60, StaggerSeconds: 600}, anytime())
	e.policy(t, "alpha", false, nil)
	e.policy(t, "bravo", false, nil)
	now := e.clock.Now()
	e.repo.Commands["manual"] = &models.Command{
		ID: "manual", DeviceID: "dev-1", Action: "docker_update", Params: map[string]interface{}{"container": "alpha"},
		Status: "running", CreatedAt: now.Add(-time.Hour), UpdatedAt: now, ExpiresAt: now.Add(time.Hour), ExecDeadlineS: 2100,
		Origin: models.OriginUser, TargetKeys: []string{"docker:container:alpha"},
	}

	e.h.checkAutoUpdates(context.Background(), "dev-1", makeTelemetryWithUpdates("alpha", "bravo"))

	assert.Len(t, e.dockerUpdates(), 2, "alpha's conflict is skipped and must not use up the stagger slot, so bravo dispatches")
}

