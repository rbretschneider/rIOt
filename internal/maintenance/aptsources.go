package maintenance

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Apt configuration locations, relative to the filesystem root (PATCH-FLOW-001
// FR-029). Shared by the agent's freshness check and the helper's post-run
// refresh decision so both look at exactly the same files.
const (
	aptSourcesListRel = "etc/apt/sources.list"
	aptSourcesDirRel  = "etc/apt/sources.list.d"
	aptPrefsDirRel    = "etc/apt/preferences.d"

	// aptDirReadChunk bounds every directory read: names are read in chunks and
	// the read stops at the caller's budget instead of loading a whole directory
	// (PATCH-FLOW-001 C-7, security review SEC-006).
	aptDirReadChunk = 128

	suffixAptList    = ".list"
	suffixAptSources = ".sources"
	suffixAptPref    = ".pref"
)

var (
	// ErrAptBudget means more directory entries exist than the caller's budget.
	ErrAptBudget = errors.New("apt configuration entry budget reached")
	// ErrAptDeadline means the caller's expiry callback fired between chunks.
	ErrAptDeadline = errors.New("apt configuration scan deadline reached")

	aptPreferencesName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// AptSourceEntry is one apt-effective configuration file.
type AptSourceEntry struct {
	Path    string
	ModTime time.Time
	Size    int64
}

// AptSourceEntries lists the apt-effective files under root's
// etc/apt/sources.list, sources.list.d and preferences.d, examining at most
// budget directory entries. It returns ErrAptBudget when the budget is
// exceeded. A missing directory or a missing sources.list is not an error.
func AptSourceEntries(root string, budget int) ([]AptSourceEntry, error) {
	return AptSourceEntriesUntil(root, budget, nil)
}

// AptSourceEntriesUntil is AptSourceEntries with an expiry callback that is
// checked between directory chunks. A nil expired never fires. The deadline is
// not enforced inside a single stat call.
func AptSourceEntriesUntil(root string, budget int, expired func() bool) ([]AptSourceEntry, error) {
	scan := &aptScan{root: root, remaining: budget, expired: expired}
	if err := scan.addFile(aptSourcesListRel); err != nil {
		return nil, err
	}
	if err := scan.addDir(aptSourcesDirRel, isAptSourcesName); err != nil {
		return nil, err
	}
	if err := scan.addDir(aptPrefsDirRel, isAptPreferencesName); err != nil {
		return nil, err
	}
	return scan.entries, nil
}

// AptSourcesFingerprint is a canonical digest of entries (sorted
// "path|mtime-ns|size" lines). Equal fingerprints mean no file was added,
// removed or touched.
func AptSourcesFingerprint(entries []AptSourceEntry) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s|%d|%d\n", e.Path, e.ModTime.UnixNano(), e.Size))
	}
	sort.Strings(lines)
	return strings.Join(lines, "")
}

type aptScan struct {
	root      string
	remaining int
	expired   func() bool
	entries   []AptSourceEntry
}

func (s *aptScan) spend() error {
	if s.remaining <= 0 {
		return ErrAptBudget
	}
	s.remaining--
	return nil
}

func (s *aptScan) addFile(rel string) error {
	if err := s.spend(); err != nil {
		return err
	}
	return s.addIfRegular(filepath.Join(s.root, filepath.FromSlash(rel)))
}

func (s *aptScan) addDir(rel string, accept func(name string) bool) error {
	dir, err := os.Open(filepath.Join(s.root, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", rel, err)
	}
	defer dir.Close()
	for {
		if s.expired != nil && s.expired() {
			return ErrAptDeadline
		}
		names, readErr := dir.Readdirnames(aptDirReadChunk)
		for _, name := range names {
			if err := s.spend(); err != nil {
				return err
			}
			if !accept(name) {
				continue
			}
			if err := s.addIfRegular(filepath.Join(dir.Name(), name)); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read %s: %w", rel, readErr)
		}
	}
}

// addIfRegular stats path (following symlinks, as apt does) and records it when
// it is a regular file. A dangling link or a vanished file is skipped.
func (s *aptScan) addIfRegular(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	s.entries = append(s.entries, AptSourceEntry{Path: path, ModTime: info.ModTime(), Size: info.Size()})
	return nil
}

func isAptSourcesName(name string) bool {
	return strings.HasSuffix(name, suffixAptList) || strings.HasSuffix(name, suffixAptSources)
}

// isAptPreferencesName mirrors apt's rule for preferences.d: a file with no dot
// in its name, or one ending in .pref. Backups such as .dpkg-old are ignored.
func isAptPreferencesName(name string) bool {
	if !aptPreferencesName.MatchString(name) {
		return false
	}
	return !strings.Contains(name, ".") || strings.HasSuffix(name, suffixAptPref)
}
