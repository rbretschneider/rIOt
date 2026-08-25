package handlers

import (
	"bytes"
	"context"
	"encoding/json"
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

