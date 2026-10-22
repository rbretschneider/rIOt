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

func collectWithPatchFlags(t *testing.T, cmds CommandsConfig) *models.UpdateInfo {
	t.Helper()
	reg := collectors.NewRegistry()
	reg.Register(&fakeUpdatesCollector{result: &models.UpdateInfo{PendingUpdates: 1}})
	a := &Agent{config: &Config{Commands: cmds}, registry: reg}
	return a.collectAll(context.Background()).Updates
}

func TestCollectAll_StampsAllowPatchingAndHoldRebootClass(t *testing.T) {
	info := collectWithPatchFlags(t, CommandsConfig{AllowPatching: true, HoldRebootClass: false})
	require.NotNil(t, info.AllowPatching, "allow_patching must be reported")
	assert.True(t, *info.AllowPatching)
	require.NotNil(t, info.HoldRebootClass, "an explicit false must be reported, not omitted")
	assert.False(t, *info.HoldRebootClass)

	raw, err := json.Marshal(info)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"hold_reboot_class":false`)
}

func TestUpdateInfo_OldPayloadDecodesWithNilFacts(t *testing.T) {
	var info models.UpdateInfo
	require.NoError(t, json.Unmarshal([]byte(`{"pending_updates":2,"allow_reboot":true}`), &info))
	assert.Nil(t, info.AllowPatching)
	assert.Nil(t, info.HoldRebootClass)
	assert.Nil(t, info.OtherHolds)
	assert.Nil(t, info.ReleaseUpgrade)
}

func TestUpdateInfo_NewPayloadDecodesIntoOldShape(t *testing.T) {
	type oldShape struct {
		PendingUpdates int   `json:"pending_updates"`
		AllowReboot    *bool `json:"allow_reboot"`
	}
	raw := `{"pending_updates":2,"allow_reboot":true,"allow_patching":true,"hold_reboot_class":false,` +
		`"other_holds":{"status":"ok","packages":["nginx"],"total":1},"release_upgrade":{"status":"none"}}`
	var old oldShape
	require.NoError(t, json.Unmarshal([]byte(raw), &old), "unknown keys must be ignored by an older decoder")
	assert.Equal(t, 2, old.PendingUpdates)
}
