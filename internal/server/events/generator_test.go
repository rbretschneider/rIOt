package events

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

func TestCompareValue(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		want      bool
	}{
		{"gt true", 91, ">", 90, true},
		{"gt false", 89, ">", 90, false},
		{"gt equal", 90, ">", 90, false},
		{"lt true", 5, "<", 10, true},
		{"lt false", 15, "<", 10, false},
		{"gte true equal", 90, ">=", 90, true},
		{"gte true above", 91, ">=", 90, true},
		{"gte false", 89, ">=", 90, false},
		{"lte true equal", 10, "<=", 10, true},
		{"lte true below", 5, "<=", 10, true},
		{"lte false", 15, "<=", 10, false},
		{"eq true", 42, "==", 42, true},
		{"eq false", 42, "==", 43, false},
		{"neq true", 42, "!=", 43, true},
		{"neq false", 42, "!=", 42, false},
		{"invalid operator", 1, "~", 1, false},
		{"empty operator", 1, "", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareValue(tt.value, tt.operator, tt.threshold)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesDeviceScope(t *testing.T) {
	tests := []struct {
		name     string
		include  string
		exclude  string
		deviceID string
		hostname string
		want     bool
	}{
		{"empty include/exclude matches all", "", "", "device-1", "host-1", true},
		{"include by device ID", "device-1", "", "device-1", "host-1", true},
		{"include by hostname", "host-1", "", "device-1", "host-1", true},
		{"include no match", "device-2", "", "device-1", "host-1", false},
		{"include comma separated match", "device-1,device-2", "", "device-1", "host-1", true},
		{"include comma separated hostname match", "host-2", "", "device-1", "host-2", true},
		{"include comma separated no match", "device-2,device-3", "", "device-1", "host-1", false},
		{"exclude by hostname", "", "host-1", "device-1", "host-1", false},
		{"exclude by device ID", "", "device-1", "device-1", "host-1", false},
		{"exclude wins over include", "device-1", "host-1", "device-1", "host-1", false},
		{"exclude doesn't match, include does", "device-1", "host-2", "device-1", "host-1", true},
		{"hostname case insensitive", "HOST-1", "", "device-1", "host-1", true},
		{"exclude case insensitive", "", "HOST-1", "device-1", "host-1", false},
		{"whitespace trimmed", "device-1, device-2 , device-3", "", "device-2", "host-2", true},
		{"partial id no match", "device-1", "", "device-10", "host-10", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesDeviceScope(tt.include, tt.exclude, tt.deviceID, tt.hostname)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesDeviceScope_Tags(t *testing.T) {
	tests := []struct {
		name     string
		include  string
		exclude  string
		deviceID string
		hostname string
		tags     []string
		want     bool
	}{
		{"include by tag", "homelab", "", "device-1", "host-1", []string{"homelab", "prod"}, true},
		{"include by tag no match", "staging", "", "device-1", "host-1", []string{"homelab", "prod"}, false},
		{"exclude by tag", "", "homelab", "device-1", "host-1", []string{"homelab"}, false},
		{"exclude tag wins over include hostname", "host-1", "homelab", "device-1", "host-1", []string{"homelab"}, false},
		{"include tag with exclude hostname miss", "homelab", "host-2", "device-1", "host-1", []string{"homelab"}, true},
		{"empty tags matches via hostname", "host-1", "", "device-1", "host-1", nil, true},
		{"empty tags no match", "homelab", "", "device-1", "host-1", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesDeviceScope(tt.include, tt.exclude, tt.deviceID, tt.hostname, tt.tags)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckWebServerAlerts(t *testing.T) {
	tests := []struct {
		name       string
		daysLeft   int
		wantEvents int
		wantType   models.EventType
	}{
		{"cert healthy (90 days)", 90, 0, ""},
		{"cert healthy (30 days)", 30, 0, ""},
		{"cert expiring (29 days)", 29, 1, models.EventCertExpiring},
		{"cert expiring (7 days)", 7, 1, models.EventCertExpiring},
		{"cert expiring (1 day)", 1, 1, models.EventCertExpiring},
		{"cert expired (0 days)", 0, 1, models.EventCertExpired},
		{"cert expired (-5 days)", -5, 1, models.EventCertExpired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventRepo := &mockEventRepo{}
			alertRepo := &mockAlertRuleRepo{rules: []models.AlertRule{
				{
					ID:              1,
					Enabled:         true,
					Metric:          "cert_days_left",
					Operator:        "<",
					Threshold:       30,
					Severity:        "warning",
					CooldownSeconds: 86400,
				},
				{
					ID:              2,
					Enabled:         true,
					Metric:          "cert_days_left",
					Operator:        "<=",
					Threshold:       0,
					Severity:        "critical",
					CooldownSeconds: 86400,
				},
			}}

			hub := websocket.NewHub()
			go hub.Run()
			gen := NewGenerator(eventRepo, hub, alertRepo, nil, nil)

			ws := &models.WebServerInfo{
				Servers: []models.ProxyServer{
					{
						Name: "nginx",
						Certs: []models.ProxyCert{
							{
								Subject:  "test.example.com",
								DaysLeft: tt.daysLeft,
							},
						},
					},
				},
			}

			gen.CheckWebServerAlerts(context.Background(), "device-1", "test-host", ws)

			require.Len(t, eventRepo.events, tt.wantEvents)
			if tt.wantEvents > 0 {
				assert.Equal(t, tt.wantType, eventRepo.events[0].Type)
				assert.Contains(t, eventRepo.events[0].Message, "test.example.com")
				assert.Contains(t, eventRepo.events[0].Message, "nginx")
			}
		})
	}
}

func TestCheckWebServerAlerts_NoCerts(t *testing.T) {
	eventRepo := &mockEventRepo{}
	alertRepo := &mockAlertRuleRepo{}
	hub := websocket.NewHub()
	go hub.Run()
	gen := NewGenerator(eventRepo, hub, alertRepo, nil, nil)

	ws := &models.WebServerInfo{
		Servers: []models.ProxyServer{
			{Name: "nginx"},
		},
	}

	gen.CheckWebServerAlerts(context.Background(), "device-1", "test-host", ws)
	assert.Empty(t, eventRepo.events)
}

func TestCheckWebServerAlerts_MultipleCerts(t *testing.T) {
	eventRepo := &mockEventRepo{}
	alertRepo := &mockAlertRuleRepo{rules: []models.AlertRule{
		{
			ID: 1, Enabled: true, Metric: "cert_days_left",
			Operator: "<", Threshold: 30, Severity: "warning", CooldownSeconds: 86400,
		},
		{
			ID: 2, Enabled: true, Metric: "cert_days_left",
			Operator: "<=", Threshold: 0, Severity: "critical", CooldownSeconds: 86400,
		},
	}}
	hub := websocket.NewHub()
	go hub.Run()
	gen := NewGenerator(eventRepo, hub, alertRepo, nil, nil)

	ws := &models.WebServerInfo{
		Servers: []models.ProxyServer{
			{
				Name: "nginx",
				Certs: []models.ProxyCert{
					{Subject: "ok.example.com", DaysLeft: 60},
					{Subject: "expiring.example.com", DaysLeft: 10},
					{Subject: "expired.example.com", DaysLeft: -1},
				},
			},
		},
	}

	gen.CheckWebServerAlerts(context.Background(), "device-1", "test-host", ws)
	// The first matching cert fires on rule 1 (< 30); the second cert matches the same
	// rule but is on cooldown. The expired cert is evaluated separately via rule 2 (<= 0)
	// but rule 1 matches first (since -1 < 30), so it shares the cooldown key.
	// Net result: 1 event for the first cert that triggers rule 1.
	require.Len(t, eventRepo.events, 1)
	assert.Contains(t, eventRepo.events[0].Message, "expiring.example.com")
}

// Minimal mock types for generator tests.
type mockEventRepo struct {
	events []models.Event
}

func (m *mockEventRepo) Create(_ context.Context, e *models.Event) error {
	e.ID = int64(len(m.events) + 1)
	m.events = append(m.events, *e)
	return nil
}
func (m *mockEventRepo) ListByDevice(context.Context, string, int) ([]models.Event, error) {
	return nil, nil
}
func (m *mockEventRepo) ListAll(context.Context, int, int) ([]models.Event, error) {
	return nil, nil
}
func (m *mockEventRepo) Purge(context.Context, time.Time) (int64, error) { return 0, nil }
func (m *mockEventRepo) CountUnacknowledged(context.Context) (int, error) { return 0, nil }
func (m *mockEventRepo) Acknowledge(context.Context, int64) error         { return nil }
func (m *mockEventRepo) AcknowledgeAll(context.Context) error             { return nil }
func (m *mockEventRepo) HasCommandEvent(context.Context, string, string, models.EventType) (bool, error) {
	return false, nil
}
func (m *mockEventRepo) ListByCommandSplit(context.Context, string, string, int, int) (models.CommandEvents, error) {
	return models.CommandEvents{}, nil
}

type mockAlertRuleRepo struct {
	rules []models.AlertRule
}

func (m *mockAlertRuleRepo) List(context.Context) ([]models.AlertRule, error) { return m.rules, nil }
func (m *mockAlertRuleRepo) ListEnabled(context.Context) ([]models.AlertRule, error) {
	var out []models.AlertRule
	for _, r := range m.rules {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *mockAlertRuleRepo) GetByID(context.Context, int64) (*models.AlertRule, error) {
	return nil, nil
}
func (m *mockAlertRuleRepo) Create(context.Context, *models.AlertRule) error  { return nil }
func (m *mockAlertRuleRepo) Update(context.Context, *models.AlertRule) error  { return nil }
func (m *mockAlertRuleRepo) Delete(context.Context, int64) error { return nil }

func TestCL_SEC002_CompletionEventReasonRuneSafe(t *testing.T) {
	t.Run("a multi-byte rune straddling the 200-byte boundary", func(t *testing.T) {
		gen, eventRepo, _, _ := setupGenerator(t)
		// 199 ASCII bytes then a 3-byte rune: the 200-byte cut lands inside it.
		message := strings.Repeat("a", 199) + "€" + strings.Repeat("b", 50)

		gen.CommandCompleted(context.Background(), "dev-1", "host", "docker_update", "error", message)

		require.Len(t, eventRepo.Events, 1)
		got := eventRepo.Events[0].Message
		assert.True(t, utf8.ValidString(got), "[SEC-002] the event message must be valid UTF-8, got %q", got)
		assert.Contains(t, got, "...", "a truncated reason is marked")
		assert.NotContains(t, got, "\xe2\x82", "no half rune may survive")
	})

	t.Run("NUL and invalid UTF-8 in the reason", func(t *testing.T) {
		gen, eventRepo, _, _ := setupGenerator(t)

		gen.CommandCompleted(context.Background(), "dev-1", "host", "reboot", "error", "bad\x00 bytes \xff\xfe end")

		require.Len(t, eventRepo.Events, 1)
		got := eventRepo.Events[0].Message
		assert.NotContains(t, got, "\x00", "[SEC-002] the event message must be NUL-free")
		assert.True(t, utf8.ValidString(got))
		assert.Contains(t, got, "bad")
		assert.Contains(t, got, "end")
	})

	t.Run("a short ASCII reason is unchanged and success carries no reason", func(t *testing.T) {
		gen, eventRepo, _, _ := setupGenerator(t)

		gen.CommandCompleted(context.Background(), "dev-1", "host", "reboot", "error", "disk full")
		gen.CommandCompleted(context.Background(), "dev-1", "host", "reboot", "success", "ignored")

		require.Len(t, eventRepo.Events, 2)
		assert.Equal(t, "Reboot failed on host: disk full", eventRepo.Events[0].Message)
		assert.Equal(t, models.SeverityWarning, eventRepo.Events[0].Severity)
		assert.Equal(t, "Reboot completed on host", eventRepo.Events[1].Message)
		assert.Equal(t, models.SeverityInfo, eventRepo.Events[1].Severity)
	})
}

// [PATCH-FLOW-001 AC-037] The command-sent event shows when the operator
// authorized a reboot with the patch.
func TestCommandSent_PF_AC037_EventMentionsAuthorization(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		params  map[string]interface{}
		wantMsg string
	}{
		{"authorized os_update", "os_update", map[string]interface{}{"mode": "full", "reboot_if_required": true},
			"OS update initiated on gpu-host (reboot authorized if required)"},
		{"unauthorized os_update", "os_update", map[string]interface{}{"mode": "full"},
			"OS update initiated on gpu-host"},
		{"explicit false", "os_update", map[string]interface{}{"reboot_if_required": false},
			"OS update initiated on gpu-host"},
		{"non-boolean value", "os_update", map[string]interface{}{"reboot_if_required": "true"},
			"OS update initiated on gpu-host"},
		{"other action never mentions it", "fetch_logs", map[string]interface{}{"reboot_if_required": true},
			"Log fetch initiated on gpu-host"},
	}
	for _, tc := range cases {
		t.Run("[AC-037] "+tc.name, func(t *testing.T) {
			gen, eventRepo, _, _ := setupGenerator(t)

			gen.CommandSent(context.Background(), "dev-1", "gpu-host", tc.action, tc.params)

			require.Len(t, eventRepo.Events, 1)
			assert.Equal(t, models.EventCommandSent, eventRepo.Events[0].Type)
			assert.Equal(t, tc.wantMsg, eventRepo.Events[0].Message)
		})
	}
}

// [PATCH-FLOW-001 AC-020] An agent that vetoes the authorized reboot reports
// reboot_required afterwards; the existing PATCH-GATE event still fires once.
func TestPatchGateEvent_PF_AC020_RebootRequiredEventFires(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(false))
	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true, "kernel 7.2.9+deb13-amd64 installed; running 6.12.48+deb13-amd64"))
	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true))

	require.Len(t, eventRepo.Events, 1, "[AC-020] exactly one reboot_required event per transition")
	assert.Equal(t, models.EventRebootRequired, eventRepo.Events[0].Type)
	assert.Contains(t, eventRepo.Events[0].Message, "kernel 7.2.9+deb13-amd64 installed", "the kernel reason reaches the event")
}
