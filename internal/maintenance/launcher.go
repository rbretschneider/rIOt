package maintenance

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

// The step launcher (EXTRA-UPD-001 AD-007). For an updater whose spec asks for
// PrivateTmp, the worker does not start the updater itself. It starts
// `riot-maintenance --run-extra <id>`, and this code then:
//
//   - leaves a private mount namespace in which only /tmp differs from the host:
//     every other mount keeps its propagation, so DietPi's own mounts and
//     unmounts (the 9 to 10 boot-partition migration) reach the host exactly as
//     in an unconfined root run (OPS-001);
//   - hides the host /tmp behind a fresh tmpfs, so DietPi's fixed working
//     directory /tmp/DietPi-Update cannot be reached by a non-root user (SEC-001);
//   - proves both properties, then execs the registry's fixed spec.
//
// When /run/dietpi exists the launcher also mounts a private, empty tmpfs on it
// (DIETPI-PATH-001 AD-2). Stock DietPi makes the host's /run/dietpi 0777 and not
// sticky, and root's writes there follow symlinks, so the step must not see it.
// Propagation of /run changes in the step's namespace only: events under /run
// on the host no longer reach the step, the same class as the private /tmp.
//
// Any failure writes one line to descriptor 3 and ends without exec: DietPi has
// not started and nothing on the host has changed.

const (
	// LauncherArg is the argv[1] of the launcher mode.
	LauncherArg = "--run-extra"
	// LauncherEnv must be "1" in the launcher's environment.
	LauncherEnv = "RIOT_EXTRA_LAUNCH"

	// The mount tables read for the proofs. The launcher's own table is read
	// through thread-self: a mount namespace belongs to one thread, and
	// /proc/self names the thread-group leader, which may be a different thread
	// with a different namespace (SEC-009).
	selfMountinfo = "/proc/thread-self/mountinfo"
	initMountinfo = "/proc/1/mountinfo"
	initTmpPath   = "/proc/1/root/tmp"

	runPath         = "/run"
	runDietPiPath   = "/run/dietpi"
	initRunDietPi   = "/proc/1/root/run/dietpi"
	runDirTmpfsData = "mode=0755,size=1m,nr_inodes=64"
	runDirMode      = 0o755

	maxLauncherDetail = 200
)

// Launcher failure reasons, the second word of an `err` status line.
const (
	ReasonLauncherRefused    = "launcher_refused"
	ReasonTmpNotPrivate      = SkipTmpNotPrivate
	ReasonMountPropagation   = SkipMountPropagationUnver
	ReasonRunDirNotPrivate   = SkipRunDirNotPrivate
	ReasonLauncherSetupError = "setup_failed"
)

// LauncherError is a launcher failure. Exit status 125 and the status line are
// derived from it.
type LauncherError struct {
	Reason string
	Detail string
}

func (e *LauncherError) Error() string { return e.Reason + ": " + e.Detail }

// launchPolicy is what the launcher trusts. Production uses the registry and
// HelperPath; only tests substitute either.
type launchPolicy struct {
	helperPath string
	lookup     func(id string) (extraupd.ApplySpec, bool)
}

func productionLaunchPolicy() launchPolicy {
	return launchPolicy{
		helperPath: HelperPath,
		lookup: func(id string) (extraupd.ApplySpec, bool) {
			u, ok := extraupd.Lookup(id)
			if !ok {
				return extraupd.ApplySpec{}, false
			}
			return u.Apply(), true
		},
	}
}

// RunExtra is the launcher for the production policy. On success it does not
// return in production: Exec replaces the process.
func RunExtra(ops nsOps, args []string) *LauncherError {
	return runExtra(ops, args, productionLaunchPolicy())
}

func cleanDetail(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return boundedText(s, maxLauncherDetail)
}

