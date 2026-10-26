//go:build linux

package maintenance

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

// The real-namespace test is opt-in. It changes mounts, so it runs only when
// RIOT_NS_FIXTURE=1 is set by the person who started a disposable container with
// CAP_SYS_ADMIN, and only as root. It is never enabled by default, never runs on a
// workstation or a fleet host, and it executes a /bin/sh fixture, never DietPi
// (SC-001). The ADD names it TestRunExtra_EU_OPS001_RealNamespace.

const (
	nsFixtureRole = "RIOT_NS_FIXTURE_ROLE"
	nsFixtureHelp = "RIOT_NS_FIXTURE_HELPER"
	nsFixtureDir  = "/mnt/riot-ns-fixture"
)

// fixtureScript runs inside the launcher's namespace as the "updater". It makes
// a mount under the shared root, which must reach the parent namespace, and it
// writes into /tmp, which must not.
const fixtureScript = `set -e
mkdir -p ` + nsFixtureDir + `
mount -t tmpfs tmpfs ` + nsFixtureDir + `
echo proof > ` + nsFixtureDir + `/proof
echo child > /tmp/child-only
ls -A /tmp > ` + nsFixtureDir + `/tmp-listing
`

// TestLauncherFixtureProcess is the re-executed child of the real-namespace
// test. It does nothing unless the parent set its role.
func TestLauncherFixtureProcess(t *testing.T) {
	if os.Getenv(nsFixtureRole) != "launcher" {
		return
	}
	// Run from a non-leader thread on purpose: the proofs must read the calling
	// thread's mount table (SEC-009).
	runtime.LockOSThread()
	policy := launchPolicy{
		helperPath: os.Getenv(nsFixtureHelp),
		lookup: func(id string) (extraupd.ApplySpec, bool) {
			return extraupd.ApplySpec{
				Path: "/bin/sh", Args: []string{"-c", fixtureScript}, Dir: "/",
				Env: []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}, PrivateTmp: true,
			}, id == "fixture"
		},
	}
	if err := runExtra(linuxOps{}, []string{LauncherArg, "fixture"}, policy); err != nil {
		fmt.Fprintln(os.Stderr, "launcher failed:", err)
		os.Exit(125)
	}
	os.Exit(99) // exec never returned an error but also never replaced the process
}

func TestRunExtra_EU_OPS001_RealNamespace(t *testing.T) {
	if os.Getenv("RIOT_NS_FIXTURE") != "1" {
		t.Skip("opt-in: set RIOT_NS_FIXTURE=1 inside a disposable container with CAP_SYS_ADMIN")
	}
	if os.Geteuid() != 0 {
		t.Skip("the real-namespace fixture needs root")
	}
	helper, err := os.Readlink("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	// Run the child from a copy outside /tmp, which the fixture hides.
	copyDir, err := os.MkdirTemp("/var/tmp", "ns-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(copyDir)
	child := filepath.Join(copyDir, "child.test")
	if err = copyFile(helper, child); err != nil {
		t.Fatal(err)
	}

	// Fixture mounts live only in this disposable container's namespace.
	if err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_SHARED, ""); err != nil {
		t.Skipf("cannot make / shared (need CAP_SYS_ADMIN and an unconfined seccomp profile): %v", err)
	}
	if err = unix.Mount("tmpfs", "/tmp", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=1777"); err != nil {
		t.Skipf("cannot mount a tmpfs on /tmp: %v", err)
	}
	defer unix.Unmount("/tmp", unix.MNT_DETACH)
	if err = os.WriteFile("/tmp/host-marker", []byte("host"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Unmount(nsFixtureDir, unix.MNT_DETACH) }()

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	cmd := exec.Command(child, "-test.run=^TestLauncherFixtureProcess$")
	cmd.Env = []string{nsFixtureRole + "=launcher", nsFixtureHelp + "=" + helper, LauncherEnv + "=1"}
	cmd.ExtraFiles = []*os.File{pw}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pw.Close()
	status, _ := io.ReadAll(bufio.NewReader(io.LimitReader(pr, 1024)))
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err = <-waitErr:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the fixture did not finish")
	}
	if err != nil {
		t.Fatalf("fixture failed: %v (status %q)", err, status)
	}
	if got := strings.TrimSpace(string(status)); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	t.Run("[OPS-001] a mount made by the fixture under the shared root appears in the parent namespace", func(t *testing.T) {
		data, err := os.ReadFile(nsFixtureDir + "/proof")
		if err != nil || strings.TrimSpace(string(data)) != "proof" {
			t.Errorf("the fixture's mount did not propagate to the host namespace: %q %v", data, err)
		}
	})
	t.Run("[SEC-001] a file the fixture wrote to /tmp is not visible in the parent /tmp", func(t *testing.T) {
		if _, err := os.Stat("/tmp/child-only"); err == nil {
			t.Error("the fixture's /tmp is the host's /tmp")
		}
		listing, err := os.ReadFile(nsFixtureDir + "/tmp-listing")
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(listing)); got != "child-only" {
			t.Errorf("the fixture saw /tmp = %q, want only its own file (an empty private tmpfs)", got)
		}
	})
	t.Run("[SEC-001] the parent /tmp is untouched", func(t *testing.T) {
		data, err := os.ReadFile("/tmp/host-marker")
		if err != nil || string(data) != "host" {
			t.Errorf("host marker = %q %v", data, err)
		}
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
