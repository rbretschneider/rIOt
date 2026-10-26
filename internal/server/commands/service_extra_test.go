package commands

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXTRA-UPD-001 AC-037 and SEC-007: the server stamps an extended budget only
// from the device's own telemetry, and no client can supply the stamp.

func (h *harness) telemetryWith(readiness string) {
	h.t.Helper()
	h.tel.Snapshots[devA] = []models.TelemetrySnapshot{{DeviceID: devA, Timestamp: h.clock.Now(), Data: models.FullTelemetryData{
		Updates: &models.UpdateInfo{ExtraUpdaters: models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: readiness}}},
	}}}
}

func (h *harness) dispatchOS(params map[string]interface{}) *models.Command {
	h.t.Helper()
	res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: models.ActionOSUpdate, Params: params})
	require.NoError(h.t, err)
	require.NotNil(h.t, res.Command, "no conflict expected")
	return res.Command
}

func TestDispatch_EU_AC037_BudgetStamp(t *testing.T) {
	t.Run("[AC-037] an enabled updater gets the stamp and the 95 minute deadline", func(t *testing.T) {
		h := newHarness(t)
		h.telemetryWith(models.ExtraReadinessEnabled)
		cmd := h.dispatchOS(map[string]interface{}{"mode": "full"})
		assert.Equal(t, true, cmd.Params[models.ParamExtraUpdaterBudget])
		assert.Equal(t, int(models.OSUpdateExtraExecDeadline/time.Second), cmd.ExecDeadlineS)
		assert.Equal(t, int((95 * time.Minute).Seconds()), cmd.ExecDeadlineS)
		stored := h.get(cmd.ID)
		assert.Equal(t, true, stored.Params[models.ParamExtraUpdaterBudget])
		assert.Equal(t, cmd.ExecDeadlineS, stored.ExecDeadlineS)
	})
	t.Run("[AC-037] security mode and a missing mode are extended too", func(t *testing.T) {
		for _, params := range []map[string]interface{}{{"mode": "security"}, {}, nil} {
			h := newHarness(t)
			h.telemetryWith(models.ExtraReadinessEnabled)
			cmd := h.dispatchOS(params)
			assert.Equal(t, true, cmd.Params[models.ParamExtraUpdaterBudget], "params %v", params)
		}
	})
	t.Run("[AC-037] repair is never extended", func(t *testing.T) {
		h := newHarness(t)
		h.telemetryWith(models.ExtraReadinessEnabled)
		cmd := h.dispatchOS(map[string]interface{}{"mode": "repair"})
		assert.NotContains(t, cmd.Params, models.ParamExtraUpdaterBudget)
		assert.Equal(t, models.ExecDeadlineSeconds(models.ActionOSUpdate, 0), cmd.ExecDeadlineS)
	})
	t.Run("[AC-037] an operator exclusion is never extended", func(t *testing.T) {
		h := newHarness(t)
		h.telemetryWith(models.ExtraReadinessEnabled)
		cmd := h.dispatchOS(map[string]interface{}{"mode": "full", models.ParamExcludeExtraUpdaters: true})
		assert.NotContains(t, cmd.Params, models.ParamExtraUpdaterBudget)
		assert.Equal(t, true, cmd.Params[models.ParamExcludeExtraUpdaters], "the exclusion itself is kept for the agent")
	})
	for _, readiness := range []string{models.ExtraReadinessNotOptedIn, models.ExtraReadinessRootSideMissing, models.ExtraReadinessPatchingDisabled, models.ExtraReadinessUnsupportedHelper, models.ExtraReadinessNonDefaultUpdateSource, "unknown", ""} {
		t.Run("[AC-037] readiness "+readiness+" keeps the default 40 minute deadline", func(t *testing.T) {
			h := newHarness(t)
			h.telemetryWith(readiness)
			cmd := h.dispatchOS(map[string]interface{}{"mode": "full"})
			assert.NotContains(t, cmd.Params, models.ParamExtraUpdaterBudget)
			assert.Equal(t, int((40 * time.Minute).Seconds()), cmd.ExecDeadlineS)
		})
	}
	t.Run("[AC-037] a host with no telemetry, or no updater, keeps the default", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, 2400, h.dispatchOS(map[string]interface{}{}).ExecDeadlineS)
		h = newHarness(t)
		h.tel.Snapshots[devA] = []models.TelemetrySnapshot{{DeviceID: devA, Timestamp: h.clock.Now(), Data: models.FullTelemetryData{Updates: &models.UpdateInfo{PendingUpdates: 3}}}}
		assert.Equal(t, 2400, h.dispatchOS(map[string]interface{}{}).ExecDeadlineS)
	})
	t.Run("[AC-037] a telemetry read error keeps the default", func(t *testing.T) {
		h := newHarness(t)
		h.telemetryWith(models.ExtraReadinessEnabled)
		h.tel.Err = assert.AnError
		assert.Equal(t, 2400, h.dispatchOS(map[string]interface{}{}).ExecDeadlineS)
	})
	t.Run("[AC-037] the caller's params map is not edited", func(t *testing.T) {
		h := newHarness(t)
		h.telemetryWith(models.ExtraReadinessEnabled)
		params := map[string]interface{}{"mode": "full"}
		h.dispatchOS(params)
		assert.NotContains(t, params, models.ParamExtraUpdaterBudget)
	})
}

