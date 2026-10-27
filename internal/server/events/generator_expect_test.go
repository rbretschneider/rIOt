package events

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The generator side of EXPECT-001 (ADD section 8, "G"). The tracker is replaced
// by an oracle that returns what the test says, so these tests pin what the
// generator does with each answer.

var expectBase = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

type fakeOracle struct {
	mu sync.Mutex

	cover    models.Coverage
	coverOK  bool
	coverErr error

	off    models.OfflineCoverage
	offOK  bool
	offErr error

	note *models.OnlineNote

	coverKeys  [][]string
	offlineAts []time.Time
	onlineAts  []time.Time
}

func (f *fakeOracle) CoverContainer(_ string, keys []string, _ time.Time) (models.Coverage, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.coverKeys = append(f.coverKeys, keys)
	return f.cover, f.coverOK, f.coverErr
}

func (f *fakeOracle) CoverOffline(_ context.Context, _ string, at time.Time) (models.OfflineCoverage, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offlineAts = append(f.offlineAts, at)
	return f.off, f.offOK, f.offErr
}

func (f *fakeOracle) NoteOnline(_ context.Context, _ string, at time.Time) *models.OnlineNote {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onlineAts = append(f.onlineAts, at)
	return f.note
}

type expectRig struct {
	gen    *Generator
	events *testutil.MockEventRepo
	rules  *testutil.MockAlertRuleRepo
	disp   *testutil.MockDispatcher
	oracle *fakeOracle
	now    time.Time
}

func newExpectRig(t *testing.T) *expectRig {
	t.Helper()
	r := &expectRig{
		events: testutil.NewMockEventRepo(), rules: testutil.NewMockAlertRuleRepo(),
		disp: testutil.NewMockDispatcher(), oracle: &fakeOracle{}, now: expectBase,
	}
	hub := websocket.NewHub()
	go hub.Run()
	r.gen = NewGenerator(r.events, hub, r.rules, r.disp, testutil.NewMockCommandRepo())
	r.gen.SetClock(func() time.Time { return r.now })
	r.gen.SetExpectedActivity(r.oracle)
	return r
}

func (r *expectRig) addRule(metric string, cooldown int, notify bool, exclude string) {
	r.rules.Rules = append(r.rules.Rules, models.AlertRule{
		ID: int64(len(r.rules.Rules) + 1), Enabled: true, Metric: metric, Operator: ">", Threshold: 0,
		Severity: "critical", CooldownSeconds: cooldown, Notify: notify, ExcludeContainers: exclude,
	})
	r.gen.InvalidateRulesCache()
}

func (r *expectRig) stored() []models.Event {
	return append([]models.Event(nil), r.events.Events...)
}

var rebootCover = models.Coverage{
	CommandID: "cmd-aaaaaaaa-1111", ShortID: "cmd-aaaa", Action: "reboot", ActionLabel: "Reboot",
	ActorPhrase: "by ryan", Class: models.WindowHost,
}

func dieEvent(name string) *models.DockerEvent {
	return &models.DockerEvent{ContainerName: name, ContainerID: "0123456789abcdef0123456789abcdef", Action: "die", TargetKeys: []string{"host", "docker:container:" + name}}
}

func TestEX_G_CoveredDeathIsInfoLinkedAndNotNotified(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("container_died", 900, true, "")
	r.oracle.cover, r.oracle.coverOK = rebootCover, true

	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", dieEvent("web"))

	evts := r.stored()
	require.Len(t, evts, 1, "one event is stored")
	e := evts[0]
	assert.Equal(t, models.EventContainerDied, e.Type)
	assert.Equal(t, models.SeverityInfo, e.Severity, "a covered death is info")
	assert.Equal(t, "Container web died during Reboot by ryan (expected)", e.Message)
	assert.Equal(t, rebootCover.CommandID, e.CommandID, "the event is linked to the covering command")
	assert.Equal(t, models.CommandAttrExpected, e.CommandAttr)
	assert.Equal(t, "web", e.ContainerName)
	assert.Equal(t, "0123456789ab", e.ContainerID, "the 12-character hex short id")
	assert.Empty(t, r.disp.Alerts, "no notification is dispatched")
	assert.Equal(t, [][]string{{"host", "docker:container:web"}}, r.oracle.coverKeys, "the event's target keys reach the tracker")
}

