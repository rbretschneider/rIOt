package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// EXPECT-007 handler tests (ADD section 8, "HT"): ReceiveAgentEvent reads the
// agent's references and structured versions inside the authenticated-ID gate,
// answers every invalid one exactly like an event without a reference, and
// stores a repeat of a reference-linked event once. They reuse the EXPECT-002
// fixture (ex2Fx); the clock is injected and nothing sleeps.

const (
	ex7Attempt  = "9f1c0123456789abcdef0123456789ab"
	ex7AttemptB = "1111222233334444aaaabbbbccccdddd"
)

// ex7Events wraps the event repository to record every HasCommandEvent call and
// to inject a failure.
type ex7Events struct {
	*testutil.MockEventRepo
	mu      sync.Mutex
	hasCall []string // "<device>|<command>|<type>"
	hasErr  error
}

func (e *ex7Events) HasCommandEvent(ctx context.Context, deviceID, commandID string, t models.EventType) (bool, error) {
	e.mu.Lock()
	e.hasCall = append(e.hasCall, deviceID+"|"+commandID+"|"+string(t))
	err := e.hasErr
	e.mu.Unlock()
	if err != nil {
		return false, err
	}
	return e.MockEventRepo.HasCommandEvent(ctx, deviceID, commandID, t)
}

func (e *ex7Events) calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.hasCall...)
}

// ex7Fx is the EX2 fixture with the recording event repository wired in.
type ex7Fx struct {
	*ex2Fx
	rec *ex7Events
}

func newEX7Fixture(t *testing.T) *ex7Fx {
	t.Helper()
	f := newEX2Fixture(t)
	rec := &ex7Events{MockEventRepo: f.evts}
	f.h.events = rec
	return &ex7Fx{ex2Fx: f, rec: rec}
}

// post sends a raw agent event body for device, authenticated as authAs.
func (f *ex7Fx) post(device string, body map[string]string, authAs string) *testutilResponse {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	rr := f.do("POST", "/api/v1/devices/"+device+"/events", string(raw), authAs)
	return &testutilResponse{Code: rr.Code, Body: strings.TrimSpace(rr.Body.String())}
}

type testutilResponse struct {
	Code int
	Body string
}

func evBody(typ, msg string, extra map[string]string) map[string]string {
	m := map[string]string{"type": typ, "severity": "info", "message": msg}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func (f *ex7Fx) startA(attempt string) *testutilResponse {
	f.t.Helper()
	return f.post(ex2Hawaii, evBody("agent_update_started", ex2Start,
		map[string]string{"attempt_id": attempt, "from_version": "2.78.0", "to_version": "2.80.0"}), ex2Hawaii)
}

// seedCmd stores a hawaii-or-vermont command row the handler reads but never writes.
func (f *ex7Fx) seedCmd(id, device, action, origin, status string, dispatched bool) models.Command {
	f.t.Helper()
	now := f.clock.Now()
	c := models.Command{ID: id, DeviceID: device, Action: action, Origin: origin, Status: status, Params: map[string]interface{}{},
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour), TargetKeys: []string{}}
	if dispatched {
		c.DispatchedAt = &now
	}
	f.cmds.Seed(&c)
	return c
}

func (f *ex7Fx) command(id string) models.Command {
	f.t.Helper()
	c, ok := f.cmds.Get(id)
	require.True(f.t, ok, "command %s exists", id)
	return c
}

const (
	hawaiiCmd  = "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c4d"
	vermontCmd = "7c1d4e2b-3a9f-4b6c-8d0e-1f2a3b4c5d6e"
)

