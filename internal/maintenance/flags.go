package maintenance

import (
	"errors"
	"io/fs"
	"strings"
)

// Host flag reconciliation (DIETPI-PATH-001 AD-3). The DietPi step runs with a
// private /run/dietpi, so its own removal of .update_available and
// .live_patches after a successful update dies with the step's namespace.
// After a successful step the worker removes the two files on the host, as
// DietPi would have (F-5).
//
// The host /run/dietpi is world-writable and not sticky on stock DietPi, so the
// removal never resolves a path through it. The directory is opened without
// following a symlink, proved root-owned on the descriptor, and the two fixed
// names are removed with unlinkat on that descriptor: a symlink is removed
// itself and never followed. Nothing is ever created or written there.

const (
	// DietPiRunDir is the host directory holding DietPi's flag files.
	DietPiRunDir = "/run/dietpi"

	maxFlagNote = 200
)

// dietPiFlagNames are the only entries the worker removes. .apt_updates and
// .dietpi_motd are DietPi's and are left alone.
var dietPiFlagNames = []string{".update_available", ".live_patches"}

// flagDir is an opened, pinned directory.
type flagDir interface {
	// OwnerUID is the uid of the opened directory.
	OwnerUID() uint32
	// Unlink removes the named entry itself, never following it.
	Unlink(name string) error
	Close() error
}

// flagOps opens the directory. Production is linux-only (flags_linux.go).
type flagOps interface {
	// OpenDir opens path as a directory without following a symlink.
	OpenDir(path string) (flagDir, error)
}

// reconcileFlags removes the two flags from dir and returns a bounded note, or
// "" when there is nothing to report. A missing directory or file is not an
// error. The note never changes an outcome.
func reconcileFlags(ops flagOps, dir string, trustedUID uint32) string {
	d, err := ops.OpenDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		return boundedText(dir+" not opened safely; flags left: "+err.Error(), maxFlagNote)
	}
	defer d.Close()
	if d.OwnerUID() != trustedUID {
		return dir + " not root-owned; flags left"
	}
	var failed []string
	for _, name := range dietPiFlagNames {
		if err := d.Unlink(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failed = append(failed, name+": "+err.Error())
		}
	}
	if len(failed) == 0 {
		return ""
	}
	return boundedText("flag cleanup failed: "+strings.Join(failed, "; "), maxFlagNote)
}
