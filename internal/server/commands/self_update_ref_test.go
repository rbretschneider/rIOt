package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-007 commands.Service tests (ADD section 8, "S"): structured versions,
// the command and attempt references of agent_update_* events, and the
// guarantees around them (device scoping, fail-open, no announcement of a
// replay). They reuse the EXPECT-002 harness (suHarness).

const (
	devB          = "dev-b"
	ex7AttemptA   = "9f1c0123456789abcdef0123456789ab"
	ex7AttemptB   = "1111222233334444aaaabbbbccccdddd"
	ex7StartMsg   = "Agent update started: 2.86.0 → 2.87.0"
	ex7DebugDiffs = "agent update start versions differ from the message"
)

// ev is one raw agent_update_* event as the handler hands it to the Service.
type ev struct {
	device              string
	typ                 models.EventType
	msg                 string
	cmdRef, attemptRef  string
	fromVersion, toVers string
}

func (h *suHarness) sendEv(e ev) AgentUpdateEventLink {
	h.t.Helper()
	if e.device == "" {
		e.device = devA
	}
	return h.svc.NoteAgentUpdateEvent(context.Background(), AgentUpdateEventInput{
		DeviceID: e.device, Hostname: suHost, Type: e.typ, Message: e.msg,
		CommandRef: e.cmdRef, AttemptRef: e.attemptRef, FromVersion: e.fromVersion, ToVersion: e.toVers, At: h.clock.Now(),
	})
}

// startA is a self-update start of hawaii with an attempt reference and
// structured versions.
func (h *suHarness) startWithAttempt(attempt string) AgentUpdateEventLink {
	h.t.Helper()
	return h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, attemptRef: attempt, fromVersion: "2.86.0", toVers: "2.87.0"})
}

// seedCommand stores a command row directly (a fabricated state the Service
// reads but never writes).
func (h *suHarness) seedCommand(id, device, action, origin, status string, dispatched, finished *time.Time) models.Command {
	h.t.Helper()
	now := h.clock.Now()
	c := models.Command{
		ID: id, DeviceID: device, Action: action, Origin: origin, Status: status,
		Params: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
		DispatchedAt: dispatched, FinishedAt: finished, TargetKeys: []string{},
	}
	h.repo.Seed(&c)
	return c
}

func ptrTime(t time.Time) *time.Time { return &t }

func (h *suHarness) updatesCount() int { return len(h.bc.Updates()) }

// TestEX7_AC011_AttemptStartStoresAttemptID: a start with an attempt reference
// creates a record that carries the attempt ID.
func TestEX7_AC011_AttemptStartStoresAttemptID(t *testing.T) {
	h := newSUHarness(t, devA)
	link := h.startWithAttempt(ex7AttemptA)

	rec := h.onlyRecord()
	assert.Equal(t, ex7AttemptA, models.SelfUpdateAttemptID(rec), "[AC-011] the record stores the attempt ID")
	from, to, ok := models.SelfUpdateVersions(rec)
	require.True(t, ok)
	assert.Equal(t, "2.86.0", from, "[AC-011] from version")
	assert.Equal(t, "2.87.0", to, "[AC-011] to version")
	assert.Equal(t, rec.ID, link.CommandID, "[AC-011] the start is linked to the record")
	assert.Equal(t, models.CommandAttrAgent, link.CommandAttr, "[AC-011] linked by a validated reference")
	assert.True(t, link.Dedupe, "[AC-035] the link names the record that owns the attempt")
	assert.Equal(t, 1, countStatus(h.obs.statuses(), "running"), "[AC-011] the create is announced once")

	// A completion with the same attempt reference closes that record.
	h.clock.Advance(40 * time.Second)
	done := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.87.0", attemptRef: ex7AttemptA, fromVersion: "2.86.0", toVers: "2.87.0"})
	assert.Equal(t, rec.ID, done.CommandID, "[AC-011] the completion is attributed to the record")
	assert.Equal(t, models.CommandAttrAgent, done.CommandAttr)
	assert.Equal(t, "succeeded", h.get(rec.ID).Status, "[AC-011] the record succeeds")
}

