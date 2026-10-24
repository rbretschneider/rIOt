package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// warnCapture records slog records at Warn and above.
type warnCapture struct {
	mu      sync.Mutex
	records []string
}

func (c *warnCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *warnCapture) Handle(_ context.Context, r slog.Record) error {
	if r.Level < slog.LevelWarn {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Level.String()+" "+r.Message)
	return nil
}
func (c *warnCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *warnCapture) WithGroup(string) slog.Handler      { return c }

func (c *warnCapture) warnings() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.records...)
}

// ingestUpdates posts a telemetry body whose updates object is updatesJSON and
// returns the stored updates section.
func ingestUpdates(t *testing.T, updatesJSON string) (*models.UpdateInfo, int, []string) {
	t.Helper()
	capture := &warnCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })

	telRepo := testutil.NewMockTelemetryRepo()
	devRepo := testutil.NewMockDeviceRepo()
	devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host"}
	hub := websocket.NewHub()
	go hub.Run()
	gen := events.NewGenerator(testutil.NewMockEventRepo(), hub,
		testutil.NewMockAlertRuleRepo(), testutil.NewMockDispatcher(), testutil.NewMockCommandRepo())
	h := &Handlers{telemetry: telRepo, devices: devRepo, hub: hub, eventGen: gen}

	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/telemetry", h.Telemetry)
	body := `{"device_id":"dev-1","data":{"updates":` + updatesJSON + `}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-1/telemetry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	stored := telRepo.Snapshots["dev-1"]
	if len(stored) == 0 {
		return nil, rec.Code, capture.warnings()
	}
	return stored[0].Data.Updates, rec.Code, capture.warnings()
}

func TestTelemetryIngest_PF_AC029_OldAndBogusAccepted2xxNoWarn(t *testing.T) {
	cases := []struct {
		name      string
		updates   string
		wantStore models.ListsFreshness
		wantNorm  string
	}{
		{"older agent, no field", `{"package_manager":"apt","pending_updates":3}`, "", "unknown"},
		{"bogus string", `{"package_manager":"apt","lists_freshness":"bogus"}`, models.ListsUnknown, "unknown"},
		{"number", `{"package_manager":"apt","lists_freshness":42}`, models.ListsUnknown, "unknown"},
		{"object", `{"package_manager":"apt","lists_freshness":{"x":1}}`, models.ListsUnknown, "unknown"},
		{"valid stale", `{"package_manager":"apt","lists_freshness":"stale"}`, models.ListsStale, "stale"},
		{"valid fresh", `{"package_manager":"apt","lists_freshness":"fresh"}`, models.ListsFresh, "fresh"},
	}
	for _, tc := range cases {
		t.Run("[AC-029] "+tc.name, func(t *testing.T) {
			updates, status, warnings := ingestUpdates(t, tc.updates)

			require.Equal(t, http.StatusOK, status, "[AC-029] the push must be accepted")
			require.NotNil(t, updates)
			assert.Equal(t, tc.wantStore, updates.ListsFreshness, "the value is stored already normalized")
			assert.Equal(t, tc.wantNorm, updates.Freshness())
			assert.Empty(t, warnings, "[AC-029] no warning may be logged for a tolerated value")
		})
	}
}

func TestTelemetryIngest_PF_AC016_CapabilityOnlyTrueIsStoredTrue(t *testing.T) {
	cases := map[string]bool{
		`true`:  true,
		`false`: false,
		`"yes"`: false,
		`42`:    false,
		`null`:  false,
		`[]`:    false,
	}
	for value, want := range cases {
		t.Run("[AC-016] "+value, func(t *testing.T) {
			updates, status, warnings := ingestUpdates(t, `{"package_manager":"apt","supports_reboot_if_required":`+value+`}`)

			require.Equal(t, http.StatusOK, status)
			require.NotNil(t, updates)
			assert.Equal(t, want, bool(updates.SupportsRebootIfRequired))
			assert.Empty(t, warnings)
		})
	}
}
