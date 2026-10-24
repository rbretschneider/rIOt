package agent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// REBOOT-DPKG-001 AD-001: a read-only, unprivileged probe that tells whether a
// process holds the dpkg lock. The lock files are 0640 root:root, so the agent
// cannot open them (and must never take the lock, FR-007). It instead stat()s
// them and looks for a matching line in /proc/locks, which is world-readable.
//
// Every empty answer is verified, never assumed: the probe returns
// dpkgLockUnknown (a skipped reboot) unless the /proc it reads shows the whole
// host, and "absent" only means "this is not a dpkg host".

const (
	// dpkgProbeMaxBytes is the most /proc/locks data one check reads (NFR-003).
	dpkgProbeMaxBytes = 1 << 20
	// dpkgProbeReadBuf keeps a typical /proc/locks to one read() so the kernel's
	// lock-list lock is taken few times (INFO-1).
	dpkgProbeReadBuf = 64 << 10
	// dpkgLineMax is the longest accepted /proc/locks line.
	dpkgLineMax = 4 << 10
	// dpkgSmallReadMax caps the self/status, <pid>/status and <pid>/comm reads.
	dpkgSmallReadMax = 8 << 10
	// dpkgProbeTimeout is the wall-time budget of one check (NFR-003).
	dpkgProbeTimeout = 1 * time.Second
	// dpkgHolderNameMax is the longest holder name kept from /proc/<pid>/comm.
	dpkgHolderNameMax = 16
	// dpkgCauseMaxBytes bounds every "unknown" cause text.
	dpkgCauseMaxBytes = 200
)

// Causes an unknown lock state carries. They reach results, events and logs, so
// they are fixed text plus, at most, an error string.
const (
	causeNoLockFiles   = "dpkg is installed but its lock files are missing or hidden"
	causeNotHostPidNS  = "agent is not in the host pid namespace"
	causeLocksTooLarge = "lock information exceeds 1 MiB"
	causeProbeTooSlow  = "lock check exceeded 1s"
)

// fileID identifies a file the way /proc/locks does: device and inode.
type fileID struct {
	Major, Minor uint32
	Ino          uint64
}

// procLock is one holder line of /proc/locks that has an inode.
type procLock struct {
	PID          int64 // <= 0: the holder is not visible (OFD -1, remote/NFS negatives)
	Major, Minor uint32
	Ino          uint64
}

type dpkgLockState int

const (
	// dpkgLockAbsent: this is not a dpkg host (no lock files and no dpkg).
	dpkgLockAbsent dpkgLockState = iota
	// dpkgLockFree: the lock files exist and nothing holds them.
	dpkgLockFree
	// dpkgLockHeld: a process holds a dpkg lock.
	dpkgLockHeld
	// dpkgLockUnknown: the state cannot be determined; the reboot is skipped.
	dpkgLockUnknown
)

// dpkgLockStatus is the outcome of one lock check.
type dpkgLockStatus struct {
	State     dpkgLockState
	PID       int64  // holder pid from /proc/locks; <= 0 = not visible
	Comm      string // sanitized /proc/<pid>/comm; "" = not visible
	UID       int64  // real uid from /proc/<pid>/status; -1 = not readable
	InodeOnly bool   // the match came from the anonymous-device leniency
	Cause     string // set only for dpkgLockUnknown, <= dpkgCauseMaxBytes
}

// holderText names the holder for results, events and logs (FR-018, C-2):
//
//	held by <comm> (pid <n>[, uid <u>][, inode-only match])
//	holder not visible (pid <n>[, uid <u>][, inode-only match])
//	holder not visible[ (inode-only match)]
//
// The uid lets an operator see that a non-root process holds "the dpkg lock",
// whatever name it gave itself.
func (s dpkgLockStatus) holderText() string {
	var detail []string
	if s.PID > 0 {
		detail = append(detail, "pid "+strconv.FormatInt(s.PID, 10))
		if s.UID >= 0 {
			detail = append(detail, "uid "+strconv.FormatInt(s.UID, 10))
		}
	}
	if s.InodeOnly {
		detail = append(detail, "inode-only match")
	}
	suffix := ""
	if len(detail) > 0 {
		suffix = " (" + strings.Join(detail, ", ") + ")"
	}
	if s.PID > 0 && s.Comm != "" {
		return "held by " + s.Comm + suffix
	}
	return "holder not visible" + suffix
}

// dpkgProbeEnv carries every input of the probe, so tests run it on fixtures.
type dpkgProbeEnv struct {
	LockPaths   []string                          // /var/lib/dpkg/lock-frontend, /var/lib/dpkg/lock
	DpkgMarkers []string                          // /var/lib/dpkg/status, /usr/bin/dpkg
	StatID      func(path string) (fileID, error) // errors.Is(err, fs.ErrNotExist) = absent
	Open        func(path string) (io.ReadCloser, error)
	ProcRoot    string // "/proc": reads <root>/locks, <root>/self/status, <root>/<pid>/{comm,status}
	Now         func() time.Time
}