func TestEX_G_CoveredDeathStartsNoCooldown(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("container_died", 900, true, "")
	r.oracle.cover, r.oracle.coverOK = rebootCover, true
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", dieEvent("web"))

	r.oracle.coverOK = false
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", dieEvent("db"))

	require.Len(t, r.disp.Alerts, 1, "the first uncovered death alerts: the covered one started no cooldown")
	assert.Equal(t, "Container db died", r.disp.Alerts[0].Event.Message)
}

func TestEX_G_UncoveredAndUndecidableDeathsAlertAsToday(t *testing.T) {
	cases := map[string]func(o *fakeOracle){
		"not covered":       func(o *fakeOracle) {},
		"tracker not ready": func(o *fakeOracle) { o.coverErr = errors.New("not ready"); o.cover, o.coverOK = rebootCover, true },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newExpectRig(t)
			r.addRule("container_died", 900, true, "")
			setup(r.oracle)
			r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", dieEvent("web"))
			evts := r.stored()
			require.Len(t, evts, 1)
			assert.Equal(t, models.EventSeverity("critical"), evts[0].Severity, "the rule's severity")
			assert.Equal(t, "Container web died", evts[0].Message)
			assert.Empty(t, evts[0].CommandAttr, "no expected marker")
			assert.Len(t, r.disp.Alerts, 1, "and the notification goes out")
		})
	}
}

func TestEX_G_ExcludedContainerNeverReachesTheTracker(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("container_died", 0, true, "web")
	r.oracle.cover, r.oracle.coverOK = rebootCover, true
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", dieEvent("web"))
	assert.Empty(t, r.stored(), "an excluded container stores nothing, inside or outside a window")
	assert.Empty(t, r.oracle.coverKeys, "the exclusion is checked before the window lookup")
	assert.Empty(t, r.disp.Alerts)
}

func TestEX_G_ContainerNameInEventsUsesTheKeyForm(t *testing.T) {
	r := newExpectRig(t)
	r.oracle.cover, r.oracle.coverOK = rebootCover, true
	hostile := "/we‮b\x00app"
	evt := dieEvent(hostile)
	evt.ContainerID = "../../etc/passwd-zz"
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", evt)
	evts := r.stored()
	require.Len(t, evts, 1)
	key := models.ContainerNameKey(hostile)
	assert.Equal(t, key, evts[0].ContainerName, "the stored name is the key form")
	assert.Contains(t, evts[0].Message, key)
	assert.NotContains(t, evts[0].Message, "‮", "no bidi override in the message")
	assert.NotContains(t, evts[0].Message, "\x00")
	assert.Equal(t, "eca", evts[0].ContainerID, "only [0-9a-f] survives of the first 12 characters")
}

func TestEX_AC009_OOMStillAlertsAndNamesTheAction(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("container_oom", 0, true, "")
	r.oracle.cover, r.oracle.coverOK = rebootCover, true
	evt := dieEvent("web")
	evt.Action = "oom"
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", evt)
	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, models.EventContainerOOM, evts[0].Type)
	assert.Equal(t, models.EventSeverity("critical"), evts[0].Severity, "an OOM kill is not downgraded")
	assert.Equal(t, "Container web OOM killed (during Reboot by ryan, command cmd-aaaa)", evts[0].Message)
	assert.Len(t, r.disp.Alerts, 1, "and it notifies")
}

func TestEX_G_OOMOutsideAWindowIsUnchanged(t *testing.T) {
	r := newExpectRig(t)
	evt := dieEvent("web")
	evt.Action = "oom"
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", evt)
	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, "Container web OOM killed", evts[0].Message)
}

