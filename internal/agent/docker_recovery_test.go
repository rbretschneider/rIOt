package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

const depRecID = "dddd000000000000000000000000000000000000000000000000000000000001"

// interrupted writes a marker like the one an update leaves when the agent dies
// mid-recreate.
func (h *dockerHarness) interrupted(step string, rec dockerRecord) {
	h.t.Helper()
	require.NoError(h.t, h.a.inflight.Put(idA, inflightMarker{
		Action: "docker_update", Target: rec.OriginalName, Step: step, StartedAt: h.clk.Now(),
		Docker: []dockerRecord{rec},
	}))
}

// SEC C-4(b): the set-aside container goes back to its original name when
// nothing holds it, and is started again only if it was running.
func TestCL_SEC004_RecoveryRenamesSetAsideBackWhenOriginalMissing(t *testing.T) {
	for _, wasRunning := range []bool{true, false} {
		name := "was stopped"
		if wasRunning {
			name = "was running"
		}
		t.Run(name, func(t *testing.T) {
			h := newDockerHarness(t)
			h.docker.add(fakeContainer{ID: webID, Name: asideWeb, Image: "nginx:latest"})
			h.interrupted(models.StepRecreate, dockerRecord{Key: "web", OriginalName: "web", OldID: webID,
				SetAsideName: asideWeb, WasRunning: wasRunning})

			h.a.recoverInterruptedCommands(context.Background())

			web, ok := h.docker.snapshot("web")
			require.True(t, ok, "[SEC-004] the previous container must be back under its original name")
			assert.Equal(t, webID, web.ID)
			assert.Equal(t, wasRunning, web.Running, "[SEC-004] it is started iff it was running before")
			assert.Equal(t, wasRunning, h.docker.has("start:"+webID), "[SEC-004] no start for a container that was stopped")
			assert.True(t, h.docker.has("rename:"+webID+":web"), "[SEC-004] the rename is addressed by ID")
			r := h.resultFor(idA)
			assert.Equal(t, "recreate", r.Step)
			assert.Equal(t, models.ReasonRestarted, r.Reason)
			assert.Equal(t, "agent restarted during execution; outcome unknown; reconciled: restored web from "+asideWeb, r.Message,
				"[SEC-004] the failure result says what was reconciled")
		})
	}
}

// SEC C-4(b): if a container already holds the original name, both are left
// alone and named in the message.
func TestCL_SEC004_RecoveryLeavesBothWhenOriginalExists(t *testing.T) {
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{ID: webID, Name: asideWeb, Image: "nginx:latest"})
	repl := h.docker.add(fakeContainer{Name: "web", Image: "nginx:latest", Running: true})
	h.interrupted(models.StepVerify, dockerRecord{Key: "web", OriginalName: "web", OldID: webID,
		SetAsideName: asideWeb, WasRunning: true, ReplacementID: repl.ID})

	h.a.recoverInterruptedCommands(context.Background())

	assert.Empty(t, h.docker.callsWithPrefix("rename:"), "[SEC-004] nothing is renamed")
	assert.Empty(t, h.docker.callsWithPrefix("start:"), "[SEC-004] nothing is started")
	assert.Empty(t, h.docker.callsWithPrefix("remove:"), "[SEC-004] nothing is removed")
	assert.Equal(t, "agent restarted during execution; outcome unknown; reconciled: left "+asideWeb+
		" (previous) and web (replacement) untouched; manual check required", h.resultFor(idA).Message)
}

// SEC C-4(b): dependents the update stopped and that are still stopped are
// restarted.
func TestCL_SEC004_RecoveryRestartsRecordedStoppedDependents(t *testing.T) {
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{Name: "vpn", Running: true})
	h.docker.add(fakeContainer{ID: depRecID, Name: "qbit", NetworkMode: "container:vpn"})
	h.docker.add(fakeContainer{Name: "bystander", NetworkMode: "container:vpn"}) // stopped, but not recorded
	h.interrupted(models.StepStopDependents, dockerRecord{Key: "vpn", StoppedDependentIDs: []string{depRecID}})

	h.a.recoverInterruptedCommands(context.Background())

	qbit, _ := h.docker.snapshot("qbit")
	assert.True(t, qbit.Running, "[SEC-004] the recorded dependent is running again")
	by, _ := h.docker.snapshot("bystander")
	assert.False(t, by.Running, "[SEC-004] a stopped container that was not recorded stays stopped")
	assert.Contains(t, h.resultFor(idA).Message, "reconciled: restarted dependents: qbit")
}

