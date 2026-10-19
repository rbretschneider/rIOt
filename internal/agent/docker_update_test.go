package agent

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

const (
	webID    = "aaaa000000000000000000000000000000000000000000000000000000000001"
	asideWeb = "web.riot-old-11111111" // idA[:8]
)

// webContainer adds a running standalone "web" container on nginx:latest.
func (h *dockerHarness) webContainer() *fakeContainer {
	return h.docker.add(fakeContainer{ID: webID, Name: "web", Image: "nginx:latest", Running: true})
}

func nameParams(name string) map[string]interface{} {
	return map[string]interface{}{"container": name}
}

func stackParams(dir string) map[string]interface{} {
	return map[string]interface{}{"compose_work_dir": dir}
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// AC-002 (FR-002): the name wins over a stale container ID.
func TestCL_AC002_ResolvesByNameOverStaleID(t *testing.T) {
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{ID: "new2", Name: "web", Image: "nginx:latest", Running: true})

	res := h.run(idA, "docker_update", map[string]interface{}{"container": "web", "container_id": "old1"})

	assert.Equal(t, "success", res.Status, "[AC-002] %s", res.Message)
	assert.True(t, h.docker.has("stop:new2"), "[AC-002] the update must target the container currently named web (ID new2)")
	assert.Empty(t, h.docker.callsWithPrefix("inspect:old1"), "[AC-002] the stale ID must never be consulted")
}

// AC-003 (FR-002): a leading slash never affects name matching.
func TestCL_AC003_ResolveIgnoresLeadingSlash(t *testing.T) {
	for _, param := range []string{"web", "/web"} {
		t.Run(param, func(t *testing.T) {
			h := newDockerHarness(t)
			h.webContainer() // the daemon lists it as "/web"

			res := h.run(idA, "docker_update", nameParams(param))

			assert.Equal(t, "success", res.Status, "[AC-003] param %q: %s", param, res.Message)
		})
	}
}

// AC-004 (FR-003): an unknown target fails at resolve and names the target.
func TestCL_AC004_UnresolvedTargetFailsAtResolve(t *testing.T) {
	h := newDockerHarness(t)

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepResolve, res.Step, "[AC-004] the failing step is resolve")
	assert.Contains(t, res.Message, "web", "[AC-004] the message names the target")
	assert.Equal(t, `container "web" not found`, res.Message)
}

// ---------------------------------------------------------------------------
// Standalone update
// ---------------------------------------------------------------------------

// AC-040 (FR-034): an errorDetail in the pull stream fails at pull with the
// registry text in stderr and leaves the original container untouched.
func TestCL_AC040_PullErrorDetailFailsAtPull(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	h.docker.pullStream = `{"status":"Pulling"}` + "\n" + `{"errorDetail":{"message":"manifest unknown"},"error":"manifest unknown"}` + "\n"

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPull, res.Step, "[AC-040]")
	assert.Equal(t, "manifest unknown", res.Stderr, "[AC-040] the registry error text goes to stderr")
	web, ok := h.docker.snapshot("web")
	require.True(t, ok)
	assert.Equal(t, webID, web.ID, "[AC-040] the original container keeps its ID")
	assert.True(t, web.Running, "[AC-040] and stays running")
	for _, op := range []string{"stop:", "rename:", "create:", "remove:"} {
		assert.Empty(t, h.docker.callsWithPrefix(op), "[AC-040] no %s call may happen after a failed pull", op)
	}
}

// AC-041 (FR-035): same image ID after the pull means nothing is recreated.
func TestCL_AC041_SameImageSkipsRecreate(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	h.docker.pulledID = "sha256:old" // the running container's image

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "success", res.Status)
	assert.Contains(t, res.Message, "already up to date", "[AC-041]")
	for _, op := range []string{"stop:", "rename:", "create:", "remove:"} {
		assert.Empty(t, h.docker.callsWithPrefix(op), "[AC-041] no %s call for an up-to-date container", op)
	}
}

