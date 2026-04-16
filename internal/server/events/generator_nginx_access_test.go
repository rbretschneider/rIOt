package events

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

// newNginxGen creates a Generator with the given alert rules for nginx tests.
func newNginxGen(t *testing.T, rules []models.AlertRule) (*Generator, *mockEventRepo) {
	t.Helper()
	eventRepo := &mockEventRepo{}
	alertRepo := &mockAlertRuleRepo{rules: rules}
	hub := websocket.NewHub()
	go hub.Run()
	return NewGenerator(eventRepo, hub, alertRepo, nil, nil), eventRepo
}

// nginxWS builds a WebServerInfo with a single nginx ProxyServer carrying
// the given access metrics.
func nginxWS(m models.NginxAccessMetrics) *models.WebServerInfo {
	return &models.WebServerInfo{
		Servers: []models.ProxyServer{
			{
				Name:          "nginx",
				AccessMetrics: &m,
			},
		},
	}
}

// ---- AC-008: alert fires when 5xx count exceeds threshold ----

func TestCheckNginxAccessAlerts_AC008_5xxRuleFires(t *testing.T) {
	// [AC-008] Device reports status_5xx=15, rule: nginx_5xx_count > 10 → event created.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_5xx_count", Operator: ">", Threshold: 10,
			Severity: "critical", CooldownSeconds: 300},
	}
	gen, eventRepo := newNginxGen(t, rules)

	ws := nginxWS(models.NginxAccessMetrics{TotalRequests: 100, Status5xx: 15})
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)

	require.Len(t, eventRepo.events, 1, "[AC-008] exactly one event must be created")
	assert.Equal(t, models.EventNginx5xxHigh, eventRepo.events[0].Type,
		"[AC-008] event type must be nginx_5xx_high")
}

// ---- AC-009: no rule configured → no event ----

func TestCheckNginxAccessAlerts_AC009_NoRules_NoEvent(t *testing.T) {
	// [AC-009] status_5xx=15, no rules → no event (no hardcoded fallback).
	gen, eventRepo := newNginxGen(t, nil)

	ws := nginxWS(models.NginxAccessMetrics{TotalRequests: 100, Status5xx: 15})
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)

	assert.Empty(t, eventRepo.events, "[AC-009] no event when no rules are configured for nginx metrics")
}

// ---- AC-010: cooldown suppresses second alert ----

func TestCheckNginxAccessAlerts_AC010_CooldownSuppressesSecondAlert(t *testing.T) {
	// [AC-010] Rule fires on first evaluation; second evaluation within cooldown is suppressed.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_5xx_count", Operator: ">", Threshold: 10,
			Severity: "critical", CooldownSeconds: 300, IncludeDevices: "webserver-01"},
	}
	gen, eventRepo := newNginxGen(t, rules)

	ws := nginxWS(models.NginxAccessMetrics{Status5xx: 20})

	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)
	require.Len(t, eventRepo.events, 1, "[AC-010] first evaluation must create event")

	// Second evaluation within cooldown window — same rule key
	ws2 := nginxWS(models.NginxAccessMetrics{Status5xx: 25})
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws2)
	assert.Len(t, eventRepo.events, 1, "[AC-010] second evaluation within cooldown must NOT create another event")
}

// ---- AC-011: exclude_devices suppresses event ----

func TestCheckNginxAccessAlerts_AC011_ExcludeDevices_NoEvent(t *testing.T) {
	// [AC-011] Rule excludes "staging-box"; device "staging-box" reports 5xx=100 → no event.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_5xx_count", Operator: ">", Threshold: 10,
			Severity: "critical", CooldownSeconds: 300, ExcludeDevices: "staging-box"},
	}
	gen, eventRepo := newNginxGen(t, rules)

	ws := nginxWS(models.NginxAccessMetrics{Status5xx: 100})
	gen.CheckNginxAccessAlerts(context.Background(), "device-staging", "staging-box", ws)

	assert.Empty(t, eventRepo.events,
		"[AC-011] no event must be created for device excluded by exclude_devices")
}

// ---- AC-012: event type and message format ----

func TestCheckNginxAccessAlerts_AC012_EventTypeAndMessage(t *testing.T) {
	// [AC-012] Event type is nginx_5xx_high; message contains the count and hostname.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_5xx_count", Operator: ">", Threshold: 10,
			Severity: "critical", CooldownSeconds: 300},
	}
	gen, eventRepo := newNginxGen(t, rules)

	ws := nginxWS(models.NginxAccessMetrics{Status5xx: 15})
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)

	require.Len(t, eventRepo.events, 1)
	e := eventRepo.events[0]
	assert.Equal(t, models.EventNginx5xxHigh, e.Type, "[AC-012] event type must be nginx_5xx_high")
	assert.Contains(t, e.Message, "15", "[AC-012] event message must contain the actual count value")
	assert.Contains(t, e.Message, "webserver-01", "[AC-012] event message must contain the device hostname")
}

// ---- AC-012 (4xx variant) ----

func TestCheckNginxAccessAlerts_AC012_4xxEventTypeAndMessage(t *testing.T) {
	// [AC-012] Event for 4xx rule: type nginx_4xx_high, message contains count and hostname.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_4xx_count", Operator: ">", Threshold: 20,
			Severity: "warning", CooldownSeconds: 300},
	}
	gen, eventRepo := newNginxGen(t, rules)

	ws := nginxWS(models.NginxAccessMetrics{Status4xx: 25})
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)

	require.Len(t, eventRepo.events, 1)
	e := eventRepo.events[0]
	assert.Equal(t, models.EventNginx4xxHigh, e.Type, "[AC-012] event type must be nginx_4xx_high")
	assert.Contains(t, e.Message, "25", "[AC-012] 4xx event message must contain the count")
	assert.Contains(t, e.Message, "webserver-01", "[AC-012] 4xx event message must contain hostname")
}

