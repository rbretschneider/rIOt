//go:build linux

package extraupd

import (
	"os"
	"syscall"
)

// openNoFollow opens a file read-only without following a final symlink and
// without blocking on a FIFO or device.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}
