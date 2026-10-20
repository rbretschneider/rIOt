package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// useFakeTrust swaps the harness's all-root filesystem for a map-backed one
// that mirrors dir (and its ancestors) as root-owned, so a test can change
// ownership, add files or links while a command runs. The compose file also
// exists on the real disk (composeDir), because the sanitized copy is read
// from there.
func (h *dockerHarness) useFakeTrust(dir string) *fakeTrustFS {
	h.t.Helper()
	fs := newFakeTrustFS()
	fs.chain(dir)
	fs.file(filepath.Join(dir, "compose.yaml"), 0, 0, 0644, "services: {}\n")
	fs.chain(filepath.Dir(defaultDockerSocket))
	fs.file(defaultDockerSocket, 0, 0, 0660, "").mode |= os.ModeSocket
	fs.file(accountPasswdPath, 0, 0, 0644, "root:x:0:0:root:/root:/bin/sh\nmallory:x:2000:2000::/home/mallory:/bin/sh\n")
	fs.file(accountGroupPath, 0, 0, 0644, "root:x:0:\nmallory:x:2000:\n")
	h.a.trustFS = fs
	return fs
}

func projectParams(project, dir string) map[string]interface{} {
	p := map[string]interface{}{"compose_project": project}
	if dir != "" {
		p["compose_work_dir"] = dir
	}
	return p
}

func (h *dockerHarness) noCompose(t *testing.T, why string) {
	t.Helper()
	assert.Empty(t, h.procs.invocations(), "%s: no docker compose process may start", why)
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ---------------------------------------------------------------------------
// AC-001 / AC-017
// ---------------------------------------------------------------------------

func TestDockerUpdate_AC001_RelativeWorkDirStartsNoComposeProcess(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")

	for _, wd := range []string{"relative/dir", "./stack", "../stack"} {
		res := h.run(idA, "docker_update", map[string]interface{}{"compose_work_dir": wd})
		assert.Equal(t, "error", res.Status, "[AC-001] %q", wd)
		assert.Equal(t, models.StepPrecheck, res.Step)
		assert.Contains(t, res.Message, "(not_absolute)")
		h.noCompose(t, "[AC-001] "+wd)
	}
	res := h.run(idB, "docker_update", map[string]interface{}{"compose_work_dir": ""})
	assert.Equal(t, "error", res.Status, "[AC-001] an empty directory counts as absent")
	assert.Contains(t, res.Message, "is required")
	h.noCompose(t, "[AC-001] empty")
}

func TestDockerUpdate_AC017_RefusalIsPrecheckWithRule(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	fs := h.useFakeTrust(dir)
	fs.get(dir).uid = 2000

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPrecheck, res.Step, "[AC-017] a refusal is a precheck failure")
	assert.Contains(t, res.Message, "compose directory refused (owner)", "[AC-017] the rule is named")
	assert.Contains(t, res.Message, filepath.Base(dir), "[AC-017] the path is named")
	assert.LessOrEqual(t, len(res.Message), maxRefusalBytes, "the stored message is bounded")
	h.noCompose(t, "[AC-017]")
	for _, op := range []string{"stop:", "rename:", "create:", "remove:"} {
		assert.Empty(t, h.docker.callsWithPrefix(op), "[AC-017] a refusal changes no container (%s)", op)
	}
}

// ---------------------------------------------------------------------------
// AC-002 resolved path and pinned project
// ---------------------------------------------------------------------------

func TestDockerUpdate_AC002_ComposeRunsAtResolvedPathWithPinnedProject(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	fs := h.useFakeTrust(dir)
	link := filepath.Join(filepath.Dir(dir), "link-to-stack")
	fs.link(link, dir, 0)
	h.composeService(link, "web", true, "") // the label names the symlink

	res := h.run(idA, "docker_update", projectParams("stack", link))

	require.Equal(t, "success", res.Status, res.Message)
	argv := h.procs.invocations()[0]
	assert.Contains(t, argv, "--project-directory="+dir, "[AC-002] compose runs at the resolved path, not the symlink")
	assert.NotContains(t, argv, "--project-directory="+link)
	assert.Contains(t, argv, "--project-name=stack", "[AC-002] the project is pinned, so a different basename cannot create a duplicate project")
	assert.Contains(t, argv, "-f="+filepath.Join(dir, "compose.yaml"))
}