// ---- nginx_request_count alert ----

func TestCheckNginxAccessAlerts_RequestCountAlertFires(t *testing.T) {
	// nginx_request_count: verify the third metric is also evaluated.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_request_count", Operator: ">", Threshold: 500,
			Severity: "info", CooldownSeconds: 900},
	}
	gen, eventRepo := newNginxGen(t, rules)

	ws := nginxWS(models.NginxAccessMetrics{TotalRequests: 1000})
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)

	require.Len(t, eventRepo.events, 1)
	assert.Equal(t, models.EventNginxRequestHigh, eventRepo.events[0].Type,
		"nginx_request_count alert must use EventNginxRequestHigh event type")
	assert.Contains(t, eventRepo.events[0].Message, "1000")
	assert.Contains(t, eventRepo.events[0].Message, "webserver-01")
}

// ---- BR-005: nil AccessMetrics is silently skipped ----

func TestCheckNginxAccessAlerts_NilAccessMetrics_Skipped(t *testing.T) {
	// [BR-005] Device has nginx running but no access log configured → AccessMetrics is nil.
	// No event should be generated.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_5xx_count", Operator: ">", Threshold: 1,
			Severity: "critical", CooldownSeconds: 300},
	}
	gen, eventRepo := newNginxGen(t, rules)

	ws := &models.WebServerInfo{
		Servers: []models.ProxyServer{
			{Name: "nginx", AccessMetrics: nil},
		},
	}
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)

	assert.Empty(t, eventRepo.events,
		"[BR-005] no event when AccessMetrics is nil (device has no access log configured)")
}

// ---- SEC-005: server cap on proxy server iteration ----

func TestCheckNginxAccessAlerts_SEC005_ProxyServerCap(t *testing.T) {
	// [SEC-005] A compromised agent sending many proxy server entries must be capped.
	// We generate 20 servers (> maxNginxProxyServers=16) and verify the cap is applied.
	rules := []models.AlertRule{
		{ID: 1, Enabled: true, Metric: "nginx_5xx_count", Operator: ">", Threshold: 0,
			Severity: "critical", CooldownSeconds: 0},
	}
	gen, eventRepo := newNginxGen(t, rules)

	const totalServers = 20
	servers := make([]models.ProxyServer, totalServers)
	for i := range servers {
		servers[i] = models.ProxyServer{
			Name:          fmt.Sprintf("nginx-%d", i),
			AccessMetrics: &models.NginxAccessMetrics{Status5xx: 1, TotalRequests: 1},
		}
	}
	ws := &models.WebServerInfo{Servers: servers}

	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "host", ws)

	// With cap=16, at most 16 servers are evaluated; each fires one event (cooldown=0).
	// We should have at most 16 events, not 20.
	assert.LessOrEqual(t, len(eventRepo.events), 16,
		"[SEC-005] proxy server iteration must be capped at maxNginxProxyServers (16)")
}

// ---- AC-013: templates include nginx entries ----

func TestAlertTemplates_AC013_NginxTemplatesPresent(t *testing.T) {
	// [AC-013] AlertTemplates() must include nginx_5xx_high and nginx_4xx_high
	// in the "webserver" category.
	templates := AlertTemplates()

	var has5xx, has4xx bool
	for _, tmpl := range templates {
		if tmpl.ID == "nginx_5xx_high" {
			has5xx = true
			assert.Equal(t, "webserver", tmpl.Category, "[AC-013] nginx_5xx_high template must be in webserver category")
			assert.Equal(t, "nginx_5xx_count", tmpl.Metric, "[AC-013] nginx_5xx_high template must use nginx_5xx_count metric")
			assert.Equal(t, ">", tmpl.Operator)
			assert.Equal(t, float64(10), tmpl.Threshold)
		}
		if tmpl.ID == "nginx_4xx_high" {
			has4xx = true
			assert.Equal(t, "webserver", tmpl.Category, "[AC-013] nginx_4xx_high template must be in webserver category")
			assert.Equal(t, "nginx_4xx_count", tmpl.Metric, "[AC-013] nginx_4xx_high template must use nginx_4xx_count metric")
			assert.Equal(t, ">", tmpl.Operator)
			assert.Equal(t, float64(50), tmpl.Threshold)
		}
	}

	assert.True(t, has5xx, "[AC-013] nginx_5xx_high template must be present in AlertTemplates()")
	assert.True(t, has4xx, "[AC-013] nginx_4xx_high template must be present in AlertTemplates()")
}

// ---- FR-018: no hardcoded fallback for nginx metrics ----

func TestEvaluateMetric_FR018_NoNginxFallback(t *testing.T) {
	// [FR-018] When no rules exist for nginx metrics and the value exceeds any
	// reasonable threshold, no event must be created (unlike mem_percent / disk_percent
	// which have hardcoded fallbacks).
	gen, eventRepo := newNginxGen(t, nil) // no rules

	// Extremely high 5xx count — would fire if there were a fallback
	ws := nginxWS(models.NginxAccessMetrics{Status5xx: 9999, TotalRequests: 9999})
	gen.CheckNginxAccessAlerts(context.Background(), "device-1", "webserver-01", ws)

	assert.Empty(t, eventRepo.events,
		"[FR-018] nginx metrics must never fire without an explicit user-configured rule (no hardcoded fallback)")
}