func TestEX_FR014_UpdateStartedNoLongerDowngradesDeaths(t *testing.T) {
	r := newExpectRig(t)
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", &models.DockerEvent{ContainerName: "stack", Action: "update_started"})
	r.gen.CheckDockerEvent(context.Background(), "hawaii", "hawaii", dieEvent("db"))
	evts := r.stored()
	require.Len(t, evts, 2)
	assert.Equal(t, models.SeverityWarning, evts[1].Severity, "the per-device update counter is gone: only a command window downgrades")
	assert.Equal(t, "Container db died", evts[1].Message)
}

func TestEX_G_OfflineInsideAWindowIsInfoAndLinked(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("device_offline", 300, true, "")
	back := expectBase.Add(15 * time.Minute)
	r.oracle.off, r.oracle.offOK = models.OfflineCoverage{
		Kind: models.OfflineKindWindow, CommandID: "cmd-aaaaaaaa-1111", ShortID: "cmd-aaaa", Action: "reboot",
		ActionLabel: "Reboot", ActorPhrase: "by ryan", BackBy: &back,
	}, true

	r.gen.DeviceOfflineAt(context.Background(), "hawaii", "hawaii", nil)

	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, models.SeverityInfo, evts[0].Severity)
	assert.Contains(t, evts[0].Message, "went offline during Reboot by ryan (expected, back by")
	assert.Equal(t, "cmd-aaaaaaaa-1111", evts[0].CommandID)
	assert.Equal(t, models.CommandAttrExpected, evts[0].CommandAttr)
	assert.Empty(t, r.disp.Alerts, "no notification")
}

func TestEX_G_PoweredOffTransitionIsInfoWithItsMessage(t *testing.T) {
	r := newExpectRig(t)
	r.oracle.off, r.oracle.offOK = models.OfflineCoverage{
		Kind: models.OfflineKindPoweredOff, CommandID: "cmd-bbbbbbbb-2222", ActorPhrase: "by ryan", ShutdownAt: expectBase,
	}, true
	r.gen.DeviceOfflineAt(context.Background(), "hawaii", "hawaii", nil)
	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, "Device hawaii powered off by ryan at "+models.FormatDisplayClock(expectBase, nil), evts[0].Message)
	assert.Equal(t, models.SeverityInfo, evts[0].Severity)
}

func TestEX_G_UncoveredOfflineAlertsAsToday(t *testing.T) {
	cases := map[string]func(o *fakeOracle){
		"not covered":       func(o *fakeOracle) {},
		"tracker not ready": func(o *fakeOracle) { o.offErr = errors.New("not ready"); o.offOK = true },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newExpectRig(t)
			r.addRule("device_offline", 300, true, "")
			setup(r.oracle)
			r.gen.DeviceOfflineAt(context.Background(), "hawaii", "hawaii", nil)
			evts := r.stored()
			require.Len(t, evts, 1)
			assert.Equal(t, models.EventSeverity("critical"), evts[0].Severity)
			assert.Equal(t, "Device hawaii went offline", evts[0].Message)
			assert.Len(t, r.disp.Alerts, 1)
		})
	}
}

func TestEX_G_OfflineAtIsTheEarlierOfNowAndHeartbeatPlusThreshold(t *testing.T) {
	r := newExpectRig(t)
	stale := expectBase.Add(-10 * time.Minute)
	fresh := expectBase.Add(-10 * time.Second)
	r.gen.DeviceOfflineAt(context.Background(), "hawaii", "hawaii", &stale)
	r.gen.DeviceOfflineAt(context.Background(), "vermont", "vermont", &fresh)
	r.gen.DeviceOfflineAt(context.Background(), "picups", "picups", nil)
	assert.Equal(t, []time.Time{
		stale.Add(models.OfflineHeartbeatThreshold), expectBase, expectBase,
	}, r.oracle.offlineAts, "min(now, last heartbeat + 45s)")
}

func TestEX_G_DeviceOfflineDelegatesWithoutHeartbeat(t *testing.T) {
	r := newExpectRig(t)
	r.gen.DeviceOffline(context.Background(), "hawaii", "hawaii")
	assert.Equal(t, []time.Time{expectBase}, r.oracle.offlineAts)
}

