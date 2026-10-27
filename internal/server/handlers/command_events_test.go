package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-002 events-endpoint tests (ADD section 8, "CE"): the record view keeps
// every server-authored event whatever the agent volume (C-3), and a malformed,
// missing and foreign id read exactly alike (C-6).

func (f *ex2Fx) addEvent(device string, typ models.EventType, attr, commandID string, at time.Time, msg string) {
	f.t.Helper()
	e := models.Event{DeviceID: device, Type: typ, Severity: models.SeverityInfo, Message: msg, CreatedAt: at,
		CommandID: commandID, CommandAttr: attr}
	require.NoError(f.t, f.evts.Create(context.Background(), &e))
}

func TestEX2_AC023_CommandEventsEndpoint(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec := f.records(ex2Hawaii)[0]

	// 200 linked agent events from a flooding agent, then the server's own events.
	for i := 0; i < 200; i++ {
		f.addEvent(ex2Hawaii, models.EventAgentUpdateStarted, models.CommandAttrInferred, rec.ID, ex2T0.Add(time.Duration(i+1)*time.Millisecond), fmt.Sprintf("flood %03d", i))
	}
	f.addEvent(ex2Hawaii, models.EventDeviceOffline, models.CommandAttrExpected, rec.ID, ex2T0.Add(time.Minute), "Device hawaii went offline during Agent update 2.78.0 → 2.80.0 by agent auto-update (expected, back by 18:01)")
	f.addEvent(ex2Hawaii, models.EventDeviceOnline, models.CommandAttrExpected, rec.ID, ex2T0.Add(2*time.Minute), "Device hawaii came online after Agent update 2.78.0 → 2.80.0 by agent auto-update (offline 1m)")
	f.addEvent(ex2Hawaii, models.EventAgentSelfUpdateFailed, models.CommandAttrOutcome, rec.ID, ex2T0.Add(3*time.Minute), "outcome")
	// Not part of the record's view: another command, another device's id.
	f.addEvent(ex2Hawaii, models.EventDeviceOffline, models.CommandAttrExpected, "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", ex2T0, "elsewhere")

	resp := f.do("GET", "/api/v1/devices/"+ex2Hawaii+"/commands/"+rec.ID+"/events", "", "")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got models.CommandEvents
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))

	var serverTypes []models.EventType
	for _, e := range got.ServerEvents {
		serverTypes = append(serverTypes, e.Type)
		assert.Equal(t, rec.ID, e.CommandID, "every event is linked to the record")
	}
	assert.Equal(t, []models.EventType{models.EventDeviceOffline, models.EventDeviceOnline, models.EventAgentSelfUpdateFailed}, serverTypes,
		"[AC-023, C-3] every server-authored event is returned, oldest first, despite 200 agent events")
	assert.Zero(t, got.ServerEventsOmitted)
	assert.Len(t, got.AgentEvents, models.CommandEventsAgentLimit, "[C-3] 50 agent events")
	assert.Equal(t, 201-models.CommandEventsAgentLimit, got.AgentEventsOmitted,
		"[C-3] agent_events_omitted counts the rest (the start event + 200 flooded - 50)")
	assert.Equal(t, models.EventAgentUpdateStarted, got.AgentEvents[0].Type, "oldest agent event first: the real start")

	// The JSON keys are the contract of Section 6.3.
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &raw))
	for _, key := range []string{"server_events", "server_events_omitted", "agent_events", "agent_events_omitted"} {
		assert.Contains(t, raw, key, "the response has %q", key)
	}
}

func TestEX2_AC023_CommandEventsEmptyArraysNotNull(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec := f.records(ex2Hawaii)[0]
	resp := f.do("GET", "/api/v1/devices/"+ex2Hawaii+"/commands/"+rec.ID+"/events", "", "")
	require.Equal(t, http.StatusOK, resp.Code)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &raw))
	assert.JSONEq(t, `[]`, string(raw["server_events"]), "an empty group is [], not null")
}

func TestEX2_AC023_CommandEventsNotFoundIdentical(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec := f.records(ex2Hawaii)[0]
	missing := "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c"

	var bodies []string
	for name, path := range map[string]string{
		"malformed id":           "/api/v1/devices/" + ex2Hawaii + "/commands/not-a-uuid/events",
		"upper-case uuid":        "/api/v1/devices/" + ex2Hawaii + "/commands/0B7A53A6-1F2E-4B6A-9D1D-2D3F5C6A7B8C/events",
		"missing id":             "/api/v1/devices/" + ex2Hawaii + "/commands/" + missing + "/events",
		"foreign device's id":    "/api/v1/devices/" + ex2Vermont + "/commands/" + rec.ID + "/events",
		"unknown device, no row": "/api/v1/devices/33333333-3333-4333-8333-333333333333/commands/" + rec.ID + "/events",
	} {
		resp := f.do("GET", path, "", "")
		assert.Equal(t, http.StatusNotFound, resp.Code, "[C-6] %s is 404", name)
		bodies = append(bodies, resp.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		assert.Equal(t, bodies[0], bodies[i], "[C-6] the 404 body is identical for every case")
	}
	assert.JSONEq(t, `{"error":"command not found","code":"command_not_found"}`, bodies[0])
}

func TestEX2_AC023_CommandEventsServerError(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec := f.records(ex2Hawaii)[0]
	f.evts.Err = fmt.Errorf("events table unavailable")
	resp := f.do("GET", "/api/v1/devices/"+ex2Hawaii+"/commands/"+rec.ID+"/events", "", "")
	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.JSONEq(t, `{"error":"failed to list events"}`, resp.Body.String(), "[Section 6.3] a failing read is a 500 with a fixed body")
}
