package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// --- AC-035: telemetry ingest is tolerant and bounded ---

func TestIngest_EU_AC035_NoWarning2xx(t *testing.T) {
	entry := func(id string) string {
		return `{"id":"` + id + `","status":"update_available","pending_count":1}`
	}
	var twelve []string
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "b1", "b2", "b3"} {
		twelve = append(twelve, entry(id))
	}
	t.Run("[AC-035] an older agent sends no field", func(t *testing.T) {
		updates, status, warnings := ingestUpdates(t, `{"package_manager":"apt","pending_updates":3}`)
		require.Equal(t, http.StatusOK, status)
		require.NotNil(t, updates)
		assert.Empty(t, updates.ExtraUpdaters)
		assert.Empty(t, warnings)
	})
	t.Run("[AC-035] twelve entries keep eight", func(t *testing.T) {
		updates, status, warnings := ingestUpdates(t, `{"package_manager":"apt","extra_updaters":[`+strings.Join(twelve, ",")+`]}`)
		require.Equal(t, http.StatusOK, status)
		require.NotNil(t, updates)
		require.Len(t, updates.ExtraUpdaters, models.MaxExtraUpdaters)
		assert.Equal(t, "a1", updates.ExtraUpdaters[0].ID, "input order is kept")
		assert.Equal(t, "a8", updates.ExtraUpdaters[7].ID)
		assert.Empty(t, warnings)
	})
	t.Run("[AC-035] a bogus status is stored unknown and a long version is cut", func(t *testing.T) {
		body := `{"package_manager":"apt","extra_updaters":[{"id":"dietpi","status":"bogus","installed_version":"` + strings.Repeat("9", 500) + `"}]}`
		updates, status, warnings := ingestUpdates(t, body)
		require.Equal(t, http.StatusOK, status)
		require.NotNil(t, updates)
		require.Len(t, updates.ExtraUpdaters, 1)
		assert.Equal(t, models.ExtraStatusUnknown, updates.ExtraUpdaters[0].Status)
		assert.Len(t, updates.ExtraUpdaters[0].InstalledVersion, models.MaxExtraUpdaterVersion)
		assert.Empty(t, warnings)
	})
	for name, value := range map[string]string{"object": `{"id":"dietpi"}`, "string": `"x"`, "number": `7`, "null": `null`, "bad entries": `[1,"x",null,{"id":"BAD ID"}]`} {
		value := value
		t.Run("[AC-035] hostile shape "+name, func(t *testing.T) {
			updates, status, warnings := ingestUpdates(t, `{"package_manager":"apt","extra_updaters":`+value+`}`)
			require.Equal(t, http.StatusOK, status, "never rejects the telemetry")
			require.NotNil(t, updates)
			assert.Empty(t, updates.ExtraUpdaters)
			assert.Empty(t, warnings)
		})
	}
}

// --- AC-036: auto-patch fires for an extra-updater-only host ---