func dpkgUnknown(format string, args ...any) dpkgLockStatus {
	cause := models.TruncateHead(models.SanitizeText(fmt.Sprintf(format, args...)), dpkgCauseMaxBytes)
	return dpkgLockStatus{State: dpkgLockUnknown, UID: -1, Cause: cause}
}

// probeDpkgLock performs one lock check (AD-001). It never opens a lock file.
func probeDpkgLock(env dpkgProbeEnv) dpkgLockStatus {
	start := env.Now()
	expired := func() bool { return env.Now().Sub(start) > dpkgProbeTimeout }

	// Lock files: stat only.
	var targets []fileID
	for _, path := range env.LockPaths {
		id, err := env.StatID(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return dpkgUnknown("cannot stat %s: %v", path, err)
		}
		targets = append(targets, id)
	}

	// No lock file: only a host without dpkg reads as absent (C-1 b).
	if len(targets) == 0 {
		for _, marker := range env.DpkgMarkers {
			_, err := env.StatID(marker)
			if err == nil {
				return dpkgUnknown("%s", causeNoLockFiles)
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return dpkgUnknown("cannot stat %s: %v", marker, err)
			}
		}
		return dpkgLockStatus{State: dpkgLockAbsent, UID: -1}
	}

	// The /proc we read must show the whole host's locks (C-1 a): the kernel
	// omits locks whose holder has no pid in the reader's namespace.
	selfStatus, err := readSmallFile(env, env.ProcRoot+"/self/status")
	if err != nil {
		return dpkgUnknown("cannot verify the pid namespace: %v", err)
	}
	levels, ok := parseNSpid(selfStatus)
	if !ok {
		return dpkgUnknown("cannot verify the pid namespace: no NSpid")
	}
	if levels != 1 {
		return dpkgUnknown("%s", causeNotHostPidNS)
	}

	// /proc/locks, capped at the reader (C-5).
	f, err := env.Open(env.ProcRoot + "/locks")
	if err != nil {
		return dpkgUnknown("cannot read /proc/locks: %v", err)
	}
	defer f.Close()

	var consumed int
	scanner := bufio.NewScanner(bufio.NewReaderSize(io.LimitReader(f, dpkgProbeMaxBytes+1), dpkgProbeReadBuf))
	scanner.Buffer(make([]byte, 0, dpkgLineMax), dpkgLineMax)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		advance, token, err := bufio.ScanLines(data, atEOF)
		consumed += advance
		return advance, token, err
	})

	var (
		match       procLock
		matchedOnly bool
		matched     bool
		lineNo      int
	)
	for scanner.Scan() {
		lineNo++
		if consumed > dpkgProbeMaxBytes {
			return dpkgUnknown("%s", causeLocksTooLarge)
		}
		if expired() {
			return dpkgUnknown("%s", causeProbeTooSlow)
		}
		lock, skip, perr := parseProcLocksLine(scanner.Text())
		if perr != nil {
			return dpkgUnknown("unrecognized /proc/locks line %d", lineNo)
		}
		if skip || matched {
			continue
		}
		for _, target := range targets {
			if hit, inodeOnly := lockMatches(lock, target); hit {
				match, matchedOnly, matched = lock, inodeOnly, true
				break
			}
		}
	}
	if consumed > dpkgProbeMaxBytes {
		return dpkgUnknown("%s", causeLocksTooLarge)
	}
	if serr := scanner.Err(); serr != nil {
		return dpkgUnknown("cannot read /proc/locks: %v", serr)
	}
	if expired() {
		return dpkgUnknown("%s", causeProbeTooSlow)
	}
	if !matched {
		return dpkgLockStatus{State: dpkgLockFree, UID: -1}
	}

	held := dpkgLockStatus{State: dpkgLockHeld, PID: match.PID, UID: -1, InodeOnly: matchedOnly}
	if match.PID > 0 {
		// The path comes from the parsed integer only, never from the raw token (C-5).
		pidDir := env.ProcRoot + "/" + strconv.FormatInt(match.PID, 10)
		if comm, cerr := readSmallFile(env, pidDir+"/comm"); cerr == nil {
			held.Comm = sanitizeHolderName([]byte(comm))
		}
		if status, serr := readSmallFile(env, pidDir+"/status"); serr == nil {
			if uid, ok := parseStatusUID(status); ok {
				held.UID = uid
			}
		}
		if expired() {
			return dpkgUnknown("%s", causeProbeTooSlow)
		}
	}
	return held
}

