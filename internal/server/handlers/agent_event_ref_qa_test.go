package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer (EXPECT-007). Fixtures and the injected clock are the ones
// of agent_event_ref_test.go; nothing sleeps.

// Covers AC-023, NFR-007, NFR-008. A reference field of the wrong JSON type
// (a number, an object, an array, null) is hostile input like any other. The
// request may be refused or answered, but it must never answer 5xx, never link
// anything, and never create a self-update record or open a window: a refused
// request fails toward alerting.
// Observed today: a wrong JSON type is refused 400 by the decoder (the same as for any
// other field of the event), a well-typed but invalid value is answered 200. The ADD
// table says malformed means 200; the 400 is a QA note, not asserted here.
func TestEX7_AC023_QA_WrongTypedReferenceFieldsNeverCreateOrLink(t *testing.T) {
	f := newEX7Fixture(t)
	cmd := f.seedCmd(hawaiiCmd, ex2Hawaii, models.ActionAgentUpdate, models.OriginUser, "running", true)
	before := f.command(cmd.ID)

	cases := map[string]string{
		"number attempt_id":  `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","attempt_id":123456789012345678}`,
		"object attempt_id":  `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","attempt_id":{"a":1}}`,
		"array command_id":   `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","command_id":["` + hawaiiCmd + `"]}`,
		"number from":        `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","from_version":2.78}`,
		"null reference":     `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","attempt_id":null,"command_id":null}`,
		"nul in the attempt": `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","attempt_id":"ab\u0000cd"}`,
		"unicode lookalike":  `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","attempt_id":"abcdef‐abcdef"}`,
		"injection payload":  `{"type":"agent_update_started","severity":"info","message":"` + ex2Start + `","attempt_id":"x'; DROP TABLE commands;--"}`,
	}
	for name, body := range cases {
		rr := f.do("POST", "/api/v1/devices/"+ex2Hawaii+"/events", body, ex2Hawaii)
		assert.Less(t, rr.Code, 500, "[NFR-007] %s: never a server error (got %d %s)", name, rr.Code, rr.Body.String())
	}
	for _, e := range f.eventsOfType(models.EventAgentUpdateStarted) {
		assert.NotEqual(t, models.CommandAttrAgent, e.CommandAttr, "[NFR-007] a malformed reference never produces a reference link")
		if e.CommandID != "" {
			assert.Equal(t, models.CommandAttrInferred, e.CommandAttr, "[NFR-007] any link is the EXPECT-002 timing link, never a reference link")
		}
	}
	assert.Equal(t, before, f.command(cmd.ID), "[NFR-007] the command is unchanged")
	for _, rec := range f.records(ex2Hawaii) {
		assert.Empty(t, models.SelfUpdateAttemptID(rec), "[NFR-007] a self-update record created by the fall-back path never stores a malformed attempt ID")
	}
}

// Covers AC-029, FR-020, BR-003, FR-035. Two devices may legitimately draw the
// same attempt ID (a clone of a disk image shares the agent's state files). The
// replay rules and the de-duplication of one device must not swallow the other
// device's first start: each device gets its own record and its own stored event.
func TestEX7_AC029_QA_SameAttemptIDOnTwoDevicesIsIndependent(t *testing.T) {
	f := newEX7Fixture(t)

	h1 := f.startA(ex7Attempt)
	require.Equal(t, http.StatusOK, h1.Code)
	h2 := f.post(ex2Vermont, evBody("agent_update_started", ex2Start,
		map[string]string{"attempt_id": ex7Attempt, "from_version": "2.78.0", "to_version": "2.80.0"}), ex2Vermont)
	require.Equal(t, http.StatusOK, h2.Code)
	assert.JSONEq(t, `{"status":"ok"}`, h2.Body, "[AC-029] vermont's first start is not a duplicate of hawaii's")

	require.Len(t, f.records(ex2Hawaii), 1, "[AC-029] hawaii has its record")
	require.Len(t, f.records(ex2Vermont), 1, "[AC-029] vermont has its own record")
	assert.NotEqual(t, f.records(ex2Hawaii)[0].ID, f.records(ex2Vermont)[0].ID)
	assert.Len(t, f.eventsOfType(models.EventAgentUpdateStarted), 2, "[AC-029] both starts are stored")

	again := f.post(ex2Vermont, evBody("agent_update_started", ex2Start,
		map[string]string{"attempt_id": ex7Attempt, "from_version": "2.78.0", "to_version": "2.80.0"}), ex2Vermont)
	assert.JSONEq(t, `{"status":"duplicate"}`, again.Body, "[FR-035] vermont's own repeat is a duplicate")
	assert.Len(t, f.eventsOfType(models.EventAgentUpdateStarted), 2, "[FR-035] and is not stored")
}