func runExtra(ops nsOps, args []string, policy launchPolicy) *LauncherError {
	fail := func(reason, format string, a ...any) *LauncherError {
		e := &LauncherError{Reason: reason, Detail: cleanDetail(fmt.Sprintf(format, a...))}
		_ = ops.WriteStatus("err " + e.Reason + " " + e.Detail + "\n")
		return e
	}

	// 1. Preconditions. Anyone who can pass these is already root.
	if len(args) != 2 || args[0] != LauncherArg {
		return fail(ReasonLauncherRefused, "launcher mode needs exactly %s <id>", LauncherArg)
	}
	spec, known := policy.lookup(args[1])
	if !known || !spec.PrivateTmp {
		return fail(ReasonLauncherRefused, "unknown or non-launchable updater")
	}
	if ops.Euid() != 0 {
		return fail(ReasonLauncherRefused, "launcher requires root")
	}
	if err := ops.StatusPipeIsFIFO(); err != nil {
		return fail(ReasonLauncherRefused, "status descriptor 3 is not a FIFO: %v", err)
	}
	if exe, err := ops.ParentExe(); err != nil || exe != policy.helperPath {
		return fail(ReasonLauncherRefused, "parent is not the maintenance helper")
	}
	if ops.Getenv(LauncherEnv) != "1" {
		return fail(ReasonLauncherRefused, "launch token missing")
	}

	// 2. Own mount namespace. Propagation of "/" is not changed: unshare(2)
	// copies shared mounts into the same peer groups.
	ops.LockThread()
	if err := ops.Unshare(cloneNewNS); err != nil {
		return fail(ReasonLauncherSetupError, "unshare: %v", err)
	}

	// 3. Propagation proof against pid 1's table (OPS-001).
	selfData, err := ops.ReadFile(selfMountinfo, maxMountinfoBytes)
	if err != nil {
		return fail(ReasonMountPropagation, "read %s: %v", selfMountinfo, err)
	}
	initData, err := ops.ReadFile(initMountinfo, maxMountinfoBytes)
	if err != nil {
		return fail(ReasonMountPropagation, "read %s: %v", initMountinfo, err)
	}
	self, err := ParseMountinfo(selfData)
	if err != nil {
		return fail(ReasonMountPropagation, "own mount table: %v", err)
	}
	initTable, err := ParseMountinfo(initData)
	if err != nil {
		return fail(ReasonMountPropagation, "pid 1 mount table: %v", err)
	}
	if err = verifyPropagation(self, initTable); err != nil {
		return fail(ReasonMountPropagation, "%v", err)
	}

	// 4. /tmp must be its own mount point. A bind mount onto it would be a new
	// mount under the shared "/" and would propagate to the host.
	tmp, ok := topEntry(self, "/tmp")
	if !ok {
		return fail(ReasonTmpNotPrivate, "/tmp is not a mount point of its own")
	}

	// 5. Make only this namespace's copy of /tmp private.
	if err = ops.Mount("", "/tmp", "", msRec|msPrivate, ""); err != nil {
		return fail(ReasonLauncherSetupError, "make /tmp private: %v", err)
	}
	// 6. Make that copy read-only, keeping its nosuid, nodev and noexec flags.
	// If the updater ever unmounts the new tmpfs it meets a read-only host view.
	ro := uintptr(msRemount | msBind | msRdonly)
	for opt, flag := range map[string]uintptr{"nosuid": msNosuid, "nodev": msNodev, "noexec": msNoexec} {
		if tmp.HasOption(opt) {
			ro |= flag
		}
	}
	if err = ops.Mount("/tmp", "/tmp", "", ro, ""); err != nil {
		return fail(ReasonLauncherSetupError, "remount /tmp read-only: %v", err)
	}
	// 7. Stack a fresh tmpfs. Its parent is the private mount, so nothing
	// propagates.
	if err = ops.Mount("tmpfs", "/tmp", "tmpfs", msNosuid|msNodev, "mode=1777,size=50%"); err != nil {
		return fail(ReasonLauncherSetupError, "mount tmpfs on /tmp: %v", err)
	}

	// 8. Isolation proof (SEC-001).
	if err = proveIsolation(ops); err != nil {
		return fail(ReasonTmpNotPrivate, "%v", err)
	}

	// 9. A private /run/dietpi, when the host has one (AD-2, DP-SEC-003).
	if lerr := isolateRunDir(ops, fail); lerr != nil {
		return lerr
	}

	// 10. Execute the registry's fixed spec.
	if err = ops.Chdir("/"); err != nil {
		return fail(ReasonLauncherSetupError, "chdir /: %v", err)
	}
	if err = ops.SetStatusCloexec(); err != nil {
		return fail(ReasonLauncherSetupError, "close-on-exec on the status descriptor: %v", err)
	}
	if err = ops.WriteStatus("ok\n"); err != nil {
		return &LauncherError{Reason: ReasonLauncherSetupError, Detail: cleanDetail("write status: " + err.Error())}
	}
	argv := append([]string{spec.Path}, spec.Args...)
	if err = ops.Exec(spec.Path, argv, spec.Env); err != nil {
		return fail(ReasonLauncherSetupError, "exec: %v", err)
	}
	return nil
}

