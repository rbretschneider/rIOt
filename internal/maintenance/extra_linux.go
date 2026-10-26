//go:build linux

package maintenance

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Root-side probes of the extra-updater step (EXTRA-UPD-001 AD-003, AD-005,
// AD-008). Each one fails closed: anything it cannot determine is reported as
// not safe to proceed.

// ReadExtraPolicy reads the root-owned opt-in policy. The file must be a regular
// root-owned file with a root-owned, non-writable path, at most 512 bytes. Any
// error means no updater is enabled.
func ReadExtraPolicy() ([]string, error) { return readExtraPolicyAt(extraupd.PolicyPath, 0) }

func readExtraPolicyAt(path string, uid int) ([]string, error) {
	data, err := readSafeMode(path, extraupd.MaxPolicyBytes, uid, -1, 0644)
	if err != nil {
		return nil, err
	}
	return extraupd.ParsePolicy(data)
}

// Dpkg lock files, in the order dpkg takes them.
var dpkgLockPaths = []string{"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock"}

// DpkgLockState asks the kernel whether another open file description holds a
// write lock on either dpkg lock file. It never takes the lock: it opens each
// file read-only and queries with F_OFD_GETLK.
func DpkgLockState() LockState { return dpkgLockStateAt(dpkgLockPaths, 0) }

func dpkgLockStateAt(paths []string, uid uint32) LockState {
	for _, path := range paths {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			return LockState{Kind: LockUnknown, Detail: fmt.Sprintf("cannot open %s: %v", path, err)}
		}
		state := queryOFDLock(fd, path, uid)
		_ = unix.Close(fd)
		if state.Kind != LockFree {
			return state
		}
	}
	return LockState{Kind: LockFree}
}

func queryOFDLock(fd int, path string, uid uint32) LockState {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return LockState{Kind: LockUnknown, Detail: fmt.Sprintf("cannot stat %s: %v", path, err)}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uid {
		return LockState{Kind: LockUnknown, Detail: path + " is not a regular root-owned file"}
	}
	lk := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err := unix.FcntlFlock(uintptr(fd), unix.F_OFD_GETLK, &lk); err != nil {
		return LockState{Kind: LockUnknown, Detail: fmt.Sprintf("cannot query %s: %v", path, err)}
	}
	if lk.Type != unix.F_UNLCK {
		return LockState{Kind: LockHeld, Detail: fmt.Sprintf("%s is held by another process (pid %d)", path, lk.Pid)}
	}
	return LockState{Kind: LockFree}
}

// Busy-scan bounds (AD-008, SEC-001).
const (
	busyMaxPIDs    = 32768
	busyMaxCmdline = 4096
	busyBudget     = 2 * time.Second
)

// DietPiBusy scans the host /proc for another DietPi program. The worker shares
// the host pid namespace. DietPi's own concurrency guard cannot see this run, so
// the worker refuses while one is going. Vanished processes are skipped; every
// other error and every bound counts as busy.
func DietPiBusy() (bool, string) {
	spec := extraupd.NewDietPi().Apply()
	return scanBusy("/proc", spec.BusyMatch, time.Now)
}

func scanBusy(procDir string, match func(argv0, argv1 string) bool, now func() time.Time) (bool, string) {
	deadline := now().Add(busyBudget)
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return true, "cannot list " + procDir + ": " + err.Error()
	}
	checked := 0
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		checked++
		if checked > busyMaxPIDs {
			return true, "more than 32768 processes; cannot prove no DietPi program runs"
		}
		if now().After(deadline) {
			return true, "process scan exceeded " + busyBudget.String()
		}
		cmdline, err := readCmdline(filepath.Join(procDir, e.Name(), "cmdline"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			return true, fmt.Sprintf("cannot read process %s: %v", e.Name(), err)
		}
		argv0, argv1, _ := strings.Cut(cmdline, "\x00")
		argv1, _, _ = strings.Cut(argv1, "\x00")
		if match(argv0, argv1) {
			return true, fmt.Sprintf("process %s runs %s", e.Name(), boundedText(strings.ReplaceAll(strings.TrimRight(cmdline, "\x00"), "\x00", " "), 120))
		}
	}
	return false, ""
}

func readCmdline(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, busyMaxCmdline))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// EffectiveUnit asks systemd, as root, what its unit really is. The argv is
// fixed, the environment is the fixed one, the output is capped at 4 KiB and
// the call at 5 seconds (SEC-002, OPS-001).
func EffectiveUnit() (UnitState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), unitShowTimeout)
	defer cancel()
	return effectiveUnitFrom(func() (CommandResult, error) {
		return runProcessBounded(ctx, unitShowPath, unitShowArgs, time.Second, io.Discard, io.Discard, unitShowBound)
	})
}

func effectiveUnitFrom(run func() (CommandResult, error)) (UnitState, error) {
	res, err := run()
	if err != nil {
		return UnitState{}, fmt.Errorf("systemctl show failed: %w", err)
	}
	if res.Truncated {
		return UnitState{}, errors.New("systemctl output exceeded its bound")
	}
	return parseUnitShow(res.Output)
}