func TestCheckAutoPatch_EU_AC036_ExtraOnlyHost(t *testing.T) {
	anytimeWin := models.MaintenanceWindow{Mode: "anytime", StartTime: "00:00", EndTime: "23:59", CooldownMinutes: 60}
	telemetryWith := func(pending int, e models.ExtraUpdater) *models.FullTelemetryData {
		return &models.FullTelemetryData{Updates: &models.UpdateInfo{PendingUpdates: pending, ExtraUpdaters: models.ExtraUpdaterList{e}}}
	}
	cases := []struct {
		name string
		data *models.FullTelemetryData
		want int
	}{
		{"update_available + enabled fires", telemetryWith(0, models.ExtraUpdater{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessEnabled}), 1},
		{"unknown + enabled does not fire", telemetryWith(0, models.ExtraUpdater{ID: "dietpi", Status: models.ExtraStatusUnknown, UnknownReason: models.ExtraReasonCheckDisabled, Readiness: models.ExtraReadinessEnabled}), 0},
		{"update_available + root_side_missing does not fire", telemetryWith(0, models.ExtraUpdater{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessRootSideMissing}), 0},
		{"update_available + not_opted_in does not fire", telemetryWith(0, models.ExtraUpdater{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessNotOptedIn}), 0},
		{"up_to_date + enabled does not fire", telemetryWith(0, models.ExtraUpdater{ID: "dietpi", Status: models.ExtraStatusUpToDate, Readiness: models.ExtraReadinessEnabled}), 0},
		{"apt pending behaves as before even when the updater is not enabled", telemetryWith(3, models.ExtraUpdater{ID: "dietpi", Status: models.ExtraStatusUnknown}), 1},
		{"nothing at all does not fire", &models.FullTelemetryData{Updates: &models.UpdateInfo{}}, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run("[AC-036] "+tc.name, func(t *testing.T) {
			h, cmdRepo := autoPatchTestHandler(t, anytimeWin)
			h.checkAutoPatch(context.Background(), "dev-1", tc.data)
			cmds := osUpdateCommands(cmdRepo)
			require.Len(t, cmds, tc.want)
			if tc.want == 1 {
				assert.Equal(t, models.OriginAutoPatch, cmds[0].Origin, "the command carries the auto_patch origin")
			}
		})
	}
	t.Run("[AC-036] out of window never fires, whatever the updater says", func(t *testing.T) {
		h, cmdRepo := autoPatchTestHandler(t, models.MaintenanceWindow{Mode: "window", StartTime: "00:00", EndTime: "00:00", CooldownMinutes: 60})
		h.checkAutoPatch(context.Background(), "dev-1", telemetryWith(0, models.ExtraUpdater{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessEnabled}))
		assert.Empty(t, osUpdateCommands(cmdRepo))
	})
}

// --- AC-037: exclusion parameter validation ---

func TestManualOSUpdate_EU_AC037_ExclusionParam(t *testing.T) {
	t.Run("[AC-037] true is stored on the command", func(t *testing.T) {
		h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
		rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","exclude_extra_updaters":true}}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, true, onlyCommand(t, repo).Params["exclude_extra_updaters"])
	})
	t.Run("[AC-037] absent leaves the params alone", func(t *testing.T) {
		h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
		rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full"}}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.NotContains(t, onlyCommand(t, repo).Params, "exclude_extra_updaters")
	})
	t.Run("[AC-037] false is stored as absent", func(t *testing.T) {
		h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
		rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","exclude_extra_updaters":false}}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.NotContains(t, onlyCommand(t, repo).Params, "exclude_extra_updaters")
	})
	for name, value := range map[string]string{"yes": `"yes"`, "true string": `"true"`, "number": `1`, "null": `null`, "object": `{}`, "array": `[true]`} {
		value := value
		t.Run("[AC-037] "+name+" is rejected with no command", func(t *testing.T) {
			h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
			rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","exclude_extra_updaters":`+value+`}}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "invalid_request", errorCode(t, rec))
			assertNoCommandCreated(t, repo, "[AC-037] a bad exclusion creates nothing")
		})
	}
	for _, key := range []string{"Exclude_Extra_Updaters", "EXCLUDE_EXTRA_UPDATERS", "exclude_extra_updateRs"} {
		key := key
		t.Run("[AC-037] case variant "+key+" is rejected", func(t *testing.T) {
			h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
			rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","`+key+`":true}}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assertNoCommandCreated(t, repo, "a case variant must not reach the agent")
		})
	}
	t.Run("[AC-037] the exclusion is scoped to os_update", func(t *testing.T) {
		h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
		rec := postCommand(t, h, `{"action":"fetch_logs","params":{"exclude_extra_updaters":"yes","hours":4}}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, "yes", onlyCommand(t, repo).Params["exclude_extra_updaters"], "other actions are not validated")
	})
}

func TestManualOSUpdate_EU_SEC007_BudgetStampNeverFromOperator(t *testing.T) {
	for _, key := range []string{"extra_updater_budget", "Extra_Updater_Budget", "EXTRA_UPDATER_BUDGET"} {
		key := key
		t.Run("[SEC-007] "+key, func(t *testing.T) {
			h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
			rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","`+key+`":true}}`)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			for stored := range onlyCommand(t, repo).Params {
				assert.False(t, strings.EqualFold(stored, "extra_updater_budget"), "a client-supplied stamp survived as %q", stored)
			}
		})
	}
}