// isolateRunDir gives the step an empty root-owned tmpfs on /run/dietpi. A host
// without /run/dietpi needs nothing: DietPi creates it itself as root. Every
// failure after the probe is rundir_not_private and nothing has been executed.
func isolateRunDir(ops nsOps, fail func(reason, format string, a ...any) *LauncherError) *LauncherError {
	// Only "does not exist" skips. Any other probe error fails closed.
	host, err := ops.StatNoFollow(runDietPiPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fail(ReasonRunDirNotPrivate, "cannot inspect %s: %v", runDietPiPath, err)
	}
	// Prove, without following a symlink, that it is a real root-owned directory.
	if !host.IsDir() || host.UID != 0 {
		return fail(ReasonRunDirNotPrivate, "%s is not a root-owned directory (uid %d, mode %#o)", runDietPiPath, host.UID, host.Mode)
	}
	data, err := ops.ReadFile(selfMountinfo, maxMountinfoBytes)
	if err != nil {
		return fail(ReasonRunDirNotPrivate, "read %s: %v", selfMountinfo, err)
	}
	table, err := ParseMountinfo(data)
	if err != nil {
		return fail(ReasonRunDirNotPrivate, "own mount table: %v", err)
	}
	// /run must be its own mount: making it private must not reach /.
	if _, ok := topEntry(table, runPath); !ok {
		return fail(ReasonRunDirNotPrivate, "%s is not a mount point of its own", runPath)
	}
	// Only this namespace's copy of /run and what is mounted below it.
	if err = ops.Mount("", runPath, "", msRec|msPrivate, ""); err != nil {
		return fail(ReasonRunDirNotPrivate, "make %s private: %v", runPath, err)
	}
	if err = ops.Mount("tmpfs", runDietPiPath, "tmpfs", msNosuid|msNodev|msNoexec, runDirTmpfsData); err != nil {
		return fail(ReasonRunDirNotPrivate, "mount tmpfs on %s: %v", runDietPiPath, err)
	}
	if err = proveRunDirIsolation(ops); err != nil {
		return fail(ReasonRunDirNotPrivate, "%v", err)
	}
	// The /run change must not have disturbed what OPS-001 proved: / and /boot
	// still propagate to the host.
	if err = reverifyPropagation(ops); err != nil {
		return fail(ReasonMountPropagation, "after the %s setup: %v", runDietPiPath, err)
	}
	return nil
}

// reverifyPropagation re-reads both mount tables and runs verifyPropagation.
func reverifyPropagation(ops nsOps) error {
	selfData, err := ops.ReadFile(selfMountinfo, maxMountinfoBytes)
	if err != nil {
		return fmt.Errorf("read %s: %v", selfMountinfo, err)
	}
	initData, err := ops.ReadFile(initMountinfo, maxMountinfoBytes)
	if err != nil {
		return fmt.Errorf("read %s: %v", initMountinfo, err)
	}
	self, err := ParseMountinfo(selfData)
	if err != nil {
		return fmt.Errorf("own mount table: %v", err)
	}
	initTable, err := ParseMountinfo(initData)
	if err != nil {
		return fmt.Errorf("pid 1 mount table: %v", err)
	}
	return verifyPropagation(self, initTable)
}

