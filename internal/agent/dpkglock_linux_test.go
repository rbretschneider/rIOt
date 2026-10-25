//go:build linux

package agent

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests use the real /proc, but only for their own temp file and their own
// /proc/self/status. They never touch a dpkg lock file.

// realProbeEnv probes lockPath through the real stat and the real /proc.
func realProbeEnv(lockPath string) dpkgProbeEnv {
	return dpkgProbeEnv{
		LockPaths:   []string{lockPath},
		DpkgMarkers: nil,
		StatID:      statFileID,
		Readlink:    os.Readlink,
		Open:        platformOpen,
		ProcRoot:    "/proc",
		Now:         time.Now,
	}
}

func platformOpen(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// lockTempFile takes an exclusive POSIX byte-range lock on a new temp file, the
// way apt and dpkg lock their files, and returns the path and an unlock func.
func lockTempFile(t *testing.T) (string, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lock-frontend")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	require.NoError(t, syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lock), "take the test lock")
	unlock := func() {
		lock.Type = syscall.F_UNLCK
		require.NoError(t, syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lock), "release the test lock")
	}
	return path, unlock
}

// requireInitPidNamespace skips, with a logged reason, a test that needs the real
// probe to see the whole host when the test process itself runs in a private pid
// namespace (a Docker run). There the probe correctly refuses, so the real-/proc
// tests cannot assert Held.
func requireInitPidNamespace(t *testing.T) {
	t.Helper()
	link, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		t.Skipf("cannot read /proc/self/ns/pid (%v): the probe would refuse here, as designed", err)
	}
	if link != initPidNamespaceLink {
		t.Skipf("the test process runs in a private pid namespace (%s, not %s): the probe would refuse here, as designed", link, initPidNamespaceLink)
	}
}

func requireProcLocks(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/proc/locks"); err != nil {
		t.Skipf("/proc/locks is not available here: %v", err)
	}
	requireInitPidNamespace(t)
}

func TestDpkgLock_RD_AC002_RealProcLocksSeesFcntlLockOnTempFile(t *testing.T) {
	requireProcLocks(t)
	path, unlock := lockTempFile(t)
	defer unlock()

	got := probeDpkgLock(realProbeEnv(path))

	if got.State == dpkgLockUnknown {
		t.Fatalf("[AC-002] the real probe could not determine the lock state: %s", got.Cause)
	}
	assert.Equal(t, dpkgLockHeld, got.State, "[AC-002] a real fcntl lock on the file must read as held through the real /proc/locks")
	assert.Equal(t, int64(os.Getpid()), got.PID, "[AC-002] the holder is this process")
	assert.Equal(t, int64(os.Getuid()), got.UID, "[AC-002] the holder uid is read from /proc/<pid>/status")
	assert.NotEmpty(t, got.Comm, "[AC-002] the holder name is read from /proc/<pid>/comm")
}

func TestDpkgLock_RD_AC018_ProbeDoesNotDisturbHeldLock(t *testing.T) {
	requireProcLocks(t)
	path, unlock := lockTempFile(t)

	for i := 0; i < 5; i++ {
		got := probeDpkgLock(realProbeEnv(path))
		require.Equal(t, dpkgLockHeld, got.State, "[AC-018] probe %d: the lock must still be held", i)
	}
	unlock()

	got := probeDpkgLock(realProbeEnv(path))
	assert.Equal(t, dpkgLockFree, got.State, "[AC-018] repeated probes must not leave a lock behind: once the holder releases, the file is free")
}

func TestDpkgLock_RD_AC021_RealProbeWorksUnprivileged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: this test proves nothing about the unprivileged agent user")
	}
	requireProcLocks(t)
	path, unlock := lockTempFile(t)
	defer unlock()

	got := probeDpkgLock(realProbeEnv(path))

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-021] the probe must work with no privilege: stat and world-readable /proc only")
}

func TestDpkgLock_RD_C1_RealSelfStatusIsInitialNamespace(t *testing.T) {
	requireInitPidNamespace(t)
	data, err := os.ReadFile("/proc/self/status")
	require.NoError(t, err)
	levels, ok := parseNSpid(string(data))
	if !ok {
		t.Skip("this kernel prints no NSpid line (before Linux 4.1)")
	}
	if levels != 1 {
		t.Skipf("CI itself runs in a nested pid namespace (NSpid has %d levels); the probe would refuse here, as designed", levels)
	}
	assert.Equal(t, 1, levels, "[C-1] a normal process reads one NSpid level")
}

func TestStatFileID_RD_AC012_MissingPathIsNotExist(t *testing.T) {
	_, err := statFileID(filepath.Join(t.TempDir(), "no-such-lock"))

	require.Error(t, err, "[AC-012] a missing file is an error")
	assert.True(t, errors.Is(err, fs.ErrNotExist), "[AC-012] a missing lock file must be reported as not-exist so the probe reads it as absent, got %v", err)
}

func TestStatFileID_RD_AC002_ReportsDeviceAndInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	id, err := statFileID(path)

	require.NoError(t, err)
	assert.Equal(t, uint64(info.Sys().(*syscall.Stat_t).Ino), id.Ino, "[AC-002] the inode comes from stat")
}

func TestDpkgLock_RD_C1_RealSelfNsPidIsInit(t *testing.T) {
	requireInitPidNamespace(t)

	link, err := os.Readlink("/proc/self/ns/pid")

	require.NoError(t, err, "[C-1] a process can always read its own pid namespace link")
	assert.Equal(t, initPidNamespaceLink, link, "[C-1] the initial pid namespace is the fixed nsfs inode PROC_PID_INIT_INO")
}