// osTrustFS lstat()s the real tree.
type osTrustFS struct{}

func (osTrustFS) Lstat(name string) (trustStat, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return trustStat{}, err
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return trustStat{}, errors.New("no ownership information")
	}
	return trustStat{Mode: info.Mode(), UID: sys.Uid, Nlink: uint64(sys.Nlink)}, nil
}

func (osTrustFS) ReadDir(name string) ([]string, error) {
	d, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	names, err := d.Readdirnames(trustMaxEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return names, nil
}

// TrustWalk runs the trust walk over the real filesystem.
func TrustWalk(spec extraupd.ApplySpec, installed models.ExtraUpdater) error {
	return trustWalk(osTrustFS{}, spec, installed)
}

// ExtraCommandAllowed admits exactly the launcher invocation of a registered
// updater whose spec asks for it: HelperPath with argv --run-extra <id>. No other
// extra-updater process may start (AC-019).
func ExtraCommandAllowed(path string, args []string) bool {
	if path != HelperPath || len(args) != 2 || args[0] != LauncherArg {
		return false
	}
	u, ok := extraupd.Lookup(args[1])
	return ok && u.Apply().PrivateTmp
}

// launcherCommand is the fixed command of the step launcher.
func launcherCommand(id string) (path string, args, env []string) {
	return HelperPath, []string{LauncherArg, id}, []string{LauncherEnv + "=1"}
}

// ExtraLaunchRunner is the production StepRunner. It starts the launcher with a
// status pipe on descriptor 3 and supervises the updater process group like the
// apt runner does: same fixed environment rules, process group, SIGTERM then
// SIGKILL, capped capture.
type ExtraLaunchRunner struct{}

func (ExtraLaunchRunner) RunStep(ctx context.Context, id string, spec extraupd.ApplySpec) StepOutcome {
	path, args, env := launcherCommand(id)
	if !ExtraCommandAllowed(path, args) {
		return StepOutcome{SetupErr: "launcher command is outside the fixed plan", ExitCode: 1}
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return StepOutcome{SetupErr: "cannot create the status pipe: " + err.Error(), ExitCode: 1}
	}
	res, runErr := runProcessSpec(ctx, processSpec{
		path:       path,
		args:       args,
		env:        env,
		dir:        "/",
		grace:      TerminationGrace,
		limit:      spec.OutputCap,
		settle:     spec.DescendantSettle,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		extraFiles: []*os.File{pw},
		gate:       func(gctx context.Context) error { return readLauncherStatus(gctx, pr, ExtraLauncherSetupBound) },
	})
	_ = pr.Close()
	if res.GateErr != nil {
		return gateOutcome(res.GateErr, res.ExitCode)
	}
	return StepOutcome{Output: res.Output, Truncated: res.Truncated, ExitCode: res.ExitCode, Err: runErr}
}

// gateOutcome maps a launcher failure to the step outcome. The three isolation
// refusals mean nothing ran and are recorded as skips; any other failure is a
// setup error and stays a failed step (DP-SEC-004).
func gateOutcome(gateErr error, exitCode int) StepOutcome {
	var le *LauncherError
	if errors.As(gateErr, &le) && (le.Reason == ReasonTmpNotPrivate || le.Reason == ReasonMountPropagation || le.Reason == ReasonRunDirNotPrivate) {
		return StepOutcome{Skip: le.Reason, SkipDetail: le.Detail, ExitCode: exitCode}
	}
	return StepOutcome{SetupErr: boundedText(gateErr.Error(), 200), ExitCode: exitCode}
}

// readLauncherStatus reads the launcher's status pipe until end-of-file, within
// bound. "ok" followed by end-of-file (the exec closed the descriptor) means the
// updater is running. An `err <reason> <detail>` line, a missing ok or a
// timeout is a setup failure.
func readLauncherStatus(ctx context.Context, pr *os.File, bound time.Duration) error {
	_ = pr.SetReadDeadline(time.Now().Add(bound))
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = pr.Close()
		case <-stop:
		}
	}()
	sc := bufio.NewScanner(io.LimitReader(pr, 512))
	ok := false
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "ok":
			ok = true
		case strings.HasPrefix(line, "err "):
			reason, detail, _ := strings.Cut(strings.TrimPrefix(line, "err "), " ")
			return &LauncherError{Reason: reason, Detail: cleanDetail(detail)}
		default:
			return &LauncherError{Reason: ReasonLauncherSetupError, Detail: "unexpected launcher status"}
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return &LauncherError{Reason: ReasonLauncherSetupError, Detail: "launcher did not reach exec within " + bound.String()}
		}
		return &LauncherError{Reason: ReasonLauncherSetupError, Detail: "status pipe: " + err.Error()}
	}
	if !ok {
		return &LauncherError{Reason: ReasonLauncherSetupError, Detail: "launcher exited without ok"}
	}
	return nil
}

// Compile-time proof the launcher runner satisfies the worker's seam.
var _ StepRunner = ExtraLaunchRunner{}