func TestEX7_AC013_NoReferenceAttributedAsEX2(t *testing.T) {
	t.Run("start and completion without references", func(t *testing.T) {
		h := newSUHarness(t, devA)
		link := h.start()
		rec := h.onlyRecord()
		assert.Equal(t, rec.ID, link.CommandID)
		assert.Equal(t, models.CommandAttrInferred, link.CommandAttr, "[AC-013] timing link, as EXPECT-002")
		assert.False(t, link.Dedupe, "[AC-013] a timing link is never de-duplicated")
		assert.Empty(t, models.SelfUpdateAttemptID(rec), "no attempt ID is stored")

		h.clock.Advance(time.Minute)
		done := h.send(models.EventAgentUpdateCompleted, "Agent updated to 2.80.0")
		assert.Equal(t, rec.ID, done.CommandID, "[AC-013] attributed to the newest record within 10 minutes")
		assert.Equal(t, models.CommandAttrInferred, done.CommandAttr)
		assert.False(t, done.Dedupe)
		assert.Equal(t, "succeeded", h.get(rec.ID).Status)
	})
	t.Run("a completion without a reference still reaches a record that has an attempt ID", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.startWithAttempt(ex7AttemptA)
		rec := h.onlyRecord()
		h.clock.Advance(time.Minute)
		done := h.send(models.EventAgentUpdateCompleted, "Agent updated to 2.87.0")
		assert.Equal(t, rec.ID, done.CommandID, "[AC-013] no reference: EXPECT-002 timing")
		assert.Equal(t, models.CommandAttrInferred, done.CommandAttr)
		assert.Equal(t, "succeeded", h.get(rec.ID).Status)
	})
}

func TestEX7_AC015_StructuredVersionsWinOneDebug(t *testing.T) {
	h := newSUHarness(t, devA)
	h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: "Agent update started: 2.86.0 → 2.99.0",
		attemptRef: ex7AttemptA, fromVersion: "2.86.0", toVers: "2.87.0"})

	rec := h.onlyRecord()
	from, to, ok := models.SelfUpdateVersions(rec)
	require.True(t, ok)
	assert.Equal(t, "2.86.0", from, "[AC-015] structured from wins")
	assert.Equal(t, "2.87.0", to, "[AC-015] structured to wins over the message's 2.99.0")
	assert.Equal(t, 1, strings.Count(h.logs.String(), ex7DebugDiffs), "[AC-015] exactly one debug line records the disagreement")
	assert.NotContains(t, h.logs.String(), "level=WARN", "[AC-015] no warning")
	assert.NotContains(t, h.logs.String(), "level=INFO", "[AC-015] no info line")
	assert.Empty(t, h.events.Events, "[AC-015] no event is produced")
	assert.NotContains(t, h.logs.String(), "2.99.0", "the agent's message text is not logged")

	// Agreeing versions are silent.
	h2 := newSUHarness(t, devA)
	h2.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, attemptRef: ex7AttemptA, fromVersion: "v2.86.0", toVers: "2.87.0"})
	assert.Zero(t, strings.Count(h2.logs.String(), ex7DebugDiffs), "a leading v is not a disagreement")
}

func TestEX7_AC016_InvalidStructuredFallsBackToMessage(t *testing.T) {
	cases := map[string][2]string{
		"latest is no version":    {"latest", "2.87.0"},
		"matching versions":       {"2.87.0", "2.87.0"},
		"300 byte to version":     {"2.86.0", strings.Repeat("9", 300)},
		"empty":                   {"", ""},
		"only one side":           {"2.86.0", ""},
		"control char in version": {"2.86.0\n", "2.87.0"},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			h := newSUHarness(t, devA)
			h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, attemptRef: ex7AttemptA, fromVersion: v[0], toVers: v[1]})
			rec := h.onlyRecord()
			from, to, ok := models.SelfUpdateVersions(rec)
			require.True(t, ok)
			assert.Equal(t, "2.86.0", from, "[AC-016] from comes from the message")
			assert.Equal(t, "2.87.0", to, "[AC-016] to comes from the message")
		})
	}
}

func TestEX7_AC017_BothInvalidNoRecord(t *testing.T) {
	h := newSUHarness(t, devA)
	link := h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: "Agent update started: dev → 2.87.0",
		attemptRef: ex7AttemptA, fromVersion: "latest", toVers: "2.87.0"})
	assert.Equal(t, AgentUpdateEventLink{}, link, "[AC-017] nothing recognised: no link")
	assert.Empty(t, h.records(), "[AC-017] no record")
	assert.Zero(t, h.repo.CallCount("CreateSelfUpdate"), "[AC-017] the repository is never called")
}

