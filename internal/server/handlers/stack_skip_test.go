package handlers

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// captureServerLog redirects slog into a buffer for the test.
func captureServerLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// stackTelemetry builds telemetry for a compose stack: one container flagged as
// having an update per (service, working directory) pair.
func stackTelemetry(project string, members ...[2]string) *models.FullTelemetryData {
	updateAvail := true
	var cs []models.ContainerInfo
	for _, m := range members {
		cs = append(cs, models.ContainerInfo{
			ID: "id-" + m[0], Name: m[0], State: "running", UpdateAvailable: &updateAvail,
			Labels: map[string]string{
				"com.docker.compose.project":             project,
				"com.docker.compose.service":             m[0],
				"com.docker.compose.project.working_dir": m[1],
			},
		})
	}
	return &models.FullTelemetryData{Docker: &models.DockerInfo{Containers: cs}}
}

func (e *autoEnv) skipEvents() []models.Event {
	var out []models.Event
	for _, ev := range e.events.Events {
		if ev.Type == models.EventStackUpdateSkipped {
			out = append(out, ev)
		}
	}
	return out
}

// AC-014: a stack policy sends the compose project and its working directory.
// The agent resolves by project; the work dir is a cross-check, the only input
// of an old agent, and the conflict-detection key.
func TestAutoUpdate_AC014_StackDispatchSendsProjectAndWorkDir(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", [2]string{"sonarr", "/srv/media"}))

	cmds := e.dockerUpdates()
	if assert.Len(t, cmds, 1) {
		assert.Equal(t, map[string]interface{}{"compose_project": "media", "compose_work_dir": "/srv/media"}, cmds[0].Params,
			"[AC-014] a stack update carries compose_project and compose_work_dir")
		assert.Equal(t, []string{"docker:compose:/srv/media"}, cmds[0].TargetKeys)
	}
}

func TestAutoUpdate_AC014_ContainerPolicySendsNeither(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "web", false, nil)

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", [2]string{"web", "/srv/media"}))

	cmds := e.dockerUpdates()
	if assert.Len(t, cmds, 1) {
		assert.NotContains(t, cmds[0].Params, "compose_project", "[AC-014] a container-level policy sends no project")
		assert.NotContains(t, cmds[0].Params, "compose_work_dir", "[AC-014] and no work dir")
		assert.Equal(t, "web", cmds[0].Params["container"])
	}
}

func TestAutoUpdate_AC014_SameDirTrailingSlashIsNotAmbiguous(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", [2]string{"a", "/srv/media"}, [2]string{"b", "/srv/media/"}))

	assert.Len(t, e.dockerUpdates(), 1, "[AC-014] one directory spelled two ways is still one directory")
	assert.Empty(t, e.skipEvents())
}

// AC-014 / condition 4: a project whose containers report two working
// directories is not dispatched, and the skip is visible as a warning event.
func TestAutoUpdate_AC014_AmbiguousProjectNotDispatched(t *testing.T) {
	captureServerLog(t)
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", [2]string{"sonarr", "/srv/media"}, [2]string{"hostile", "/tmp/evil"}))

	assert.Empty(t, e.dockerUpdates(), "[AC-014] the stack is not dispatched")
	evs := e.skipEvents()
	if assert.Len(t, evs, 1, "a log-only skip is not acceptable: the dashboard must show it") {
		ev := evs[0]
		assert.Equal(t, models.SeverityWarning, ev.Severity)
		assert.Equal(t, "dev-1", ev.DeviceID)
		assert.Contains(t, ev.Message, `"media"`, "the event names the project")
		assert.Contains(t, ev.Message, "host", "and the device")
		assert.Contains(t, ev.Message, `"/srv/media"`, "and the conflicting working directories")
		assert.Contains(t, ev.Message, `"/tmp/evil"`)
	}
}