// AC-042 (FR-036, FR-040): the old container is renamed aside, not removed,
// until verification passes; it is removed afterwards and the replacement has
// the original name.
func TestCL_AC042_OldRenamedAsideRemovedAfterVerify(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()

	res := h.run(idA, "docker_update", nameParams("web"))

	require.Equal(t, "success", res.Status, res.Message)
	rename := h.docker.indexOf("rename:" + webID + ":" + asideWeb)
	create := h.docker.indexOf("create:web")
	remove := h.docker.indexOf("remove:" + webID)
	pause := h.log.index("pause")
	require.GreaterOrEqual(t, rename, 0, "[AC-042] the old container is renamed aside")
	require.Greater(t, create, rename, "[AC-042] the replacement is created after the rename")
	require.Greater(t, remove, create, "[AC-042] the old container is removed last")
	assert.Greater(t, h.log.index("docker:remove:"+webID), pause, "[AC-042] removal waits for the verification window")
	web, ok := h.docker.snapshot("web")
	require.True(t, ok)
	assert.NotEqual(t, webID, web.ID, "[AC-042] web is now the replacement")
	assert.True(t, web.Running)
	_, asideExists := h.docker.snapshot(asideWeb)
	assert.False(t, asideExists, "[AC-042] the set-aside container is gone after a verified update")
}

// AC-043 (FR-039): a failed create rolls back to the previous container.
func TestCL_AC043_CreateFailureRollsBack(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	h.docker.failIf = func(call string) error {
		if call == "create:web" {
			return errors.New("image platform mismatch")
		}
		return nil
	}

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepRecreate, res.Step, "[AC-043]")
	assert.Contains(t, res.Message, "rolled back to previous container", "[AC-043]")
	assert.Equal(t, models.ReasonRolledBack, res.Reason)
	web, ok := h.docker.snapshot("web")
	require.True(t, ok, "[AC-043] the old container has its original name back")
	assert.Equal(t, webID, web.ID)
	assert.True(t, web.Running, "[AC-043] and is running again")
}

// AC-044 (FR-039): a failed start removes the replacement and restores the old
// container.
func TestCL_AC044_StartFailureRemovesReplacement(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	h.docker.failIf = func(call string) error {
		// only the replacement's start fails; the old container may start again
		if strings.HasPrefix(call, "start:") && !strings.HasPrefix(call, "start:"+webID) {
			return errors.New("port is already allocated")
		}
		return nil
	}

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepRecreate, res.Step)
	assert.Contains(t, res.Message, "rolled back to previous container")
	assert.Len(t, h.docker.callsWithPrefix("remove:"), 1, "[AC-044] exactly the replacement is removed")
	assert.Contains(t, h.docker.callsWithPrefix("remove:")[0], ":force", "[AC-044] the replacement is force-removed")
	web, ok := h.docker.snapshot("web")
	require.True(t, ok)
	assert.Equal(t, webID, web.ID, "[AC-044] the old container is back under its name")
	assert.True(t, web.Running)
}

// AC-045 (FR-038, FR-039): a replacement that exits inside the verification
// window is rolled back and the failure is at verify.
func TestCL_AC045_ExitDuringVerifyRollsBack(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	h.pauseHook = func(time.Duration) {
		if c := h.docker.get("web"); c != nil && c.ID != webID {
			h.docker.mu.Lock()
			c.Running = false
			h.docker.mu.Unlock()
		}
	}

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepVerify, res.Step, "[AC-045]")
	assert.Contains(t, res.Message, "rolled back to previous container")
	web, _ := h.docker.snapshot("web")
	assert.Equal(t, webID, web.ID, "[AC-045] the old container is restored")
	assert.True(t, web.Running)
	assert.False(t, h.docker.has("remove:"+webID), "[AC-045] the old container is never removed")
}

// AC-046 (FR-037): a container that was stopped before the update ends stopped,
// and nothing is verified.
func TestCL_AC046_StoppedStaysStopped(t *testing.T) {
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{ID: webID, Name: "web", Image: "nginx:latest", Running: false})

	res := h.run(idA, "docker_update", nameParams("web"))

	require.Equal(t, "success", res.Status, res.Message)
	web, ok := h.docker.snapshot("web")
	require.True(t, ok)
	assert.NotEqual(t, webID, web.ID, "[AC-046] the container was recreated")
	assert.False(t, web.Running, "[AC-046] the replacement stays stopped")
	assert.Empty(t, h.docker.callsWithPrefix("start:"), "[AC-046] nothing is started")
	assert.Equal(t, -1, h.log.index("pause"), "[AC-046] no verification for a stopped container")
}