func TestComposeCore_AC002_VerifyMatchesResolvedWorkingDirLabel(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	fs := h.useFakeTrust(dir)
	link := filepath.Join(filepath.Dir(dir), "link-to-stack")
	fs.link(link, dir, 0)
	web := h.composeService(link, "web", true, "")
	// Compose, run at the resolved path, writes the resolved path as the label.
	h.procs.configure = func(args []string, p *fakeProcess) {
		if contains(args, "up") {
			h.docker.mu.Lock()
			web.Labels = composeLabels(dir, "web")
			h.docker.mu.Unlock()
		}
	}

	res := h.run(idA, "docker_update", projectParams("stack", link))

	assert.Equal(t, "success", res.Status, "[AC-002] verification finds the service under the resolved label: %s", res.Message)
}

// ---------------------------------------------------------------------------
// AC-009 / AC-010 / AC-011 stack resolution
// ---------------------------------------------------------------------------

func TestUpdateStack_AC009_ProjectResolvesToOneDir(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")

	res := h.run(idA, "docker_update", projectParams("stack", ""))

	require.Equal(t, "success", res.Status, res.Message)
	assert.Contains(t, h.procs.invocations()[0], "--project-directory="+dir, "[AC-009] the directory comes from the project, not from the server")
}

func TestUpdateStack_AC009_ProjectNotFoundRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	h.composeService(composeDir(t), "web", true, "")

	res := h.run(idA, "docker_update", projectParams("nosuch", ""))

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(project_not_found)", "[AC-009]")
	h.noCompose(t, "[AC-009]")
}

func TestUpdateStack_AC009_ProjectAmbiguousRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	h.composeService(composeDir(t), "web", true, "")
	h.composeService(composeDir(t), "db", true, "") // same project label, second directory

	res := h.run(idA, "docker_update", projectParams("stack", ""))

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(ambiguous_project)", "[AC-009]")
	h.noCompose(t, "[AC-009]")
}

func TestUpdateStack_AC009_InvalidProjectNameRefused(t *testing.T) {
	for _, bad := range []string{"--file=/x", "Stack", "a b", "-x"} {
		t.Run(bad, func(t *testing.T) {
			captureLogs(t)
			h := newDockerHarness(t)
			h.composeService(composeDir(t), "web", true, "")

			res := h.run(idA, "docker_update", projectParams(bad, ""))

			assert.Contains(t, res.Message, "(invalid_project)", "[AC-009] %q", bad)
			h.noCompose(t, "[AC-009]")
			assert.Empty(t, h.docker.callList(), "[AC-009] a bad project name is refused before any Docker call")
		})
	}
}

func TestUpdateStack_AC010_MismatchedWorkDirRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")

	res := h.run(idA, "docker_update", projectParams("stack", filepath.Join(filepath.Dir(dir), "elsewhere")))

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(work_dir_mismatch)", "[AC-010]")
	h.noCompose(t, "[AC-010]")
}

func TestUpdateStack_AC010_TrailingSlashWorkDirAccepted(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")

	res := h.run(idA, "docker_update", projectParams("stack", dir+"/"))

	assert.Equal(t, "success", res.Status, "[AC-010] a trailing slash is cleaned before the comparison: %s", res.Message)
}

func TestUpdateStack_AC011_WorkDirAloneMapsToOneProject(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")

	res := h.run(idA, "docker_update", stackParams(dir))

	require.Equal(t, "success", res.Status, res.Message)
	assert.Contains(t, h.procs.invocations()[0], "--project-name=stack", "[AC-011] the project is derived from the live containers")
}

func TestUpdateStack_AC011_WorkDirAloneTwoProjectsRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	other := composeLabels(dir, "x")
	other[models.LabelComposeProject] = "other"
	h.docker.add(fakeContainer{Name: "other-x-1", Image: "x", Running: true, Labels: other})

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Contains(t, res.Message, "(ambiguous_project)", "[AC-011]")
	h.noCompose(t, "[AC-011]")
}

func TestUpdateStack_AC011_WorkDirAloneUntrustedRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	h.useFakeTrust(dir).get(dir).uid = 2000

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Contains(t, res.Message, "(owner)", "[AC-011] the old-server form still goes through validation")
	h.noCompose(t, "[AC-011]")
}

