//go:build linux

package extraupd

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// openNoFollow opens a file read-only without following a final symlink and
// without blocking on a FIFO or device.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}

// isSymlinkRefusal reports that O_NOFOLLOW refused a symlink.
func isSymlinkRefusal(err error) bool { return errors.Is(err, syscall.ELOOP) }

// trustedFlagInfo applies the descriptor checks of ReadTrustedFlag to the stat
// of the opened file: regular, owned by owner, not group or world writable and
// exactly one link. It mirrors validateFile in the maintenance package.
func trustedFlagInfo(info fs.FileInfo, owner uint32) bool {
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == owner && st.Nlink == 1
}