// AC-047 (FR-041): when the rollback's rename fails, the set-aside container
// still exists and the failure at rollback names it.
func TestCL_AC047_RollbackRenameFailureKeepsAside(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	h.webContainer()
	h.docker.failIf = func(call string) error {
		switch call {
		case "create:web":
			return errors.New("no space left on device")
		case "rename:" + webID + ":web":
			return errors.New("name conflict")
		}
		return nil
	}

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepRollback, res.Step, "[AC-047]")
	assert.Contains(t, res.Message, asideWeb, "[AC-047] the message names the set-aside container")
	aside, ok := h.docker.snapshot(asideWeb)
	require.True(t, ok, "[AC-047] the set-aside container still exists")
	assert.Equal(t, webID, aside.ID)
	assert.False(t, h.docker.has("remove:"+webID), "[AC-047] the old container is never removed")
}

// AC-048 (FR-042): a leftover set-aside container blocks the update, naming it,
// and nothing is deleted.
func TestCL_AC048_LeftoverAsideBlocksUpdate(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	h.docker.add(fakeContainer{Name: "web.riot-old-deadbeef", Image: "nginx:latest"})

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPrecheck, res.Step, "[AC-048]")
	assert.Contains(t, res.Message, "web.riot-old-deadbeef", "[AC-048] the leftover is named")
	for _, op := range []string{"stop:", "rename:", "create:", "remove:", "pull:"} {
		assert.Empty(t, h.docker.callsWithPrefix(op), "[AC-048] %s must not happen", op)
	}
}

// NFR-004: the old container is removed only after a verified replacement, in
// every scenario.
func TestCL_NFR004_OldContainerNeverRemovedBeforeVerify(t *testing.T) {
	type scenario struct {
		name         string
		failIf       func(call string) error
		pause        func(h *dockerHarness)
		wantRemoved  bool
		wantNameBack bool
	}
	scenarios := []scenario{
		{name: "stop fails", failIf: func(c string) error {
			if c == "stop:"+webID {
				return errors.New("stop timeout")
			}
			return nil
		}, wantNameBack: true},
		{name: "rename fails", failIf: func(c string) error {
			if c == "rename:"+webID+":"+asideWeb {
				return errors.New("rename denied")
			}
			return nil
		}, wantNameBack: true},
		{name: "create fails", failIf: func(c string) error {
			if c == "create:web" {
				return errors.New("create denied")
			}
			return nil
		}, wantNameBack: true},
		{name: "start fails", failIf: func(c string) error {
			if strings.HasPrefix(c, "start:") && !strings.HasPrefix(c, "start:"+webID) {
				return errors.New("start denied")
			}
			return nil
		}, wantNameBack: true},
		{name: "verify fails", pause: func(h *dockerHarness) {
			if c := h.docker.get("web"); c != nil && c.ID != webID {
				h.docker.mu.Lock()
				c.Restarting = true
				h.docker.mu.Unlock()
			}
		}, wantNameBack: true},
		{name: "verified success", wantRemoved: true},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			captureLogs(t)
			h := newDockerHarness(t)
			h.webContainer()
			h.docker.failIf = sc.failIf
			if sc.pause != nil {
				h.pauseHook = func(time.Duration) { sc.pause(h) }
			}

			h.run(idA, "docker_update", nameParams("web"))

			removed := h.docker.has("remove:" + webID)
			assert.Equal(t, sc.wantRemoved, removed, "[NFR-004] old container removal in scenario %q", sc.name)
			if removed {
				assert.Greater(t, h.log.index("docker:remove:"+webID), h.log.index("pause"),
					"[NFR-004] removal must come after the verification window")
			}
			if sc.wantNameBack {
				web, ok := h.docker.snapshot("web")
				require.True(t, ok, "[NFR-004] some container must hold the name web")
				assert.Equal(t, webID, web.ID, "[NFR-004] the old container is the one serving the name")
			}
		})
	}
}

