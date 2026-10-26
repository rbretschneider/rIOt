//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

const (
	aptListsDir      = "/var/lib/apt/lists"
	aptSourcesBudget = 1000
	rootUID          = 0

	// launcherExitCode is the exit status of a launcher that refused or failed
	// before exec (EXTRA-UPD-001 section 6.1).
	launcherExitCode = 125
)

// init locks the main goroutine to the main thread. The step launcher unshares
// a mount namespace, which belongs to one thread and must be the thread that
// execs (SEC-009). Locking in init keeps every later "own table" read on it.
func init() { runtime.LockOSThread() }

// launcherExit carries the exit status of a failed launcher.
type launcherExit struct{ err *maintenance.LauncherError }

func (e launcherExit) Error() string { return e.err.Error() }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "maintenance:", err)
		var le launcherExit
		if errors.As(err, &le) {
			os.Exit(launcherExitCode)
		}
		os.Exit(1)
	}
}

// capabilitiesText is what `--capabilities` prints: the request versions this
// helper accepts and the updaters it implements. The agent reads it to decide
// whether it may send a version 2 request (AD-002).
func capabilitiesText() string {
	var ids []string
	for _, u := range extraupd.Registry() {
		ids = append(ids, u.ID())
	}
	return "request-versions=1,2\nextra-updaters=" + strings.Join(ids, ",") + "\n"
}

func run(args []string) error { return runWith(args, os.Stdout) }

// runWith handles the argv modes in a fixed order. --capabilities comes first,
// before any environment, file, lock or privilege check, so an unprivileged
// agent can probe the installed binary (SEC-007). --run-extra is the step
// launcher. No argument is the worker. Anything else is an error.
func runWith(args []string, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "--capabilities" {
		_, err := io.WriteString(stdout, capabilitiesText())
		return err
	}
	if len(args) > 0 && args[0] == maintenance.LauncherArg {
		if err := maintenance.RunExtraMain(args); err != nil {
			return launcherExit{err}
		}
		return errors.New("launcher returned without exec")
	}
	if len(args) != 0 {
		return errors.New("maintenance helper accepts no arguments")
	}
	if os.Geteuid() != 0 {
		return errors.New("maintenance helper requires root")
	}
	syscall.Umask(0027)
	lock, err := maintenance.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	store, err := maintenance.OpenStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err = maintenance.CheckPrerequisites(ctx); err != nil {
		return err
	}
	worker := maintenance.Worker{
		Store:      store,
		Runner:     maintenance.ExecRunner{},
		Holds:      collectors.NewDetachedAptHolds(),
		Sources:    aptSourcesFingerprint,
		TouchLists: touchAptLists,
		Extras: &maintenance.ExtraEnv{
			FS:        extraupd.OSRoot(),
			Policy:    maintenance.ReadExtraPolicy,
			Lock:      maintenance.DpkgLockState,
			Busy:      maintenance.DietPiBusy,
			Unit:      maintenance.EffectiveUnit,
			Trust:     maintenance.TrustWalk,
			LastStart: store.LatestExtraStart,
			Step:      maintenance.ExtraLaunchRunner{},

			ReconcileFlags: maintenance.ReconcileDietPiFlags,
		},
	}
	prior, err := store.Current()
	if err == nil && (!prior.Terminal() || prior.HoldsReleased) {
		if archived, archiveErr := maintenance.ReadTerminal(prior.CommandID); archiveErr == nil && archived.Terminal() && !archived.HoldsReleased {
			if err = store.Save(archived); err != nil {
				return err
			}
			return errors.New("previous durable terminal result restored; fresh operator retry required")
		}
		_, recoveryErr := worker.RecoverInterrupted(ctx, prior)
		return errors.Join(errors.New("previous maintenance interrupted; holds recovery attempted; fresh operator retry required"), recoveryErr)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read previous status: %w", err)
	}
	req, err := maintenance.ReadInbox()
	if err != nil {
		return err
	}
	_, err = worker.Execute(ctx, req)
	return err
}

// aptSourcesFingerprint is the production Worker.Sources: a stat-only digest of
// the apt-effective configuration files.
func aptSourcesFingerprint() (string, error) {
	entries, err := maintenance.AptSourceEntries("/", aptSourcesBudget)
	if err != nil {
		return "", err
	}
	return maintenance.AptSourcesFingerprint(entries), nil
}

// touchAptLists is the production Worker.TouchLists.
func touchAptLists() error {
	return touchDirNoFollow(aptListsDir, time.Now(), rootUID)
}

// touchDirNoFollow sets path's access and modification times to now. It opens
// the final component with O_NOFOLLOW, verifies the opened descriptor is a
// directory owned by ownerUID, and then updates that same descriptor, so a
// symlink swapped in after the check cannot redirect the write (PATCH-FLOW-001
// C-5). Any doubt is an error and nothing is touched.
func touchDirNoFollow(path string, now time.Time, ownerUID uint32) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s without following symlinks: %w", path, err)
	}
	defer syscall.Close(fd)
	var st syscall.Stat_t
	if err = syscall.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return fmt.Errorf("%s is not a directory", path)
	}
	if st.Uid != ownerUID {
		return fmt.Errorf("%s is owned by uid %d, want %d", path, st.Uid, ownerUID)
	}
	stamp := syscall.NsecToTimeval(now.UnixNano())
	return syscall.Futimes(fd, []syscall.Timeval{stamp, stamp})
}