func TestEX7_AC018_ReplayedAttemptNoRecordNoCount(t *testing.T) {
	h := newSUHarness(t, devA)
	first := h.startWithAttempt(ex7AttemptA)
	rec := h.onlyRecord()
	h.clock.Advance(time.Minute)
	h.sendEv(ev{typ: models.EventAgentUpdateFailed, msg: "Agent update failed: x", attemptRef: ex7AttemptA})
	closed := h.get(rec.ID)
	require.Equal(t, "failed", closed.Status)

	observerBefore, updatesBefore := len(h.obs.statuses()), h.updatesCount()
	h.clock.Advance(19 * time.Minute) // 10:20
	for i := 0; i < 3; i++ {
		replay := h.startWithAttempt(ex7AttemptA)
		assert.Equal(t, first.CommandID, replay.CommandID, "[AC-018] the replay links to the existing record")
		assert.Equal(t, models.CommandAttrAgent, replay.CommandAttr)
		assert.True(t, replay.Dedupe, "[AC-034] and is a candidate for de-duplication")
		assert.Empty(t, replay.Events, "[AC-018] no cap event")
	}
	assert.Len(t, h.records(), 1, "[AC-018] no record is created and the 6-per-24h count is unchanged")
	assert.Equal(t, observerBefore, len(h.obs.statuses()), "[C-3 a] a replay makes zero observer calls")
	assert.Equal(t, updatesBefore, h.updatesCount(), "[C-3 a] and zero dashboard command_update broadcasts")
	after := h.get(rec.ID)
	assert.Equal(t, closed.Status, after.Status, "[AC-018] the record is not reopened")
	assert.True(t, closed.DeadlineAt.Equal(*after.DeadlineAt), "[AC-018] its window is not extended")
	assert.Empty(t, h.events.Events, "no event is written")
}

func TestEX7_AC019_UnknownAttemptOnlyNoAttemptRecord(t *testing.T) {
	t.Run("the open record has another attempt ID", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.startWithAttempt(ex7AttemptA)
		r1 := h.onlyRecord()
		h.clock.Advance(2 * time.Minute)
		link := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.87.0", attemptRef: ex7AttemptB})
		assert.Equal(t, AgentUpdateEventLink{}, link, "[AC-019] stored with no link")
		got := h.get(r1.ID)
		assert.Equal(t, "running", got.Status, "[AC-019] R1's outcome is unchanged")
		assert.Equal(t, r1.ResultMsg, got.ResultMsg)
	})
	t.Run("the record has no attempt ID", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start() // an older agent's start: no reference
		r0 := h.onlyRecord()
		h.clock.Advance(2 * time.Minute)
		link := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.80.0", attemptRef: ex7AttemptB})
		assert.Equal(t, r0.ID, link.CommandID, "[AC-019] attributed to R0 by the EXPECT-002 timing rule")
		assert.Equal(t, models.CommandAttrInferred, link.CommandAttr, "[AC-019] a timing link")
		assert.False(t, link.Dedupe, "[AC-019] and not de-duplicated")
		assert.Equal(t, "succeeded", h.get(r0.ID).Status)
	})
}

func TestEX7_AC021_CommandRefFailureLinksCommandNotRecord(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start() // R started 10:00
	r := h.onlyRecord()
	h.clock.Advance(time.Minute)
	h.send(models.EventAgentUpdateFailed, "Agent update failed: x") // R failed 10:01
	rFailed := h.get(r.ID)
	require.Equal(t, "failed", rFailed.Status)

	h.clock.Advance(4 * time.Minute)
	c := h.dispatchAgentUpdate() // Ryan's command, 10:05
	h.clock.Advance(30 * time.Second)
	observerBefore, updatesBefore, eventsBefore := len(h.obs.statuses()), h.updatesCount(), len(h.events.Events)

	link := h.sendEv(ev{typ: models.EventAgentUpdateFailed, msg: "Agent update failed: download: connection reset", cmdRef: c.ID})

	assert.Equal(t, c.ID, link.CommandID, "[AC-021] linked to the command")
	assert.Equal(t, models.CommandAttrAgent, link.CommandAttr, "[AC-021] by a validated reference")
	assert.True(t, link.Dedupe)
	assert.Equal(t, rFailed, h.get(r.ID), "[AC-021] R's outcome, links and fields are unchanged")
	assert.Equal(t, observerBefore, len(h.obs.statuses()), "[AC-021] nothing is announced")
	assert.Equal(t, updatesBefore, h.updatesCount())
	assert.Equal(t, eventsBefore, len(h.events.Events), "[AC-021] no self-update outcome event is written")
	assert.Len(t, h.records(), 1, "[AC-021] no record is created")
}

