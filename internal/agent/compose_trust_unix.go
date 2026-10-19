//go:build !windows

package agent

import (
	"os"
	"path/filepath"
	"syscall"
)

// osTrustFS is the production trustFS on Unix: real ownership from Stat_t.
type osTrustFS struct{}

func unixMeta(fi os.FileInfo) fileMeta {
	m := fileMeta{Mode: fi.Mode()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		m.UID, m.GID, m.HasOwner = st.Uid, st.Gid, true
	}
	return m
}

func (osTrustFS) Lstat(path string) (fileMeta, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return unixMeta(fi), nil
}

func (osTrustFS) Stat(path string) (fileMeta, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return unixMeta(fi), nil
}

func (osTrustFS) EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }

func (osTrustFS) ReadFile(path string, max int64) ([]byte, error) { return osReadFile(path, max) }

func (osTrustFS) ReadDirNames(path string) ([]string, error) { return osReadDirNames(path) }

func (osTrustFS) HasAccessACL(path string) (bool, error) { return accessACLPresent(path) }

func (osTrustFS) EUID() int { return os.Geteuid() }
