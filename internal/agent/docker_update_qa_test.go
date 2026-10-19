package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer.
//
// The WP-GO-AGENT AC-036 test fails the compose pull. Dependents are stopped only
// after the pull succeeds (AD-014), so that test passes even when the deferred
// dependent restore is removed (mutation-checked by QA). The tests below fail a
// step AFTER the dependents were stopped, which is where FR-030 ("whatever the
// outcome") actually bites.

// Covers AC-036 (FR-030): the compose `up` step fails with an ordinary exit error
// (not the up-limit timeout covered by AC-037) after the dependent was stopped;
// the dependent must be running again.
func TestCL_AC036_QA_DependentRestoredAfterComposeUpFailure(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	da := h.docker.add(fakeContainer{Name: "da", Running: true, NetworkMode: "container:" + a.Name})
	h.procs.configure = func(args []string, p *fakeProcess) {
		if !contains(args, "up") {
			return
		}
		p.exitErr = errors.New("exit status 1")
		p.out = []byte("Error response from daemon: driver failed programming external connectivity")
	}

	res := h.run(idA, "docker_update", map[string]interface{}{"container": a.Name})

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepRecreate, res.Step, "the main update failed at the recreate step")
	require.True(t, h.docker.has("stop:"+da.ID), "precondition: the dependent was stopped before the up step failed")
	assert.True(t, h.docker.has("start:"+da.ID), "[AC-036] the stopped dependent must be started again after the failure")
	got, _ := h.docker.snapshot("da")
	assert.True(t, got.Running, "[AC-036] the dependent is running after a failed up")
}

// Covers AC-036 / AC-043 (FR-030, FR-039): a standalone parent whose replacement
// cannot be created is rolled back AND its stopped network dependent is running
// again. The result is the rollback failure (reason rolled_back), not a
// restart_dependents failure, because the restore succeeded.
func TestCL_AC043_QA_CreateFailureAlsoRestoresDependents(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	h.docker.add(fakeContainer{ID: webID, Name: "vpn", Image: "gluetun:latest", Running: true})
	dep := h.docker.add(fakeContainer{Name: "qbit", Running: true, NetworkMode: "container:vpn"})
	h.docker.failIf = func(call string) error {
		if call == "create:vpn" {
			return errors.New("image platform mismatch")
		}
		return nil
	}

	res := h.run(idA, "docker_update", nameParams("vpn"))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepRecreate, res.Step)
	assert.Equal(t, models.ReasonRolledBack, res.Reason)
	require.True(t, h.docker.has("stop:"+dep.ID), "precondition: the dependent was stopped")
	got, ok := h.docker.snapshot("qbit")
	require.True(t, ok)
	assert.True(t, got.Running, "[AC-036] the dependent must be running again after the rollback")
	vpn, ok := h.docker.snapshot("vpn")
	require.True(t, ok, "[AC-043] the old parent has its name back")
	assert.Equal(t, webID, vpn.ID)
	assert.True(t, vpn.Running)
}