// The skip event is raised at most once per policy per window, whatever the
// telemetry rate, and again once the window has passed.
func TestAutoUpdate_AC014_SkipEventRaisedOncePerWindow(t *testing.T) {
	captureServerLog(t)
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)
	data := stackTelemetry("media", [2]string{"a", "/srv/one"}, [2]string{"b", "/srv/two"})

	for i := 0; i < 50; i++ { // a telemetry cycle every 30s for 25 minutes
		e.h.checkAutoUpdates(context.Background(), "dev-1", data)
		e.clock.Advance(30 * time.Second)
	}
	assert.Len(t, e.skipEvents(), 1, "50 cycles raise one event")

	e.clock.Advance(stackSkipMinWindow)
	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	assert.Len(t, e.skipEvents(), 2, "after the window the still-open conflict is raised again")
	assert.Empty(t, e.dockerUpdates())
}

// A longer maintenance cooldown stretches the window.
func TestAutoUpdate_AC014_SkipEventWindowFollowsLongerCooldown(t *testing.T) {
	captureServerLog(t)
	long := models.MaintenanceWindow{Mode: "anytime", StartTime: "00:00", EndTime: "00:00", CooldownMinutes: 24 * 60}
	e := newAutoEnv(t, long, anytime())
	e.policy(t, "media", true, nil)
	data := stackTelemetry("media", [2]string{"a", "/srv/one"}, [2]string{"b", "/srv/two"})

	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	e.clock.Advance(stackSkipMinWindow + time.Hour)
	e.h.checkAutoUpdates(context.Background(), "dev-1", data)

	assert.Len(t, e.skipEvents(), 1, "the 24h cooldown applies, not the 6h minimum")
}

// The event message and the log line are bounded, and quote what telemetry says.
func TestAutoUpdate_AC014_SkipEventIsBoundedAndQuoted(t *testing.T) {
	buf := captureServerLog(t)
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)
	var members [][2]string
	for i := 0; i < 40; i++ {
		members = append(members, [2]string{fmt.Sprintf("svc%d", i), fmt.Sprintf("/srv/%d/%s", i, strings.Repeat("x", 5000))})
	}
	members = append(members, [2]string{"nl", "/srv/a\nINJECTED\x00"})

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", members...))

	evs := e.skipEvents()
	if assert.Len(t, evs, 1) {
		assert.LessOrEqual(t, len(evs[0].Message), maxSkipEventBytes, "an event message has a fixed cap")
		assert.NotContains(t, evs[0].Message, "\n", "a newline from a label is escaped")
		assert.NotContains(t, evs[0].Message, "\x00")
	}
	assert.NotContains(t, buf.String(), "xxxxxxxxxxxx", "the log line carries counts, not the directories")
	assert.Less(t, buf.Len(), 2000, "the log entry is small")
}

func TestAutoUpdate_AC014_NilLimiterStillWorks(t *testing.T) {
	var l *skipLimiter
	assert.True(t, l.allow("k", time.Now(), time.Hour), "a Handlers built without a limiter raises every skip")
}

// A refused stack update is an ordinary failed run: the policy backs off, so a
// persistent refusal is retried on the backoff schedule and never in a storm.
func TestAutoUpdate_AC014_RefusedStackDispatchBacksOff(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	pol := e.policy(t, "media", true, nil)
	data := stackTelemetry("media", [2]string{"sonarr", "/srv/media"})

	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	require.Len(t, e.dockerUpdates(), 1, "first cycle dispatches")

	// The agent refuses the directory: a precheck failure, which the command
	// service records as one failure of the policy (see the command service test).
	first := e.dockerUpdates()[0]
	e.repo.Commands[first.ID].Status = string(models.CommandFailed) // the refused command is over
	failedAt := e.clock.Now()
	pol.ConsecutiveFailures, pol.LastFailureAt = 1, &failedAt

	for i := 0; i < 20; i++ { // 20 telemetry cycles, 30s apart
		e.clock.Advance(30 * time.Second)
		e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	}
	assert.Len(t, e.dockerUpdates(), 1, "no retry inside the 15 minute backoff of the first failure")

	e.clock.Advance(15 * time.Minute)
	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	assert.Len(t, e.dockerUpdates(), 2, "one retry once the backoff has elapsed")
}
