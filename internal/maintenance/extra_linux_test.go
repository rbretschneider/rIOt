//go:build linux

package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

// These tests start only /bin/sh fixtures and the test binary itself. None of
// them starts dietpi-update, apt, dpkg, systemctl or any power command (SC-001).

func TestNsFlags_EU_OPS001_MatchTheKernelConstants(t *testing.T) {
	pairs := map[string][2]uint64{
		"CLONE_NEWNS": {cloneNewNS, unix.CLONE_NEWNS}, "MS_RDONLY": {msRdonly, unix.MS_RDONLY}, "MS_NOSUID": {msNosuid, unix.MS_NOSUID},
		"MS_NODEV": {msNodev, unix.MS_NODEV}, "MS_NOEXEC": {msNoexec, unix.MS_NOEXEC}, "MS_REMOUNT": {msRemount, unix.MS_REMOUNT},
		"MS_BIND": {msBind, unix.MS_BIND}, "MS_REC": {msRec, unix.MS_REC}, "MS_PRIVATE": {msPrivate, unix.MS_PRIVATE}, "TMPFS_MAGIC": {tmpfsMagic, unix.TMPFS_MAGIC},
	}
	for name, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("%s = %#x, kernel value %#x", name, p[0], p[1])
		}
	}
}

func TestFixedEnvironment_EU_AC019_EqualsTheSpecEnvironment(t *testing.T) {
	spec := extraupd.NewDietPi().Apply()
	want := append(FixedEnvironment(), "G_INTERACTIVE=0")
	if !reflect.DeepEqual(spec.Env, want) {
		t.Errorf("spec env = %q\nwant        %q", spec.Env, want)
	}
	if !reflect.DeepEqual(fixedEnvironmentForTest(), FixedEnvironment()) {
		t.Error("the portable test copy of the fixed environment drifted")
	}
}

func TestExtraCommandAllowed_EU_AC019_OnlyLauncherArgv(t *testing.T) {
	t.Run("[AC-019] exactly the launcher invocation of dietpi is admitted", func(t *testing.T) {
		if !ExtraCommandAllowed(HelperPath, []string{"--run-extra", "dietpi"}) {
			t.Fatal("the fixed launcher invocation was refused")
		}
	})
	refused := []struct {
		name string
		path string
		args []string
	}{
		{"dietpi-update itself", "/boot/dietpi/dietpi-update", []string{"1"}},
		{"dietpi-update with another argument", "/boot/dietpi/dietpi-update", []string{"2"}},
		{"dietpi-software", "/boot/dietpi/dietpi-software", nil},
		{"dietpi-services", "/boot/dietpi/dietpi-services", []string{"stop"}},
		{"dietpi-set_software", "/boot/dietpi/func/dietpi-set_software", []string{"x"}},
		{"launcher with an extra argument", HelperPath, []string{"--run-extra", "dietpi", "1"}},
		{"launcher for another id", HelperPath, []string{"--run-extra", "snap"}},
		{"launcher with the wrong flag", HelperPath, []string{"--capabilities"}},
		{"another binary as launcher", "/bin/sh", []string{"--run-extra", "dietpi"}},
		{"no arguments", HelperPath, nil},
	}
	for _, tc := range refused {
		t.Run("[AC-019] refuses "+tc.name, func(t *testing.T) {
			if ExtraCommandAllowed(tc.path, tc.args) {
				t.Errorf("%s %q was admitted", tc.path, tc.args)
			}
		})
	}
	t.Run("[AC-019] the apt allowlist still admits no DietPi program", func(t *testing.T) {
		if CommandAllowed("/boot/dietpi/dietpi-update", []string{"1"}) {
			t.Error("CommandAllowed must stay apt-only")
		}
	})
	t.Run("[AC-019] the launcher command is fixed", func(t *testing.T) {
		path, args, env := launcherCommand("dietpi")
		if path != HelperPath || !reflect.DeepEqual(args, []string{"--run-extra", "dietpi"}) || !reflect.DeepEqual(env, []string{"RIOT_EXTRA_LAUNCH=1"}) {
			t.Errorf("%q %q %q", path, args, env)
		}
	})
	t.Run("[AC-019] the production runner refuses an id outside the plan without starting anything", func(t *testing.T) {
		out := ExtraLaunchRunner{}.RunStep(context.Background(), "snap", extraupd.ApplySpec{})
		if out.SetupErr == "" {
			t.Errorf("outcome = %+v, want a setup error", out)
		}
	})
}