func TestEX7_AC022_CommandRefCompletionLinksCommand(t *testing.T) {
	h := newSUHarness(t, devA)
	h.start()
	r := h.onlyRecord()
	h.clock.Advance(2 * time.Minute)
	h.send(models.EventAgentUpdateCompleted, "Agent updated to 2.80.0") // R succeeded 10:02
	rDone := h.get(r.ID)
	require.Equal(t, "succeeded", rDone.Status)

	h.clock.Advance(4 * time.Minute)
	c := h.dispatchAgentUpdate() // 10:06
	h.clock.Advance(20 * time.Second)
	link := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.88.0", cmdRef: c.ID})

	assert.Equal(t, c.ID, link.CommandID, "[AC-022] linked to the command, not to R")
	assert.Equal(t, models.CommandAttrAgent, link.CommandAttr)
	assert.Equal(t, rDone, h.get(r.ID), "[AC-022] R is untouched")
	assert.Equal(t, "dispatched", h.get(c.ID).Status, "[AC-021] the command itself is not changed by the event")
}

// TestEX7_AC023_InvalidCommandRefsFallBack runs ten invalid command references.
// Each case runs in its own harness. Every start is processed exactly as
// EXPECT-002 processes a start without a reference: a record is created, the
// link names that record and never the referenced row, and the referenced row
// is untouched.
func TestEX7_AC023_InvalidCommandRefsFallBack(t *testing.T) {
	now := func(h *suHarness) time.Time { return h.clock.Now() }
	vermontCmd := uid(101)
	// Until EXPECT-008 narrows the timing predicate of CreateSelfUpdate step 1, an
	// agent_update row of a synthetic server-created origin is captured by the
	// EXPECT-002 timing rule. What this test pins (C-2) is that the REFERENCE
	// never links it: the link is a timing link, never command_attr "agent".
	timingCaptured := map[string]bool{"a server-created origin other than agent_self_update (C-2)": true}
	cases := []struct {
		name  string
		setup func(h *suHarness) (ref string, row *models.Command)
	}{
		{"another device's real dispatched agent_update command", func(h *suHarness) (string, *models.Command) {
			c := h.seedCommand(vermontCmd, devB, models.ActionAgentUpdate, models.OriginUser, "running", ptrTime(now(h)), nil)
			return c.ID, &c
		}},
		{"unknown command", func(h *suHarness) (string, *models.Command) { return uid(102), nil }},
		{"another action", func(h *suHarness) (string, *models.Command) {
			c := h.seedCommand(uid(103), devA, models.ActionDockerUpdate, models.OriginUser, "running", ptrTime(now(h)), nil)
			return c.ID, &c
		}},
		{"the device's own self-update record", func(h *suHarness) (string, *models.Command) {
			c := h.seedCommand(uid(104), devA, models.ActionAgentUpdate, models.OriginAgentSelfUpdate, "succeeded",
				ptrTime(now(h).Add(-time.Hour)), ptrTime(now(h).Add(-50*time.Minute)))
			return c.ID, &c
		}},
		{"queued and never dispatched", func(h *suHarness) (string, *models.Command) {
			c := h.seedCommand(uid(105), devA, models.ActionAgentUpdate, models.OriginUser, "queued", nil, nil)
			return c.ID, &c
		}},
		{"finished 11 minutes earlier", func(h *suHarness) (string, *models.Command) {
			c := h.seedCommand(uid(106), devA, models.ActionAgentUpdate, models.OriginUser, "succeeded",
				ptrTime(now(h).Add(-15*time.Minute)), ptrTime(now(h).Add(-11*time.Minute)))
			return c.ID, &c
		}},
		{"300 byte string", func(h *suHarness) (string, *models.Command) { return strings.Repeat("a", 300), nil }},
		{"control characters", func(h *suHarness) (string, *models.Command) { return "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c\x00\n", nil }},
		{"the EXPECT-008 installer origin (C-2, SEC-002)", func(h *suHarness) (string, *models.Command) {
			c := h.seedCommand(uid(109), devA, models.ActionAgentUpdate, models.OriginInstaller, "running", ptrTime(now(h)), nil)
			return c.ID, &c
		}},
		{"a terminal row with finished_at nil (C-3 c)", func(h *suHarness) (string, *models.Command) {
			c := h.seedCommand(uid(110), devA, models.ActionAgentUpdate, models.OriginUser, "failed", ptrTime(now(h).Add(-time.Minute)), nil)
			return c.ID, &c
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSUHarness(t, devA, devB)
			ref, row := tc.setup(h)
			recordsBefore := len(h.records())
			var before models.Command
			if row != nil {
				before = h.get(row.ID)
			}

			var link AgentUpdateEventLink
			require.NotPanics(t, func() {
				link = h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, cmdRef: ref})
			}, "a crafted reference must never panic")

			assert.NotEmpty(t, link.CommandID)
			if timingCaptured[tc.name] {
				// EXPECT-002's own timing rule (its SQL excludes only the
				// agent_self_update origin) attributes the start to this row, exactly as
				// it would without a reference. The reference is not what links it.
				assert.Len(t, h.records(), recordsBefore, "processed as EXPECT-002 processes a start without a reference")
				assert.Equal(t, row.ID, link.CommandID, "EXPECT-002's timing rule captures the start")
			} else {
				assert.Len(t, h.records(), recordsBefore+1, "processed as EXPECT-002 processes a start without a reference: a record is created")
				if row != nil {
					assert.NotEqual(t, row.ID, link.CommandID, "never linked to the referenced row")
				}
			}
			if row != nil {
				assert.Equal(t, before, h.get(row.ID), "the referenced row is unchanged")
			}
			assert.Equal(t, models.CommandAttrInferred, link.CommandAttr, "a timing link, as EXPECT-002")
			assert.False(t, link.Dedupe, "an invalid reference never marks the link for de-duplication")
			assert.NotContains(t, h.logs.String(), "level=WARN", "no warning for a bad reference")
			assert.NotContains(t, h.logs.String(), "\x00", "agent-supplied bytes are never logged")
		})
	}

	t.Run("a valid reference links, creates nothing and is not a timing link", func(t *testing.T) {
		h := newSUHarness(t, devA)
		c := h.seedCommand(uid(120), devA, models.ActionAgentUpdate, models.OriginAutoUpdate, "running", ptrTime(h.clock.Now()), nil)
		link := h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, cmdRef: c.ID})
		assert.Equal(t, c.ID, link.CommandID)
		assert.Equal(t, models.CommandAttrAgent, link.CommandAttr)
		assert.True(t, link.Dedupe)
		assert.Empty(t, h.records(), "no record")
		assert.Zero(t, h.repo.CallCount("CreateSelfUpdate"), "the record logic is not consulted")
	})
}