// proveRunDirIsolation checks that /run/dietpi is now an empty root-owned 0755
// tmpfs that is not the host's directory and is not shared.
func proveRunDirIsolation(ops nsOps) error {
	kind, err := ops.StatfsType(runDietPiPath)
	if err != nil || kind != tmpfsMagic {
		return fmt.Errorf("%s is not a tmpfs after setup (type %#x, %v)", runDietPiPath, kind, err)
	}
	id, err := ops.StatID(runDietPiPath)
	if err != nil || id.UID != 0 || !id.IsDir() || id.Perm() != runDirMode {
		return fmt.Errorf("%s is not a root-owned 0755 directory after setup (uid %d, mode %#o, %v)", runDietPiPath, id.UID, id.Mode, err)
	}
	n, err := ops.EntryCount(runDietPiPath)
	if err != nil || n != 0 {
		return fmt.Errorf("%s is not empty after setup (%d entries, %v)", runDietPiPath, n, err)
	}
	host, err := ops.StatID(initRunDietPi)
	if err != nil {
		return fmt.Errorf("cannot identify the host %s: %v", runDietPiPath, err)
	}
	if host.Dev == id.Dev && host.Ino == id.Ino {
		return fmt.Errorf("%s is still the host directory", runDietPiPath)
	}
	data, err := ops.ReadFile(selfMountinfo, maxMountinfoBytes)
	if err != nil {
		return fmt.Errorf("read %s: %v", selfMountinfo, err)
	}
	table, err := ParseMountinfo(data)
	if err != nil {
		return fmt.Errorf("own mount table: %v", err)
	}
	top, ok := topEntry(table, runDietPiPath)
	if !ok || top.Shared != 0 {
		return fmt.Errorf("%s is still shared with the host", runDietPiPath)
	}
	return nil
}

// verifyPropagation requires that "/", the mount holding /boot and, when
// mounted, /boot/firmware are shared and peers of pid 1's mounts: the same
// shared:N on both sides.
func verifyPropagation(self, initTable []MountEntry) error {
	targets := []string{"/"}
	if boot, ok := containing(self, "/boot"); ok {
		targets = append(targets, boot.MountPoint)
	}
	if _, ok := topEntry(self, "/boot/firmware"); ok {
		targets = append(targets, "/boot/firmware")
	}
	seen := map[string]bool{}
	for _, mp := range targets {
		if seen[mp] {
			continue
		}
		seen[mp] = true
		own, ok := topEntry(self, mp)
		if !ok || own.Shared == 0 {
			return fmt.Errorf("%s is not a shared mount in the step's namespace", mp)
		}
		host, ok := topEntry(initTable, mp)
		if !ok || host.Shared != own.Shared {
			return fmt.Errorf("%s is not a peer of pid 1's mount (shared:%d)", mp, own.Shared)
		}
	}
	return nil
}

// proveIsolation checks that /tmp is now a root-owned, empty tmpfs that is not
// the host's /tmp and is not shared.
func proveIsolation(ops nsOps) error {
	kind, err := ops.StatfsType("/tmp")
	if err != nil || kind != tmpfsMagic {
		return fmt.Errorf("/tmp is not a tmpfs after setup (type %#x, %v)", kind, err)
	}
	id, err := ops.StatID("/tmp")
	if err != nil || id.UID != 0 {
		return fmt.Errorf("/tmp is not root-owned after setup (%v)", err)
	}
	n, err := ops.EntryCount("/tmp")
	if err != nil || n != 0 {
		return fmt.Errorf("/tmp is not empty after setup (%d entries, %v)", n, err)
	}
	host, err := ops.StatID(initTmpPath)
	if err != nil {
		return fmt.Errorf("cannot identify the host /tmp: %v", err)
	}
	if host.Dev == id.Dev && host.Ino == id.Ino {
		return fmt.Errorf("/tmp is still the host /tmp")
	}
	data, err := ops.ReadFile(selfMountinfo, maxMountinfoBytes)
	if err != nil {
		return fmt.Errorf("read %s: %v", selfMountinfo, err)
	}
	table, err := ParseMountinfo(data)
	if err != nil {
		return fmt.Errorf("own mount table: %v", err)
	}
	top, ok := topEntry(table, "/tmp")
	if !ok || top.Shared != 0 {
		return fmt.Errorf("/tmp is still shared with the host")
	}
	return nil
}