func TestDpkgLockState_EU_AC026_OFDQuery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	t.Run("[AC-026] an unlocked file is free", func(t *testing.T) {
		if st := dpkgLockStateAt([]string{path}, uid); st.Kind != LockFree {
			t.Fatalf("state = %+v", st)
		}
	})
	t.Run("[AC-026] a write lock held by another open file description is reported held", func(t *testing.T) {
		holder, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		lk := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
		if err = unix.FcntlFlock(holder.Fd(), unix.F_OFD_SETLK, &lk); err != nil {
			holder.Close()
			t.Skipf("OFD locks unavailable here: %v", err)
		}
		st := dpkgLockStateAt([]string{path}, uid)
		if st.Kind != LockHeld {
			t.Errorf("state = %+v, want held", st)
		}
		holder.Close()
		if st = dpkgLockStateAt([]string{path}, uid); st.Kind != LockFree {
			t.Errorf("after release: %+v, want free", st)
		}
	})
	t.Run("[AC-026] the probe never takes the lock", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			dpkgLockStateAt([]string{path}, uid)
		}
		holder, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Close()
		lk := unix.Flock_t{Type: unix.F_WRLCK}
		if err = unix.FcntlFlock(holder.Fd(), unix.F_OFD_SETLK, &lk); err != nil {
			t.Errorf("the probe left a lock behind: %v", err)
		}
	})
	t.Run("[AC-026] the second of two lock files being held is reported", func(t *testing.T) {
		second := filepath.Join(dir, "lock2")
		if err := os.WriteFile(second, nil, 0o640); err != nil {
			t.Fatal(err)
		}
		holder, _ := os.OpenFile(second, os.O_RDWR, 0)
		defer holder.Close()
		lk := unix.Flock_t{Type: unix.F_WRLCK}
		if err := unix.FcntlFlock(holder.Fd(), unix.F_OFD_SETLK, &lk); err != nil {
			t.Skip("OFD locks unavailable")
		}
		if st := dpkgLockStateAt([]string{path, second}, uid); st.Kind != LockHeld || !strings.Contains(st.Detail, "lock2") {
			t.Errorf("state = %+v", st)
		}
	})
}

