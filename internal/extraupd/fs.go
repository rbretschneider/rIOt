package extraupd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Errors returned by FS implementations.
var (
	// ErrTooLarge means a file is longer than the read bound. Callers treat the
	// affected facts as unknown, never as up to date (FR-015).
	ErrTooLarge = errors.New("file exceeds the read bound")
	// ErrNotRegular means the path is not a regular file (a symlink, directory
	// or device does not count as a fact source).
	ErrNotRegular = errors.New("not a regular file")
	// ErrUntrustedFlag means a flag file did not pass the descriptor checks of
	// ReadTrustedFlag (regular file, trusted owner, no group or world write, one
	// link). Detection treats such a flag as absent (FR-010, DP-SEC-002).
	ErrUntrustedFlag = errors.New("flag file is not trusted")
)

// FS is the read-only view detection uses. Names are slash-separated paths
// relative to the root, with no leading slash ("boot/dietpi/.version").
type FS interface {
	// ReadFile reads a regular file, never following a symlink, and fails with
	// ErrTooLarge instead of returning more than max bytes.
	ReadFile(name string, max int64) ([]byte, error)
	// Lstat stats a path without following a symlink.
	Lstat(name string) (fs.FileInfo, error)
	// ReadTrustedFlag reads one of DietPi's flag files, believing it only when
	// root wrote it. It opens without following a symlink, then checks the
	// opened descriptor: a regular file, owned by the trusted owner, not group
	// or world writable, with exactly one link. A file that fails the checks
	// yields ErrUntrustedFlag, so a swap between a probe and the read cannot
	// slip a forged flag through (FR-010, DP-SEC-002). Like ReadFile it fails
	// with ErrTooLarge instead of returning more than max bytes.
	ReadTrustedFlag(name string, max int64) ([]byte, error)
}

// dirFS is the production FS. owner is the uid whose files are believed as
// flags: 0 everywhere in production (FR-011); only testseam.go can change it.
type dirFS struct {
	root  string
	owner uint32
}

// DirFS returns an FS rooted at a directory that trusts only root-owned flags.
// It is the view of a containerised agent's host root. A user-namespace uid
// mapping is unsupported for DietPi detection (DP-SEC-006).
func DirFS(root string) FS { return dirFS{root: root} }

// OSRoot returns the real root filesystem. It trusts only uid 0 flags.
func OSRoot() FS { return dirFS{root: string(filepath.Separator)} }

func (d dirFS) path(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("invalid name %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("invalid name %q", name)
		}
	}
	return filepath.Join(d.root, filepath.FromSlash(name)), nil
}

func (d dirFS) Lstat(name string) (fs.FileInfo, error) {
	p, err := d.path(name)
	if err != nil {
		return nil, err
	}
	return os.Lstat(p)
}

func (d dirFS) ReadFile(name string, max int64) ([]byte, error) {
	p, err := d.path(name)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	f, err := openNoFollow(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}

func (d dirFS) ReadTrustedFlag(name string, max int64) ([]byte, error) {
	p, err := d.path(name)
	if err != nil {
		return nil, err
	}
	f, err := openNoFollow(p)
	if err != nil {
		if isSymlinkRefusal(err) {
			return nil, ErrUntrustedFlag
		}
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !trustedFlagInfo(opened, d.owner) {
		return nil, ErrUntrustedFlag
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}