func TestEX_G_OfflineCooldownUsesTheInjectedClock(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("device_offline", 300, true, "")
	ctx := context.Background()
	r.gen.DeviceOffline(ctx, "hawaii", "hawaii")
	r.now = r.now.Add(299 * time.Second)
	r.gen.DeviceOffline(ctx, "hawaii", "hawaii")
	assert.Len(t, r.disp.Alerts, 1, "inside the cooldown")
	r.now = r.now.Add(2 * time.Second)
	r.gen.DeviceOffline(ctx, "hawaii", "hawaii")
	assert.Len(t, r.disp.Alerts, 2, "after the cooldown")
}

func TestEX_G_NoteOnlineRunsBeforeTheOnlineCooldown(t *testing.T) {
	r := newExpectRig(t)
	ctx := context.Background()
	r.gen.DeviceOnline(ctx, "hawaii", "hawaii")
	r.gen.DeviceOnline(ctx, "hawaii", "hawaii")
	assert.Len(t, r.oracle.onlineAts, 2, "the tracker is told of both returns")
	assert.Len(t, r.stored(), 1, "the second online event is held by its 5-minute cooldown")
}

func TestEX_G_OnlineAfterAWindowUsesTheNote(t *testing.T) {
	r := newExpectRig(t)
	r.oracle.note = &models.OnlineNote{CommandID: "cmd-aaaaaaaa-1111", ActionLabel: "Reboot", ActorPhrase: "by ryan", Offline: 90 * time.Second}
	r.gen.DeviceOnline(context.Background(), "hawaii", "hawaii")
	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, "Device hawaii came online after Reboot by ryan (offline 1m30s)", evts[0].Message)
	assert.Equal(t, models.EventDeviceOnline, evts[0].Type)
	assert.Equal(t, "cmd-aaaaaaaa-1111", evts[0].CommandID)
	assert.Equal(t, models.CommandAttrExpected, evts[0].CommandAttr)
}

func TestEX_G_OnlineWithoutANoteIsUnchanged(t *testing.T) {
	r := newExpectRig(t)
	r.gen.DeviceOnline(context.Background(), "hawaii", "hawaii")
	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, "Device hawaii came online", evts[0].Message)
	assert.Empty(t, evts[0].CommandID)
}

func TestEX_G_NilTrackerPointerInTheInterfaceIsIgnored(t *testing.T) {
	var typedNil *fakeOracle
	r := newExpectRig(t)
	r.gen.SetExpectedActivity(typedNil)
	ctx := context.Background()
	assert.NotPanics(t, func() {
		r.gen.CheckDockerEvent(ctx, "hawaii", "hawaii", dieEvent("web"))
		r.gen.DeviceOffline(ctx, "hawaii", "hawaii")
		r.gen.DeviceOnline(ctx, "hawaii", "hawaii")
	}, "a nil pointer wrapped in the interface must not be called")
	assert.Len(t, r.stored(), 3)
	r.gen.SetExpectedActivity(nil)
	assert.NotPanics(t, func() { r.gen.DeviceOffline(ctx, "vermont", "vermont") })
}

func TestEX_G_NoOracleMeansNoDowngradeBySpuriousCommand(t *testing.T) {
	// The old recentDisruptiveCommand downgrade is gone: a recent reboot command
	// on the device no longer turns an offline alert into info by itself.
	r := newExpectRig(t)
	r.gen.SetExpectedActivity(nil)
	r.gen.DeviceOffline(context.Background(), "hawaii", "hawaii")
	evts := r.stored()
	require.Len(t, evts, 1)
	assert.Equal(t, models.SeverityWarning, evts[0].Severity)
	assert.Equal(t, "Device hawaii went offline", evts[0].Message)
}

// ---------------------------------------------------------------------------
// Escalation
// ---------------------------------------------------------------------------