// SEC C-9: cleanup and rollback address the old container by its immutable ID,
// never by the set-aside name.
func TestCL_SEC011_CleanupAndRollbackUseOldContainerID(t *testing.T) {
	t.Run("cleanup", func(t *testing.T) {
		h := newDockerHarness(t)
		h.webContainer()

		h.run(idA, "docker_update", nameParams("web"))

		assert.True(t, h.docker.has("remove:"+webID), "[SEC-011] cleanup removes by ID")
		assert.False(t, h.docker.has("remove:"+asideWeb), "[SEC-011] never by the set-aside name")
	})

	t.Run("rollback", func(t *testing.T) {
		h := newDockerHarness(t)
		h.webContainer()
		h.docker.failIf = func(call string) error {
			if call == "create:web" {
				return errors.New("boom")
			}
			return nil
		}

		h.run(idA, "docker_update", nameParams("web"))

		assert.True(t, h.docker.has("rename:"+webID+":web"), "[SEC-011] rename back is addressed by ID")
		assert.True(t, h.docker.has("start:"+webID), "[SEC-011] the restart is addressed by ID")
		for _, c := range h.docker.callList() {
			assert.False(t, strings.HasPrefix(c, "rename:"+asideWeb) || strings.HasPrefix(c, "start:"+asideWeb) ||
				strings.HasPrefix(c, "remove:"+asideWeb), "[SEC-011] call %q addresses the set-aside name", c)
		}
	})
}

// SEC C-4(a): the marker is durable before the rename and before dependents are
// stopped.
func TestCL_SEC004_MarkerPersistedBeforeRenameAndDependentStop(t *testing.T) {
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{ID: webID, Name: "web", Image: "nginx:latest", Running: true})
	h.docker.add(fakeContainer{ID: "dep00000000000000000000000000000000000000000000000000000000001", Name: "dl",
		Running: true, NetworkMode: "container:web"})
	depID := "dep00000000000000000000000000000000000000000000000000000000001"

	snapshots := map[string]string{}
	h.docker.onCall = func(call string) {
		if call == "stop:"+depID || call == "rename:"+webID+":"+asideWeb || strings.HasPrefix(call, "create:") {
			data, err := os.ReadFile(h.inflightPath)
			if err == nil {
				snapshots[call] = string(data)
			}
		}
	}

	res := h.run(idA, "docker_update", nameParams("web"))
	require.Equal(t, "success", res.Status, res.Message)

	stopSnap := snapshots["stop:"+depID]
	assert.Contains(t, stopSnap, depID, "[SEC-004] the stopped dependent's ID is on disk before it is stopped")
	renameSnap := snapshots["rename:"+webID+":"+asideWeb]
	for _, want := range []string{`"original_name":"web"`, `"old_id":"` + webID + `"`, `"set_aside_name":"` + asideWeb + `"`, `"was_running":true`} {
		assert.Contains(t, renameSnap, want, "[SEC-004] marker field %s must be on disk before the rename", want)
	}
	createSnap := snapshots["create:web"]
	assert.NotContains(t, createSnap, `"replacement_id"`, "[SEC-004] the replacement ID cannot be known before the create")
	assert.Less(t, h.docker.indexOf("stop:"+depID), h.docker.indexOf("rename:"+webID+":"+asideWeb),
		"[SEC-004] dependents are stopped before the parent is touched")
}

// A marker that cannot be written aborts before the next Docker mutation.
func TestCL_SEC004_MarkerWriteFailureAbortsBeforeMutation(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	h.webContainer()
	// Intake succeeds (the first marker write), later marker writes fail.
	h.docker.onCall = func(call string) {
		if call == "list" {
			return
		}
	}
	h.docker.failIf = nil
	var once sync.Once
	h.docker.onCall = func(call string) {
		if strings.HasPrefix(call, "imageinspect:") {
			once.Do(func() { h.fs.failOn("rename", errors.New("disk full")) })
		}
	}

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.ReasonPersistence, res.Reason, "[SEC-004] a marker failure is reported as persistence")
	assert.Equal(t, models.StepRecreate, res.Step)
	for _, op := range []string{"stop:" + webID, "rename:", "create:", "remove:"} {
		assert.Empty(t, h.docker.callsWithPrefix(op), "[SEC-004] %s must not run without a durable marker", op)
	}
}

// ---------------------------------------------------------------------------
// Dependents
// ---------------------------------------------------------------------------

