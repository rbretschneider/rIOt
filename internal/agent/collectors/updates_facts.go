package collectors

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Read-only facts reported beside the update counts: operator holds, and a
// pending OS release upgrade. Nothing here needs sudo, and nothing here
// changes how patching or rebooting behaves.

const (
	// maxFactPackages caps the package names reported in OtherHolds.
	maxFactPackages = 50
	// maxFactNameLen caps a single reported name.
	maxFactNameLen = 128

	releaseUpgradeFile     = "/var/lib/ubuntu-release-upgrader/release-upgrade-available"
	releaseUpgradeBinary   = "/usr/bin/do-release-upgrade"
	releaseCheckTimeout    = 20 * time.Second
	releaseCheckCacheTTL   = 12 * time.Hour
	osReleasePath          = "/etc/os-release"
	maxReleaseNameLen      = 64
	dnfVersionlockListPath = "/etc/dnf/plugins/versionlock.list"
)

// Fixed dnf configuration locations that can carry excludes. Globs expand
// to files only inside these directories.
var (
	dnfExcludeFiles = []string{"/etc/dnf/dnf.conf", "/etc/yum.conf"}
	dnfExcludeGlobs = []string{"/etc/dnf/libdnf5.conf.d/*.conf", "/etc/yum.repos.d/*.repo"}
)

// factNameRe is the conservative charset for a reported package name or
// pattern. It is a superset of ValidPackageName that also allows the glob
// characters dnf accepts in excludes.
var factNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:~_*?-]*$`)

// releaseNameRe extracts the release label from "New release '24.04.1 LTS' available."
var releaseNameRe = regexp.MustCompile(`New release '([^']{1,64})' available`)

// releaseNameCharsRe is the charset allowed in a reported release label.
var releaseNameCharsRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 .()_-]*$`)

// parseDpkgHolds returns the package names whose selection state is "hold"
// in `dpkg --get-selections` output (arch qualifiers stripped).
func parseDpkgHolds(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "hold" {
			continue
		}
		name := fields[0]
		if idx := strings.Index(name, ":"); idx > 0 {
			name = name[:idx]
		}
		names = append(names, name)
	}
	return names
}

// parseDnfExcludes returns the patterns listed on exclude= and excludepkgs=
// lines of a dnf/yum configuration file.
func parseDnfExcludes(content string) []string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "exclude", "excludepkgs":
			out = append(out, strings.FieldsFunc(value, func(r rune) bool {
				return r == ',' || r == ' ' || r == '\t'
			})...)
		}
	}
	return out
}

// versionlockNameRe matches "name-epoch:version-release.arch" entries.
var versionlockNameRe = regexp.MustCompile(`^(.+?)-[0-9]+:`)

// parseVersionlockList returns the package names locked in a dnf
// versionlock.list file.
func parseVersionlockList(content string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		if m := versionlockNameRe.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// boundFactNames filters names to the allowed charset and length, removes
// duplicates and any name in managed, sorts, and keeps at most
// maxFactPackages. total counts all valid names before the cap.
func boundFactNames(names []string, managed []string) (kept []string, total int) {
	skip := make(map[string]bool, len(managed))
	for _, m := range managed {
		skip[m] = true
	}
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if len(n) > maxFactNameLen || !factNameRe.MatchString(n) || skip[n] || seen[n] {
			continue
		}
		seen[n] = true
		kept = append(kept, n)
	}
	sort.Strings(kept)
	total = len(kept)
	if len(kept) > maxFactPackages {
		kept = kept[:maxFactPackages]
	}
	return kept, total
}

// aptOtherHolds builds OtherHolds from dpkg selections output. managed are
// the holds rIOt placed; a read error on the managed list is passed as
// managedErr and makes the result unavailable rather than guessing.
func aptOtherHolds(selections string, managed []string, managedErr error) *models.OtherHolds {
	if managedErr != nil {
		return &models.OtherHolds{Status: models.FactStatusUnavailable}
	}
	kept, total := boundFactNames(parseDpkgHolds(selections), managed)
	return &models.OtherHolds{Status: models.FactStatusOK, Packages: kept, Total: total}
}

// factFS is the file access used by the dnf scan, injectable for tests.
type factFS struct {
	ReadFile func(path string) ([]byte, error)
	Glob     func(pattern string) ([]string, error)
}

var osFactFS = factFS{ReadFile: os.ReadFile, Glob: filepath.Glob}

// dnfOtherHolds scans the fixed dnf exclude locations, skipping the rIOt
// fragment. A file that exists but cannot be read makes the result
// unavailable; a missing file is skipped.
func dnfOtherHolds(fsys factFS, managed []string) *models.OtherHolds {
	unavailable := &models.OtherHolds{Status: models.FactStatusUnavailable}
	files := append([]string{}, dnfExcludeFiles...)
	for _, pattern := range dnfExcludeGlobs {
		matches, err := fsys.Glob(pattern)
		if err != nil {
			return unavailable
		}
		sort.Strings(matches)
		files = append(files, matches...)
	}

	var names []string
	read := func(path string) (string, bool) {
		data, err := fsys.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return "", true
		}
		if err != nil {
			return "", false
		}
		return string(data), true
	}
	for _, path := range files {
		if path == DNFFragmentPath {
			continue
		}
		content, ok := read(path)
		if !ok {
			return unavailable
		}
		names = append(names, parseDnfExcludes(content)...)
	}
	content, ok := read(dnfVersionlockListPath)
	if !ok {
		return unavailable
	}
	names = append(names, parseVersionlockList(content)...)

	kept, total := boundFactNames(names, managed)
	return &models.OtherHolds{Status: models.FactStatusOK, Packages: kept, Total: total}
}

// releaseCheckRunner runs the OS release check and returns its exit code.
// err is non-nil only when the command could not run to completion
// (missing binary, timeout).
type releaseCheckRunner func(ctx context.Context) (exitCode int, output string, err error)

// releaseUpgradeChecker reports a pending Ubuntu release upgrade. It reads
// the marker file the OS maintains, and otherwise runs the OS's own check
// at most once per releaseCheckCacheTTL.
type releaseUpgradeChecker struct {
	mu       sync.Mutex
	now      func() time.Time
	readFile func(path string) ([]byte, error)
	run      releaseCheckRunner

	cached   *models.ReleaseUpgrade
	cachedAt time.Time
}

func newReleaseUpgradeChecker() *releaseUpgradeChecker {
	return &releaseUpgradeChecker{now: time.Now, readFile: os.ReadFile, run: runReleaseCheck}
}

// defaultReleaseChecker is shared by all collector passes so the cache holds.
var defaultReleaseChecker = newReleaseUpgradeChecker()

// isUbuntu reports whether /etc/os-release identifies Ubuntu.
func (c *releaseUpgradeChecker) isUbuntu() bool {
	data, err := c.readFile(osReleasePath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == "ID" {
			return strings.Trim(v, `"'`) == "ubuntu"
		}
	}
	return false
}