func TestEX7_AC023_InvalidRefsAnsweredOkAndStored(t *testing.T) {
	f := newEX7Fixture(t)
	f.seedCmd(vermontCmd, ex2Vermont, models.ActionAgentUpdate, models.OriginUser, "running", true)
	dockerCmd := f.seedCmd("11111111-aaaa-4bbb-8ccc-222222222222", ex2Hawaii, models.ActionDockerUpdate, models.OriginUser, "running", true)
	queuedCmd := f.seedCmd("33333333-aaaa-4bbb-8ccc-444444444444", ex2Hawaii, models.ActionAgentUpdate, models.OriginUser, "queued", false)
	vermontBefore := f.command(vermontCmd)

	refs := map[string]string{
		"vermont's real dispatched agent_update command": vermontCmd,
		"unknown id":           "55555555-aaaa-4bbb-8ccc-666666666666",
		"docker_update":        dockerCmd.ID,
		"queued, undispatched": queuedCmd.ID,
		"300 bytes":            strings.Repeat("a", 300),
		"control characters":   "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c\x00\n",
	}
	sent := 0
	for name, ref := range refs {
		resp := f.post(ex2Hawaii, evBody("agent_update_started", ex2Start, map[string]string{"command_id": ref}), ex2Hawaii)
		assert.Equal(t, http.StatusOK, resp.Code, "[AC-023] %s: the request succeeds", name)
		assert.JSONEq(t, `{"status":"ok"}`, resp.Body, "[AC-023] %s: the body is the ordinary one", name)
		sent++
	}
	stored := f.eventsOfType(models.EventAgentUpdateStarted)
	assert.Len(t, stored, sent, "[AC-023] every event is stored, none is dropped as a duplicate")
	for _, e := range stored {
		assert.NotEqual(t, vermontCmd, e.CommandID, "[AC-023] no event is ever linked to vermont's command")
		assert.NotEqual(t, dockerCmd.ID, e.CommandID)
		assert.NotEqual(t, queuedCmd.ID, e.CommandID)
		assert.NotEqual(t, models.CommandAttrAgent, e.CommandAttr, "[AC-023] an invalid reference never produces a reference link")
	}
	assert.Equal(t, vermontBefore, f.command(vermontCmd), "[AC-023] vermont's command is unchanged")
	assert.Len(t, f.records(ex2Hawaii), 1, "[AC-023] processed as EXPECT-002: the first start created the one record")
	assert.Empty(t, f.records(ex2Vermont), "and nothing was created for vermont")

	t.Run("another device's key or a mismatched id: the reference is ignored", func(t *testing.T) {
		f := newEX7Fixture(t)
		f.seedCmd(hawaiiCmd, ex2Hawaii, models.ActionAgentUpdate, models.OriginUser, "running", true)
		before := f.command(hawaiiCmd)
		baseline := f.post(ex2Hawaii, evBody("agent_update_failed", "x", nil), ex2Hawaii)
		for name, authAs := range map[string]string{"vermont's key": ex2Vermont, "no authentication": ""} {
			resp := f.post(ex2Hawaii, evBody("agent_update_failed", "x", map[string]string{"command_id": hawaiiCmd}), authAs)
			assert.Equal(t, baseline.Code, resp.Code, "[AC-023] %s: the same status", name)
			assert.JSONEq(t, baseline.Body, resp.Body, "[AC-023] %s: the same body", name)
		}
		for _, e := range f.eventsOfType(models.EventAgentUpdateFailed) {
			assert.Empty(t, e.CommandID, "[AC-023] a reference from another device's key is never read: no link")
		}
		assert.Equal(t, before, f.command(hawaiiCmd))
	})
}

func TestEX7_AC029_CrossDeviceRefsIgnoredHTTP(t *testing.T) {
	f := newEX7Fixture(t)
	resp := f.startA(ex7Attempt) // hawaii's open record owns attempt A
	require.Equal(t, http.StatusOK, resp.Code)
	rec := f.records(ex2Hawaii)
	require.Len(t, rec, 1)
	f.seedCmd(hawaiiCmd, ex2Hawaii, models.ActionAgentUpdate, models.OriginUser, "running", true)
	recBefore, cmdBefore := f.command(rec[0].ID), f.command(hawaiiCmd)
	commandUpdates := len(f.hub.commands)

	baseline := f.post(ex2Vermont, evBody("agent_update_completed", "x", map[string]string{"attempt_id": ex7AttemptB}), ex2Vermont)
	done := f.post(ex2Vermont, evBody("agent_update_completed", "Agent updated to 2.80.0", map[string]string{"attempt_id": ex7Attempt}), ex2Vermont)
	failed := f.post(ex2Vermont, evBody("agent_update_failed", "x", map[string]string{"command_id": hawaiiCmd}), ex2Vermont)

	assert.Equal(t, baseline.Code, done.Code, "[AC-029] hawaii's real attempt ID: the same status as any unknown one")
	assert.JSONEq(t, baseline.Body, done.Body, "[AC-029] and the same body")
	assert.Equal(t, baseline.Code, failed.Code, "[AC-029] hawaii's real command ID: the same status")
	assert.JSONEq(t, baseline.Body, failed.Body, "[AC-029] and the same body")
	assert.Equal(t, recBefore, f.command(rec[0].ID), "[AC-029] hawaii's record is unchanged")
	assert.Equal(t, cmdBefore, f.command(hawaiiCmd), "[AC-029] hawaii's command is unchanged")
	assert.Equal(t, commandUpdates, len(f.hub.commands), "[AC-029] nothing is announced")
	for _, typ := range []models.EventType{models.EventAgentUpdateCompleted, models.EventAgentUpdateFailed} {
		for _, e := range f.eventsOfType(typ) {
			if e.DeviceID == ex2Vermont {
				assert.Empty(t, e.CommandID, "[AC-029] vermont's events are processed as for no reference: unlinked")
			}
		}
	}
}