func TestUpdateStack_AC018_ImageLabelContainerRefusesPathAgreement(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	hostile := h.docker.add(fakeContainer{Name: "hostile", Image: "evil", Running: true, ImageID: "sha256:evil",
		Labels: composeLabels(filepath.Join(filepath.Dir(dir), "evil-dir"), "pwn")})
	h.docker.imageLabels[hostile.ImageID] = map[string]string{models.LabelComposeWorkingDir: "x"}

	res := h.run(idA, "docker_update", projectParams("stack", ""))

	assert.Equal(t, "error", res.Status, "DOCKER-UX SEC-024: every observed member must agree on source-validated stack paths")
	assert.Contains(t, res.Message, "untrusted stack provenance")
	h.noCompose(t, "untrusted member labels")
}

// ---------------------------------------------------------------------------
// AC-012 / AC-018 / AC-025 every path
// ---------------------------------------------------------------------------

func TestDockerUpdate_AC012_SingleServiceUntrustedLabelRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	h.useFakeTrust(dir).get(dir).uid = 2000

	res := h.run(idA, "docker_update", map[string]interface{}{"container": a.Name})

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(owner)", "[AC-012]")
	h.noCompose(t, "[AC-012]")
}

func TestDockerBulkUpdate_AC012_UntrustedComposeMemberRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	a := h.composeService(dir, "a", true, "")
	h.useFakeTrust(dir).get(dir).uid = 2000

	res := h.run(idA, "docker_bulk_update", map[string]interface{}{"container_ids": []interface{}{a.ID}})

	assert.Equal(t, "error", res.Status)
	assert.Contains(t, res.Message, "(owner)", "[AC-012] a bulk member goes through the same gate")
	h.noCompose(t, "[AC-012]")
}

// dependentSetup builds a stale compose dependent next to a parent.
func (h *dockerHarness) dependentSetup(dir string, mod func(c *fakeContainer)) depRecord {
	h.docker.add(fakeContainer{Name: "vpn", Running: true})
	c := fakeContainer{Name: "app", Running: true, NetworkMode: "container:stale-parent-id", Labels: composeLabels(dir, "app")}
	if mod != nil {
		mod(&c)
	}
	dep := h.docker.add(c)
	return depRecord{ID: dep.ID, Name: "app", ParentName: "vpn", Running: true}
}

func TestRestoreDependents_AC012_UntrustedDependentDirNotRecreated(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	dep := h.dependentSetup(dir, nil)
	h.useFakeTrust(dir).get(dir).uid = 2000

	failed := h.a.restoreDependents(context.Background(), h.docker, []depRecord{dep})

	require.Len(t, failed, 1)
	assert.Contains(t, failed[0].Reason, "(owner)", "[AC-012] the refusal is that dependent's restore failure")
	h.noCompose(t, "[AC-012]")
}

func TestDockerUpdate_AC018_SingleServiceImageLabelRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	c := h.docker.add(fakeContainer{Name: "stack-a-1", Image: "evil", Running: true, ImageID: "sha256:evil", Labels: composeLabels(dir, "a")})
	h.docker.imageLabels["sha256:evil"] = map[string]string{models.LabelComposeWorkingDir: dir}

	res := h.run(idA, "docker_update", map[string]interface{}{"container": c.Name})

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(label_provenance)", "[AC-018]")
	assert.Contains(t, res.Message, "stack-a-1", "[AC-018] the container is named")
	h.noCompose(t, "[AC-018]")
}

func TestDockerBulkUpdate_AC018_ImageLabelMemberRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	c := h.docker.add(fakeContainer{Name: "stack-a-1", Image: "evil", Running: true, ImageID: "sha256:evil", Labels: composeLabels(dir, "a")})
	h.docker.imageLabels["sha256:evil"] = map[string]string{models.LabelComposeWorkingDir: dir}

	res := h.run(idA, "docker_bulk_update", map[string]interface{}{"container_ids": []interface{}{c.ID}})

	assert.Contains(t, res.Message, "(label_provenance)", "[AC-018]")
	h.noCompose(t, "[AC-018]")
}

