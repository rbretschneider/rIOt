//go:build linux

package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func trustedDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-equivalent fixture ownership requires disposable root Linux environment")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestLinuxExclusion(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-005] kernel flock excludes independent worker apply and boot opens", func(t *testing.T) {
		dir := trustedDir(t)
		path := filepath.Join(dir, "lock")
		first, err := AcquireLockAt(path)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		for i := 0; i < 3; i++ {
			if second, err := AcquireLockAt(path); !errors.Is(err, ErrBusy) {
				if second != nil {
					second.Close()
				}
				t.Fatalf("parallel mutator acquired lock: %v", err)
			}
		}
		before, _ := os.Stat(path)
		first.Close()
		next, err := AcquireLockAt(path)
		if err != nil {
			t.Fatal(err)
		}
		defer next.Close()
		after, _ := os.Stat(path)
		if !os.SameFile(before, after) {
			t.Fatal("lock inode replaced")
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] precreated unsafe lock is refused", func(t *testing.T) {
		dir := trustedDir(t)
		path := filepath.Join(dir, "lock")
		if err := os.WriteFile(path, []byte{}, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 1, 1); err != nil {
			t.Fatal(err)
		}
		if lock, err := AcquireLockAt(path); err == nil {
			lock.Close()
			t.Fatal("nonroot lock accepted")
		}
	})
}

