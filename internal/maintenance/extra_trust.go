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
	Mode fs.FileMode
	UID  uint32
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
//     root-owned symlinks allowed;
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
	for _, root := range append(append([]string(nil), spec.TrustRoots...), required...) {
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
