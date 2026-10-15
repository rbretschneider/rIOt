package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer (REBOOT-ETA-001).
// The server-side tests inject UpdateInfo.AllowReboot directly, so nothing
// proved the agent actually stamps commands.allow_reboot onto update
// telemetry. Without this stamp every device reads as "unknown" and AC-012
// (allow_reboot=false -> manual reboot) can never fire in production.

type fakeUpdatesCollector struct{ result interface{} }

func (f *fakeUpdatesCollector) Name() string { return "updates" }
func (f *fakeUpdatesCollector) Collect(context.Context) (interface{}, error) {
	return f.result, nil
}

func collectWithAllowReboot(t *testing.T, allow bool, info *models.UpdateInfo) models.FullTelemetryData {
	t.Helper()
	reg := collectors.NewRegistry()
	reg.Register(&fakeUpdatesCollector{result: info})
	a := &Agent{
		config:   &Config{Commands: CommandsConfig{AllowReboot: allow}},
		registry: reg,
	}
	return a.collectAll(context.Background())
}

// Covers AC-012
func TestCollectAll_AC012_StampsAllowRebootTrue(t *testing.T) {
	data := collectWithAllowReboot(t, true, &models.UpdateInfo{PendingUpdates: 1, RebootRequired: true})

	require.NotNil(t, data.Updates)
	require.NotNil(t, data.Updates.AllowReboot, "[AC-012] agent must report allow_reboot, even when true")
	assert.True(t, *data.Updates.AllowReboot, "[AC-012] allow_reboot=true must be reported as true")
}

// Covers AC-012
func TestCollectAll_AC012_StampsAllowRebootFalseAsExplicitFalse(t *testing.T) {
	data := collectWithAllowReboot(t, false, &models.UpdateInfo{PendingUpdates: 1, RebootRequired: true})

	require.NotNil(t, data.Updates)
	require.NotNil(t, data.Updates.AllowReboot, "[AC-012] false must be reported as explicit false, not omitted (nil means unknown)")
	assert.False(t, *data.Updates.AllowReboot)

	raw, err := json.Marshal(data.Updates)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"allow_reboot":false`, "[AC-012] explicit false must survive omitempty on the wire")
}

// Covers AC-012 (robustness): a collector that yields a nil *UpdateInfo must not panic.
func TestCollectAll_AC012_NilUpdateInfoDoesNotPanic(t *testing.T) {
	var nilInfo *models.UpdateInfo
	assert.NotPanics(t, func() {
		data := collectWithAllowReboot(t, true, nilInfo)
		assert.Nil(t, data.Updates)
	})
}