func TestSafeRootIO(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "directory", "oversize", "writable", "hardlink", "wrong-owner", "unsafe-parent"} {
		t.Run("[MAINT-RECOVERY AC-010] rejects "+kind, func(t *testing.T) {
			dir := trustedDir(t)
			path := filepath.Join(dir, "record.json")
			switch kind {
			case "symlink":
				if err := os.Symlink("/etc/passwd", path); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "oversize":
				_ = os.WriteFile(path, []byte(strings.Repeat("x", 100)), 0600)
			case "writable":
				_ = os.Chmod(path, 0666)
			case "hardlink":
				_ = os.Link(path, filepath.Join(dir, "link"))
			case "wrong-owner":
				_ = os.Chown(path, 1, 1)
			case "unsafe-parent":
				_ = os.Chmod(dir, 0777)
			}
			if _, err := readSafe(path, 16, 0, -1); err == nil {
				t.Fatal("unsafe file accepted")
			}
		})
	}
	t.Run("[MAINT-RECOVERY AC-010] does not follow swapped record symlinks", func(t *testing.T) {
		dir := trustedDir(t)
		target := filepath.Join(dir, "record.json")
		outside := filepath.Join(dir, "secret")
		_ = os.WriteFile(outside, []byte("SECRET"), 0600)
		for i := 0; i < 25; i++ {
			_ = os.WriteFile(target, []byte("safe"), 0600)
			data, err := readSafe(target, 16, 0, -1)
			if err != nil || string(data) != "safe" {
				t.Fatal(string(data), err)
			}
			_ = os.Rename(target, filepath.Join(dir, "old"))
			_ = os.Symlink(outside, target)
			if data, err = readSafe(target, 16, 0, -1); err == nil || strings.Contains(string(data), "SECRET") {
				t.Fatal("followed replacement link")
			}
			_ = os.Rename(filepath.Join(dir, "old"), target)
		}
	})
}
func runningRecord() Record {
	now := time.Now().UTC()
	return Record{Version: 1, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Outcome: "running", Phase: "accepted"}
}
func TestRootHistory(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-006] terminal lookup rejects a different UUID or unfinished record body", func(t *testing.T) {
		dir := trustedDir(t)
		id := testRequest().CommandID
		path := filepath.Join(dir, id+".json")
		rec := runningRecord()
		finished := rec.UpdatedAt
		rec.FinishedAt = &finished
		rec.Outcome = "success"
		rec.CommandID = generatedID(90)
		data, _ := json.Marshal(rec)
		if err := os.WriteFile(path, data, 0640); err != nil {
			t.Fatal(err)
		}
		if _, err := readTerminalAt(dir, id); err == nil {
			t.Fatal("mismatched body attributed to requested UUID")
		}
		rec = runningRecord()
		data, _ = json.Marshal(rec)
		_ = os.WriteFile(path, data, 0640)
		if _, err := readTerminalAt(dir, id); err == nil {
			t.Fatal("running body accepted as terminal archive")
		}
		finished = rec.UpdatedAt
		rec.FinishedAt = &finished
		rec.Outcome = "success"
		data, _ = json.Marshal(rec)
		_ = os.WriteFile(path, data, 0640)
		if got, err := readTerminalAt(dir, id); err != nil || got.CommandID != id {
			t.Fatal(got, err)
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] failed terminal archive cannot publish a clean current result", func(t *testing.T) {
		dir := trustedDir(t)
		store, err := openStore(dir, os.Getegid())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		rec := runningRecord()
		if err = store.Begin(rec); err != nil {
			t.Fatal(err)
		}
		if err = os.Mkdir(filepath.Join(dir, rec.CommandID+".json"), 0700); err != nil {
			t.Fatal(err)
		}
		finished := rec.StartedAt
		rec.FinishedAt = &finished
		rec.Outcome = "success"
		if err = store.Finish(rec); err == nil {
			t.Fatal("unsafe archive accepted")
		}
		current, err := store.Current()
		if err != nil || current.Terminal() {
			t.Fatalf("unsafe clean publication %+v %v", current, err)
		}
	})
	t.Run("[MAINT-RECOVERY AC-006] duplicate UUID never begins again after completed current or archive", func(t *testing.T) {
		dir := trustedDir(t)
		store, err := openStore(dir, os.Getegid())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		rec := runningRecord()
		if err = store.Begin(rec); err != nil {
			t.Fatal(err)
		}
		if err = store.Begin(rec); err == nil {
			t.Fatal("active duplicate accepted")
		}
		finish := rec.StartedAt
		rec.FinishedAt = &finish
		rec.Outcome = "success"
		if err = store.Finish(rec); err != nil {
			t.Fatal(err)
		}
		if err = store.Begin(runningRecord()); err == nil {
			t.Fatal("completed duplicate accepted")
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] record symlink cannot be silently overwritten", func(t *testing.T) {
		dir := trustedDir(t)
		store, err := openStore(dir, os.Getegid())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		target := filepath.Join(dir, "external")
		_ = os.WriteFile(target, []byte("untouched"), 0600)
		_ = os.Symlink(target, filepath.Join(dir, "current.json"))
		if err = store.Save(runningRecord()); err == nil {
			t.Fatal("symlink silently replaced")
		}
		data, _ := os.ReadFile(target)
		if string(data) != "untouched" {
			t.Fatal("root write escaped")
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] history capacity fails closed without deleting records", func(t *testing.T) {
		dir := trustedDir(t)
		store, err := openStore(dir, os.Getegid())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		for i := 0; i < MaxTerminalRecords; i++ {
			rec := runningRecord()
			rec.CommandID = generatedID(i)
			if err = store.Begin(rec); err != nil {
				t.Fatal(i, err)
			}
			finish := rec.StartedAt
			rec.FinishedAt = &finish
			rec.Outcome = "success"
			if err = store.Finish(rec); err != nil {
				t.Fatal(err)
			}
		}
		rec := runningRecord()
		rec.CommandID = generatedID(99)
		if err = store.Begin(rec); !errors.Is(err, ErrHistoryFull) {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != MaxTerminalRecords+1 {
			t.Fatal("history removed", len(entries))
		}
	})
}
func generatedID(i int) string {
	const digits = "0123456789abcdef"
	return "3db2d182-61f4-4a4a-b515-243000a43d" + string([]byte{digits[(i>>4)&15], digits[i&15]})
}