// AC-035 (FR-030): after a successful standalone update the dependent is running
// and attached to the new parent.
func TestCL_AC035_DependentOnNewParentNamespace(t *testing.T) {
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{ID: webID, Name: "vpn", Image: "gluetun:latest", Running: true})
	depC := h.docker.add(fakeContainer{Name: "qbit", Running: true, NetworkMode: "container:vpn"})

	res := h.run(idA, "docker_update", nameParams("vpn"))

	require.Equal(t, "success", res.Status, res.Message)
	depNow, _ := h.docker.snapshot("qbit")
	dep := depNow
	parent, _ := h.docker.snapshot("vpn")
	assert.True(t, dep.Running, "[AC-035] the dependent is running")
	assert.NotEqual(t, webID, parent.ID, "[AC-035] the parent was recreated")
	assert.Equal(t, "container:vpn", dep.NetworkMode, "[AC-035] the dependent still refers to the parent by name, which now resolves to the new container")
	assert.Greater(t, h.docker.indexOf("start:"+depC.ID), h.docker.indexOf("create:vpn"), "[AC-035] the dependent is started after the new parent exists")
}

// composeProject adds a running compose service container.
func (h *dockerHarness) composeService(dir, service string, running bool, netMode string) *fakeContainer {
	return h.docker.add(fakeContainer{Name: "stack-" + service + "-1", Image: service + ":latest", Running: running,
		Labels: composeLabels(dir, service), NetworkMode: netMode})
}

// AC-034 (FR-029): updating one service stops only that service's dependents.
func TestCL_AC034_SingleServiceStopsOnlyItsDependents(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	b := h.composeService(dir, "b", true, "")
	h.docker.add(fakeContainer{Name: "da", Running: true, NetworkMode: "container:" + a.Name})
	h.docker.add(fakeContainer{Name: "db", Running: true, NetworkMode: "container:" + b.Name})

	res := h.run(idA, "docker_update", map[string]interface{}{"container": a.Name})

	require.Equal(t, "success", res.Status, res.Message)
	assert.True(t, h.docker.has("stop:"+h.docker.get("da").ID), "[AC-034] service a's dependent is stopped")
	assert.False(t, h.docker.has("stop:"+h.docker.get("db").ID), "[AC-034] service b's dependent is left alone")
	assert.Equal(t, []string{"pull", "up"}, h.procs.subcommands())
	argv := h.procs.invocations()[0]
	assert.Equal(t, "a", argv[len(argv)-1], "[AC-034] only service a is pulled")
}

// AC-036 (FR-030): a failure at pull leaves the dependents running.
func TestCL_AC036_DependentRestoredAfterPullFailure(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	h.docker.add(fakeContainer{Name: "da", Running: true, NetworkMode: "container:" + a.Name})
	h.procs.configure = func(args []string, p *fakeProcess) {
		p.exitErr = errors.New("exit status 1")
		p.out = []byte("pull access denied")
	}

	res := h.run(idA, "docker_update", map[string]interface{}{"container": a.Name})

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPull, res.Step)
	da, _ := h.docker.snapshot("da")
	assert.True(t, da.Running, "[AC-036] the dependent is running after a failed pull")
}

