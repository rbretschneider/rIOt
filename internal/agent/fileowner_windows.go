//go:build windows

package agent

import "os"

// ownedByEUID has no uid on Windows; any regular file counts as agent-owned.
func ownedByEUID(fi os.FileInfo) bool {
	return fi.Mode().IsRegular()
}