func escalationReq(kind models.EscalationKind, container string) models.EscalationRequest {
	return models.EscalationRequest{
		Kind: kind, Command: models.Command{ID: "cmd-aaaaaaaa-1111", Action: "reboot"},
		DeviceID: "hawaii", Hostname: "hawaii", ContainerName: container, At: expectBase, Message: "escalated",
	}
}

func TestEX_G_PrepareEscalationUsesTheMatchingRule(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("device_offline", 300, true, "")
	r.addRule("container_died", 900, false, "")

	dev, err := r.gen.PrepareEscalation(context.Background(), escalationReq(models.EscDeviceNotBack, ""))
	require.NoError(t, err)
	require.NotNil(t, dev.Rule, "the device_offline rule")
	assert.Equal(t, "device_offline", dev.Rule.Metric)
	assert.Equal(t, models.EventDeviceOffline, dev.Event.Type)
	assert.Equal(t, models.EventSeverity("critical"), dev.Event.Severity)
	assert.Equal(t, models.CommandAttrEscalation, dev.Event.CommandAttr)
	assert.Equal(t, "cmd-aaaaaaaa-1111", dev.Event.CommandID)
	assert.Equal(t, "escalated", dev.Event.Message)

	con, err := r.gen.PrepareEscalation(context.Background(), escalationReq(models.EscContainer, "web"))
	require.NoError(t, err)
	require.NotNil(t, con.Rule, "the container_died rule")
	assert.Equal(t, "container_died", con.Rule.Metric)
	assert.Equal(t, models.EventContainerDied, con.Event.Type)
	assert.Equal(t, "web", con.Event.ContainerName)

	sum, err := r.gen.PrepareEscalation(context.Background(), escalationReq(models.EscContainerSummary, ""))
	require.NoError(t, err)
	require.NotNil(t, sum.Rule, "the summary also goes through the container_died rule")
	assert.Equal(t, models.EventContainerDied, sum.Event.Type)
}

func TestEX_AC022_EscalationWithoutRuleIsWarning(t *testing.T) {
	r := newExpectRig(t)
	prep, err := r.gen.PrepareEscalation(context.Background(), escalationReq(models.EscDeviceNonSuccess, ""))
	require.NoError(t, err)
	assert.Nil(t, prep.Rule, "no enabled rule")
	assert.Equal(t, models.SeverityWarning, prep.Event.Severity, "[AC-022] a warning event with no push")
	assert.False(t, prep.Skip)
}

func TestEX_G_PrepareEscalationSkipsExcludedContainers(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("container_died", 0, true, "web")
	prep, err := r.gen.PrepareEscalation(context.Background(), escalationReq(models.EscContainer, "web"))
	require.NoError(t, err)
	assert.True(t, prep.Skip, "an excluded container raises nothing")
	other, err := r.gen.PrepareEscalation(context.Background(), escalationReq(models.EscContainer, "db"))
	require.NoError(t, err)
	assert.False(t, other.Skip)
}

func TestEX_G_PrepareEscalationFailsWhenRulesCannotBeRead(t *testing.T) {
	r := newExpectRig(t)
	r.rules.Err = errors.New("db down")
	r.gen.InvalidateRulesCache()
	_, err := r.gen.PrepareEscalation(context.Background(), escalationReq(models.EscDeviceNotBack, ""))
	require.Error(t, err, "the tracker must retry rather than escalate without its rule")
	assert.Empty(t, r.stored(), "preparing stores nothing")
}

func TestEX_AC023_EscalationIgnoresCooldown(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("device_offline", 900, true, "")
	ctx := context.Background()

	// An ordinary alert starts the rule's cooldown.
	r.gen.DeviceOffline(ctx, "hawaii", "hawaii")
	require.Len(t, r.disp.Alerts, 1)

	prep, err := r.gen.PrepareEscalation(ctx, escalationReq(models.EscDeviceNotBack, ""))
	require.NoError(t, err)
	require.NoError(t, r.gen.PublishEscalation(ctx, prep.Event, prep.Rule, "hawaii"))
	require.NoError(t, r.gen.PublishEscalation(ctx, prep.Event, prep.Rule, "hawaii"))
	assert.Len(t, r.disp.Alerts, 3, "[AC-023] an escalation is pushed although the rule is on cooldown")
	assert.Equal(t, "escalated", r.disp.Alerts[1].Event.Message)
	assert.Equal(t, "hawaii", r.disp.Alerts[1].Hostname)
}