// AC-037 (FR-030, FR-032): after the up limit is exceeded the dependents are
// restored on their own 2-minute budget, even though the operation context is
// dead.
func TestCL_AC037_RestoreHasOwnBudgetAfterUpTimeout(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	da := h.docker.add(fakeContainer{Name: "da", Running: true, NetworkMode: "container:" + a.Name})
	h.procs.configure = func(args []string, p *fakeProcess) {
		if !contains(args, "up") {
			return
		}
		p.autoExit = false
		p.honorTerm = true
		// When compose up is terminated, the whole command context dies too.
		p.onTerminate = h.a.cmdCancel
	}
	upStarted := make(chan *fakeProcess, 1)
	go func() {
		for p := range h.procs.created {
			if p != nil && !p.autoExit {
				upStarted <- p
			}
		}
	}()

	h.submit(idA, "docker_update", map[string]interface{}{"container": a.Name})
	p := <-upStarted
	<-p.started
	h.clk.Advance(models.ComposeUpLimit) // SIGTERM arrives and expires the operation context
	h.wait()

	res := resultView{h.resultFor(idA)}
	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepRecreate, res.Step)
	assert.Equal(t, models.ReasonTimeout, res.Reason, "[AC-037] the up limit is reported as a timeout")
	assert.Equal(t, "timed out after 10m", res.Message)
	snap, ok := h.docker.ctxInfo["start:"+da.ID]
	require.True(t, ok, "[AC-037] the stopped dependent must be started again")
	assert.NoError(t, snap.Err, "[AC-037] the restore context is alive although the operation context expired")
	assert.True(t, snap.HasDeadline)
	assert.LessOrEqual(t, snap.Remaining, models.DependentRestoreBudget, "[AC-037] the restore has its own 2-minute budget")
	assert.Greater(t, snap.Remaining, time.Minute)
	assert.Contains(t, h.tr.dockerEventActions(), "update_failed", "[AC-053] the failure event still goes out")
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// AC-038 (FR-031): a dependent that was stopped before the update stays stopped.
func TestCL_AC038_StoppedDependentStaysStopped(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	h.docker.add(fakeContainer{Name: "da", Running: false, NetworkMode: "container:" + a.Name})

	res := h.run(idA, "docker_update", map[string]interface{}{"container": a.Name})

	require.Equal(t, "success", res.Status, res.Message)
	assert.False(t, h.docker.has("start:"+h.docker.get("da").ID), "[AC-038] a stopped dependent is never started")
	da, _ := h.docker.snapshot("da")
	assert.False(t, da.Running)
}

// AC-039 (FR-033): a dependent that cannot be restarted fails the command at
// restart_dependents, naming it and the main update's outcome.
func TestCL_AC039_FailedRestoreFailsCommand(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	vpn := h.docker.add(fakeContainer{Name: "vpn-client", Running: true, NetworkMode: "container:" + a.Name})
	h.docker.failIf = func(call string) error {
		if call == "start:"+vpn.ID {
			return errors.New("cannot start")
		}
		return nil
	}

	res := h.run(idA, "docker_update", map[string]interface{}{"container": a.Name})

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepRestartDependents, res.Step, "[AC-039]")
	assert.Contains(t, res.Message, "vpn-client", "[AC-039] the dependent is named")
	assert.Contains(t, res.Message, "main update succeeded", "[AC-039] the main outcome is stated")
}

// ---------------------------------------------------------------------------
// Compose limits and steps
// ---------------------------------------------------------------------------

// blockPullThenFinish configures pull as a blocking process (so the test controls
// when it ends) and every other process as an instant success.
func blockPull(p *fakeProcess, args []string) {
	if contains(args, "pull") {
		p.autoExit = false
		p.honorTerm = true
	}
}

// AC-049 (FR-043): a pull that took 14 minutes leaves `up` its full 10 minutes.
func TestCL_AC049_UpGetsFullBudgetAfterSlowPull(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "a", true, "")
	h.procs.configure = func(args []string, p *fakeProcess) { blockPull(p, args) }

	h.submit(idA, "docker_update", stackParams(dir))
	pull := <-h.procs.created
	<-pull.started
	h.clk.Advance(14 * time.Minute)
	pull.finish(nil)
	h.wait()

	res := resultView{h.resultFor(idA)}
	require.Equal(t, "success", res.Status, res.Message)
	durations := h.clk.timerDurations()
	assert.Equal(t, models.ComposePullLimit, durations[0], "[AC-049] pull runs under the 15m limit")
	assert.Equal(t, models.ComposeUpLimit, durations[1], "[AC-049] up starts a fresh 10m limit, undiminished by the 14m pull")
	assert.Equal(t, []string{"pull", "up"}, h.procs.subcommands())
}

// AC-052 (FR-045): when the pull limit fires, the command fails at pull with
// "timed out after 15m".
func TestCL_AC052_PullLimitMessage(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "a", true, "")
	h.procs.configure = func(args []string, p *fakeProcess) { blockPull(p, args) }

	h.submit(idA, "docker_update", stackParams(dir))
	pull := <-h.procs.created
	<-pull.started
	h.clk.Advance(models.ComposePullLimit)
	h.wait()

	res := resultView{h.resultFor(idA)}
	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPull, res.Step, "[AC-052]")
	assert.Equal(t, "timed out after 15m", res.Message, "[AC-052]")
	assert.Equal(t, models.ReasonTimeout, res.Reason)
	assert.Equal(t, []string{"SIGTERM"}, pull.sent(), "[AC-052] the process got SIGTERM first")
}

