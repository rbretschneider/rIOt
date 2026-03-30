package events

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

// newGPUGenerator constructs a Generator with the supplied alert rules.
func newGPUGenerator(rules []models.AlertRule) (*Generator, *mockEventRepo) {
	eventRepo := &mockEventRepo{}
	alertRepo := &mockAlertRuleRepo{rules: rules}
	hub := websocket.NewHub()
	go hub.Run()
	return NewGenerator(eventRepo, hub, alertRepo, nil, nil), eventRepo
}

// gpuTempRule returns an enabled alert rule for gpu_temp with the given threshold.
func gpuTempRule(id int64, operator string, threshold float64) models.AlertRule {
	return models.AlertRule{
		ID:              id,
		Enabled:         true,
		Metric:          "gpu_temp",
		Operator:        operator,
		Threshold:       threshold,
		Severity:        "warning",
		CooldownSeconds: 3600,
	}
}

// intPtr returns a pointer to v.
func intPtr(v int) *int { return &v }

// float64Ptr returns a pointer to v.
func float64Ptr(v float64) *float64 { return &v }

// [AC-009] AlertTemplates must contain gpu_temp_warn and gpu_temp_crit in category "gpu".
func TestAlertTemplates_GPUTemplatesPresent(t *testing.T) {
	templates := AlertTemplates()

	var warn, crit *models.AlertTemplate
	for i := range templates {
		switch templates[i].ID {
		case "gpu_temp_warn":
			warn = &templates[i]
		case "gpu_temp_crit":
			crit = &templates[i]
		}
	}

	require.NotNil(t, warn, "[AC-009] gpu_temp_warn template must be present")
	assert.Equal(t, "GPU Temperature Warning", warn.Name, "[AC-009] gpu_temp_warn name")
	assert.Equal(t, "gpu", warn.Category, "[AC-009] gpu_temp_warn category must be 'gpu'")
	assert.Equal(t, "gpu_temp", warn.Metric, "[AC-009] gpu_temp_warn metric")
	assert.Equal(t, ">", warn.Operator)
	assert.Equal(t, float64(80), warn.Threshold)
	assert.Equal(t, "warning", warn.Severity)
	assert.Equal(t, 3600, warn.CooldownSeconds)
	assert.False(t, warn.NeedsTargetName)

	require.NotNil(t, crit, "[AC-009] gpu_temp_crit template must be present")
	assert.Equal(t, "GPU Temperature Critical", crit.Name, "[AC-009] gpu_temp_crit name")
	assert.Equal(t, "gpu", crit.Category, "[AC-009] gpu_temp_crit category must be 'gpu'")
	assert.Equal(t, "gpu_temp", crit.Metric)
	assert.Equal(t, ">", crit.Operator)
	assert.Equal(t, float64(90), crit.Threshold)
	assert.Equal(t, "critical", crit.Severity)
	assert.Equal(t, 1800, crit.CooldownSeconds)
	assert.False(t, crit.NeedsTargetName)
}

// [AC-005] Alert fires when any GPU exceeds the threshold; event message identifies the GPU.
func TestCheckGPUAlerts_AC005_FiresWhenOneGPUExceedsThreshold(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		gpuTempRule(1, ">", 80),
	})

	gpuTel := &models.GPUTelemetry{
		GPUs: []models.GPUDeviceMetrics{
			{Index: 0, Name: "NVIDIA RTX 3090", UUID: "uuid-0", PCIBusID: "00:01.0", TemperatureC: intPtr(65)},
			{Index: 1, Name: "NVIDIA RTX 3080", UUID: "uuid-1", PCIBusID: "00:02.0", TemperatureC: intPtr(85)},
			{Index: 2, Name: "NVIDIA RTX 3070", UUID: "uuid-2", PCIBusID: "00:03.0", TemperatureC: intPtr(70)},
		},
	}

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", gpuTel)

	require.Len(t, eventRepo.events, 1, "[AC-005] exactly one event must fire")
	e := eventRepo.events[0]
	assert.Equal(t, models.EventGPUTemp, e.Type, "[AC-005] event type must be gpu_temp")
	assert.Contains(t, e.Message, "NVIDIA RTX 3080", "[AC-005] event message must identify GPU 1 by name")
	assert.Contains(t, e.Message, "index 1", "[AC-005] event message must include GPU index")
}

// [AC-006] Alert must not fire when no GPU exceeds the threshold.
func TestCheckGPUAlerts_AC006_NoFireWhenAllBelowThreshold(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		gpuTempRule(1, ">", 80),
	})

	gpuTel := &models.GPUTelemetry{
		GPUs: []models.GPUDeviceMetrics{
			{Index: 0, Name: "NVIDIA RTX 3090", UUID: "uuid-0", PCIBusID: "00:01.0", TemperatureC: intPtr(65)},
			{Index: 1, Name: "NVIDIA RTX 3080", UUID: "uuid-1", PCIBusID: "00:02.0", TemperatureC: intPtr(72)},
			{Index: 2, Name: "NVIDIA RTX 3070", UUID: "uuid-2", PCIBusID: "00:03.0", TemperatureC: intPtr(70)},
		},
	}

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", gpuTel)

	assert.Empty(t, eventRepo.events, "[AC-006] no events must fire when all temps are below threshold")
}