func TestRestoreDependents_AC018_ImageLabelDependentNotRecreated(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	dep := h.dependentSetup(dir, func(c *fakeContainer) { c.ImageID = "sha256:evil" })
	h.docker.imageLabels["sha256:evil"] = map[string]string{models.LabelComposeWorkingDir: dir}

	failed := h.a.restoreDependents(context.Background(), h.docker, []depRecord{dep})

	require.Len(t, failed, 1)
	assert.Contains(t, failed[0].Reason, "(label_provenance)", "[AC-018]")
	h.noCompose(t, "[AC-018]")
}

func TestDockerUpdate_AC025_SingleServiceProjectMustOwnDir(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	// A second project claiming the same directory would start duplicate
	// containers over the same data.
	evil := composeLabels(dir, "s")
	evil[models.LabelComposeProject] = "evil"
	c := h.docker.add(fakeContainer{Name: "evil-s-1", Image: "x", Running: true, Labels: evil})

	res := h.run(idA, "docker_update", map[string]interface{}{"container": c.Name})

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Regexp(t, `\((ambiguous_project|project_mismatch)\)`, res.Message, "[AC-025]")
	h.noCompose(t, "[AC-025]")
}

func TestRestoreDependents_AC025_DependentProjectMismatchRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	dep := h.dependentSetup(dir, func(c *fakeContainer) { c.Labels[models.LabelComposeProject] = "evil" })

	failed := h.a.restoreDependents(context.Background(), h.docker, []depRecord{dep})

	require.Len(t, failed, 1)
	assert.Regexp(t, `\((ambiguous_project|project_mismatch)\)`, failed[0].Reason, "[AC-025]")
	h.noCompose(t, "[AC-025]")
}

// ---------------------------------------------------------------------------
// AC-013 the sanitized copy and later checks
// ---------------------------------------------------------------------------

// failFirstPull makes the first pull trip compose's file validation and runs
// onFail (a test's change to the world) at that moment.
func failFirstPull(h *dockerHarness, onFirstPull func(), onRetry func(sanitizedExists bool)) {
	h.procs.configure = func(args []string, p *fakeProcess) {
		switch {
		case contains(args, "pull") && !isSanitizedF(args):
			p.exitErr, p.out = errors.New("exit status 1"), []byte(validationFailure)
			if onFirstPull != nil {
				onFirstPull()
			}
		case contains(args, "pull") && isSanitizedF(args) && onRetry != nil:
			onRetry(true)
		}
	}
}

func TestComposeCore_AC013_SanitizedNotWrittenWhenRecheckFails(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	fs := h.useFakeTrust(dir)
	failFirstPull(h, func() { fs.get(dir).uid = 2000 }, nil)

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(owner)", "[AC-013] check #2 refuses")
	assert.False(t, fileExists(filepath.Join(dir, sanitizedComposeName)), "[AC-013] the sanitized copy is never written into a directory that failed validation")
	assert.Equal(t, []string{"pull"}, h.procs.subcommands(), "[AC-013] no retry pull and no up")
}

func TestComposeCore_AC013_DirChangedBetweenChecksRefused(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	fs := h.useFakeTrust(dir)
	failFirstPull(h, func() { fs.file(filepath.Join(dir, "compose.override.yml"), 0, 0, 0644, "") }, nil)

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(dir_changed)", "[AC-013] a file set that changed between checks is refused")
	assert.False(t, fileExists(filepath.Join(dir, sanitizedComposeName)))
	assert.NotContains(t, h.procs.subcommands(), "up")
}

// Cleanup on a failure path: check #3 refuses after the sanitized copy exists.
func TestComposeCore_AC013_SanitizedRemovedWhenThirdCheckRefuses(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	fs := h.useFakeTrust(dir)
	existedDuringRetry := false
	failFirstPull(h, nil, func(bool) {
		existedDuringRetry = fileExists(filepath.Join(dir, sanitizedComposeName))
		fs.get(dir).uid = 2000 // the directory turns untrusted before check #3
	})

	res := h.run(idA, "docker_update", stackParams(dir))

	require.True(t, existedDuringRetry, "precondition: the sanitized copy was written for the retry pull")
	assert.Equal(t, "error", res.Status)
	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(owner)", "[AC-013] check #3 refuses")
	assert.False(t, fileExists(filepath.Join(dir, sanitizedComposeName)), "[AC-013] the refusal leaves no sanitized copy behind")
	assert.NotContains(t, h.procs.subcommands(), "up", "[AC-013] nothing is recreated, nothing was stopped")
	for _, op := range []string{"stop:", "rename:", "create:", "remove:"} {
		assert.Empty(t, h.docker.callsWithPrefix(op), "[AC-013] no container changed (%s)", op)
	}
}