// NFR-004: recovery never removes a container, in any scenario.
func TestCL_NFR004_RecoveryNeverRemovesContainers(t *testing.T) {
	scenarios := map[string]func(h *dockerHarness){
		"set-aside only": func(h *dockerHarness) {
			h.docker.add(fakeContainer{ID: webID, Name: asideWeb})
			h.interrupted(models.StepRecreate, dockerRecord{Key: "web", OriginalName: "web", OldID: webID, SetAsideName: asideWeb, WasRunning: true})
		},
		"set-aside and replacement": func(h *dockerHarness) {
			h.docker.add(fakeContainer{ID: webID, Name: asideWeb})
			h.docker.add(fakeContainer{Name: "web"})
			h.interrupted(models.StepVerify, dockerRecord{Key: "web", OriginalName: "web", OldID: webID, SetAsideName: asideWeb})
		},
		"rename back fails": func(h *dockerHarness) {
			h.docker.add(fakeContainer{ID: webID, Name: asideWeb})
			h.docker.failIf = func(call string) error { return errors.New("denied") }
			h.interrupted(models.StepRecreate, dockerRecord{Key: "web", OriginalName: "web", OldID: webID, SetAsideName: asideWeb})
		},
		"dependents": func(h *dockerHarness) {
			h.docker.add(fakeContainer{ID: depRecID, Name: "qbit"})
			h.interrupted(models.StepStopDependents, dockerRecord{Key: "vpn", StoppedDependentIDs: []string{depRecID}})
		},
	}
	for name, setup := range scenarios {
		t.Run(name, func(t *testing.T) {
			captureLogs(t)
			h := newDockerHarness(t)
			setup(h)

			h.a.recoverInterruptedCommands(context.Background())

			assert.Empty(t, h.docker.callsWithPrefix("remove:"), "[NFR-004] recovery must never remove a container")
		})
	}
}

// SEC C-4(b): with Docker unreachable the failure is reported once, the marker
// is kept, and the next start reconciles without reporting again.
func TestCL_SEC004_RecoveryDockerUnreachableKeepsMarkerAndReportsOnce(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{ID: webID, Name: asideWeb})
	h.interrupted(models.StepRecreate, dockerRecord{Key: "web", OriginalName: "web", OldID: webID, SetAsideName: asideWeb, WasRunning: true})
	h.a.dockerFactory = func() (dockerAPI, error) { return nil, errors.New("cannot connect to the Docker daemon") }

	h.a.recoverInterruptedCommands(context.Background())

	m, kept := h.a.inflight.Get(idA)
	require.True(t, kept, "[SEC-004] the marker is kept while Docker is unreachable")
	assert.True(t, m.Reported, "[SEC-004] and marked as already reported")
	assert.Equal(t, restartedMessage, h.resultFor(idA).Message, "[SEC-004] the failure is reported without a summary")

	// Next start: Docker is back.
	h.a.dockerFactory = func() (dockerAPI, error) { return h.docker, nil }
	require.NoError(t, h.a.out.Add(models.CommandResult{CommandID: idB, Status: "success"})) // unrelated entry
	h.a.recoverInterruptedCommands(context.Background())

	web, ok := h.docker.snapshot("web")
	require.True(t, ok, "[SEC-004] the retry restores the previous container")
	assert.Equal(t, webID, web.ID)
	_, kept = h.a.inflight.Get(idA)
	assert.False(t, kept, "[SEC-004] the marker is cleared once reconciled")
	assert.Equal(t, restartedMessage, h.resultFor(idA).Message, "[SEC-004] the original report is not replaced or repeated")
}
