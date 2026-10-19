package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

const validationFailure = "validating /srv/stack/compose.yaml: services.web.deploy additional properties 'resources' not allowed"

// SEC C-5: an existing symlink at the sanitized path is never followed; the step
// fails and the symlink's target is unchanged. Linux only (symlink creation
// needs privileges on Windows).
func TestCL_SEC005_SanitizedFileRefusesPreexistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevated privileges on Windows")
	}
	dir := composeDir(t)
	victim := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(victim, []byte("api_key: precious\n"), 0600))
	link := filepath.Join(dir, sanitizedComposeName)
	require.NoError(t, os.Symlink(victim, link))

	_, err := sanitizeComposeFile(dir)

	require.Error(t, err, "[SEC-005] a symlink at the target name must fail the step")
	assert.Contains(t, err.Error(), "refusing to write sanitized compose file")
	assert.Contains(t, err.Error(), "is not an agent-owned regular file")
	data, readErr := os.ReadFile(victim)
	require.NoError(t, readErr)
	assert.Equal(t, "api_key: precious\n", string(data), "[SEC-005] the symlink target must be untouched")
	fi, lerr := os.Lstat(link)
	require.NoError(t, lerr)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "[SEC-005] the symlink itself is left in place")
}

// SEC C-5: the file is created exclusively with mode 0600.
func TestCL_SEC005_SanitizedFileMode0600(t *testing.T) {
	dir := composeDir(t)
	var gotFlag int
	var gotPerm os.FileMode
	rec := func(name string, flag int, perm os.FileMode) (*os.File, error) {
		gotFlag, gotPerm = flag, perm
		return os.OpenFile(name, flag, perm)
	}

	path, err := writeSanitizedComposeWith(rec, dir, []byte("services: {}\n"))

	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), gotPerm, "[SEC-005] the file must be requested with mode 0600")
	assert.NotZero(t, gotFlag&os.O_EXCL, "[SEC-005] the file must be created with O_EXCL")
	assert.NotZero(t, gotFlag&os.O_CREATE)
	if runtime.GOOS != "windows" {
		fi, serr := os.Stat(path)
		require.NoError(t, serr)
		assert.Equal(t, os.FileMode(0600), fi.Mode().Perm(), "[SEC-005] the created file has mode 0600 on disk")
	}
	assert.Equal(t, filepath.Join(dir, sanitizedComposeName), path)
}

// SEC C-5: a stale agent-owned regular file (a legacy leftover) is replaced; a
// directory at the path is refused.
func TestCL_SEC005_StaleOwnFileReplacedDirectoryRefused(t *testing.T) {
	dir := composeDir(t)
	path := filepath.Join(dir, sanitizedComposeName)
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0644))

	_, err := writeSanitizedCompose(dir, []byte("new\n"))
	require.NoError(t, err, "[SEC-005] an agent-owned leftover regular file is cleared")
	data, _ := os.ReadFile(path)
	assert.Equal(t, "new\n", string(data))

	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0755))
	_, err = writeSanitizedCompose(dir, []byte("x"))
	require.Error(t, err, "[SEC-005] anything that is not an agent-owned regular file is refused")
	assert.Contains(t, err.Error(), "refusing to write sanitized compose file")
}

// SEC C-5: the sanitized copy is removed on every exit path: success, pull
// failure, up failure and panic.
func TestCL_SEC005_SanitizedFileRemovedOnEveryExit(t *testing.T) {
	scenarios := []struct {
		name       string
		configure  func(h *dockerHarness) func(args []string, p *fakeProcess)
		wantStatus string
		wantStep   string
	}{
		{name: "success", wantStatus: "success"},
		{name: "pull failure", wantStatus: "error", wantStep: models.StepPull,
			configure: func(h *dockerHarness) func([]string, *fakeProcess) {
				return func(args []string, p *fakeProcess) {
					if contains(args, "pull") && containsPrefix(args, "-f=") {
						p.exitErr, p.out = errors.New("exit status 1"), []byte("pull access denied")
					}
				}
			}},
		{name: "up failure", wantStatus: "error", wantStep: models.StepRecreate,
			configure: func(h *dockerHarness) func([]string, *fakeProcess) {
				return func(args []string, p *fakeProcess) {
					if contains(args, "up") {
						p.exitErr, p.out = errors.New("exit status 1"), []byte("port in use")
					}
				}
			}},
		{name: "panic", wantStatus: "error",
			configure: func(h *dockerHarness) func([]string, *fakeProcess) {
				return func(args []string, p *fakeProcess) {
					if contains(args, "up") {
						panic("compose exploded")
					}
				}
			}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			captureLogs(t)
			h := newDockerHarness(t)
			dir := composeDir(t)
			h.composeService(dir, "web", true, "")
			path := filepath.Join(dir, sanitizedComposeName)

			var existedDuringUp bool
			scenario := func([]string, *fakeProcess) {}
			if sc.configure != nil {
				scenario = sc.configure(h)
			}
			h.procs.configure = func(args []string, p *fakeProcess) {
				switch {
				case contains(args, "pull") && !containsPrefix(args, "-f="):
					// the first pull trips compose's file validation
					p.exitErr, p.out = errors.New("exit status 1"), []byte(validationFailure)
					return
				case contains(args, "up"):
					_, statErr := os.Stat(path)
					existedDuringUp = statErr == nil
				}
				scenario(args, p)
			}

			res := h.run(idA, "docker_update", stackParams(dir))

			assert.Equal(t, sc.wantStatus, res.Status, "[SEC-005] %s: %s", sc.name, res.Message)
			if sc.wantStep != "" {
				assert.Equal(t, sc.wantStep, res.Step)
			}
			_, statErr := os.Lstat(path)
			assert.True(t, os.IsNotExist(statErr), "[SEC-005] the sanitized copy must be gone after: %s", sc.name)
			if sc.name == "success" {
				assert.True(t, existedDuringUp, "[SEC-005] the copy is used by compose up and exists until the update ends")
				_, kept := h.a.inflight.Get(idA)
				assert.False(t, kept, "[SEC-005] the marker is cleared with the result")
			}
		})
	}
}

