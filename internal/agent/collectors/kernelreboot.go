package collectors

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Kernel comparison limits (PATCH-FLOW-001 AD-002, NFR-004).
const (
	bootDirRel         = "boot"
	osReleaseRel       = "proc/sys/kernel/osrelease"
	kernelImagePrefix  = "vmlinuz-"
	bootEntryCap       = 256
	bootReadChunk      = 128
	osReleaseReadCap   = 256
	maxKernelReasonLen = 160
	dnfRestartTimeout  = 20 * time.Second
)

// kernelReleasePattern is the charset and length filter applied to every
// release string taken from a file name or /proc before it is compared or
// reported.
var kernelReleasePattern = regexp.MustCompile(`^[A-Za-z0-9.+~_-]{1,64}$`)

// rebootProbe holds the filesystem roots and process runner the reboot-required
// evaluation reads, so tests can point it at a temp tree.
type rebootProbe struct {
	root       string // "" = "/" (kernel comparison root)
	markerPath string // "" = defaultRebootRequiredPath
	exit       exitRunner
}

// RebootRequiredNow evaluates the FR-002 definition of "a reboot is required
// now" for pm ("apt" or "dnf"). A source that cannot be evaluated counts as
// not required. The updates collector and the agent's post-patch reboot
// decision both call this, so they can never disagree.
func RebootRequiredNow(ctx context.Context, pm string) bool {
	return rebootProbe{}.required(ctx, pm)
}

func (p rebootProbe) required(ctx context.Context, pm string) bool {
	switch pm {
	case "apt":
		return p.markerExists() || p.kernelNewer()
	case "dnf":
		return p.dnfNeedsRestart(ctx)
	default:
		return false
	}
}

func (p rebootProbe) markerExists() bool {
	path := p.markerPath
	if path == "" {
		path = defaultRebootRequiredPath
	}
	_, err := os.Stat(path)
	return err == nil
}

func (p rebootProbe) kernelNewer() bool {
	newer, _, _, ok := kernelNewerThanRunning(p.root)
	return ok && newer
}

func (p rebootProbe) dnfNeedsRestart(ctx context.Context) bool {
	run := p.exit
	if run == nil {
		run = defaultExitRunner
	}
	ctx, cancel := context.WithTimeout(ctx, dnfRestartTimeout)
	defer cancel()
	code, err := run(ctx, "dnf", "needs-restarting", "-r")
	return err == nil && code == 1
}

// kernelNewerThanRunning reports whether an installed kernel of the running
// kernel's flavor is newer than the running one. root "" means "/". ok is false
// when the running release cannot be read, /boot cannot be listed, or the
// entry bound is reached; callers treat that as "not required". installed is
// the newest newer release and is empty when newer is false.
func kernelNewerThanRunning(root string) (newer bool, installed, running string, ok bool) {
	if root == "" {
		root = string(filepath.Separator)
	}
	running, ok = readRunningRelease(root)
	if !ok {
		return false, "", "", false
	}
	releases, ok := listInstalledReleases(root)
	if !ok {
		return false, "", running, false
	}
	runningBase, runningFlavor := splitKernelFlavor(running)
	best, bestBase := "", runningBase
	for _, release := range releases {
		base, flavor := splitKernelFlavor(release)
		if flavor != runningFlavor {
			continue
		}
		if compareDebVersion(base, bestBase) > 0 {
			best, bestBase = release, base
		}
	}
	return best != "", best, running, true
}

func readRunningRelease(root string) (string, bool) {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(osReleaseRel)))
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, osReleaseReadCap))
	if err != nil {
		return "", false
	}
	release := strings.TrimSpace(string(data))
	return release, kernelReleasePattern.MatchString(release)
}