// readSmallFile reads at most dpkgSmallReadMax bytes of path. When the file is
// longer, the text is cut back to its last complete line so that a parser never
// sees a half line (a cut NSpid line would otherwise read as fewer levels).
func readSmallFile(env dpkgProbeEnv, path string) (string, error) {
	f, err := env.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, dpkgSmallReadMax+1))
	if err != nil {
		return "", err
	}
	if len(data) > dpkgSmallReadMax {
		data = data[:dpkgSmallReadMax]
		if i := strings.LastIndexByte(string(data), '\n'); i >= 0 {
			data = data[:i+1]
		} else {
			data = nil
		}
	}
	return string(data), nil
}

// parseProcLocksLine parses one /proc/locks line. skip is true for a blocked
// waiter (it does not hold the lock) and for a lock with no inode. Anything the
// parser does not recognize is an error: the state is then unknown (FR-010).
//
//	1: POSIX  ADVISORY  WRITE 803049 00:16d:3947933 0 EOF
//	3: -> POSIX  ADVISORY  WRITE 803049 00:16d:3947933 0 EOF
func parseProcLocksLine(line string) (procLock, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return procLock{}, false, errors.New("empty line")
	}
	id := fields[0]
	if len(id) < 2 || id[len(id)-1] != ':' || !isDecimal(id[:len(id)-1]) {
		return procLock{}, false, errors.New("no lock id")
	}
	rest := fields[1:]
	for _, token := range rest {
		if token == "->" {
			return procLock{}, true, nil
		}
	}
	// TYPE MODE ACCESS PID DEV START END
	if len(rest) < 7 {
		return procLock{}, false, errors.New("too few fields")
	}
	pid, err := strconv.ParseInt(rest[3], 10, 64)
	if err != nil {
		return procLock{}, false, errors.New("bad pid")
	}
	dev := rest[4]
	if dev == "<none>:0" {
		return procLock{}, true, nil
	}
	parts := strings.Split(dev, ":")
	if len(parts) != 3 {
		return procLock{}, false, errors.New("bad device")
	}
	major, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return procLock{}, false, errors.New("bad major")
	}
	minor, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return procLock{}, false, errors.New("bad minor")
	}
	ino, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return procLock{}, false, errors.New("bad inode")
	}
	return procLock{PID: pid, Major: uint32(major), Minor: uint32(minor), Ino: ino}, false, nil
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// lockMatches reports whether l is a lock on target t. The match is exact on
// device and inode. /proc/locks prints the superblock device, while stat()
// returns an anonymous device on btrfs and some overlay or tmpfs roots; when both
// majors are 0 the inode alone decides, and the match is marked inode-only.
func lockMatches(l procLock, t fileID) (match, inodeOnly bool) {
	if l.Ino != t.Ino {
		return false, false
	}
	if l.Major == t.Major && l.Minor == t.Minor {
		return true, false
	}
	if t.Major == 0 && l.Major == 0 {
		return true, true
	}
	return false, false
}

// parseNSpid returns the number of pid-namespace levels on the NSpid line of a
// /proc/<pid>/status text. One level is the initial pid namespace.
func parseNSpid(statusText string) (levels int, ok bool) {
	for _, line := range strings.Split(statusText, "\n") {
		if !strings.HasPrefix(line, "NSpid:") {
			continue
		}
		n := len(strings.Fields(strings.TrimPrefix(line, "NSpid:")))
		return n, n > 0
	}
	return 0, false
}

// parseStatusUID returns the real uid, the first value of the Uid line.
func parseStatusUID(statusText string) (uid int64, ok bool) {
	for _, line := range strings.Split(statusText, "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
		if len(fields) == 0 {
			return 0, false
		}
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// sanitizeHolderName makes a /proc/<pid>/comm value safe to forward. comm is
// chosen by the process (prctl PR_SET_NAME), and warning events reach notifiers
// that do not escape, so only [A-Za-z0-9._+:-] survives; every other byte
// becomes '?'. At most dpkgHolderNameMax bytes are kept.
func sanitizeHolderName(b []byte) string {
	end := len(b)
	for end > 0 && (b[end-1] == '\n' || b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r') {
		end--
	}
	b = b[:end]
	if len(b) > dpkgHolderNameMax {
		b = b[:dpkgHolderNameMax]
	}
	out := make([]byte, len(b))
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '+', c == ':', c == '-':
			out[i] = c
		default:
			out[i] = '?'
		}
	}
	return string(out)
}

// decodeLinuxDev splits a stat() st_dev into major and minor like glibc's
// gnu_dev_major and gnu_dev_minor.
func decodeLinuxDev(dev uint64) (major, minor uint32) {
	major = uint32(((dev >> 8) & 0xfff) | ((dev >> 32) &^ 0xfff))
	minor = uint32((dev & 0xff) | ((dev >> 12) &^ 0xff))
	return major, minor
}

// probeDpkg runs one lock check through the test seam or the platform probe.
func (a *Agent) probeDpkg() dpkgLockStatus {
	if a.dpkgLockProbe != nil {
		return a.dpkgLockProbe()
	}
	return platformDpkgProbe(a.clock().Now)
}
