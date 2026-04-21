package events

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

// authFailureRule returns an enabled alert rule for failed_logins_interval.
func authFailureRule(id int64, operator string, threshold float64) models.AlertRule {
	return models.AlertRule{
		ID:              id,
		Enabled:         true,
		Metric:          "failed_logins_interval",
		Operator:        operator,
		Threshold:       threshold,
		Severity:        "warning",
		CooldownSeconds: 300,
	}
}

func newAuthFailureGenerator(rules []models.AlertRule) (*Generator, *mockEventRepo) {
	eventRepo := &mockEventRepo{}
	alertRepo := &mockAlertRuleRepo{rules: rules}
	hub := websocket.NewHub()
	go hub.Run()
	return NewGenerator(eventRepo, hub, alertRepo, nil, nil), eventRepo
}

// [AC-001, AC-024] CheckTelemetryThresholds fires an event when FailedLoginsInterval > 0
// and a matching rule exists.
func TestAC001_AC024_CheckTelemetryThresholds_AuthFailure_FiresWhenAboveThreshold(t *testing.T) {
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{
		authFailureRule(1, ">", 0),
	})

	v := 3
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{
			FailedLoginsInterval: &v,
		},
	}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	require.Len(t, eventRepo.events, 1, "[AC-001/AC-024] exactly one event must fire")
	e := eventRepo.events[0]
	assert.Equal(t, models.EventAuthFailure, e.Type, "[AC-001] event type must be auth_failure")
	assert.Contains(t, e.Message, "authentication failure",
		"[AC-024] event message must mention authentication failure")
	assert.Contains(t, e.Message, "test-host",
		"[AC-024] event message must identify the hostname")
}

// [AC-008] CheckTelemetryThresholds does NOT fire when FailedLoginsInterval is nil.
func TestAC008_CheckTelemetryThresholds_NilFailedLoginsInterval_NoEvent(t *testing.T) {
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{
		authFailureRule(1, ">", 0),
	})

	// nil pointer — non-Linux agent omits the field.
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{
			FailedLoginsInterval: nil,
		},
	}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	assert.Empty(t, eventRepo.events,
		"[AC-008] nil FailedLoginsInterval must not fire any event even with a matching rule")
}

// [AC-008] CheckTelemetryThresholds does NOT fire when Security is nil entirely.
func TestAC008_CheckTelemetryThresholds_NilSecurity_NoEvent(t *testing.T) {
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{
		authFailureRule(1, ">", 0),
	})

	data := &models.FullTelemetryData{Security: nil}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	assert.Empty(t, eventRepo.events,
		"[AC-008] nil Security must not fire any event")
}

// [AC-024] Cooldown prevents a second event on a repeated snapshot within the cooldown window.
func TestAC024_CheckTelemetryThresholds_AuthFailure_CooldownPreventsRepeat(t *testing.T) {
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{
		authFailureRule(1, ">", 0),
	})

	v := 2
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{FailedLoginsInterval: &v},
	}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)
	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	require.Len(t, eventRepo.events, 1,
		"[AC-024] cooldown must prevent a second event within the cooldown window")
}

// [AC-024] Device-scope filtering: a rule that excludes the device does not fire.
func TestAC024_CheckTelemetryThresholds_AuthFailure_DeviceScopeExcludes(t *testing.T) {
	rule := authFailureRule(1, ">", 0)
	rule.IncludeDevices = "device-2" // device-1 is not in scope
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{rule})

	v := 5
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{FailedLoginsInterval: &v},
	}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	assert.Empty(t, eventRepo.events,
		"[AC-024] device excluded by scope filter must not fire an event")
}

// [SEC-006] Negative FailedLoginsInterval is clamped to 0 before evaluation.
// A rule with operator ">" threshold 0 must NOT fire on a clamped-to-zero value.
func TestSEC006_CheckTelemetryThresholds_NegativeValueClamped_RuleGTZeroDoesNotFire(t *testing.T) {
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{
		authFailureRule(1, ">", 0),
	})

	v := -5
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{FailedLoginsInterval: &v},
	}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	assert.Empty(t, eventRepo.events,
		"[SEC-006] negative FailedLoginsInterval must be clamped to 0; rule '> 0' must not fire on 0")
}

// [SEC-006] Negative FailedLoginsInterval is clamped to 0; a rule with "<" operator
// and threshold 0 must also NOT fire (clamped value 0 is not < 0).
func TestSEC006_CheckTelemetryThresholds_NegativeValueClamped_RuleLTZeroDoesNotFire(t *testing.T) {
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{
		{
			ID: 1, Enabled: true, Metric: "failed_logins_interval",
			Operator: "<", Threshold: 0, Severity: "warning", CooldownSeconds: 300,
		},
	})

	v := -5
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{FailedLoginsInterval: &v},
	}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	assert.Empty(t, eventRepo.events,
		"[SEC-006] negative value clamped to 0; rule '< 0' must not fire on 0")
}

// [AC-001] CheckTelemetryThresholds does NOT fire when value equals threshold (operator ">").
func TestAC001_CheckTelemetryThresholds_AuthFailure_ValueEqualsThreshold_NoFire(t *testing.T) {
	gen, eventRepo := newAuthFailureGenerator([]models.AlertRule{
		authFailureRule(1, ">", 0),
	})

	zero := 0
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{FailedLoginsInterval: &zero},
	}

	gen.CheckTelemetryThresholds(context.Background(), "device-1", "test-host", data)

	assert.Empty(t, eventRepo.events,
		"[AC-001] value == threshold with operator '>' must not fire (strict greater-than)")
}