// listInstalledReleases returns the release suffixes of /boot/vmlinuz-*. It
// reads /boot in chunks and gives up (ok=false) once more than bootEntryCap
// entries exist, so a huge directory cannot cost unbounded reads.
func listInstalledReleases(root string) ([]string, bool) {
	dir, err := os.Open(filepath.Join(root, bootDirRel))
	if err != nil {
		return nil, false
	}
	defer dir.Close()
	var releases []string
	examined := 0
	for {
		chunk := bootReadChunk
		if room := bootEntryCap + 1 - examined; room < chunk {
			chunk = room
		}
		entries, err := dir.ReadDir(chunk)
		for _, entry := range entries {
			examined++
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), kernelImagePrefix) {
				continue
			}
			release := strings.TrimPrefix(entry.Name(), kernelImagePrefix)
			if kernelReleasePattern.MatchString(release) {
				releases = append(releases, release)
			}
		}
		if examined > bootEntryCap {
			return nil, false
		}
		if errors.Is(err, io.EOF) {
			return releases, true
		}
		if err != nil {
			return nil, false
		}
	}
}

// splitKernelFlavor separates a trailing flavor token (amd64, generic, v8) from
// a kernel release. The token after the last "-" is a flavor only when it holds
// a letter; "6.8.0-47" has no flavor, "6.8.0-47-generic" has "generic".
func splitKernelFlavor(release string) (base, flavor string) {
	i := strings.LastIndex(release, "-")
	if i < 0 {
		return release, ""
	}
	tail := release[i+1:]
	if strings.IndexFunc(tail, unicode.IsLetter) < 0 {
		return release, ""
	}
	return release[:i], tail
}

// compareDebVersion orders two Debian version strings (epoch, upstream,
// revision) with dpkg's rules and returns -1, 0 or 1. It is a port of dpkg's
// public verrevcmp algorithm. It is total: it never panics on any input and
// compares digit runs without converting them to integers, so a run of any
// length cannot overflow (PATCH-FLOW-001 C-6).
func compareDebVersion(a, b string) int {
	epochA, restA := splitEpoch(a)
	epochB, restB := splitEpoch(b)
	if c := compareDigitRuns(epochA, epochB); c != 0 {
		return c
	}
	upstreamA, revisionA := splitRevision(restA)
	upstreamB, revisionB := splitRevision(restB)
	if c := verrevcmp(upstreamA, upstreamB); c != 0 {
		return c
	}
	return verrevcmp(revisionA, revisionB)
}

// splitEpoch returns the all-digit text before the first ':' and the rest.
// Without a valid epoch it returns ("", v).
func splitEpoch(v string) (epoch, rest string) {
	i := strings.IndexByte(v, ':')
	if i <= 0 {
		return "", v
	}
	for j := 0; j < i; j++ {
		if !isDigit(v[j]) {
			return "", v
		}
	}
	return v[:i], v[i+1:]
}

// splitRevision splits at the last '-'. A version without '-' has no revision.
func splitRevision(v string) (upstream, revision string) {
	i := strings.LastIndexByte(v, '-')
	if i < 0 {
		return v, ""
	}
	return v[:i], v[i+1:]
}

// compareDigitRuns compares two all-digit strings numerically without
// overflow: strip leading zeros, then length, then lexical order.
func compareDigitRuns(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	switch {
	case len(a) != len(b):
		return sign(len(a) - len(b))
	default:
		return sign(strings.Compare(a, b))
	}
}

func verrevcmp(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for (i < len(a) && !isDigit(a[i])) || (j < len(b) && !isDigit(b[j])) {
			if c := order(byteAt(a, i)) - order(byteAt(b, j)); c != 0 {
				return sign(c)
			}
			i++
			j++
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		firstDiff := 0
		for i < len(a) && j < len(b) && isDigit(a[i]) && isDigit(b[j]) {
			if firstDiff == 0 {
				firstDiff = int(a[i]) - int(b[j])
			}
			i++
			j++
		}
		if i < len(a) && isDigit(a[i]) {
			return 1
		}
		if j < len(b) && isDigit(b[j]) {
			return -1
		}
		if firstDiff != 0 {
			return sign(firstDiff)
		}
	}
	return 0
}

// order is dpkg's character weight: '~' sorts before everything, including the
// end of the string; letters sort by value; other characters sort after letters.
func order(c byte) int {
	switch {
	case c == '~':
		return -1
	case isDigit(c) || c == 0:
		return 0
	case isLetter(c):
		return int(c)
	default:
		return int(c) + 256
	}
}

func byteAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