func TestEX_AC023_EscalationStartsNoCooldown(t *testing.T) {
	r := newExpectRig(t)
	r.addRule("device_offline", 900, true, "")
	ctx := context.Background()
	prep, err := r.gen.PrepareEscalation(ctx, escalationReq(models.EscDeviceNotBack, ""))
	require.NoError(t, err)
	require.NoError(t, r.gen.PublishEscalation(ctx, prep.Event, prep.Rule, "hawaii"))
	r.gen.DeviceOffline(ctx, "hawaii", "hawaii")
	assert.Len(t, r.disp.Alerts, 2, "[AC-023] the unrelated offline alert is not hidden by the escalation")
}

func TestEX_G_PublishEscalationWithoutNotifyDispatchesNothing(t *testing.T) {
	r := newExpectRig(t)
	ctx := context.Background()
	evt := models.Event{DeviceID: "hawaii", Message: "x"}
	require.NoError(t, r.gen.PublishEscalation(ctx, evt, nil, "hawaii"), "no rule")
	require.NoError(t, r.gen.PublishEscalation(ctx, evt, &models.AlertRule{Notify: false}, "hawaii"), "rule without notify")
	assert.Empty(t, r.disp.Alerts)
}

func TestEX_G_PublishEscalationReportsAnExpiredDeadline(t *testing.T) {
	r := newExpectRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.gen.PublishEscalation(ctx, models.Event{DeviceID: "hawaii"}, &models.AlertRule{Notify: true}, "hawaii")
	assert.ErrorIs(t, err, context.Canceled, "the publisher learns that its deadline passed")
}

// PublishEscalation must not broadcast to the hub: the tracker's publisher
// already did. A hub that nobody runs accepts 256 messages and then blocks, so a
// broadcast per call would hang this loop.
func TestEX_G_PublishEscalationDoesNotBroadcast(t *testing.T) {
	r := newExpectRig(t)
	hub := websocket.NewHub() // never run
	gen := NewGenerator(r.events, hub, r.rules, r.disp, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 400; i++ {
			_ = gen.PublishEscalation(context.Background(), models.Event{DeviceID: "hawaii"}, &models.AlertRule{Notify: true}, "hawaii")
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("PublishEscalation blocked on the hub: it must not broadcast")
	}
}

func TestEX_G_CommandLabelsCoverTheNewActions(t *testing.T) {
	want := map[string]string{
		"shutdown": "Shutdown", "docker_stack_redeploy": "Docker stack redeploy", "docker_stack_restart": "Docker stack restart",
		"docker_stack_up": "Docker stack up", "docker_stack_down": "Docker stack down", "docker_stack_pull": "Docker stack pull",
		"docker_remove_items": "Docker remove items", "docker_inventory_refresh": "Docker inventory refresh",
		"agent_uninstall": "Agent uninstall",
	}
	r := newExpectRig(t)
	for action, label := range want {
		assert.Equal(t, label, commandLabels[action], action)
		r.gen.CommandSent(context.Background(), "hawaii", "hawaii", action, nil)
	}
	for _, e := range r.stored() {
		assert.True(t, strings.Contains(e.Message, "initiated on hawaii"), e.Message)
		assert.NotContains(t, e.Message, "docker_stack", "the label, not the raw action")
	}
}

func TestEX_G_HexShortID(t *testing.T) {
	cases := map[string]string{
		"0123456789abcdef0123": "0123456789ab",
		"ABCDEF":               "",
		"ab\x00cd":             "abcd",
		"":                     "",
		"zzzzzzzzzzzzzabc":     "",
	}
	for in, want := range cases {
		assert.Equal(t, want, hexShortID(in), "input %q", in)
	}
}