func containsPrefix(ss []string, prefix string) bool {
	for _, s := range ss {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// SEC C-5: the marker records the sanitized path before the file is created.
func TestCL_SEC005_SanitizedPathRecordedBeforeCreation(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	path := filepath.Join(dir, sanitizedComposeName)
	var markerBefore string
	h.procs.configure = func(args []string, p *fakeProcess) {
		if contains(args, "pull") && !containsPrefix(args, "-f=") {
			p.exitErr, p.out = errors.New("exit status 1"), []byte(validationFailure)
			return
		}
		if contains(args, "pull") {
			data, _ := os.ReadFile(h.inflightPath)
			markerBefore = string(data)
		}
	}

	h.run(idA, "docker_update", stackParams(dir))

	assert.Contains(t, markerBefore, filepath.ToSlash(path)[len(filepath.ToSlash(path))-len(sanitizedComposeName):],
		"[SEC-005] the sanitized file name is in the marker before compose uses the copy")
	assert.Contains(t, markerBefore, `"sanitized_file"`)
}

// SEC C-5: startup removes leftovers: files a marker recorded, and files found
// in the compose project directories the daemon knows. A symlink is unlinked and
// its target is never touched.
func TestCL_SEC005_StartupRemovesLeftoverSanitizedFile(t *testing.T) {
	t.Run("marker-recorded file", func(t *testing.T) {
		h := newDockerHarness(t)
		dir := composeDir(t)
		path := filepath.Join(dir, sanitizedComposeName)
		require.NoError(t, os.WriteFile(path, []byte("services: {}\n"), 0600))
		h.interrupted(models.StepPull, dockerRecord{Key: "compose:" + dir, SanitizedFile: path})

		h.a.recoverInterruptedCommands(context.Background())

		_, err := os.Lstat(path)
		assert.True(t, os.IsNotExist(err), "[SEC-005] the marker-recorded sanitized file is removed at startup")
	})

	t.Run("found through container labels", func(t *testing.T) {
		h := newDockerHarness(t)
		dir := composeDir(t)
		h.composeService(dir, "web", true, "")
		path := filepath.Join(dir, sanitizedComposeName)
		require.NoError(t, os.WriteFile(path, []byte("services: {}\n"), 0644))

		h.a.recoverInterruptedCommands(context.Background())

		_, err := os.Lstat(path)
		assert.True(t, os.IsNotExist(err), "[SEC-005] a leftover in a known project directory is removed")
	})

	t.Run("symlink is unlinked not followed", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("creating symlinks needs elevated privileges on Windows")
		}
		h := newDockerHarness(t)
		dir := composeDir(t)
		h.composeService(dir, "web", true, "")
		victim := filepath.Join(t.TempDir(), "agent.yaml")
		require.NoError(t, os.WriteFile(victim, []byte("api_key: precious\n"), 0600))
		link := filepath.Join(dir, sanitizedComposeName)
		require.NoError(t, os.Symlink(victim, link))

		h.a.recoverInterruptedCommands(context.Background())

		_, err := os.Lstat(link)
		assert.True(t, os.IsNotExist(err), "[SEC-005] the planted symlink is unlinked")
		data, readErr := os.ReadFile(victim)
		require.NoError(t, readErr)
		assert.Equal(t, "api_key: precious\n", string(data), "[SEC-005] the symlink target is never touched")
	})
}
