//go:build linux

package agent

import (
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

// statFileID returns the device and inode of path the way /proc/locks prints
// them. It follows symlinks and never opens the file, so it works on the 0640
// root-owned dpkg lock files as the unprivileged agent user.
func statFileID(path string) (fileID, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileID{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, errors.New("no device and inode information")
	}
	major, minor := decodeLinuxDev(uint64(st.Dev))
	return fileID{Major: major, Minor: minor, Ino: uint64(st.Ino)}, nil
}

// platformDpkgProbe checks the real host: the dpkg lock files, dpkg's own
// presence markers and /proc.
func platformDpkgProbe(now func() time.Time) dpkgLockStatus {
	return probeDpkgLock(dpkgProbeEnv{
		LockPaths:   []string{"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock"},
		DpkgMarkers: []string{"/var/lib/dpkg/status", "/usr/bin/dpkg"},
		StatID:      statFileID,
		Open: func(path string) (io.ReadCloser, error) {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			return f, nil
		},
		Readlink: os.Readlink,
		ProcRoot: "/proc",
		Now:      now,
	})
}