// AC-093 (FR-076): a successful compose update reports precheck, pull, recreate,
// verifying, in that order, then succeeds.
func TestCL_AC093_ComposeProgressOrder(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "a", true, "")

	res := h.run(idA, "docker_update", stackParams(dir))
	require.Equal(t, "success", res.Status, res.Message)
	h.deliver()

	var seq []string
	for _, m := range h.tr.wsMessages("command_progress") {
		var p models.CommandProgress
		require.NoError(t, json.Unmarshal(m.Data, &p))
		seq = append(seq, p.State+":"+p.Step)
	}
	assert.Equal(t, []string{"running:precheck", "running:pull", "running:recreate", "verifying:verify"}, seq,
		"[AC-093] progress steps in order")
	assert.Equal(t, "succeeded", h.results()[len(h.results())-1].Outcome, "[AC-093] and finally succeeded")
	assert.Equal(t, "verify", res.Step)
}

// FR-030 / compose verify: a service that was running and is not running after
// the update fails the command at verify (no compose rollback).
func TestCL_AC093_ComposeVerifyFailsWhenServiceDown(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	h.pauseHook = func(time.Duration) {
		h.docker.mu.Lock()
		a.Running = false
		h.docker.mu.Unlock()
	}

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepVerify, res.Step)
	assert.Contains(t, res.Message, "service a is not running")
}

// ---------------------------------------------------------------------------
// Locks and freshness
// ---------------------------------------------------------------------------

// AC-063 (FR-054, FR-055): a second update of the same container fails at
// precheck without a single Docker call, leaving the first undisturbed.
func TestCL_AC063_SecondUpdateFailsPrecheckNoDockerCalls(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	_, _, ok := h.a.locks.TryLockAll([]string{models.TargetKeyContainer("web")}, idB, "docker_update")
	require.True(t, ok)

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPrecheck, res.Step, "[AC-063]")
	assert.Equal(t, models.ReasonConflict, res.Reason)
	assert.Contains(t, res.Message, "already in progress", "[AC-063]")
	assert.Contains(t, res.Message, idB, "[AC-063] the holder is named")
	assert.Empty(t, h.docker.callList(), "[AC-063] the second update must make no Docker calls")
	holder, held := h.a.locks.Holder(models.TargetKeyContainer("web"))
	assert.True(t, held)
	assert.Equal(t, idB, holder.CmdID, "[AC-063] the first update's lock is unaffected")
}

// AC-064 (FR-054): while a single-service update runs, a full update of its
// project is rejected.
func TestCL_AC064_StackRejectedDuringServiceUpdate(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	h.procs.configure = func(args []string, p *fakeProcess) { blockPull(p, args) }

	h.submit(idA, "docker_update", map[string]interface{}{"container": a.Name})
	pull := <-h.procs.created
	<-pull.started // the service update now holds its locks

	h.submit(idB, "docker_update", stackParams(dir))
	require.Eventually(t, func() bool { return h.a.out.Has(idB) }, 5*time.Second, time.Millisecond)
	rejected := resultView{h.resultFor(idB)}
	pull.finish(nil)
	h.wait()

	assert.Equal(t, "error", rejected.Status, "[AC-064] the stack update is rejected")
	assert.Equal(t, models.StepPrecheck, rejected.Step)
	assert.Contains(t, rejected.Message, "already in progress")
	assert.Equal(t, "success", resultView{h.resultFor(idA)}.Status, "[AC-064] the first update completes")
}

