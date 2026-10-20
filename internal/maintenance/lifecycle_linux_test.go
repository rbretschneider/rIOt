//go:build linux

package maintenance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// These helper roles execute only in this test binary. They never call a
// package manager or a production service.
func TestMaintenanceFixtureProcess(t *testing.T) {
	role := os.Getenv("RIOT_MAINT_FIXTURE")
	if role == "" {
		return
	}
	dir := os.Getenv("RIOT_MAINT_FIXTURE_DIR")
	switch role {
	case "lock-contender":
		lock, err := AcquireLockAt(filepath.Join(dir, "lock"))
		if lock != nil {
			lock.Close()
		}
		if !errors.Is(err, ErrBusy) {
			os.Exit(4)
		}
		os.Exit(0)
	case "observer":
		out, err := os.OpenFile(filepath.Join(dir, "worker-output"), os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(5)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestMaintenanceFixtureProcess$")
		cmd.Env = []string{"RIOT_MAINT_FIXTURE=worker", "RIOT_MAINT_FIXTURE_DIR=" + dir}
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Stdin = nil
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err = cmd.Start(); err != nil {
			os.Exit(6)
		}
		if err = os.WriteFile(filepath.Join(dir, "ready"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
			os.Exit(7)
		}
		_ = cmd.Process.Release()
		out.Close()
		time.Sleep(10 * time.Second)
		os.Exit(8)
	case "worker":
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(dir, "continue")); err == nil {
				_ = os.WriteFile(filepath.Join(dir, "complete"), []byte("durable worker result"), 0600)
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(9)
	default:
		os.Exit(10)
	}
}
func awaitFixture(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fixture did not produce %s", filepath.Base(path))
}

func TestIndependentLifetime(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-005] root exclusion is observed by another real process", func(t *testing.T) {
		dir := trustedDir(t)
		lock, err := AcquireLockAt(filepath.Join(dir, "lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		cmd := exec.Command(os.Args[0], "-test.run=^TestMaintenanceFixtureProcess$")
		cmd.Env = []string{"RIOT_MAINT_FIXTURE=lock-contender", "RIOT_MAINT_FIXTURE_DIR=" + dir}
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(data))
		}
	})
	t.Run("[MAINT-RECOVERY AC-006] independent fixture worker completes after observer is killed", func(t *testing.T) {
		dir := t.TempDir()
		observer := exec.Command(os.Args[0], "-test.run=^TestMaintenanceFixtureProcess$")
		observer.Env = []string{"RIOT_MAINT_FIXTURE=observer", "RIOT_MAINT_FIXTURE_DIR=" + dir}
		if err := observer.Start(); err != nil {
			t.Fatal(err)
		}
		defer observer.Process.Kill()
		awaitFixture(t, filepath.Join(dir, "ready"))
		if err := observer.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = observer.Wait()
		if err := os.WriteFile(filepath.Join(dir, "continue"), []byte("release worker after observer death"), 0600); err != nil {
			t.Fatal(err)
		}
		awaitFixture(t, filepath.Join(dir, "complete"))
		data, err := os.ReadFile(filepath.Join(dir, "complete"))
		if err != nil || string(data) != "durable worker result" {
			t.Fatal(string(data), err)
		}
	})
}
