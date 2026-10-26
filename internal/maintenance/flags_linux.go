//go:build linux

package maintenance

import (
	"os"

	"golang.org/x/sys/unix"
)

// osFlagOps is the real flagOps. It opens the directory with O_NOFOLLOW and
// O_DIRECTORY and reads the owner from the descriptor, never from a path.
type osFlagOps struct{}

func (osFlagOps) OpenDir(path string) (flagDir, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, &os.PathError{Op: "fstat", Path: path, Err: err}
	}
	return osFlagDir{fd: fd, uid: st.Uid}, nil
}

type osFlagDir struct {
	fd  int
	uid uint32
}

func (d osFlagDir) OwnerUID() uint32 { return d.uid }

// Unlink is unlinkat(2) with flags 0: it removes the entry, a symlink included,
// and does not resolve it.
func (d osFlagDir) Unlink(name string) error {
	if err := unix.Unlinkat(d.fd, name, 0); err != nil {
		return &os.PathError{Op: "unlinkat", Path: name, Err: err}
	}
	return nil
}

func (d osFlagDir) Close() error { return unix.Close(d.fd) }

// ReconcileDietPiFlags removes the host's DietPi update flags after a
// successful step (AD-3). Root owns the directory in production, so only uid 0
// is trusted.
func ReconcileDietPiFlags(dir string) string { return reconcileFlags(osFlagOps{}, dir, 0) }