// --- AC-034 / SEC-005: the server composes the event from the command row ---

const euCmdID = "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c"

type eventFixture struct {
	h      *Handlers
	events *testutil.MockEventRepo
	cmds   *testutil.MockCommandRepo
}

func newEventFixture(t *testing.T) *eventFixture {
	t.Helper()
	f := &eventFixture{events: testutil.NewMockEventRepo(), cmds: testutil.NewMockCommandRepo()}
	devices := testutil.NewMockDeviceRepo()
	devices.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "pivpn"}
	hub := websocket.NewHub()
	go hub.Run()
	f.h = &Handlers{events: f.events, commandRepo: f.cmds, devices: devices, hub: hub}
	f.cmds.Commands[euCmdID] = &models.Command{ID: euCmdID, DeviceID: "dev-1", Action: models.ActionOSUpdate, Origin: models.OriginAutoPatch, Status: "running", CreatedAt: time.Now()}
	return f
}

func (f *eventFixture) post(t *testing.T, deviceID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/events", f.h.ReceiveAgentEvent)
	req := httptest.NewRequest("POST", "/api/v1/devices/"+deviceID+"/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func eventBody(typ, cmd, updater, outcome, message string) string {
	m := map[string]string{"type": typ, "command_id": cmd, "updater": updater, "outcome": outcome, "message": message, "severity": "info"}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestReceiveAgentEvent_EU_AC034_OriginFromCommand(t *testing.T) {
	f := newEventFixture(t)
	rec := f.post(t, "dev-1", eventBody("extra_updater_started", euCmdID, "dietpi", "", "FORGED <b>message</b> origin user"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = f.post(t, "dev-1", eventBody("extra_updater_finished", euCmdID, "dietpi", "success", "FORGED"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, f.events.Events, 2, "exactly two events per run")
	start, end := f.events.Events[0], f.events.Events[1]
	assert.Equal(t, models.EventExtraUpdaterStarted, start.Type)
	assert.Equal(t, models.EventExtraUpdaterFinished, end.Type)
	assert.Equal(t, "DietPi update started on pivpn (command "+euCmdID+", origin auto_patch)", start.Message, "origin comes from the command row, the message from the server")
	assert.Equal(t, "DietPi update finished on pivpn (command "+euCmdID+", origin auto_patch)", end.Message)
	assert.Equal(t, euCmdID, start.CommandID)
	assert.Equal(t, models.SeverityInfo, end.Severity)
	assert.NotContains(t, start.Message, "FORGED")
}

func TestReceiveAgentEvent_EU_AC034_FailureOutcomesAreWarnings(t *testing.T) {
	for outcome, words := range map[string]string{"failed": "failed", "timed_out": "timed out", "interrupted": "was interrupted"} {
		outcome, words := outcome, words
		t.Run("[AC-034] "+outcome, func(t *testing.T) {
			f := newEventFixture(t)
			rec := f.post(t, "dev-1", eventBody("extra_updater_finished", euCmdID, "dietpi", outcome, ""))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, f.events.Events, 1)
			assert.Equal(t, models.SeverityWarning, f.events.Events[0].Severity)
			assert.Contains(t, f.events.Events[0].Message, "DietPi update "+words+" on pivpn")
		})
	}
}

func TestReceiveAgentEvent_EU_SEC005_DuplicateIsNoop(t *testing.T) {
	f := newEventFixture(t)
	for i := 0; i < 3; i++ {
		rec := f.post(t, "dev-1", eventBody("extra_updater_started", euCmdID, "dietpi", "", ""))
		require.Equal(t, http.StatusOK, rec.Code, "a retry is a success, not an error")
		if i > 0 {
			assert.Contains(t, rec.Body.String(), "duplicate")
		}
	}
	assert.Len(t, f.events.Events, 1, "[SEC-005] at most one start event per command")
	f.post(t, "dev-1", eventBody("extra_updater_finished", euCmdID, "dietpi", "failed", ""))
	f.post(t, "dev-1", eventBody("extra_updater_finished", euCmdID, "dietpi", "success", ""))
	assert.Len(t, f.events.Events, 2, "[SEC-005] at most one end event per command, whatever the second says")
	assert.Equal(t, models.SeverityWarning, f.events.Events[1].Severity, "the first end wins")
}

func TestReceiveAgentEvent_EU_SEC005_UnknownUpdaterRejected(t *testing.T) {
	bad := map[string]string{
		"unknown updater":         eventBody("extra_updater_started", euCmdID, "snap", "", ""),
		"empty updater":           eventBody("extra_updater_started", euCmdID, "", "", ""),
		"updater with markup":     eventBody("extra_updater_started", euCmdID, "<script>", "", ""),
		"non-canonical uuid":      eventBody("extra_updater_started", strings.ToUpper(euCmdID), "dietpi", "", ""),
		"not a uuid":              eventBody("extra_updater_started", "../../x", "dietpi", "", ""),
		"unknown command":         eventBody("extra_updater_started", "1c8b64b7-2a3f-4c7b-8e2e-3e4a6d7b8c9d", "dietpi", "", ""),
		"bad outcome":             eventBody("extra_updater_finished", euCmdID, "dietpi", "exploded", ""),
		"missing outcome":         eventBody("extra_updater_finished", euCmdID, "dietpi", "", ""),
		"skipped is never an end": eventBody("extra_updater_finished", euCmdID, "dietpi", "skipped", ""),
		"outcome on a start":      eventBody("extra_updater_started", euCmdID, "dietpi", "success", ""),
	}
	for name, body := range bad {
		body := body
		t.Run("[SEC-005] "+name, func(t *testing.T) {
			f := newEventFixture(t)
			rec := f.post(t, "dev-1", body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, f.events.Events, "a rejected event stores nothing")
		})
	}
	t.Run("[SEC-005] another device's command", func(t *testing.T) {
		f := newEventFixture(t)
		rec := f.post(t, "dev-2", eventBody("extra_updater_started", euCmdID, "dietpi", "", ""))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, f.events.Events)
	})
	t.Run("[SEC-005] a command that is not an os_update", func(t *testing.T) {
		f := newEventFixture(t)
		f.cmds.Commands[euCmdID].Action = models.ActionReboot
		rec := f.post(t, "dev-1", eventBody("extra_updater_started", euCmdID, "dietpi", "", ""))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, f.events.Events)
	})
	t.Run("[SEC-005] the existing agent-update events still work", func(t *testing.T) {
		f := newEventFixture(t)
		rec := f.post(t, "dev-1", `{"type":"agent_update_started","severity":"info","message":"x"}`)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Len(t, f.events.Events, 1)
	})
}

// --- BulkPatch includes enabled extras ---

func TestHasEnabledPendingExtra_EU_FR048_OnlyAppliedUpdatersCount(t *testing.T) {
	enabled := &models.UpdateInfo{ExtraUpdaters: models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessEnabled}}}
	notOptedIn := &models.UpdateInfo{ExtraUpdaters: models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessNotOptedIn}}}
	unknown := &models.UpdateInfo{ExtraUpdaters: models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUnknown, Readiness: models.ExtraReadinessEnabled}}}
	assert.True(t, hasEnabledPendingExtra(enabled))
	assert.False(t, hasEnabledPendingExtra(notOptedIn), "an updater that would not be applied is not a reason to patch")
	assert.False(t, hasEnabledPendingExtra(unknown), "unknown never triggers")
	assert.False(t, hasEnabledPendingExtra(nil))
}