// Check returns the release-upgrade fact for this host. Non-Ubuntu hosts
// report "unavailable" because no equivalent check exists.
func (c *releaseUpgradeChecker) Check(ctx context.Context) *models.ReleaseUpgrade {
	c.mu.Lock()
	defer c.mu.Unlock()

	unavailable := &models.ReleaseUpgrade{Status: models.FactStatusUnavailable}
	if !c.isUbuntu() {
		return unavailable
	}

	if data, err := c.readFile(releaseUpgradeFile); err == nil {
		return &models.ReleaseUpgrade{
			Status: models.FactStatusAvailable,
			Name:   parseReleaseName(string(data)),
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return unavailable
	}

	if c.cached != nil && c.now().Sub(c.cachedAt) < releaseCheckCacheTTL {
		cp := *c.cached
		return &cp
	}

	runCtx, cancel := context.WithTimeout(ctx, releaseCheckTimeout)
	defer cancel()
	code, output, err := c.run(runCtx)
	result := classifyReleaseCheck(code, output, err)
	c.cached, c.cachedAt = result, c.now()
	cp := *result
	return &cp
}

// classifyReleaseCheck maps `do-release-upgrade -c` results: exit 0 means a
// new release is available, exit 1 means none, anything else is unknown.
func classifyReleaseCheck(code int, output string, err error) *models.ReleaseUpgrade {
	if err != nil {
		return &models.ReleaseUpgrade{Status: models.FactStatusUnavailable}
	}
	switch code {
	case 0:
		return &models.ReleaseUpgrade{Status: models.FactStatusAvailable, Name: parseReleaseName(output)}
	case 1:
		return &models.ReleaseUpgrade{Status: models.FactStatusNone}
	}
	return &models.ReleaseUpgrade{Status: models.FactStatusUnavailable}
}

// parseReleaseName extracts a bounded, charset-checked release label, or "".
func parseReleaseName(text string) string {
	m := releaseNameRe.FindStringSubmatch(text)
	if m == nil || len(m[1]) > maxReleaseNameLen || !releaseNameCharsRe.MatchString(m[1]) {
		return ""
	}
	return m[1]
}

// releaseCheckEnv is the fixed environment for the release check.
var releaseCheckEnv = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