func TestEX7_AC024_BothRefsProcessedAsEX2(t *testing.T) {
	h := newSUHarness(t, devA)
	c := h.dispatchAgentUpdate()
	h.clock.Advance(2 * time.Second)

	link := h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, cmdRef: c.ID, attemptRef: ex7AttemptA, fromVersion: "2.86.0", toVers: "2.87.0"})

	assert.Equal(t, c.ID, link.CommandID, "[AC-024] the in-flight command captures the start by timing, as EXPECT-002")
	assert.Equal(t, models.CommandAttrInferred, link.CommandAttr, "[AC-024] a timing link, not a reference link")
	assert.False(t, link.Dedupe, "[AC-024] the references were not used")
	assert.Empty(t, h.records(), "[AC-024] no record, and no attempt ID stored anywhere")
}

func TestEX7_AC028_StoreUnreadableFailsOpen(t *testing.T) {
	t.Run("the command cannot be read", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.repo.FailOn("GetForDevice", errors.New("database is down"))
		var link AgentUpdateEventLink
		require.NotPanics(t, func() {
			link = h.sendEv(ev{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, cmdRef: uid(130)})
		})
		rec := h.onlyRecord()
		assert.Equal(t, rec.ID, link.CommandID, "[AC-028] processed as EXPECT-002 without a reference: a record is created")
		assert.False(t, link.Dedupe)
		assert.NotContains(t, h.logs.String(), "level=WARN", "an unreadable reference is a debug matter")
	})
	t.Run("the start cannot be recorded", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.repo.FailOn("CreateSelfUpdate", errors.New("database is down"))
		link := h.startWithAttempt(ex7AttemptA)
		assert.Equal(t, AgentUpdateEventLink{}, link, "[AC-028] an empty link, so the event is stored and the device keeps alerting")
		assert.False(t, link.Dedupe, "[C-3 b] a fail-open link is never de-duplicated")
		assert.Empty(t, h.records())
		assert.Equal(t, 1, strings.Count(h.logs.String(), "level=WARN"), "one throttled warning")
		assert.NotContains(t, h.logs.String(), ex7AttemptA, "the attempt ID is never logged")
	})
	t.Run("the record cannot be read by attempt", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.startWithAttempt(ex7AttemptA)
		rec := h.onlyRecord()
		h.clock.Advance(time.Minute)
		h.repo.FailOn("SelfUpdateByAttempt", errors.New("database is down"))
		link := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.87.0", attemptRef: ex7AttemptA})
		assert.Equal(t, rec.ID, link.CommandID, "[AC-028] falls back to the timing rule, as EXPECT-002")
		assert.Equal(t, models.CommandAttrInferred, link.CommandAttr)
		assert.False(t, link.Dedupe)
		assert.NotContains(t, h.logs.String(), ex7AttemptA, "the attempt ID is never logged")
	})
	t.Run("both reads fail", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.repo.Err = errors.New("database is down")
		for _, e := range []ev{
			{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, cmdRef: uid(131)},
			{typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, attemptRef: ex7AttemptA},
			{typ: models.EventAgentUpdateFailed, msg: "x", attemptRef: ex7AttemptA},
			{typ: models.EventAgentUpdateCompleted, msg: "x", cmdRef: uid(132)},
		} {
			assert.NotPanics(t, func() {
				assert.Equal(t, AgentUpdateEventLink{}, h.sendEv(e), "[AC-028] an unreadable store yields no link")
			})
		}
	})
}

