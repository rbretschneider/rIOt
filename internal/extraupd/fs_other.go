//go:build !linux

package extraupd

import (
	"io/fs"
	"os"
)

// openNoFollow is the portable fallback used for fixture trees on development
// hosts. ReadFile has already refused anything that is not a regular file.
func openNoFollow(path string) (*os.File, error) { return os.Open(path) }

// isSymlinkRefusal is never true here: the fallback open follows symlinks, so
// ReadTrustedFlag relies on the regular-file check instead.
func isSymlinkRefusal(error) bool { return false }

// trustedFlagInfo checks only that the opened file is regular. DietPi is Linux
// only, so owner, link count and mode checks have nothing to match elsewhere
// (the mode bits of a Windows file are synthetic).
func trustedFlagInfo(info fs.FileInfo, _ uint32) bool { return info.Mode().IsRegular() }