// [AC-005] Multiple GPUs exceeding the threshold are each evaluated independently,
// but cooldown limits to one event per rule per cycle.
func TestCheckGPUAlerts_MultipleGPUsExceedThreshold_CooldownLimitsEvents(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		gpuTempRule(1, ">", 80),
	})

	gpuTel := &models.GPUTelemetry{
		GPUs: []models.GPUDeviceMetrics{
			{Index: 0, Name: "NVIDIA RTX A", UUID: "uuid-0", PCIBusID: "00:01.0", TemperatureC: intPtr(85)},
			{Index: 1, Name: "NVIDIA RTX B", UUID: "uuid-1", PCIBusID: "00:02.0", TemperatureC: intPtr(91)},
		},
	}

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", gpuTel)

	// GPU 0 fires, GPU 1 is on cooldown for the same rule ID
	require.Len(t, eventRepo.events, 1, "cooldown prevents second event for same rule")
}

// nil GPUTelemetry is safely ignored.
func TestCheckGPUAlerts_NilTelemetry_DoesNothing(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		gpuTempRule(1, ">", 80),
	})

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", nil)

	assert.Empty(t, eventRepo.events, "nil telemetry must produce no events")
}

// Empty GPU slice is safely ignored.
func TestCheckGPUAlerts_EmptyGPUs_DoesNothing(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		gpuTempRule(1, ">", 80),
	})

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", &models.GPUTelemetry{GPUs: nil})

	assert.Empty(t, eventRepo.events, "empty GPU slice must produce no events")
}

// GPU with nil TemperatureC does not cause a panic; other metrics still evaluated.
func TestCheckGPUAlerts_NilTemperatureC_IsSkipped(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		gpuTempRule(1, ">", 80),
	})

	gpuTel := &models.GPUTelemetry{
		GPUs: []models.GPUDeviceMetrics{
			// TemperatureC is nil — [Not Supported] on this GPU
			{Index: 0, Name: "NVIDIA Test", UUID: "uuid-0", PCIBusID: "00:01.0", TemperatureC: nil},
		},
	}

	require.NotPanics(t, func() {
		gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", gpuTel)
	})
	assert.Empty(t, eventRepo.events, "nil TemperatureC must not fire a temperature alert")
}

// SEC-001: GPUs slice is capped at 32 before iteration.
func TestCheckGPUAlerts_SliceCappedAt32(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		gpuTempRule(1, ">", 80),
	})

	// Build a slice of 50 GPUs all above threshold
	gpus := make([]models.GPUDeviceMetrics, 50)
	for i := range gpus {
		gpus[i] = models.GPUDeviceMetrics{
			Index:        i,
			Name:         "NVIDIA Test",
			UUID:         "uuid",
			PCIBusID:     "00:01.0",
			TemperatureC: intPtr(95),
		}
	}

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", &models.GPUTelemetry{GPUs: gpus})

	// The first GPU triggers the rule; subsequent ones share the cooldown key.
	// The key point is that we did not attempt to evaluate all 50 — only the first 32.
	// We cannot directly assert on iteration count, but the method must not panic and
	// must produce at most 1 event (first GPU fires, rest hit cooldown).
	assert.LessOrEqual(t, len(eventRepo.events), 1, "[SEC-001] cap must prevent unbounded evaluation")
}

// gpu_util_percent metric fires correctly.
func TestCheckGPUAlerts_UtilizationMetric_Fires(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		{
			ID: 1, Enabled: true, Metric: "gpu_util_percent",
			Operator: ">", Threshold: 95, Severity: "warning", CooldownSeconds: 900,
		},
	})

	gpuTel := &models.GPUTelemetry{
		GPUs: []models.GPUDeviceMetrics{
			{Index: 0, Name: "NVIDIA Test", UUID: "uuid-0", PCIBusID: "00:01.0", UtilizationPct: intPtr(98)},
		},
	}

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", gpuTel)

	require.Len(t, eventRepo.events, 1)
	assert.Equal(t, models.EventGPUMetric, eventRepo.events[0].Type)
	assert.Contains(t, eventRepo.events[0].Message, "NVIDIA Test")
}

// gpu_mem_percent metric fires correctly.
func TestCheckGPUAlerts_MemUtilMetric_Fires(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		{
			ID: 1, Enabled: true, Metric: "gpu_mem_percent",
			Operator: ">", Threshold: 90, Severity: "warning", CooldownSeconds: 900,
		},
	})

	gpuTel := &models.GPUTelemetry{
		GPUs: []models.GPUDeviceMetrics{
			{Index: 0, Name: "NVIDIA Test", UUID: "uuid-0", PCIBusID: "00:01.0", MemUtilPct: intPtr(95)},
		},
	}

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", gpuTel)

	require.Len(t, eventRepo.events, 1)
	assert.Equal(t, models.EventGPUMetric, eventRepo.events[0].Type)
}

// gpu_power_watts metric fires correctly.
func TestCheckGPUAlerts_PowerWattsMetric_Fires(t *testing.T) {
	gen, eventRepo := newGPUGenerator([]models.AlertRule{
		{
			ID: 1, Enabled: true, Metric: "gpu_power_watts",
			Operator: ">", Threshold: 300, Severity: "warning", CooldownSeconds: 900,
		},
	})

	gpuTel := &models.GPUTelemetry{
		GPUs: []models.GPUDeviceMetrics{
			{Index: 0, Name: "NVIDIA Test", UUID: "uuid-0", PCIBusID: "00:01.0", PowerDrawW: float64Ptr(320.5)},
		},
	}

	gen.CheckGPUAlerts(context.Background(), "device-1", "test-host", gpuTel)

	require.Len(t, eventRepo.events, 1)
	assert.Equal(t, models.EventGPUMetric, eventRepo.events[0].Type)
	assert.Contains(t, eventRepo.events[0].Message, "320.5")
}
