package maintenance

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Trust-walk bounds (AD-005 check 5, SEC-004).
const (
	trustMaxDepth   = 4
	trustMaxEntries = 4096
)

// trustParents are always checked as ancestors, whether or not a trust root sits
// below them. /var/tmp is sticky and world-writable by design, so it is allowed
// only as a parent.
var trustParents = []string{"/", "/boot", "/run", "/var", "/var/tmp"}

// trustStat is what the walk needs to know about one path.
type trustStat struct {
	Mode  fs.FileMode
	UID   uint32
	Nlink uint64
}

// trustFS is the filesystem view of the trust walk. The production
// implementation lstat()s the real tree; tests use an in-memory one.
type trustFS interface {
	Lstat(name string) (trustStat, error)
	ReadDir(name string) ([]string, error)
}

func (s trustStat) isDir() bool     { return s.Mode.IsDir() }
func (s trustStat) isSymlink() bool { return s.Mode&fs.ModeSymlink != 0 }
func (s trustStat) sticky() bool    { return s.Mode&fs.ModeSticky != 0 }

// groupOrWorldWritable reports a write bit for group or other.
func (s trustStat) groupOrWorldWritable() bool { return s.Mode.Perm()&0o022 != 0 }

// trustWalk is the root-side proof that nothing a non-root user can write sits
// in the updater's trusted path before it runs as root (SEC-004):
//
//   - every ancestor of every root is a root-owned directory that is not group or
//     world writable (a root-owned sticky directory is allowed as an ancestor);
//   - every trust root that exists, and every entry below it to depth 4 and
//     4096 entries, is root-owned and not group or world writable, with only
//     root-owned symlinks allowed. These are the code the updater executes and
//     the settings it reads;
//   - every exact root that exists is lstat()ed and nothing below it is walked:
//     root-owned, not a symlink, of its kind, not group or world writable even
//     when sticky, and for a file exactly one link (DIETPI-PATH-001 AD-1);
//   - every required root exists;
//   - reaching a bound is a failure.
//
// There is no filesystem-type special case: a vfat mount with a lax umask fails
// the same way a chmod 777 does.
func trustWalk(fsys trustFS, spec extraupd.ApplySpec, installed models.ExtraUpdater) error {
	var required []string
	if spec.RequiredRoots != nil {
		required = spec.RequiredRoots(installed)
	}
	isRequired := func(p string) bool {
		for _, r := range required {
			if r == p {
				return true
			}
		}
		return false
	}
	for _, p := range trustParents {
		if err := checkAncestor(fsys, p); err != nil {
			return err
		}
	}
	entries := 0
	for _, root := range append(append([]string(nil), spec.TrustRoots...), unlisted(required, spec)...) {
		if err := checkAncestors(fsys, root); err != nil {
			return err
		}
		st, err := fsys.Lstat(root)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && !isRequired(root) {
				continue
			}
			return fmt.Errorf("%s: %w", root, err)
		}
		if err = walkEntry(fsys, root, st, 0, &entries); err != nil {
			return err
		}
	}
	for _, root := range spec.ExactRoots {
		if err := checkAncestors(fsys, root.Path); err != nil {
			return err
		}
		st, err := fsys.Lstat(root.Path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && !isRequired(root.Path) {
				continue
			}
			return fmt.Errorf("%s: %w", root.Path, err)
		}
		if err = checkExact(root, st); err != nil {
			return err
		}
	}
	return nil
}

// unlisted returns the required roots that are neither a trust root nor an exact
// root. A required root named by neither is still proved, as a tree.
func unlisted(required []string, spec extraupd.ApplySpec) []string {
	var out []string
next:
	for _, r := range required {
		for _, t := range spec.TrustRoots {
			if t == r {
				continue next
			}
		}
		for _, e := range spec.ExactRoots {
			if e.Path == r {
				continue next
			}
		}
		out = append(out, r)
	}
	return out
}

// checkExact is the lstat-only proof of one exact root. Mode 0777 and 1777 both
// fail: a sticky bit does not make a directory root interprets safe
// (DP-SEC-005).
func checkExact(root extraupd.ExactRoot, st trustStat) error {
	p := root.Path
	if st.UID != 0 {
		return fmt.Errorf("%s is owned by uid %d, not root", p, st.UID)
	}
	if st.isSymlink() {
		return fmt.Errorf("%s is a symbolic link", p)
	}
	switch root.Kind {
	case extraupd.ExactDir:
		if !st.isDir() {
			return fmt.Errorf("%s is not a directory", p)
		}
	case extraupd.ExactFile:
		if !st.Mode.IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		if st.Nlink != 1 {
			return fmt.Errorf("%s has %d links, want exactly 1", p, st.Nlink)
		}
	}
	if st.groupOrWorldWritable() {
		return fmt.Errorf("%s is writable by group or others (mode %04o)", p, st.Mode.Perm())
	}
	return nil
}

func checkAncestors(fsys trustFS, p string) error {
	dir := path.Dir(p)
	var chain []string
	for dir != "/" && dir != "." {
		chain = append([]string{dir}, chain...)
		dir = path.Dir(dir)
	}
	for _, c := range chain {
		if err := checkAncestor(fsys, c); err != nil {
			return err
		}
	}
	return nil
}

func checkAncestor(fsys trustFS, p string) error {
	st, err := fsys.Lstat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && p != "/" && p != "/boot" && p != "/run" && p != "/var" {
			return nil
		}
		return fmt.Errorf("%s: %w", p, err)
	}
	if st.UID != 0 {
		return fmt.Errorf("%s is owned by uid %d, not root", p, st.UID)
	}
	if st.isSymlink() {
		return nil
	}
	if !st.isDir() {
		return fmt.Errorf("%s is not a directory", p)
	}
	if st.groupOrWorldWritable() && !st.sticky() {
		return fmt.Errorf("%s is writable by group or others (mode %04o)", p, st.Mode.Perm())
	}
	return nil
}

func walkEntry(fsys trustFS, p string, st trustStat, depth int, entries *int) error {
	*entries++
	if *entries > trustMaxEntries {
		return fmt.Errorf("trust walk exceeded %d entries", trustMaxEntries)
	}
	if st.UID != 0 {
		return fmt.Errorf("%s is owned by uid %d, not root", p, st.UID)
	}
	if st.isSymlink() {
		return nil
	}
	if st.groupOrWorldWritable() {
		return fmt.Errorf("%s is writable by group or others (mode %04o)", p, st.Mode.Perm())
	}
	if !st.isDir() {
		return nil
	}
	names, err := fsys.ReadDir(p)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	if len(names) > 0 && depth >= trustMaxDepth {
		return fmt.Errorf("%s is nested deeper than %d levels", p, trustMaxDepth)
	}
	for _, name := range names {
		child := strings.TrimRight(p, "/") + "/" + name
		cst, err := fsys.Lstat(child)
		if err != nil {
			return fmt.Errorf("%s: %w", child, err)
		}
		if err = walkEntry(fsys, child, cst, depth+1, entries); err != nil {
			return err
		}
	}
	return nil
}
