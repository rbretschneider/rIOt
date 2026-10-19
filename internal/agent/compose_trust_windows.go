//go:build windows

package agent

import (
	"os"
	"path/filepath"
)

// osTrustFS is the production trustFS on Windows. There is no Unix owner or
// ACL to read, so ownership checks do not apply and compose operations require
// docker.compose_roots instead (AD-009).
type osTrustFS struct{}

func (osTrustFS) Lstat(path string) (fileMeta, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return fileMeta{Mode: fi.Mode()}, nil
}

func (osTrustFS) Stat(path string) (fileMeta, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return fileMeta{Mode: fi.Mode()}, nil
}

func (osTrustFS) EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }

func (osTrustFS) ReadFile(path string, max int64) ([]byte, error) { return osReadFile(path, max) }

func (osTrustFS) ReadDirNames(path string) ([]string, error) { return osReadDirNames(path) }

func (osTrustFS) HasAccessACL(string) (bool, error) { return false, nil }

func (osTrustFS) EUID() int { return -1 }
