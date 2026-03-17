package events

import (
	"context"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGenerator(t *testing.T) (*Generator, *testutil.MockEventRepo, *testutil.MockAlertRuleRepo, *testutil.MockDispatcher) {
	t.Helper()
	eventRepo := testutil.NewMockEventRepo()
	alertRuleRepo := testutil.NewMockAlertRuleRepo()
	dispatcher := testutil.NewMockDispatcher()
	commandRepo := testutil.NewMockCommandRepo()
	hub := websocket.NewHub()
	go hub.Run()

	gen := NewGenerator(eventRepo, hub, alertRuleRepo, dispatcher, commandRepo)
	return gen, eventRepo, alertRuleRepo, dispatcher
}

func TestDeviceOnline(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.DeviceOnline(ctx, "dev-1", "test-host")

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventDeviceOnline, eventRepo.Events[0].Type)
	assert.Contains(t, eventRepo.Events[0].Message, "test-host")
}

func TestDeviceOffline_NoRule(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.DeviceOffline(ctx, "dev-1", "test-host")

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventDeviceOffline, eventRepo.Events[0].Type)
	assert.Equal(t, models.SeverityWarning, eventRepo.Events[0].Severity)
}

func TestDeviceOffline_WithRule(t *testing.T) {
	gen, eventRepo, alertRuleRepo, dispatcher := setupGenerator(t)
	ctx := context.Background()

	alertRuleRepo.Rules = []models.AlertRule{{
		ID:              1,
		Enabled:         true,
		Metric:          "device_offline",
		Operator:        ">",
		Threshold:       0,
		Severity:        "critical",
		CooldownSeconds: 300,
		Notify:          true,
	}}

	gen.DeviceOffline(ctx, "dev-1", "test-host")

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventSeverity("critical"), eventRepo.Events[0].Severity)
	require.Len(t, dispatcher.Alerts, 1)
	assert.Equal(t, "dev-1", dispatcher.Alerts[0].DeviceID)
}

func TestDeviceOffline_CooldownDedup(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.DeviceOffline(ctx, "dev-1", "test-host")
	gen.DeviceOffline(ctx, "dev-1", "test-host") // should be suppressed

	assert.Len(t, eventRepo.Events, 1, "second call should be suppressed by cooldown")
}

func TestCheckHeartbeatThresholds_HighMemory(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	data := &models.HeartbeatData{MemPercent: 95.0}
	gen.CheckHeartbeatThresholds(ctx, "dev-1", "test-host", data)

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventMemHigh, eventRepo.Events[0].Type)
}

func TestCheckHeartbeatThresholds_Normal(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	data := &models.HeartbeatData{MemPercent: 50.0, DiskRootPercent: 40.0}
	gen.CheckHeartbeatThresholds(ctx, "dev-1", "test-host", data)

	assert.Empty(t, eventRepo.Events, "no events for normal values")
}

func TestCheckHeartbeatThresholds_WithRule(t *testing.T) {
	gen, eventRepo, alertRuleRepo, dispatcher := setupGenerator(t)
	ctx := context.Background()

	alertRuleRepo.Rules = []models.AlertRule{{
		ID:              1,
		Enabled:         true,
		Metric:          "mem_percent",
		Operator:        ">",
		Threshold:       80,
		Severity:        "warning",
		CooldownSeconds: 60,
		Notify:          true,
	}}

	data := &models.HeartbeatData{MemPercent: 85.0}
	gen.CheckHeartbeatThresholds(ctx, "dev-1", "test-host", data)

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventSeverity("warning"), eventRepo.Events[0].Severity)
	require.Len(t, dispatcher.Alerts, 1)
}

func TestCheckHeartbeatThresholds_DeviceFilter(t *testing.T) {
	gen, eventRepo, alertRuleRepo, _ := setupGenerator(t)
	ctx := context.Background()

	alertRuleRepo.Rules = []models.AlertRule{{
		ID:           1,
		Enabled:      true,
		Metric:       "mem_percent",
		Operator:     ">",
		Threshold:    80,
		Severity:     "warning",
		IncludeDevices: "dev-2,dev-3", // only applies to dev-2 and dev-3
		CooldownSeconds: 60,
	}}

	data := &models.HeartbeatData{MemPercent: 95.0}
	gen.CheckHeartbeatThresholds(ctx, "dev-1", "test-host", data)

	// Rule exists for mem_percent but doesn't include dev-1 — no fallback
	// should fire because user-configured rules take precedence over defaults.
	require.Len(t, eventRepo.Events, 0)
}