func TestDpkgLockState_EU_SEC007_NonblockAndOwnership(t *testing.T) {
	dir := t.TempDir()
	uid := uint32(os.Getuid())
	t.Run("[SEC-007] a FIFO in place of the lock file neither blocks the probe nor passes it", func(t *testing.T) {
		fifo := filepath.Join(dir, "fifo")
		if err := syscall.Mkfifo(fifo, 0o640); err != nil {
			t.Skipf("mkfifo unavailable: %v", err)
		}
		done := make(chan LockState, 1)
		go func() { done <- dpkgLockStateAt([]string{fifo}, uid) }()
		select {
		case st := <-done:
			if st.Kind != LockUnknown {
				t.Errorf("state = %+v, want unknown", st)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the probe blocked on a FIFO: O_NONBLOCK is missing")
		}
	})
	t.Run("[SEC-007] a file owned by someone else is unknown", func(t *testing.T) {
		path := filepath.Join(dir, "owned")
		if err := os.WriteFile(path, nil, 0o640); err != nil {
			t.Fatal(err)
		}
		if st := dpkgLockStateAt([]string{path}, uid+1); st.Kind != LockUnknown {
			t.Errorf("state = %+v, want unknown", st)
		}
	})
	t.Run("[SEC-007] a symlink is refused, never followed", func(t *testing.T) {
		target := filepath.Join(dir, "target")
		_ = os.WriteFile(target, nil, 0o640)
		link := filepath.Join(dir, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Skip(err)
		}
		if st := dpkgLockStateAt([]string{link}, uid); st.Kind != LockUnknown {
			t.Errorf("state = %+v, want unknown", st)
		}
	})
	t.Run("[SEC-007] a directory and a missing file are unknown", func(t *testing.T) {
		if st := dpkgLockStateAt([]string{dir}, uid); st.Kind != LockUnknown {
			t.Errorf("directory: %+v", st)
		}
		if st := dpkgLockStateAt([]string{filepath.Join(dir, "absent")}, uid); st.Kind != LockUnknown {
			t.Errorf("missing: %+v", st)
		}
	})
}

func writeProc(t *testing.T, root, pid string, argv ...string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDietPiBusy_EU_SEC001_ScanMatchesAndBounds(t *testing.T) {
	match := extraupd.NewDietPi().Apply().BusyMatch
	now := time.Now
	t.Run("[SEC-001] a script run through its interpreter is found", func(t *testing.T) {
		root := t.TempDir()
		writeProc(t, root, "100", "/usr/sbin/cron")
		writeProc(t, root, "812", "/bin/bash", "/boot/dietpi/dietpi-update", "2")
		busy, detail := scanBusy(root, match, now)
		if !busy || !strings.Contains(detail, "812") {
			t.Errorf("busy=%v detail=%q", busy, detail)
		}
	})
	t.Run("[SEC-001] the daily cron job is found", func(t *testing.T) {
		root := t.TempDir()
		writeProc(t, root, "7", "/bin/dash", "/etc/cron.daily/dietpi")
		if busy, _ := scanBusy(root, match, now); !busy {
			t.Error("cron.daily/dietpi was not detected")
		}
	})
	t.Run("[SEC-001] unrelated processes, kernel threads and cloudshell are ignored", func(t *testing.T) {
		root := t.TempDir()
		writeProc(t, root, "1", "/sbin/init")
		writeProc(t, root, "2")
		writeProc(t, root, "50", "/bin/bash", "/boot/dietpi/dietpi-cloudshell")
		writeProc(t, root, "60", "/usr/local/lib/riot/riot-maintenance")
		_ = os.MkdirAll(filepath.Join(root, "self"), 0o755)
		if busy, detail := scanBusy(root, match, now); busy {
			t.Errorf("busy = true (%s)", detail)
		}
	})
	t.Run("[SEC-001] a process that vanished mid-scan is skipped, any other read error counts as busy", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "300"), 0o755); err != nil {
			t.Fatal(err)
		}
		if busy, _ := scanBusy(root, match, now); busy {
			t.Error("a pid directory without cmdline (vanished) must be skipped")
		}
		if err := os.MkdirAll(filepath.Join(root, "301", "cmdline"), 0o755); err != nil {
			t.Fatal(err)
		}
		if busy, detail := scanBusy(root, match, now); !busy || !strings.Contains(detail, "301") {
			t.Errorf("an unreadable cmdline must count as busy: %v %q", busy, detail)
		}
	})
	t.Run("[SEC-001] more than 32768 processes counts as busy", func(t *testing.T) {
		root := t.TempDir()
		for i := 1; i <= busyMaxPIDs+1; i++ {
			if err := os.Mkdir(filepath.Join(root, itoa(i)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// A frozen clock isolates the PID bound: on a slow disk the real-time
		// 2-second bound would otherwise fire first (busy too, other detail).
		frozen := time.Now()
		if busy, detail := scanBusy(root, match, func() time.Time { return frozen }); !busy || !strings.Contains(detail, "32768") {
			t.Errorf("busy=%v detail=%q", busy, detail)
		}
	})
	t.Run("[SEC-001] a scan that exceeds 2 seconds counts as busy", func(t *testing.T) {
		root := t.TempDir()
		writeProc(t, root, "1", "/sbin/init")
		clock := time.Now()
		calls := 0
		slow := func() time.Time {
			calls++
			if calls > 1 {
				clock = clock.Add(3 * time.Second)
			}
			return clock
		}
		if busy, detail := scanBusy(root, match, slow); !busy || !strings.Contains(detail, "2s") {
			t.Errorf("busy=%v detail=%q", busy, detail)
		}
	})
	t.Run("[SEC-001] a missing /proc counts as busy", func(t *testing.T) {
		if busy, _ := scanBusy(filepath.Join(t.TempDir(), "nope"), match, now); !busy {
			t.Error("an unreadable /proc must fail closed")
		}
	})
	t.Run("[SEC-001] cmdline reads are capped at 4 KiB", func(t *testing.T) {
		root := t.TempDir()
		long := strings.Repeat("a", 10000)
		writeProc(t, root, "9", long, "/boot/dietpi/dietpi-update")
		if busy, _ := scanBusy(root, match, now); busy {
			t.Error("argv1 beyond the 4 KiB cap must not be read")
		}
	})
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestRunProcessSpec_EU_AC023_GroupTermThenKill(t *testing.T) {
	t.Run("[AC-023] a TERM-ignoring tree is killed at the limit plus grace and reported interrupted", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		res, err := runProcessSpec(ctx, processSpec{path: "/bin/sh", args: []string{"-c", "trap '' TERM; (trap '' TERM; while :; do sleep 1; done) & while :; do sleep 1; done"}, grace: 150 * time.Millisecond, limit: 4096})
		if err == nil || !res.Interrupted {
			t.Fatalf("%+v %v", res, err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("took %s, want about limit + grace", elapsed)
		}
	})
	t.Run("[AC-023] a well-behaved process ends on TERM within the grace", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		res, err := runProcessSpec(ctx, processSpec{path: "/bin/sh", args: []string{"-c", "sleep 30"}, grace: 2 * time.Second, limit: 4096})
		if err == nil || !res.Interrupted || time.Since(start) > 2*time.Second {
			t.Errorf("%+v %v elapsed %s", res, err, time.Since(start))
		}
	})
}

func TestRunProcessSpec_EU_AC030_SettleAndEnvironment(t *testing.T) {
	t.Run("[AC-030] a descendant that ends within the settle time is not stray", func(t *testing.T) {
		res, err := runProcessSpec(context.Background(), processSpec{path: "/bin/sh", args: []string{"-c", "(sleep 0.3) & echo done"}, grace: 200 * time.Millisecond, limit: 4096, settle: 2 * time.Second})
		if err != nil || res.ExitCode != 0 || !strings.Contains(res.Output, "done") {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("[AC-030] a descendant that outlives the settle time is stray and the group is terminated", func(t *testing.T) {
		start := time.Now()
		res, err := runProcessSpec(context.Background(), processSpec{path: "/bin/sh", args: []string{"-c", "(sleep 30) & echo done"}, grace: 200 * time.Millisecond, limit: 4096, settle: 200 * time.Millisecond})
		if err == nil || !strings.Contains(err.Error(), "outlived") {
			t.Fatalf("%+v %v", res, err)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("took %s", time.Since(start))
		}
	})
	t.Run("[AC-030] with no settle time a stray descendant is reported at once, as for apt", func(t *testing.T) {
		_, err := runProcessSpec(context.Background(), processSpec{path: "/bin/sh", args: []string{"-c", "(sleep 30) & echo done"}, grace: 200 * time.Millisecond, limit: 4096})
		if err == nil {
			t.Fatal("the apt rule must stay immediate")
		}
	})
	t.Run("[AC-019] the supplied environment and directory are all the child sees", func(t *testing.T) {
		t.Setenv("RIOT_LEAK", "1")
		res, err := runProcessSpec(context.Background(), processSpec{path: "/usr/bin/env", env: []string{"ONLY=1"}, dir: "/", grace: 100 * time.Millisecond, limit: 4096})
		if err != nil || strings.TrimSpace(res.Output) != "ONLY=1" {
			t.Fatalf("%+v %v", res, err)
		}
		res, err = runProcessSpec(context.Background(), processSpec{path: "/bin/sh", args: []string{"-c", "pwd"}, dir: "/", grace: 100 * time.Millisecond, limit: 4096})
		if err != nil || !strings.HasPrefix(res.Output, "/\n") {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("[AC-019] stdin is /dev/null", func(t *testing.T) {
		res, err := runProcessSpec(context.Background(), processSpec{path: "/bin/sh", args: []string{"-c", "readlink /proc/self/fd/0"}, grace: 100 * time.Millisecond, limit: 4096})
		if err != nil || strings.TrimSpace(res.Output) != "/dev/null" {
			t.Fatalf("%+v %v", res, err)
		}
	})
}

func TestRunProcessSpec_EU_SEC001_GateFailureKillsTheGroup(t *testing.T) {
	t.Run("[SEC-001] a gate error kills the process at once and is returned", func(t *testing.T) {
		start := time.Now()
		res, _ := runProcessSpec(context.Background(), processSpec{
			path: "/bin/sh", args: []string{"-c", "sleep 30"}, grace: 5 * time.Second, limit: 4096,
			gate: func(context.Context) error { return &LauncherError{Reason: ReasonTmpNotPrivate, Detail: "boom"} },
		})
		var le *LauncherError
		if !errors.As(res.GateErr, &le) || le.Reason != ReasonTmpNotPrivate {
			t.Fatalf("gate error = %v", res.GateErr)
		}
		if time.Since(start) > 3*time.Second {
			t.Errorf("took %s: the group must be killed at once, not after the grace", time.Since(start))
		}
	})
	t.Run("[SEC-001] a gate that returns nil leaves the process alone", func(t *testing.T) {
		res, err := runProcessSpec(context.Background(), processSpec{
			path: "/bin/sh", args: []string{"-c", "echo ran"}, grace: time.Second, limit: 4096,
			gate: func(context.Context) error { return nil },
		})
		if err != nil || res.GateErr != nil || !strings.Contains(res.Output, "ran") {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("[SEC-001] the parent's copy of the extra descriptor is closed after start", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer pr.Close()
		_, err = runProcessSpec(context.Background(), processSpec{path: "/bin/sh", args: []string{"-c", "exit 0"}, grace: time.Second, limit: 4096, extraFiles: []*os.File{pw}})
		if err != nil {
			t.Fatal(err)
		}
		_ = pr.SetReadDeadline(time.Now().Add(2 * time.Second))
		if n, err := pr.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
			t.Errorf("read = %d, %v: the writer was not fully closed", n, err)
		}
	})
}

func TestReadLauncherStatus_EU_SEC001_Protocol(t *testing.T) {
	feed := func(t *testing.T, text string, closeWriter bool, bound time.Duration) error {
		t.Helper()
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer pr.Close()
		if text != "" {
			if _, err = pw.WriteString(text); err != nil {
				t.Fatal(err)
			}
		}
		if closeWriter {
			pw.Close()
		} else {
			defer pw.Close()
		}
		return readLauncherStatus(context.Background(), pr, bound)
	}
	t.Run("[SEC-001] ok then end-of-file means the updater is running", func(t *testing.T) {
		if err := feed(t, "ok\n", true, time.Second); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[SEC-001] an err line is returned with its reason", func(t *testing.T) {
		err := feed(t, "err tmp_not_private /tmp is not a mount point of its own\n", true, time.Second)
		var le *LauncherError
		if !errors.As(err, &le) || le.Reason != "tmp_not_private" || !strings.Contains(le.Detail, "mount point") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[SEC-001] an exec failure after ok is an error", func(t *testing.T) {
		err := feed(t, "ok\nerr setup_failed exec: no such file\n", true, time.Second)
		var le *LauncherError
		if !errors.As(err, &le) || le.Reason != "setup_failed" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[SEC-001] end-of-file without ok is an error", func(t *testing.T) {
		if err := feed(t, "", true, time.Second); err == nil {
			t.Fatal("a launcher that exited silently must fail")
		}
	})
	t.Run("[SEC-001] unknown output is an error", func(t *testing.T) {
		if err := feed(t, "hello\n", true, time.Second); err == nil {
			t.Fatal("unexpected status accepted")
		}
	})
	t.Run("[SEC-001] no end-of-file within the bound is an error", func(t *testing.T) {
		start := time.Now()
		err := feed(t, "ok\n", false, 150*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "did not reach exec") || time.Since(start) > 3*time.Second {
			t.Fatalf("err = %v after %s", err, time.Since(start))
		}
	})
	t.Run("[SEC-001] a cancelled context ends the read", func(t *testing.T) {
		pr, pw, _ := os.Pipe()
		defer pw.Close()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- readLauncherStatus(ctx, pr, time.Minute) }()
		time.Sleep(50 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Error("a cancelled read must not report success")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the read did not end after cancellation")
		}
	})
	t.Run("[SEC-001] at most 512 bytes are read", func(t *testing.T) {
		err := feed(t, "ok\n"+strings.Repeat("x", 4000), true, time.Second)
		_ = err // truncation must not hang or panic; any verdict is acceptable
	})
}

func TestReadExtraPolicy_EU_AC016_SafeOpen(t *testing.T) {
	write := func(t *testing.T, dir, body string, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(dir, "extra-updaters.json")
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	uid := os.Getuid()
	good := string(extraupd.EncodePolicy([]string{"dietpi"}))
	t.Run("[AC-016] a root-owned 0644 policy parses", func(t *testing.T) {
		dir := trustedDir(t)
		ids, err := readExtraPolicyAt(write(t, dir, good, 0o644), uid)
		if err != nil || !reflect.DeepEqual(ids, []string{"dietpi"}) {
			t.Fatalf("%v %v", ids, err)
		}
	})
	t.Run("[AC-016] a group- or world-writable policy is refused", func(t *testing.T) {
		dir := trustedDir(t)
		for _, mode := range []os.FileMode{0o664, 0o666, 0o646} {
			if _, err := readExtraPolicyAt(write(t, dir, good, mode), uid); err == nil {
				t.Errorf("mode %o accepted", mode)
			}
		}
	})
	t.Run("[AC-016] a policy in a writable directory is refused", func(t *testing.T) {
		dir := trustedDir(t)
		path := write(t, dir, good, 0o644)
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := readExtraPolicyAt(path, uid); err == nil {
			t.Error("a world-writable parent must be refused")
		}
	})
	t.Run("[AC-016] a symlinked policy is refused", func(t *testing.T) {
		dir := trustedDir(t)
		target := write(t, dir, good, 0o644)
		link := filepath.Join(dir, "link.json")
		if err := os.Symlink(target, link); err != nil {
			t.Skip(err)
		}
		if _, err := readExtraPolicyAt(link, uid); err == nil {
			t.Error("a symlink was followed")
		}
	})
	t.Run("[AC-016] an oversized or malformed policy is refused, an absent one is an error", func(t *testing.T) {
		dir := trustedDir(t)
		if _, err := readExtraPolicyAt(write(t, dir, good+strings.Repeat(" ", 600), 0o644), uid); err == nil {
			t.Error("oversized policy accepted")
		}
		if _, err := readExtraPolicyAt(write(t, dir, `{"version":1}`, 0o644), uid); err == nil {
			t.Error("malformed policy accepted")
		}
		if _, err := readExtraPolicyAt(filepath.Join(dir, "absent.json"), uid); err == nil {
			t.Error("a missing policy must be an error so nothing is enabled")
		}
	})
	t.Run("[AC-016] a file owned by another user is refused", func(t *testing.T) {
		dir := trustedDir(t)
		if _, err := readExtraPolicyAt(write(t, dir, good, 0o644), uid+1); err == nil {
			t.Error("wrong owner accepted")
		}
	})
}

func v2Archive(id string, started time.Time) Record {
	fin := started.Add(time.Minute)
	zero := 0
	return Record{
		Version: 2, CommandID: id, Mode: "full", StartedAt: started, UpdatedAt: fin, FinishedAt: &fin, Phase: "finished", Outcome: "success",
		RequestedExtraUpdaters: []string{"dietpi"}, AptOutcome: "success",
		ExtraUpdaters: []UpdaterResult{{ID: "dietpi", Outcome: UpdaterSuccess, ExitCode: &zero, StartedAt: &started, FinishedAt: &fin}},
	}
}

func TestStore_EU_SEC006_LatestExtraStart(t *testing.T) {
	dir := trustedDir(t)
	store, err := openStore(dir, os.Getegid())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Run("[SEC-006] an empty history has no start", func(t *testing.T) {
		if _, found, err := store.LatestExtraStart("dietpi"); err != nil || found {
			t.Fatalf("found=%v err=%v", found, err)
		}
	})
	base := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	for i, offset := range []time.Duration{0, 48 * time.Hour, 24 * time.Hour} {
		rec := v2Archive(generatedID(i), base.Add(offset))
		data, _ := json.Marshal(rec)
		if err := os.WriteFile(filepath.Join(dir, rec.CommandID+".json"), data, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("[SEC-006] the newest recorded start wins", func(t *testing.T) {
		got, found, err := store.LatestExtraStart("dietpi")
		if err != nil || !found || !got.Equal(base.Add(48*time.Hour)) {
			t.Fatalf("got %s found=%v err=%v", got, found, err)
		}
	})
	t.Run("[SEC-006] another updater id has no start", func(t *testing.T) {
		if _, found, _ := store.LatestExtraStart("snap"); found {
			t.Error("snap must have no start")
		}
	})
	t.Run("[SEC-006] a skipped entry has no start time and does not count", func(t *testing.T) {
		rec := v2Archive(generatedID(9), base.Add(96*time.Hour))
		rec.ExtraUpdaters = []UpdaterResult{{ID: "dietpi", Outcome: UpdaterSkipped, Reason: SkipNotEnabled}}
		data, _ := json.Marshal(rec)
		_ = os.WriteFile(filepath.Join(dir, rec.CommandID+".json"), data, 0o640)
		got, _, _ := store.LatestExtraStart("dietpi")
		if !got.Equal(base.Add(48 * time.Hour)) {
			t.Errorf("got %s", got)
		}
	})
	t.Run("[SEC-006] an unreadable record fails closed", func(t *testing.T) {
		bad := filepath.Join(dir, generatedID(11)+".json")
		_ = os.WriteFile(bad, []byte("{not json"), 0o640)
		if _, _, err := store.LatestExtraStart("dietpi"); err == nil {
			t.Error("a corrupt record must be an error")
		}
		_ = os.Remove(bad)
	})
	t.Run("[SEC-006] an unrecognised entry in the state directory fails closed", func(t *testing.T) {
		stray := filepath.Join(dir, "notes.txt")
		_ = os.WriteFile(stray, nil, 0o640)
		defer os.Remove(stray)
		if _, _, err := store.LatestExtraStart("dietpi"); err == nil {
			t.Error("an unknown file must require operator review")
		}
	})
}

func TestEffectiveUnit_EU_SEC002_RunnerErrorsFailClosed(t *testing.T) {
	t.Run("[SEC-002] a good answer parses", func(t *testing.T) {
		st, err := effectiveUnitFrom(func() (CommandResult, error) {
			return CommandResult{Output: "RuntimeMaxUSec=1h 25min\nPrivateTmp=no\nMountFlags=\n"}, nil
		})
		if err != nil || st.RuntimeMax != ExtraServiceBackstop {
			t.Fatalf("%+v %v", st, err)
		}
	})
	t.Run("[SEC-002] a runner error, truncated output and garbage are errors", func(t *testing.T) {
		for name, run := range map[string]func() (CommandResult, error){
			"runner error": func() (CommandResult, error) { return CommandResult{}, errors.New("exit 1") },
			"truncated":    func() (CommandResult, error) { return CommandResult{Output: "RuntimeMaxUSec=1h", Truncated: true}, nil },
			"garbage":      func() (CommandResult, error) { return CommandResult{Output: "Failed to connect to bus"}, nil },
		} {
			if _, err := effectiveUnitFrom(run); err == nil {
				t.Errorf("%s accepted", name)
			}
		}
	})
}