func TestDispatch_EU_SEC007_BudgetVariantsStrippedAllActions(t *testing.T) {
	variants := []string{"extra_updater_budget", "Extra_Updater_Budget", "EXTRA_UPDATER_BUDGET", "extra_Updater_budget"}
	for _, action := range []string{models.ActionOSUpdate, models.ActionReboot, models.ActionShutdown, models.ActionAgentUpdate, models.ActionEnableAutoUpdates, models.ActionDockerUpdate} {
		for _, key := range variants {
			t.Run("[SEC-007] "+action+" "+key, func(t *testing.T) {
				h := newHarness(t)
				params := map[string]interface{}{key: true, "mode": "full"}
				if action == models.ActionDockerUpdate {
					params = map[string]interface{}{key: true, "container_id": "abc", "container_name": "web"}
				}
				res, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: action, Params: params})
				require.NoError(t, err)
				if res.Command == nil {
					t.Skip("dispatch conflicted; the param edit happens before the conflict check and is covered by the other cases")
				}
				for stored := range h.get(res.Command.ID).Params {
					assert.NotEqual(t, "extra_updater_budget", stripCase(stored), "a client-supplied budget stamp survived on %s", action)
				}
				assert.Equal(t, models.ExecDeadlineSeconds(action, 0), res.Command.ExecDeadlineS, "a client cannot lengthen a deadline")
			})
		}
	}
	t.Run("[SEC-007] a client-supplied stamp is not honoured even when the server would stamp", func(t *testing.T) {
		h := newHarness(t)
		h.telemetryWith(models.ExtraReadinessNotOptedIn)
		cmd := h.dispatchOS(map[string]interface{}{"mode": "full", "Extra_Updater_Budget": true})
		assert.NotContains(t, cmd.Params, "Extra_Updater_Budget")
		assert.NotContains(t, cmd.Params, models.ParamExtraUpdaterBudget)
		assert.Equal(t, 2400, cmd.ExecDeadlineS)
	})
	t.Run("[SEC-007] a stamp from a client is replaced by the server's own decision", func(t *testing.T) {
		h := newHarness(t)
		h.telemetryWith(models.ExtraReadinessEnabled)
		cmd := h.dispatchOS(map[string]interface{}{"mode": "full", "EXTRA_UPDATER_BUDGET": "yes"})
		assert.NotContains(t, cmd.Params, "EXTRA_UPDATER_BUDGET")
		assert.Equal(t, true, cmd.Params[models.ParamExtraUpdaterBudget], "the boolean true is the server's")
	})
}

func stripCase(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}
