package handlers

import (
	"bytes"
	"context"
	"log/slog"
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

// One directory spelled two ways is one directory: project and work dir.
func TestAutoUpdate_AC014_SameDirTrailingSlashKeepsWorkDir(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", [2]string{"a", "/srv/media"}, [2]string{"b", "/srv/media/"}))

	cmds := e.dockerUpdates()
	if assert.Len(t, cmds, 1) {
		assert.Contains(t, cmds[0].Params, "compose_work_dir", "[AC-014] a trailing slash is not a second directory")
	}
}

// AC-014 (QA-D1): a project whose containers carry two different working-dir
// labels is dispatched by project name alone. The server cannot resolve
// symlinks, so the agent decides, and a genuinely ambiguous project fails there
// as an ordinary failed command with the usual backoff.
func TestAutoUpdate_AC014_SeveralWorkDirsDispatchProjectOnly(t *testing.T) {
	buf := captureServerLog(t)
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", [2]string{"sonarr", "/srv/media"}, [2]string{"other", "/tmp/elsewhere"}))

	cmds := e.dockerUpdates()
	if assert.Len(t, cmds, 1, "[AC-014] the stack is still dispatched") {
		assert.Equal(t, map[string]interface{}{"compose_project": "media"}, cmds[0].Params,
			"[AC-014] project only: no compose_work_dir, so an old agent fails closed on its own precheck")
	}
	assert.Empty(t, e.events.Events, "the server raises no event of its own")
	assert.NotContains(t, buf.String(), "skipped", "and writes no per-cycle warning")
}

// AC-014 (QA-D1): a symlinked stack directory after a partial recreate carries
// the link label on some containers and the resolved label on others.
func TestAutoUpdate_AC014_MixedLinkAndResolvedLabelsDispatchProjectOnly(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)

	e.h.checkAutoUpdates(context.Background(), "dev-1", stackTelemetry("media", [2]string{"a", "/opt/stacks/media"}, [2]string{"b", "/mnt/data/stacks/media"}))

	cmds := e.dockerUpdates()
	if assert.Len(t, cmds, 1) {
		assert.Equal(t, map[string]interface{}{"compose_project": "media"}, cmds[0].Params, "[AC-014] the agent resolves the two labels to one directory")
	}
}

// Dispatching on every telemetry cycle is gated by the policy cooldown and the
// failure backoff, exactly as for a single-directory stack.
func TestAutoUpdate_AC014_ProjectOnlyDispatchIsBackoffGated(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	pol := e.policy(t, "media", true, nil)
	data := stackTelemetry("media", [2]string{"a", "/srv/one"}, [2]string{"b", "/srv/two"})
	e.tel.Snapshots["dev-1"] = []models.TelemetrySnapshot{{DeviceID: "dev-1", Data: *data}}

	e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	require.Len(t, e.dockerUpdates(), 1)

	first := e.dockerUpdates()[0]
	e.repo.Commands[first.ID].Status = string(models.CommandFailed) // the agent refused: ambiguous_project
	failedAt := e.clock.Now()
	pol.ConsecutiveFailures, pol.LastFailureAt = 1, &failedAt

	for i := 0; i < 50; i++ { // 25 minutes of telemetry cycles at 30s
		e.clock.Advance(30 * time.Second)
		e.h.checkAutoUpdates(context.Background(), "dev-1", data)
	}
	assert.Len(t, e.dockerUpdates(), 2, "one retry once the 15 minute backoff has elapsed, none before")
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

// A project-only dispatch still holds the conflict keys of the directories the
// device reports, so a command that is queued or running is never duplicated by
// the next telemetry cycle.
func TestAutoUpdate_AC014_ProjectOnlyDispatchHoldsKeysAgainstDuplicates(t *testing.T) {
	e := newAutoEnv(t, anytime(), anytime())
	e.policy(t, "media", true, nil)
	data := stackTelemetry("media", [2]string{"a", "/opt/stacks/media"}, [2]string{"b", "/mnt/data/stacks/media"})
	e.tel.Snapshots["dev-1"] = []models.TelemetrySnapshot{{DeviceID: "dev-1", Data: *data}}

	for i := 0; i < 30; i++ { // the first command stays queued: the agent has not finished it
		e.h.checkAutoUpdates(context.Background(), "dev-1", data)
		e.clock.Advance(30 * time.Second)
	}

	cmds := e.dockerUpdates()
	if assert.Len(t, cmds, 1, "no duplicate while the first command is in flight") {
		assert.Equal(t, []string{"docker:compose:/mnt/data/stacks/media", "docker:compose:/opt/stacks/media"}, cmds[0].TargetKeys)
	}
}