// AC-065 (FR-054): updates of different standalone containers run together.
func TestCL_AC065_DifferentStandaloneTargetsBothRun(t *testing.T) {
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{Name: "web", Image: "nginx:latest", Running: true})
	h.docker.add(fakeContainer{Name: "db", Image: "postgres:16", Running: true})

	// Both pulls must be in flight at the same time: each waits for the other.
	var arrived sync.WaitGroup
	arrived.Add(2)
	h.docker.onCall = func(call string) {
		if strings.HasPrefix(call, "pull:") {
			arrived.Done()
			arrived.Wait()
		}
	}
	h.submit(idA, "docker_update", nameParams("web"))
	h.submit(idB, "docker_update", nameParams("db"))
	h.wait()

	assert.Equal(t, "success", resultView{h.resultFor(idA)}.Status, "[AC-065] web updated")
	assert.Equal(t, "success", resultView{h.resultFor(idB)}.Status, "[AC-065] db updated")
}

// AC-061 (FR-052): after an update, only the references involved are
// invalidated.
func TestCL_AC061_DockerUpdateInvalidatesItsRefs(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	h.docker.add(fakeContainer{Name: "cache", Image: "redis:7", Running: true})

	h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, []string{"nginx:latest"}, h.fresh.invalidated, "[AC-061] only the updated image is invalidated")
	assert.False(t, h.fresh.cleared, "[AC-061] the whole cache is not cleared")
}

// Restart exclusion, symmetric half (SEC C-4c): a Docker update is refused while
// agent_update holds the host lock.
func TestCL_SEC004_DockerUpdateRefusedWhileAgentRestartRunning(t *testing.T) {
	h := newDockerHarness(t)
	h.webContainer()
	_, _, ok := h.a.locks.TryLockAll([]string{models.TargetKeyHost}, idB, models.ActionAgentUpdate)
	require.True(t, ok)

	res := h.run(idA, "docker_update", nameParams("web"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Equal(t, models.ReasonConflict, res.Reason)
	assert.Empty(t, h.docker.callList(), "[SEC-004] no Docker call while the agent restarts")
}

// Bulk: children first, locks for the union, one result with a count.
func TestCL_AC034_BulkUpdatesChildrenBeforeParents(t *testing.T) {
	h := newDockerHarness(t)
	parent := h.docker.add(fakeContainer{ID: webID, Name: "vpn", Image: "gluetun:latest", Running: true})
	child := h.docker.add(fakeContainer{Name: "qbit", Image: "qbit:latest", Running: true, NetworkMode: "container:vpn"})

	res := h.run(idA, "docker_bulk_update", map[string]interface{}{"container_ids": []interface{}{parent.ID, child.ID}})

	require.Equal(t, "success", res.Status, res.Message)
	assert.Contains(t, res.Message, "bulk update: 2/2 succeeded")
	assert.Less(t, h.docker.indexOf("pull:qbit:latest"), h.docker.indexOf("pull:gluetun:latest"), "children are updated before their parent")
	assert.Equal(t, []string{"gluetun:latest", "qbit:latest"}, sortedCopy(h.fresh.invalidated))
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Bulk: a target held by another command fails the bulk at precheck.
func TestCL_AC063_BulkConflictFailsPrecheck(t *testing.T) {
	h := newDockerHarness(t)
	c := h.webContainer()
	_, _, ok := h.a.locks.TryLockAll([]string{models.TargetKeyContainer("web")}, idB, "docker_update")
	require.True(t, ok)

	res := h.run(idA, "docker_bulk_update", map[string]interface{}{"container_ids": []interface{}{c.ID}})

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "already in progress")
}

// Bulk wall-time budget: when the next container cannot finish inside the
// budget the rest are skipped with the documented message.
func TestCL_AC049_BulkSkipsRemainingWhenBudgetExhausted(t *testing.T) {
	h := newDockerHarness(t)
	one := h.docker.add(fakeContainer{Name: "one", Image: "one:latest", Running: true})
	two := h.docker.add(fakeContainer{Name: "two", Image: "two:latest", Running: true})
	h.pauseHook = func(time.Duration) { h.clk.Advance(models.BulkAgentBudget(2)) }

	res := h.run(idA, "docker_bulk_update", map[string]interface{}{"container_ids": []interface{}{one.ID, two.ID}})

	assert.Equal(t, "error", res.Status)
	assert.Contains(t, res.Message, "two: "+models.BulkSkippedMessage, "[AC-049] the second container is skipped")
	assert.Contains(t, res.Message, "bulk update: 1/2 succeeded")
	assert.False(t, h.docker.has("pull:two:latest"), "[AC-049] a skipped container is not touched")
}