func TestCleanupSanitized_AC013_SkipsRefusedDir(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	h.useFakeTrust(dir).get(dir).uid = 2000
	path := filepath.Join(dir, sanitizedComposeName)
	require.NoError(t, os.WriteFile(path, []byte("services: {}\n"), 0600))

	h.a.cleanupSanitizedLeftovers(context.Background())

	assert.True(t, fileExists(path), "[AC-013] a directory that fails validation is not touched by startup cleanup")
}

func TestReconcile_AC013_SanitizedRemovalSkipsRefusedDir(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	h.useFakeTrust(dir).get(dir).uid = 2000
	path := filepath.Join(dir, sanitizedComposeName)
	require.NoError(t, os.WriteFile(path, []byte("services: {}\n"), 0600))
	h.interrupted(models.StepPull, dockerRecord{Key: "compose:" + dir, SanitizedFile: path})

	h.a.recoverInterruptedCommands(context.Background())

	assert.True(t, fileExists(path), "[AC-013] the recorded path is only a string on disk: a refused directory keeps its file")
	assert.Contains(t, h.resultFor(idA).Message, "sanitized compose file left in place", "[AC-013] and the summary says so")
	assert.LessOrEqual(t, len(h.resultFor(idA).Message), 1024, "the summary is bounded")
}

func TestCleanupSanitized_AC018_IgnoresImageLabelDirs(t *testing.T) {
	h := newDockerHarness(t)
	good := composeDir(t)
	h.composeService(good, "web", true, "")
	evil := composeDir(t)
	h.docker.add(fakeContainer{Name: "hostile", Image: "evil", ImageID: "sha256:evil", Labels: composeLabels(evil, "pwn")})
	h.docker.imageLabels["sha256:evil"] = map[string]string{models.LabelComposeWorkingDir: evil}
	goodFile := filepath.Join(good, sanitizedComposeName)
	evilFile := filepath.Join(evil, sanitizedComposeName)
	require.NoError(t, os.WriteFile(goodFile, []byte("x"), 0600))
	require.NoError(t, os.WriteFile(evilFile, []byte("x"), 0600))

	h.a.cleanupSanitizedLeftovers(context.Background())

	assert.False(t, fileExists(goodFile), "an ordinary project directory is cleaned")
	assert.True(t, fileExists(evilFile), "[AC-018] a directory named only by an image-defined label is not touched")
}

// ---------------------------------------------------------------------------
// AC-018 standalone recreate, AC-026 env
// ---------------------------------------------------------------------------

func TestStandaloneUpdate_AC018_RecreateDropsWorkingDirLabel(t *testing.T) {
	h := newDockerHarness(t)
	labels := map[string]string{models.LabelComposeWorkingDir: "/srv/x", "app.example/keep": "yes"}
	web := h.docker.add(fakeContainer{ID: webID, Name: "web", Image: "nginx:latest", Running: true, Labels: labels})

	res := h.run(idA, "docker_update", nameParams("web"))

	require.Equal(t, "success", res.Status, res.Message)
	repl, ok := h.docker.snapshot("web")
	require.True(t, ok)
	assert.NotEqual(t, web.ID, repl.ID, "the container was recreated")
	assert.NotContains(t, repl.Labels, models.LabelComposeWorkingDir, "[AC-018] the replacement does not carry the working-dir label")
	assert.Equal(t, "yes", repl.Labels["app.example/keep"], "other labels are kept")
	assert.Equal(t, "/srv/x", labels[models.LabelComposeWorkingDir], "the input config is not modified")
}