func TestEX7_AC034_RetriedEventsStoredOnce(t *testing.T) {
	f := newEX7Fixture(t)

	var bodies []string
	for i := 0; i < 3; i++ {
		resp := f.startA(ex7Attempt)
		require.Equal(t, http.StatusOK, resp.Code, "[AC-034] every request is answered with success")
		bodies = append(bodies, resp.Body)
	}
	assert.JSONEq(t, `{"status":"ok"}`, bodies[0])
	assert.JSONEq(t, `{"status":"duplicate"}`, bodies[1], "[AC-034] a repeat is answered duplicate")
	assert.JSONEq(t, `{"status":"duplicate"}`, bodies[2])
	assert.Len(t, f.eventsOfType(models.EventAgentUpdateStarted), 1, "[AC-034] exactly one start is stored")
	assert.Len(t, f.hub.eventsOf(models.EventAgentUpdateStarted), 1, "[AC-034] and broadcast")
	recs := f.records(ex2Hawaii)
	require.Len(t, recs, 1, "[AC-034] exactly one record")
	assert.True(t, recs[0].DeadlineAt.Equal(ex2T0.Add(models.ExpectSelfUpdateBound)), "[AC-034] its window ends at its original bound")
	assert.Len(t, f.hub.commands, 1, "[C-3 a] the create is announced once; the repeats make no command_update broadcast")
	stored := f.eventsOfType(models.EventAgentUpdateStarted)[0]
	assert.Equal(t, recs[0].ID, stored.CommandID, "the stored start is linked to the record")
	assert.Equal(t, models.CommandAttrAgent, stored.CommandAttr)

	// A failed event with a command reference, twice.
	c := f.seedCmd(hawaiiCmd, ex2Hawaii, models.ActionAgentUpdate, models.OriginUser, "running", true)
	before := f.command(c.ID)
	first := f.post(ex2Hawaii, evBody("agent_update_failed", "Agent update failed: x", map[string]string{"command_id": c.ID}), ex2Hawaii)
	second := f.post(ex2Hawaii, evBody("agent_update_failed", "Agent update failed: x", map[string]string{"command_id": c.ID}), ex2Hawaii)
	assert.JSONEq(t, `{"status":"ok"}`, first.Body)
	assert.JSONEq(t, `{"status":"duplicate"}`, second.Body, "[AC-034] the repeat is a success")
	failed := f.eventsOfType(models.EventAgentUpdateFailed)
	require.Len(t, failed, 1, "[AC-034] exactly one failed event is stored")
	assert.Equal(t, c.ID, failed[0].CommandID)
	assert.Len(t, f.hub.eventsOf(models.EventAgentUpdateFailed), 1)
	assert.Equal(t, before, f.command(c.ID), "[AC-034] C is unchanged by the events and the repeat")
	for _, call := range f.rec.calls() {
		parts := strings.Split(call, "|")
		assert.NotEmpty(t, parts[1], "[C-3 b] HasCommandEvent is never called with an empty command ID: %s", call)
	}
}

func TestEX7_AC034_CappedAndFailOpenAttemptStartsStored(t *testing.T) {
	t.Run("a capped start", func(t *testing.T) {
		f := newEX7Fixture(t)
		for i := 0; i < models.SelfUpdateRateCap; i++ {
			f.clock.Set(ex2T0.Add(time.Duration(i) * 2 * time.Hour))
			id := strings.Repeat(string(rune('a'+i)), 32)
			require.Equal(t, http.StatusOK, f.startA(id).Code)
			f.clock.Set(f.clock.Now().Add(time.Minute))
			f.post(ex2Hawaii, evBody("agent_update_completed", "ok", map[string]string{"attempt_id": id}), ex2Hawaii)
		}
		f.clock.Set(ex2T0.Add(13 * time.Hour))
		startsBefore := len(f.eventsOfType(models.EventAgentUpdateStarted))
		for i := 0; i < 2; i++ { // the same capped start twice: both are stored
			resp := f.startA(ex7AttemptB)
			assert.Equal(t, http.StatusOK, resp.Code)
			assert.JSONEq(t, `{"status":"ok"}`, resp.Body, "[C-3 b] a capped start is not a duplicate")
		}
		assert.Len(t, f.eventsOfType(models.EventAgentUpdateStarted), startsBefore+2, "[C-3 b] both capped starts are stored")
		for _, call := range f.rec.calls() {
			assert.NotEmpty(t, strings.Split(call, "|")[1], "[C-3 b] no dedupe lookup with an empty command ID: %s", call)
		}
	})
	t.Run("a fail-open start", func(t *testing.T) {
		f := newEX7Fixture(t)
		f.cmds.FailOn("CreateSelfUpdate", errors.New("the record store is down"))
		for i := 0; i < 2; i++ {
			resp := f.startA(ex7Attempt)
			assert.Equal(t, http.StatusOK, resp.Code)
			assert.JSONEq(t, `{"status":"ok"}`, resp.Body, "[C-3 b] a fail-open start is not a duplicate")
		}
		stored := f.eventsOfType(models.EventAgentUpdateStarted)
		assert.Len(t, stored, 2, "[C-3 b] both are stored")
		for _, e := range stored {
			assert.Empty(t, e.CommandID)
		}
		assert.Empty(t, f.rec.calls(), "[C-3 b] HasCommandEvent is never called without a link")
	})
}

