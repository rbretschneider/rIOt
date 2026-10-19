//go:build !windows

package agent

import (
	"os"
	"syscall"
)

// ownedByEUID reports whether the file is owned by the agent's effective user.
func ownedByEUID(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(st.Uid) == os.Geteuid()
}