func TestEX7_AC029_OtherDeviceRefsIgnored(t *testing.T) {
	h := newSUHarness(t, devA, devB)
	h.startWithAttempt(ex7AttemptA) // hawaii's open record with attempt A
	rec := h.onlyRecord()
	cmd := h.seedCommand(uid(140), devA, models.ActionAgentUpdate, models.OriginUser, "running", ptrTime(h.clock.Now()), nil)
	recBefore, cmdBefore := h.get(rec.ID), h.get(cmd.ID)
	observerBefore, updatesBefore := len(h.obs.statuses()), h.updatesCount()
	h.clock.Advance(time.Minute)

	done := h.sendEv(ev{device: devB, typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.87.0", attemptRef: ex7AttemptA})
	failed := h.sendEv(ev{device: devB, typ: models.EventAgentUpdateFailed, msg: "x", cmdRef: cmd.ID})

	assert.Equal(t, AgentUpdateEventLink{}, done, "[AC-029] vermont has no record: processed as for no reference, unlinked")
	assert.Equal(t, AgentUpdateEventLink{}, failed, "[AC-029] hawaii's real command ID names nothing for vermont")
	assert.Equal(t, recBefore, h.get(rec.ID), "[AC-029] hawaii's record is unchanged")
	assert.Equal(t, cmdBefore, h.get(cmd.ID), "[AC-029] hawaii's command is unchanged")
	assert.Equal(t, observerBefore, len(h.obs.statuses()), "nothing is announced")
	assert.Equal(t, updatesBefore, h.updatesCount())

	// Vermont's own start with hawaii's attempt ID is vermont's own attempt.
	own := h.sendEv(ev{device: devB, typ: models.EventAgentUpdateStarted, msg: ex7StartMsg, attemptRef: ex7AttemptA})
	assert.NotEqual(t, rec.ID, own.CommandID, "[AC-029] a record of hawaii never matches vermont's start")
	assert.Len(t, h.records(), 2, "vermont gets its own record")
}

func TestEX7_FR019_OutcomeVersionsIgnored(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.startWithAttempt(ex7AttemptA)
		rec := h.onlyRecord()
		h.clock.Advance(time.Minute)
		link := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.87.0", attemptRef: ex7AttemptA, fromVersion: "9.9.9", toVers: "9.9.8"})
		assert.Equal(t, rec.ID, link.CommandID, "[FR-019] a disagreement does not prevent attribution")
		got := h.get(rec.ID)
		assert.Equal(t, "succeeded", got.Status)
		from, to, _ := models.SelfUpdateVersions(got)
		assert.Equal(t, "2.86.0", from, "[FR-019] the record's versions are not changed")
		assert.Equal(t, "2.87.0", to)
		assert.NotContains(t, got.ResultMsg, "9.9.9")
	})
	t.Run("failed", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.startWithAttempt(ex7AttemptA)
		rec := h.onlyRecord()
		h.clock.Advance(time.Minute)
		h.sendEv(ev{typ: models.EventAgentUpdateFailed, msg: "boom", attemptRef: ex7AttemptA, fromVersion: "9.9.9", toVers: "9.9.8"})
		got := h.get(rec.ID)
		assert.Equal(t, "failed", got.Status)
		assert.Contains(t, got.ResultMsg, "2.86.0 → 2.87.0", "[FR-019] the label uses the record's own versions")
		assert.NotContains(t, got.ResultMsg, "9.9.9")
	})
}

