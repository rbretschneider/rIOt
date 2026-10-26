package maintenance

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Linux values of the mount(2) and unshare(2) flags the launcher uses. They are
// spelled out here so the launcher logic and its tests build on every host; a
// Linux test pins each one against golang.org/x/sys/unix.
const (
	cloneNewNS = 0x00020000
	msRdonly   = 1
	msNosuid   = 2
	msNodev    = 4
	msNoexec   = 8
	msRemount  = 32
	msBind     = 4096
	msRec      = 16384
	msPrivate  = 1 << 18

	tmpfsMagic = 0x01021994
)

// Bounds of a mountinfo read (SEC-009, AD-007 step 3).
const (
	maxMountinfoBytes = 1 << 20
	maxMountinfoLines = 8192
)

// MountEntry is one parsed /proc/<pid>/mountinfo line (proc(5)).
type MountEntry struct {
	ID, ParentID int
	Root         string
	MountPoint   string
	Options      []string
	// Shared and Master are the peer-group numbers of the shared:N and master:N
	// optional fields. Zero means the field is absent.
	Shared, Master int
	FSType, Source string
}

// HasOption reports whether the per-mount options contain opt.
func (e MountEntry) HasOption(opt string) bool {
	for _, o := range e.Options {
		if o == opt {
			return true
		}
	}
	return false
}

// ParseMountinfo is a strict parser: at most 1 MiB and 8192 lines, every line
// well formed, the optional fields terminated by "-", mount points decoded from
// their octal escapes. Any malformation is an error, so a proof built on the
// table is never built on a guess.
func ParseMountinfo(data []byte) ([]MountEntry, error) {
	if len(data) > maxMountinfoBytes {
		return nil, errors.New("mountinfo exceeds its size bound")
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, errors.New("mountinfo is empty")
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxMountinfoLines {
		return nil, errors.New("mountinfo exceeds its line bound")
	}
	out := make([]MountEntry, 0, len(lines))
	for i, line := range lines {
		e, err := parseMountLine(line)
		if err != nil {
			return nil, fmt.Errorf("mountinfo line %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func parseMountLine(line string) (MountEntry, error) {
	var e MountEntry
	f := strings.Split(line, " ")
	if len(f) < 10 {
		return e, errors.New("too few fields")
	}
	var err error
	if e.ID, err = strconv.Atoi(f[0]); err != nil {
		return e, errors.New("bad mount id")
	}
	if e.ParentID, err = strconv.Atoi(f[1]); err != nil {
		return e, errors.New("bad parent id")
	}
	if !strings.Contains(f[2], ":") {
		return e, errors.New("bad device number")
	}
	if e.Root, err = unescapeMountField(f[3]); err != nil {
		return e, err
	}
	if e.MountPoint, err = unescapeMountField(f[4]); err != nil {
		return e, err
	}
	e.Options = strings.Split(f[5], ",")
	sep := -1
	for i := 6; i < len(f); i++ {
		if f[i] == "-" {
			sep = i
			break
		}
	}
	if sep < 0 || len(f)-sep < 4 {
		return e, errors.New("missing optional-field terminator or filesystem fields")
	}
	for _, opt := range f[6:sep] {
		kind, num, found := strings.Cut(opt, ":")
		if !found {
			continue // unbindable and similar flag fields carry no number
		}
		n, err := strconv.Atoi(num)
		if err != nil || n <= 0 {
			return e, errors.New("bad peer group number")
		}
		switch kind {
		case "shared":
			e.Shared = n
		case "master":
			e.Master = n
		}
	}
	e.FSType = f[sep+1]
	e.Source = f[sep+2]
	return e, nil
}

// unescapeMountField decodes the \ooo octal escapes the kernel uses for
// spaces, tabs, newlines and backslashes in mountinfo paths.
func unescapeMountField(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+4 > len(s) {
			return "", errors.New("truncated escape")
		}
		v, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
		if err != nil {
			return "", errors.New("bad escape")
		}
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String(), nil
}

// topEntry returns the last entry at mountPoint: the one a path lookup meets.
func topEntry(entries []MountEntry, mountPoint string) (MountEntry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].MountPoint == mountPoint {
			return entries[i], true
		}
	}
	return MountEntry{}, false
}

// containing returns the entry of the mount that holds path: the one with the
// longest mount point that is a prefix of path at a path boundary.
func containing(entries []MountEntry, path string) (MountEntry, bool) {
	best, found := MountEntry{}, false
	for _, e := range entries {
		mp := strings.TrimRight(e.MountPoint, "/")
		if mp == "" || path == mp || strings.HasPrefix(path, mp+"/") {
			if !found || len(e.MountPoint) >= len(best.MountPoint) {
				best, found = e, true
			}
		}
	}
	return best, found
}

// st_mode file type bits, spelled out so the launcher logic builds everywhere.
const (
	modeTypeMask = 0o170000
	modeTypeDir  = 0o040000
)

// FileID identifies a file for the isolation proof. Mode is the raw st_mode:
// the file type in the high bits and the permission bits below.
type FileID struct {
	Dev, Ino uint64
	UID      uint32
	Mode     uint32
}

// IsDir reports whether the file is a directory.
func (f FileID) IsDir() bool { return f.Mode&modeTypeMask == modeTypeDir }

// Perm returns the permission bits, with setuid, setgid and sticky included.
func (f FileID) Perm() uint32 { return f.Mode & 0o7777 }

// nsOps is every system effect the launcher has. Tests script it; the Linux
// implementation (nsops_linux.go) is the only one that touches the kernel.
type nsOps interface {
	Euid() int
	Getenv(key string) string
	// StatusPipeIsFIFO reports whether descriptor 3 is a FIFO.
	StatusPipeIsFIFO() error
	ParentExe() (string, error)
	LockThread()
	Unshare(flags int) error
	Mount(source, target, fstype string, flags uintptr, data string) error
	ReadFile(path string, max int) ([]byte, error)
	StatfsType(path string) (int64, error)
	StatID(path string) (FileID, error)
	// StatNoFollow is StatID without following a final symlink (lstat).
	StatNoFollow(path string) (FileID, error)
	EntryCount(path string) (int, error)
	Chdir(path string) error
	// WriteStatus writes one line to descriptor 3.
	WriteStatus(line string) error
	// SetStatusCloexec marks descriptor 3 close-on-exec.
	SetStatusCloexec() error
	Exec(path string, argv, env []string) error
}