func TestLinuxProcessLifetime(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-008] hold command sink accepts only fixed queries and bounded validated names", func(t *testing.T) {
		valid := []invocation{{"/usr/bin/apt-mark", []string{"showhold"}}, {"/usr/bin/apt-mark", []string{"hold", "linux-image-generic"}}, {"/usr/bin/apt-mark", []string{"unhold", "nvidia-driver-550"}}, {"/usr/bin/dpkg-query", []string{"-W", "-f=${Package}\t${Version}\t${db:Status-Status}\n"}}}
		for _, call := range valid {
			if !HoldCommandAllowed(call.path, call.args) {
				t.Fatal("fixed hold invocation rejected", call)
			}
		}
		invalid := []invocation{{"/bin/sh", []string{"-c", "id"}}, {"/usr/bin/apt-mark", []string{"hold", "--file=/etc/shadow"}}, {"/usr/bin/apt-mark", []string{"unhold", "a;id"}}, {"/usr/bin/apt-mark", []string{"hold"}}, {"/usr/bin/dpkg-query", []string{"-W", "attacker-package"}}}
		for _, call := range invalid {
			if HoldCommandAllowed(call.path, call.args) {
				t.Fatal("unsafe hold invocation accepted", call)
			}
			if _, err := RunHoldCommand(context.Background(), call.path, call.args...); err == nil {
				t.Fatal("unsafe hold invocation executed")
			}
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] prerequisite version parsing fails closed", func(t *testing.T) {
		for _, tc := range []struct {
			out, prefix string
			min         int
			want        bool
		}{{"apt 2.6.1 (amd64)", "apt", 2, true}, {"apt 1.8", "apt", 2, false}, {"systemd 241 (241)", "systemd", 241, true}, {"systemd 240", "systemd", 241, false}, {"garbage 999", "systemd", 241, false}} {
			if supportedVersion(tc.out, tc.prefix, tc.min) != tc.want {
				t.Fatal(tc)
			}
		}
	})
	t.Run("[MAINT-RECOVERY AC-002] fixed execution matrix rejects arbitrary package options and operands", func(t *testing.T) {
		for _, call := range []invocation{{"/bin/sh", []string{"-c", "id"}}, {"/usr/bin/apt-get", []string{"install", "evil"}}, {"/usr/bin/apt-get", append(aptOptions(), "--allow-unauthenticated", "upgrade")}, {"/usr/bin/dpkg", []string{"--force-all", "--configure", "-a"}}} {
			if CommandAllowed(call.path, call.args) {
				t.Fatal("unsafe invocation", call)
			}
		}
		for _, tail := range [][]string{{"--fix-broken", "install"}, {"upgrade"}, {"dist-upgrade"}} {
			if !CommandAllowed("/usr/bin/apt-get", append(aptOptions(), tail...)) {
				t.Fatal("fixed invocation rejected")
			}
		}
	})
	t.Run("[MAINT-RECOVERY AC-007] TERM-ignoring fixture is force terminated with unknown outcome", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		start := time.Now()
		result, err := runProcess(ctx, "/bin/sh", []string{"-c", "trap '' TERM; while :; do sleep 1; done"}, 60*time.Millisecond, io.Discard, io.Discard)
		if err == nil || !result.Interrupted || time.Since(start) > 3*time.Second {
			t.Fatalf("%+v %v elapsed=%v", result, err, time.Since(start))
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] output exceeding cap is drained without pipe blockage", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := runProcess(ctx, "/bin/sh", []string{"-c", "head -c 1048576 /dev/zero; printf END"}, 50*time.Millisecond, io.Discard, io.Discard)
		if err != nil || !result.Truncated || len(result.Output) > MaxOutputBytes || !strings.HasSuffix(result.Output, "END") {
			t.Fatalf("len=%d trunc=%v err=%v", len(result.Output), result.Truncated, err)
		}
	})
	t.Run("[MAINT-RECOVERY AC-002] environment is closed rather than caller inherited", func(t *testing.T) {
		t.Setenv("APT_CONFIG", "/attacker/config")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result, err := runProcess(ctx, "/usr/bin/env", nil, 50*time.Millisecond, io.Discard, io.Discard)
		if err != nil || strings.Contains(result.Output, "APT_CONFIG") || !strings.Contains(result.Output, "DEBIAN_FRONTEND=noninteractive") {
			t.Fatal(result.Output, err)
		}
	})
}
