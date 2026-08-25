package events

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func rebootRequiredInfo(required bool, reasons ...string) *models.UpdateInfo {
	return &models.UpdateInfo{
		RebootRequired:        required,
		RebootRequiredReasons: reasons,
	}
}

// [AC-019] Exactly one reboot-required event per false→true transition.
func TestCheckRebootRequired_AC019_OneEventPerTransition(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(false))
	assert.Empty(t, eventRepo.Events, "[AC-019] no event while state is false")

	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true, "linux-image-6.8.0-45-generic"))
	require.Len(t, eventRepo.Events, 1, "[AC-019] exactly one event on false→true")
	assert.Equal(t, models.EventRebootRequired, eventRepo.Events[0].Type)
	assert.Equal(t, models.SeverityWarning, eventRepo.Events[0].Severity)
	assert.Contains(t, eventRepo.Events[0].Message, "gpu-host")
	assert.Contains(t, eventRepo.Events[0].Message, "linux-image-6.8.0-45-generic")
}

// [AC-019] No further events while the state remains continuously true.
func TestCheckRebootRequired_AC019_NoDuplicateWhileTruePersists(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true))
	for i := 0; i < 3; i++ {
		gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true))
	}

	assert.Len(t, eventRepo.Events, 1, "[AC-019] continuously-true state emits exactly one event")
}

// [AC-019] After the state clears (post-reboot) and becomes true again, a
// new event fires.
func TestCheckRebootRequired_AC019_RefiresAfterClear(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true))
	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(false)) // host rebooted
	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true))  // new kernel pending again

	assert.Len(t, eventRepo.Events, 2, "[AC-019] a fresh transition after clear emits a new event")
}

// [AC-019] With a matching rule the event goes through notification
// fan-out; the rule's severity is applied.
func TestCheckRebootRequired_AC019_NotificationEligibleWithRule(t *testing.T) {
	gen, eventRepo, alertRuleRepo, dispatcher := setupGenerator(t)
	ctx := context.Background()

	alertRuleRepo.Rules = []models.AlertRule{{
		ID:              7,
		Enabled:         true,
		Metric:          "reboot_required",
		Operator:        "==",
		Threshold:       1,
		Severity:        "warning",
		CooldownSeconds: 86400,
		Notify:          true,
	}}

	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true))

	require.Len(t, eventRepo.Events, 1)
	require.Len(t, dispatcher.Alerts, 1, "[AC-019] event is eligible for notification fan-out")
	assert.Equal(t, "dev-1", dispatcher.Alerts[0].DeviceID)
}

// [AC-019] Without a matching rule the event still lands in the event log
// (no notification).
func TestCheckRebootRequired_AC019_EventLogOnlyWithoutRule(t *testing.T) {
	gen, eventRepo, _, dispatcher := setupGenerator(t)
	ctx := context.Background()

	gen.CheckRebootRequired(ctx, "dev-1", "gpu-host", rebootRequiredInfo(true))

	assert.Len(t, eventRepo.Events, 1)
	assert.Empty(t, dispatcher.Alerts, "no rule ⇒ event log only")
}

// [AC-019] Per-device isolation: one device's state does not suppress another's.
func TestCheckRebootRequired_AC019_PerDeviceTracking(t *testing.T) {
	gen, eventRepo, _, _ := setupGenerator(t)
	ctx := context.Background()

	gen.CheckRebootRequired(ctx, "dev-1", "host-a", rebootRequiredInfo(true))
	gen.CheckRebootRequired(ctx, "dev-2", "host-b", rebootRequiredInfo(true))

	assert.Len(t, eventRepo.Events, 2, "each device gets its own transition event")
}

// [AC-019 / AD-011] The reboot_required alert template is present with the
// specified shape.
func TestAlertTemplates_RebootRequiredPresent(t *testing.T) {
	var tpl *models.AlertTemplate
	for i, template := range AlertTemplates() {
		if template.ID == "reboot_required" {
			tpl = &AlertTemplates()[i]
			break
		}
	}
	require.NotNil(t, tpl, "reboot_required template must exist")
	assert.Equal(t, "Reboot Required", tpl.Name)
	assert.Equal(t, "system", tpl.Category)
	assert.Equal(t, "reboot_required", tpl.Metric)
	assert.Equal(t, "==", tpl.Operator)
	assert.Equal(t, float64(1), tpl.Threshold)
	assert.Equal(t, "warning", tpl.Severity)
	assert.Equal(t, 86400, tpl.CooldownSeconds)
	assert.False(t, tpl.NeedsTargetName)
}