func TestEX7_FR025_AttemptMatchAnyAge(t *testing.T) {
	t.Run("an open record is closed by its attempt however late", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.startWithAttempt(ex7AttemptA)
		rec := h.onlyRecord()
		h.clock.Advance(25 * time.Minute) // far past the 10-minute timing window
		link := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "Agent updated to 2.87.0", attemptRef: ex7AttemptA})
		assert.Equal(t, rec.ID, link.CommandID, "[FR-025] attributed whatever the record's age")
		assert.Equal(t, models.CommandAttrAgent, link.CommandAttr)
		assert.Equal(t, "succeeded", h.get(rec.ID).Status)
	})
	t.Run("a terminal record is linked only", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.startWithAttempt(ex7AttemptA)
		rec := h.onlyRecord()
		h.sendEv(ev{typ: models.EventAgentUpdateFailed, msg: "x", attemptRef: ex7AttemptA})
		failed := h.get(rec.ID)
		h.clock.Advance(3 * time.Hour)
		before := len(h.obs.statuses())
		link := h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "later", attemptRef: ex7AttemptA})
		assert.Equal(t, rec.ID, link.CommandID, "[FR-025] still linked")
		assert.Equal(t, failed, h.get(rec.ID), "[FR-022] the outcome is set once")
		assert.Equal(t, before, len(h.obs.statuses()))
	})
}

// TestEX7_AC034_DedupeFlagOnlyForReferenceDerivedLinks pins the invariant the
// handler relies on (security condition C-3 b).
func TestEX7_AC034_DedupeFlagOnlyForReferenceDerivedLinks(t *testing.T) {
	t.Run("a capped start with an attempt reference", func(t *testing.T) {
		h := newSUHarness(t, devA)
		base := h.clock.Now()
		for i := 0; i < models.SelfUpdateRateCap; i++ {
			h.setClock(base.Add(time.Duration(i) * 2 * time.Hour))
			link := h.startWithAttempt(strings.Repeat(string(rune('a'+i)), 32))
			require.NotEmpty(t, link.CommandID)
			h.clock.Advance(time.Minute)
			h.sendEv(ev{typ: models.EventAgentUpdateCompleted, msg: "ok", attemptRef: strings.Repeat(string(rune('a'+i)), 32)})
		}
		h.setClock(base.Add(13 * time.Hour))
		capped := h.startWithAttempt(ex7AttemptB)
		assert.Empty(t, capped.CommandID, "over the cap: no link")
		assert.False(t, capped.Dedupe, "[C-3 b] a capped start is never de-duplicated")
	})
	t.Run("a start linked by timing", func(t *testing.T) {
		h := newSUHarness(t, devA)
		h.start()
		again := h.startWithAttempt(ex7AttemptA) // the open record absorbs it
		assert.NotEmpty(t, again.CommandID)
		assert.Equal(t, models.CommandAttrInferred, again.CommandAttr, "the open record of another attempt is a timing link")
		assert.False(t, again.Dedupe, "[C-3 b] and is not de-duplicated")
	})
}