func TestDockerUpdate_AC026_ComposeProcessGetsPinnedEnv(t *testing.T) {
	t.Setenv("COMPOSE_FILE", "/evil/compose.yaml")
	t.Setenv("COMPOSE_PROJECT_NAME", "evil")
	t.Setenv("DOCKER_HOST", "tcp://attacker.invalid:2375")
	t.Setenv("DOCKER_CONTEXT", "evil")
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")

	res := h.run(idA, "docker_update", stackParams(dir))

	require.Equal(t, "success", res.Status, res.Message)
	h.procs.mu.Lock()
	envs := append([][]string(nil), h.procs.envs...)
	h.procs.mu.Unlock()
	require.NotEmpty(t, envs)
	for i, env := range envs {
		joined := strings.Join(env, "\n")
		assert.Contains(t, env, "DOCKER_HOST=unix://"+defaultDockerSocket, "[AC-026] invocation %d: DOCKER_HOST is pinned", i)
		assert.NotContains(t, joined, "COMPOSE_FILE", "[AC-026] invocation %d", i)
		assert.NotContains(t, joined, "COMPOSE_PROJECT_NAME", "[AC-026] invocation %d", i)
		assert.NotContains(t, joined, "DOCKER_CONTEXT", "[AC-026] invocation %d", i)
		assert.NotContains(t, joined, "attacker.invalid", "[AC-026] invocation %d", i)
	}
}

func TestMarkerTarget_IncludesComposeProject(t *testing.T) {
	got := markerTarget(models.CommandPayload{Params: map[string]interface{}{"compose_project": "media"}})
	assert.Equal(t, "media", got, "the marker names a project-only stack update")
	got = markerTarget(models.CommandPayload{Params: map[string]interface{}{"compose_project": "media", "compose_work_dir": "/srv/media"}})
	assert.Equal(t, "/srv/media", got, "compose_work_dir keeps priority")
}

// ---------------------------------------------------------------------------
// QA-D1: labels that resolve to one real directory are one directory
// ---------------------------------------------------------------------------

func TestUpdateStack_AC009_LinkAndTargetLabelsAreOneDirectory(t *testing.T) {
	dir := composeDir(t)
	link := filepath.Join(filepath.Dir(dir), "link-to-stack")

	for name, params := range map[string]map[string]interface{}{
		"project only":                 projectParams("stack", ""),
		"server names the link":        projectParams("stack", link),
		"server names the real target": projectParams("stack", dir),
		"old server, link":             stackParams(link),
		"old server, target":           stackParams(dir),
	} {
		t.Run(name, func(t *testing.T) {
			h := newDockerHarness(t)
			fs := h.useFakeTrust(dir)
			fs.link(link, dir, 0)
			h.composeService(dir, "a", true, "")
			h.composeService(link, "b", true, "")

			res := h.run(idA, "docker_update", params)

			assert.Equal(t, "success", res.Status, "[AC-009] %s: %s", name, res.Message)
			assert.Contains(t, h.procs.invocations()[0], "--project-directory="+dir, "compose runs at the resolved directory")
		})
	}
}

func TestUpdateStack_AC009_TrulyDifferentDirsStayAmbiguous(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	other := composeDir(t)
	fs := h.useFakeTrust(dir)
	fs.chain(other)
	link := filepath.Join(filepath.Dir(dir), "link-to-other")
	fs.link(link, other, 0)
	h.composeService(dir, "a", true, "")
	h.composeService(link, "b", true, "") // resolves to a different directory

	res := h.run(idA, "docker_update", projectParams("stack", ""))

	assert.Contains(t, res.Message, "(ambiguous_project)", "[AC-009] a link to another directory is another directory")
	h.noCompose(t, "[AC-009]")
}

func TestComposeCore_AC002_VerifyFindsContainersUnderEitherLabelForm(t *testing.T) {
	h := newDockerHarness(t)
	dir := composeDir(t)
	fs := h.useFakeTrust(dir)
	link := filepath.Join(filepath.Dir(dir), "link-to-stack")
	fs.link(link, dir, 0)
	a := h.composeService(link, "a", true, "")
	h.composeService(dir, "b", true, "")
	h.procs.configure = func(args []string, p *fakeProcess) {
		if contains(args, "up") { // compose, run at the resolved path, relabels what it recreates
			h.docker.mu.Lock()
			a.Labels = composeLabels(dir, "a")
			h.docker.mu.Unlock()
		}
	}

	res := h.run(idA, "docker_update", projectParams("stack", ""))

	assert.Equal(t, "success", res.Status, "[AC-002] services under either label form are verified: %s", res.Message)
}