func TestCheckDockerEvent_Die(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	evt := &models.DockerEvent{
		ContainerName: "web-app",
		Action:        "die",
	}
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",evt)

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventContainerDied, eventRepo.Events[0].Type)
	assert.Equal(t, models.SeverityWarning, eventRepo.Events[0].Severity)
}

func TestCheckDockerEvent_DieDowngradedDuringUpdate(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	// Simulate an update in progress
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",&models.DockerEvent{
		ContainerName: "bookstack stack",
		Action:        "update_started",
	})

	// Container dies during the update — should be info, not warning
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",&models.DockerEvent{
		ContainerName: "bookstack_db",
		Action:        "die",
	})

	require.Len(t, eventRepo.Events, 2) // update_started + die
	dieEvt := eventRepo.Events[1]
	assert.Equal(t, models.EventContainerDied, dieEvt.Type)
	assert.Equal(t, models.SeverityInfo, dieEvt.Severity)
	assert.Contains(t, dieEvt.Message, "update in progress")
}

func TestCheckDockerEvent_DieWarningAfterUpdateCompletes(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	// Start and complete an update
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",&models.DockerEvent{
		ContainerName: "bookstack stack",
		Action:        "update_started",
	})
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",&models.DockerEvent{
		ContainerName: "bookstack stack",
		Action:        "update_completed",
	})

	// Container dies after update finished — should be warning
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",&models.DockerEvent{
		ContainerName: "bookstack_db",
		Action:        "die",
	})

	require.Len(t, eventRepo.Events, 3)
	dieEvt := eventRepo.Events[2]
	assert.Equal(t, models.EventContainerDied, dieEvt.Type)
	assert.Equal(t, models.SeverityWarning, dieEvt.Severity)
}

func TestCheckDockerEvent_DieNoNotifyDuringUpdate(t *testing.T) {
	gen, eventRepo, alertRuleRepo, dispatcher := setupGenerator(t)
	ctx := context.Background()

	alertRuleRepo.Rules = []models.AlertRule{{
		ID:              1,
		Enabled:         true,
		Metric:          "container_died",
		Operator:        "==",
		Threshold:       1,
		Severity:        "warning",
		CooldownSeconds: 900,
		Notify:          true,
	}}

	// Update in progress
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",&models.DockerEvent{
		ContainerName: "myapp",
		Action:        "update_started",
	})

	// Die during update — should NOT dispatch notification
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",&models.DockerEvent{
		ContainerName: "myapp_web",
		Action:        "die",
	})

	require.Len(t, eventRepo.Events, 2)
	assert.Equal(t, models.SeverityInfo, eventRepo.Events[1].Severity)
	assert.Empty(t, dispatcher.Alerts, "no notification should be dispatched during update")
}

func TestCheckDockerEvent_OOM(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	evt := &models.DockerEvent{
		ContainerName: "memory-hog",
		Action:        "oom",
	}
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",evt)

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventContainerOOM, eventRepo.Events[0].Type)
	assert.Equal(t, models.SeverityCrit, eventRepo.Events[0].Severity)
}

func TestCheckDockerEvent_Start(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	evt := &models.DockerEvent{
		ContainerName: "web-app",
		Action:        "start",
		Image:         "nginx:latest",
	}
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",evt)

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventContainerStart, eventRepo.Events[0].Type)
}

func TestCheckDockerEvent_Stop(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	evt := &models.DockerEvent{
		ContainerName: "web-app",
		Action:        "stop",
	}
	gen.CheckDockerEvent(ctx, "dev-1", "test-host",evt)

	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.EventContainerStop, eventRepo.Events[0].Type)
}

func TestOnCooldown(t *testing.T) {
	gen, _, _, _ := setupGenerator(t)

	// First call: not on cooldown
	assert.False(t, gen.onCooldown("key1", 1*time.Hour))

	// Second call: on cooldown
	assert.True(t, gen.onCooldown("key1", 1*time.Hour))

	// Different key: not on cooldown
	assert.False(t, gen.onCooldown("key2", 1*time.Hour))
}