func TestEX7_AC034_DedupeReadFailureStoresEvent(t *testing.T) {
	f := newEX7Fixture(t)
	require.Equal(t, http.StatusOK, f.startA(ex7Attempt).Code)
	f.rec.mu.Lock()
	f.rec.hasErr = errors.New("the event store cannot be read")
	f.rec.mu.Unlock()

	resp := f.startA(ex7Attempt)

	assert.Equal(t, http.StatusOK, resp.Code, "[NFR-008] a failed duplicate check never fails the request")
	assert.JSONEq(t, `{"status":"ok"}`, resp.Body)
	assert.Len(t, f.eventsOfType(models.EventAgentUpdateStarted), 2, "[NFR-008] the event is stored rather than dropped")
}

func TestEX7_AC035_DoubleDeliveryStoresOnePerType(t *testing.T) {
	f := newEX7Fixture(t)
	for i := 0; i < 2; i++ {
		require.Equal(t, http.StatusOK, f.startA(ex7Attempt).Code)
	}
	f.clock.Set(ex2T0.Add(2 * time.Minute))
	for i := 0; i < 2; i++ {
		resp := f.post(ex2Hawaii, evBody("agent_update_completed", "Agent updated to 2.80.0",
			map[string]string{"attempt_id": ex7Attempt, "from_version": "2.78.0", "to_version": "2.80.0"}), ex2Hawaii)
		require.Equal(t, http.StatusOK, resp.Code)
	}
	assert.Len(t, f.eventsOfType(models.EventAgentUpdateStarted), 1, "[AC-035] one started event")
	assert.Len(t, f.eventsOfType(models.EventAgentUpdateCompleted), 1, "[AC-035] one completed event")
	recs := f.records(ex2Hawaii)
	require.Len(t, recs, 1)
	assert.Equal(t, "succeeded", recs[0].Status)
	attempt, _ := recs[0].Params[models.SelfUpdateParamAttempt].(string)
	assert.Equal(t, ex7Attempt, attempt, "[AC-035] the record carries the attempt ID")
	assert.LessOrEqual(t, len(attempt), 64)
}

func TestEX7_AC038_UpdateAvailableUnchanged(t *testing.T) {
	f := newEX7Fixture(t)
	// An available event with every new field set is stored as today: no link,
	// no record, no de-duplication.
	for i := 0; i < 2; i++ {
		resp := f.post(ex2Hawaii, evBody("agent_update_available", "Agent update available: 2.78.0 → 2.80.0",
			map[string]string{"attempt_id": ex7Attempt, "command_id": hawaiiCmd, "from_version": "2.78.0", "to_version": "2.80.0"}), ex2Hawaii)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `{"status":"ok"}`, resp.Body, "[FR-037] never answered duplicate")
	}
	stored := f.eventsOfType(models.EventAgentUpdateAvail)
	require.Len(t, stored, 2, "[FR-037] stored and broadcast as today")
	for _, e := range stored {
		assert.Empty(t, e.CommandID, "[FR-037] no link")
		assert.Empty(t, e.CommandAttr)
	}
	assert.Empty(t, f.records(ex2Hawaii))
	assert.Zero(t, f.cmds.CallCount("CreateSelfUpdate"))
	assert.Empty(t, f.rec.calls(), "no duplicate lookup")
}

func TestEX7_AC038_ExtraUpdaterIgnoresNewFields(t *testing.T) {
	f := newEventFixture(t)
	body := func(typ, outcome string) string {
		m := map[string]string{"type": typ, "command_id": euCmdID, "updater": "dietpi", "outcome": outcome, "message": "x", "severity": "info",
			"attempt_id": ex7Attempt, "from_version": "2.78.0", "to_version": "2.80.0"}
		b, _ := json.Marshal(m)
		return string(b)
	}
	rec := f.post(t, "dev-1", body("extra_updater_started", ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = f.post(t, "dev-1", body("extra_updater_started", ""))
	assert.Contains(t, rec.Body.String(), "duplicate", "[FR-038] the extra-updater de-duplication is unchanged")
	rec = f.post(t, "dev-1", body("extra_updater_finished", "success"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, f.events.Events, 2, "[FR-038] one start and one end event, as before")
	assert.Equal(t, euCmdID, f.events.Events[0].CommandID, "[FR-038] linked as before")
}
