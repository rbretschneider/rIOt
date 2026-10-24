package agent

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// REBOOT-DPKG-001 AD-001: the /proc/locks probe, tested on fixtures and injected
// readers. No test in this file reads the real /proc or touches a real lock.

const (
	lockFrontendPath = "/var/lib/dpkg/lock-frontend"
	lockLegacyPath   = "/var/lib/dpkg/lock"
	// Hawaii (Debian 13, ext4 on NVMe): stat reports dev 66306 = 259:2.
	hawaiiFrontendIno = 262338
	hawaiiLegacyIno   = 262339
)

var (
	hawaiiFrontend = fileID{Major: 259, Minor: 2, Ino: hawaiiFrontendIno}
	hawaiiLegacy   = fileID{Major: 259, Minor: 2, Ino: hawaiiLegacyIno}
)

// hawaiiFrontendLine is /proc/locks as the kernel prints a POSIX write lock on
// lock-frontend held by pid 1234 (major and minor in hex, inode in decimal).
const hawaiiFrontendLine = "3: POSIX  ADVISORY  WRITE 1234 103:02:262338 0 EOF"

// fakeDpkgHost is an in-memory host for probeDpkgLock. It records every path the
// probe opens, and its clock advances by tick on every Now call.
type fakeDpkgHost struct {
	stats    map[string]fileID
	statErrs map[string]error
	files    map[string]string
	openErrs map[string]error
	readers  map[string]func() io.Reader
	opened   []string
	onOpen   func(path string)

	clock time.Time
	tick  time.Duration
}

func newFakeDpkgHost() *fakeDpkgHost {
	h := &fakeDpkgHost{
		stats: map[string]fileID{
			lockFrontendPath:       hawaiiFrontend,
			lockLegacyPath:         hawaiiLegacy,
			"/var/lib/dpkg/status": {Major: 259, Minor: 2, Ino: 1},
			"/usr/bin/dpkg":        {Major: 259, Minor: 2, Ino: 2},
		},
		statErrs: map[string]error{},
		files: map[string]string{
			"/proc/self/status": "Name:\triot-agent\nUid:\t999\t999\t999\t999\nNSpid:\t4242\n",
			"/proc/locks":       "",
		},
		openErrs: map[string]error{},
		readers:  map[string]func() io.Reader{},
		clock:    time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC),
	}
	return h
}

func (h *fakeDpkgHost) env() dpkgProbeEnv {
	return dpkgProbeEnv{
		LockPaths:   []string{lockFrontendPath, lockLegacyPath},
		DpkgMarkers: []string{"/var/lib/dpkg/status", "/usr/bin/dpkg"},
		StatID: func(path string) (fileID, error) {
			if err := h.statErrs[path]; err != nil {
				return fileID{}, err
			}
			if id, ok := h.stats[path]; ok {
				return id, nil
			}
			return fileID{}, &fs.PathError{Op: "stat", Path: path, Err: fs.ErrNotExist}
		},
		Open: func(path string) (io.ReadCloser, error) {
			h.opened = append(h.opened, path)
			if h.onOpen != nil {
				h.onOpen(path)
			}
			if err := h.openErrs[path]; err != nil {
				return nil, err
			}
			if mk, ok := h.readers[path]; ok {
				return io.NopCloser(mk()), nil
			}
			if content, ok := h.files[path]; ok {
				return io.NopCloser(strings.NewReader(content)), nil
			}
			return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
		},
		ProcRoot: "/proc",
		Now: func() time.Time {
			now := h.clock
			h.clock = h.clock.Add(h.tick)
			return now
		},
	}
}

func (h *fakeDpkgHost) setLocks(lines ...string) {
	if len(lines) == 0 {
		h.files["/proc/locks"] = ""
		return
	}
	h.files["/proc/locks"] = strings.Join(lines, "\n") + "\n"
}

func (h *fakeDpkgHost) setHolder(pid int, comm string, uid int) {
	dir := fmt.Sprintf("/proc/%d", pid)
	h.files[dir+"/comm"] = comm + "\n"
	h.files[dir+"/status"] = fmt.Sprintf("Name:\t%s\nUid:\t%d\t%d\t%d\t%d\n", comm, uid, uid, uid, uid)
}

func (h *fakeDpkgHost) probe() dpkgLockStatus { return probeDpkgLock(h.env()) }

// ---------------------------------------------------------------------------
// AC-002: /proc/locks matching
// ---------------------------------------------------------------------------

func TestProcLocks_RD_AC002_PosixWriteLineMatchesLockFrontend(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine)
	h.setHolder(1234, "unattended-upgr", 0)

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-002] a POSIX write lock on the lock-frontend inode must read as held")
	assert.Equal(t, int64(1234), got.PID, "[AC-002] the holder pid comes from the matching line")
	assert.False(t, got.InodeOnly, "[AC-002] an exact device match is not an inode-only match")
}

func TestProcLocks_RD_AC002_HexMajorMinorDecimalInode(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		major uint32
		minor uint32
		ino   uint64
	}{
		{"btrfs-style anonymous device with a hex minor", "1: POSIX  ADVISORY  WRITE 803049 00:16d:3947933 0 EOF", 0, 0x16d, 3947933},
		{"ext4 on NVMe 259:2", hawaiiFrontendLine, 259, 2, 262338},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lock, skip, err := parseProcLocksLine(tc.line)

			require.NoError(t, err, "[AC-002] the line must parse")
			assert.False(t, skip, "[AC-002] a held lock with an inode is not skipped")
			assert.Equal(t, tc.major, lock.Major, "[AC-002] major is hexadecimal")
			assert.Equal(t, tc.minor, lock.Minor, "[AC-002] minor is hexadecimal")
			assert.Equal(t, tc.ino, lock.Ino, "[AC-002] the inode is decimal")
		})
	}
}

func TestProcLocks_RD_AC002_BlockDeviceRequiresExactDevice(t *testing.T) {
	target := hawaiiFrontend

	t.Run("same inode on another block device is not the lock", func(t *testing.T) {
		match, _ := lockMatches(procLock{Major: 8, Minor: 1, Ino: target.Ino}, target)
		assert.False(t, match, "[AC-002] a block-device filesystem must match on device and inode")
	})
	t.Run("a block-device lock is not matched by an anonymous-device line", func(t *testing.T) {
		match, _ := lockMatches(procLock{Major: 0, Minor: 0x2e, Ino: target.Ino}, target)
		assert.False(t, match, "[AC-002] the inode-only leniency needs major 0 on both sides")
	})
	t.Run("the exact device and inode match", func(t *testing.T) {
		match, inodeOnly := lockMatches(procLock{Major: 259, Minor: 2, Ino: target.Ino}, target)
		assert.True(t, match, "[AC-002] exact match")
		assert.False(t, inodeOnly, "[AC-002] exact match is not inode-only")
	})
	t.Run("another inode on the same device is not the lock", func(t *testing.T) {
		match, _ := lockMatches(procLock{Major: 259, Minor: 2, Ino: target.Ino + 1}, target)
		assert.False(t, match, "[AC-002] the inode must match")
	})
}

// The btrfs line below is synthetic: only Hawaii (ext4) has been sampled on real
// hardware (security review C-6). The inode-only path is unverified there.
func TestProcLocks_RD_AC002_AnonymousDeviceMatchesOnInodeAndMarksInodeOnly(t *testing.T) {
	h := newFakeDpkgHost()
	h.stats[lockFrontendPath] = fileID{Major: 0, Minor: 0x2e, Ino: 262338} // stat on btrfs: anonymous dev
	h.stats[lockLegacyPath] = fileID{Major: 0, Minor: 0x2e, Ino: 262339}
	h.setLocks("1: POSIX  ADVISORY  WRITE 812 00:16d:262338 0 EOF")
	h.setHolder(812, "apt-get", 0)

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-002] major-0 on both sides must match on the inode alone")
	assert.True(t, got.InodeOnly, "[AC-002] the lenient match must be marked inode-only")
	assert.Equal(t, "held by apt-get (pid 812, uid 0, inode-only match)", got.holderText(), "[AC-002] the report states that the match is inode-only")
}

func TestProcLocks_RD_AC002_KernelVariantFixtures(t *testing.T) {
	cases := []struct {
		name string
		line string
		skip bool
		pid  int64
	}{
		{"POSIX write", "1: POSIX  ADVISORY  WRITE 803049 00:16d:3947933 0 EOF", false, 803049},
		{"ACCESS type", "2: ACCESS ADVISORY  READ  12 08:01:55 0 EOF", false, 12},
		{"NOINODE has no device", "3: POSIX  *NOINODE* WRITE 12 <none>:0 0 EOF", true, 0},
		{"FLOCK MSNFS", "4: FLOCK  MSNFS     WRITE 12 08:01:77 0 EOF", false, 12},
		{"LEASE BREAKING", "5: LEASE  BREAKING  READ  12 08:01:78 0 EOF", false, 12},
		{"LEASE BREAKER has no device", "6: LEASE  BREAKER   NONE  12 <none>:0 0 EOF", true, 0},
		{"UNKNOWN UNKNOWN", "7: UNKNOWN UNKNOWN  READ  12 08:01:79 0 EOF", false, 12},
		{"DELEG ACTIVE", "8: DELEG  ACTIVE   WRITE 12 08:01:83 0 EOF", false, 12},
		{"waiter", "9: -> POSIX  ADVISORY  WRITE 12 08:01:80 0 EOF", true, 0},
		{"nested indented waiter", " 10:  -> POSIX  ADVISORY  WRITE 12 08:01:81 0 EOF", true, 0},
		{"OFD lock has pid -1", "11: OFDLCK ADVISORY  WRITE -1 08:01:82 0 EOF", false, -1},
		{"negative remote pid", "12: POSIX  ADVISORY  READ  -2 00:2e:5 0 100", false, -2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lock, skip, err := parseProcLocksLine(tc.line)

			require.NoError(t, err, "[AC-002] a line the kernel is known to print must parse, never read as unknown")
			assert.Equal(t, tc.skip, skip, "[AC-002] skip decision")
			if !tc.skip {
				assert.Equal(t, tc.pid, lock.PID, "[AC-002] pid")
			}
		})
	}
}

func TestProcLocks_RD_AC002_WaiterDoesNotHoldTheLock(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks("1: -> POSIX  ADVISORY  WRITE 99 103:02:262338 0 EOF")

	got := h.probe()

	assert.Equal(t, dpkgLockFree, got.State, "[AC-002] a blocked waiter on the lock file does not hold it")
}

func TestDecodeLinuxDev_RD_AC002_HawaiiDevIs259_2(t *testing.T) {
	major, minor := decodeLinuxDev(66306)

	assert.Equal(t, uint32(259), major, "[AC-002] Hawaii's st_dev 66306 is major 0x103")
	assert.Equal(t, uint32(2), minor, "[AC-002] Hawaii's st_dev 66306 is minor 2")
}

func TestDecodeLinuxDev_RD_AC002_LargeDeviceNumbers(t *testing.T) {
	// glibc makedev(0x12345, 0x67890): the high bits of the major sit above bit 32
	// and the high bits of the minor above bit 12.
	const wantMajor, wantMinor = uint64(0x12345), uint64(0x67890)
	dev := (wantMajor&0xfffff000)<<32 | (wantMajor&0xfff)<<8 | (wantMinor&0xffffff00)<<12 | (wantMinor & 0xff)
	major, minor := decodeLinuxDev(dev)

	assert.Equal(t, uint32(wantMajor), major, "[AC-002] major from its low and high fields")
	assert.Equal(t, uint32(wantMinor), minor, "[AC-002] minor from its low and high fields")
}

// ---------------------------------------------------------------------------
// AC-003: the legacy lock alone also counts
// ---------------------------------------------------------------------------

func TestProcLocks_RD_AC003_LegacyLockAloneIsHeld(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks("2: POSIX  ADVISORY  WRITE 55 103:02:262339 0 EOF") // lock, not lock-frontend
	h.setHolder(55, "dpkg", 0)

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-003] a lock on /var/lib/dpkg/lock alone must read as held")
	assert.Equal(t, "held by dpkg (pid 55, uid 0)", got.holderText(), "[AC-003] the holder is named")
}

func TestDpkgProbe_RD_AC003_FrontendAbsentLegacyPresentStillChecked(t *testing.T) {
	h := newFakeDpkgHost()
	delete(h.stats, lockFrontendPath) // dpkg older than 1.19.1 has no lock-frontend
	h.setLocks("2: POSIX  ADVISORY  WRITE 55 103:02:262339 0 EOF")

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-003] a missing lock-frontend must not stop the legacy lock from being checked")
}

// ---------------------------------------------------------------------------
// AC-006 / AC-007 / C-2: naming the holder
// ---------------------------------------------------------------------------

func TestDpkgProbe_RD_AC006_HolderNamedFromCommWithUid(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine)
	h.setHolder(1234, "unattended-upgr", 0)

	got := h.probe()

	assert.Equal(t, "unattended-upgr", got.Comm, "[AC-006] comm of the holder")
	assert.Equal(t, int64(0), got.UID, "[AC-006] the real uid of the holder")
	assert.Equal(t, "held by unattended-upgr (pid 1234, uid 0)", got.holderText(), "[AC-006] FR-018 text")
}

func TestDpkgProbe_RD_AC006_CommPathFromParsedPidOnly(t *testing.T) {
	t.Run("a positive pid reads exactly its comm and status", func(t *testing.T) {
		h := newFakeDpkgHost()
		h.setLocks(hawaiiFrontendLine)
		h.setHolder(1234, "dpkg", 0)

		h.probe()

		assert.Contains(t, h.opened, "/proc/1234/comm", "[AC-006] comm is read for the parsed pid")
		assert.Contains(t, h.opened, "/proc/1234/status", "[AC-006] status is read for the parsed pid")
	})
	for _, pid := range []string{"-1", "-5", "0"} {
		t.Run("pid "+pid+" reads no holder file", func(t *testing.T) {
			h := newFakeDpkgHost()
			h.setLocks("3: POSIX  ADVISORY  WRITE " + pid + " 103:02:262338 0 EOF")

			h.probe()

			for _, p := range h.opened {
				assert.NotContains(t, p, "/comm", "[AC-006] no comm path may be built for pid %s", pid)
				assert.NotRegexp(t, `^/proc/-?\d+/status$`, p, "[AC-006] no status path may be built for pid %s", pid)
			}
		})
	}
}

func TestDpkgProbe_RD_AC007_PidMinusOneHolderNotVisible(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks("3: OFDLCK ADVISORY  WRITE -1 103:02:262338 0 EOF")

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-007] an OFD lock still holds the lock")
	assert.Equal(t, "holder not visible", got.holderText(), "[AC-007] pid -1 is not a visible holder")
}

func TestDpkgProbe_RD_AC007_NegativeRemotePidHolderNotVisible(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks("3: POSIX  ADVISORY  WRITE -7 103:02:262338 0 EOF")

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-007] a remote holder still holds the lock")
	assert.Equal(t, "holder not visible", got.holderText(), "[AC-007] a negative pid is not a visible holder")
}

func TestDpkgProbe_RD_AC007_CommUnreadableSaysHolderNotVisibleWithPid(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine)
	h.files["/proc/1234/status"] = "Name:\tx\nUid:\t0\t0\t0\t0\n"
	// no /proc/1234/comm

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-007] an unreadable comm is not an unknown lock state")
	assert.Equal(t, "holder not visible (pid 1234, uid 0)", got.holderText(), "[AC-007] pid and uid stay, the name is dropped")
}

func TestDpkgProbe_RD_AC007_StatusUnreadableOmitsUid(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine)
	h.files["/proc/1234/comm"] = "apt\n"
	// no /proc/1234/status

	got := h.probe()

	assert.Equal(t, dpkgLockHeld, got.State, "[AC-007] an unreadable status is not an unknown lock state")
	assert.Equal(t, int64(-1), got.UID, "[AC-007] an unreadable uid is -1")
	assert.Equal(t, "held by apt (pid 1234)", got.holderText(), "[AC-007] the uid is omitted when it cannot be read")
}

func TestDpkgProbe_RD_C2_HolderTextCarriesUidAndInodeOnly(t *testing.T) {
	cases := []struct {
		name string
		st   dpkgLockStatus
		want string
	}{
		{"named holder", dpkgLockStatus{State: dpkgLockHeld, PID: 1234, Comm: "unattended-upgr", UID: 0}, "held by unattended-upgr (pid 1234, uid 0)"},
		{"named holder, inode-only", dpkgLockStatus{State: dpkgLockHeld, PID: 1234, Comm: "unattended-upgr", UID: 0, InodeOnly: true}, "held by unattended-upgr (pid 1234, uid 0, inode-only match)"},
		{"named holder, no uid", dpkgLockStatus{State: dpkgLockHeld, PID: 1234, Comm: "apt", UID: -1}, "held by apt (pid 1234)"},
		{"comm unreadable", dpkgLockStatus{State: dpkgLockHeld, PID: 812, UID: 1000, InodeOnly: true}, "holder not visible (pid 812, uid 1000, inode-only match)"},
		{"pid not visible", dpkgLockStatus{State: dpkgLockHeld, PID: -1, UID: -1}, "holder not visible"},
		{"pid 0, inode-only", dpkgLockStatus{State: dpkgLockHeld, PID: 0, UID: -1, InodeOnly: true}, "holder not visible (inode-only match)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.st.holderText(), "[C-2] holder text grammar")
		})
	}
}

func TestDpkgProbe_RD_C2_NonRootHolderUidReported(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine)
	h.files["/proc/1234/comm"] = "unattended-upgr\n" // attacker-chosen name
	h.files["/proc/1234/status"] = "Name:\tunattended-upgr\nUid:\t1000\t1000\t1000\t1000\n"

	got := h.probe()

	assert.Equal(t, int64(1000), got.UID, "[C-2] the real uid is the first Uid value")
	assert.Contains(t, got.holderText(), "uid 1000", "[C-2] a non-root holder of the dpkg lock must stand out whatever its name")
}

func TestParseStatusUID_RD_C2_ParsesRealUidOnly(t *testing.T) {
	uid, ok := parseStatusUID("Name:\tx\nUid:\t1000\t0\t0\t0\nGid:\t5\n")
	assert.True(t, ok, "[C-2] a Uid line parses")
	assert.Equal(t, int64(1000), uid, "[C-2] the real uid, not the effective one")

	_, ok = parseStatusUID("Name:\tx\n")
	assert.False(t, ok, "[C-2] no Uid line is not ok")
	_, ok = parseStatusUID("Uid:\tabc\t0\t0\t0\n")
	assert.False(t, ok, "[C-2] a non-numeric uid is not ok")
}

func TestSanitizeHolderName_RD_C5_CharsetAndLength(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"markdown link", "[ok](http://a.b)", "?ok??http:??a.b?"},
		{"allowed punctuation survives", "a.b_c+d:e-f", "a.b_c+d:e-f"},
		{"slash and star", "a/b*c", "a?b?c"},
		{"spaces", "a b", "a?b"},
		{"control bytes", "a\x01b\x1b[0m", "a?b??0m"},
		{"non-ASCII bytes", "caf\xc3\xa9", "caf??"},
		{"trailing newline is trimmed", "dpkg\n", "dpkg"},
		{"kept to 16 bytes", "abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeHolderName([]byte(tc.in))
			assert.Equal(t, tc.want, got, "[C-5] holder name sanitization")
			assert.LessOrEqual(t, len(got), dpkgHolderNameMax, "[C-5] at most 16 bytes")
		})
	}
}

// ---------------------------------------------------------------------------
// AC-010: unknown lock state fails closed
// ---------------------------------------------------------------------------

func TestDpkgProbe_RD_AC010_StatErrorOtherThanNotExistIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.statErrs[lockFrontendPath] = &fs.PathError{Op: "stat", Path: lockFrontendPath, Err: fs.ErrPermission}

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-010] a stat failure other than not-exist must not read as free or absent")
	assert.Contains(t, got.Cause, "cannot stat "+lockFrontendPath, "[AC-010] the cause names the path")
}

func TestDpkgProbe_RD_AC010_ProcLocksMissingIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	delete(h.files, "/proc/locks") // ProcSubset=pid hides /proc/locks

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-010] a missing /proc/locks must be unknown")
	assert.Contains(t, got.Cause, "cannot read /proc/locks", "[AC-010] cause")
}

func TestDpkgProbe_RD_AC010_ProcLocksPermissionDeniedIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.openErrs["/proc/locks"] = &fs.PathError{Op: "open", Path: "/proc/locks", Err: fs.ErrPermission}

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-010] an unreadable /proc/locks must be unknown")
	assert.Contains(t, got.Cause, "cannot read /proc/locks", "[AC-010] cause")
}

func TestProcLocks_RD_AC010_MalformedLineIsUnknown(t *testing.T) {
	cases := map[string]string{
		"blank line":          "",
		"not a lock":          "garbage",
		"non-numeric id":      "x: POSIX ADVISORY WRITE 1 08:01:2 0 EOF",
		"missing colon":       "1 POSIX ADVISORY WRITE 1 08:01:2 0 EOF",
		"too few fields":      "1: POSIX ADVISORY WRITE 1 08:01:2 0",
		"non-numeric pid":     "1: POSIX ADVISORY WRITE x 08:01:2 0 EOF",
		"device without ino":  "1: POSIX ADVISORY WRITE 1 08:01 0 EOF",
		"non-hex major":       "1: POSIX ADVISORY WRITE 1 zz:01:2 0 EOF",
		"non-hex minor":       "1: POSIX ADVISORY WRITE 1 08:zz:2 0 EOF",
		"non-decimal inode":   "1: POSIX ADVISORY WRITE 1 08:01:xx 0 EOF",
		"four device parts":   "1: POSIX ADVISORY WRITE 1 08:01:2:3 0 EOF",
		"signed lock id only": "-1: POSIX ADVISORY WRITE 1 08:01:2 0 EOF",
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			h := newFakeDpkgHost()
			h.setLocks("1: POSIX  ADVISORY  WRITE 5 08:01:99 0 EOF", bad)

			got := h.probe()

			assert.Equal(t, dpkgLockUnknown, got.State, "[AC-010] a line the parser does not recognize must be unknown")
			assert.Equal(t, "unrecognized /proc/locks line 2", got.Cause, "[AC-010] the cause names the line number and never echoes the line")
		})
	}
}

func TestDpkgProbe_RD_AC010_MalformedLineAfterAMatchStillUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine, "garbage")

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-010] the file is read to the end, so the outcome never depends on line order")
}

func TestDpkgProbe_RD_AC010_NestedPidNamespaceIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.files["/proc/self/status"] = "Name:\triot-agent\nNSpid:\t4242\t1\n"
	h.setLocks("1: POSIX  ADVISORY  WRITE 5 08:01:99 0 EOF") // no match: would read as free

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[C-1] a /proc that does not show the whole host must not read as free")
	assert.Equal(t, "agent is not in the host pid namespace", got.Cause, "[C-1] cause")
}

func TestDpkgProbe_RD_AC010_NSpidMissingIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.files["/proc/self/status"] = "Name:\triot-agent\nUid:\t999\t999\t999\t999\n" // kernel before 4.1

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[C-1] an unverifiable namespace must be unknown")
	assert.Equal(t, "cannot verify the pid namespace: no NSpid", got.Cause, "[C-1] cause")
}

func TestDpkgProbe_RD_AC010_SelfStatusUnreadableIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	delete(h.files, "/proc/self/status")

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[C-1] an unreadable self/status must be unknown")
	assert.Contains(t, got.Cause, "cannot verify the pid namespace", "[C-1] cause")
}

func TestDpkgProbe_RD_AC010_DpkgStatusPresentLockFilesMissingIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	delete(h.stats, lockFrontendPath) // an admin ran "rm /var/lib/dpkg/lock*"
	delete(h.stats, lockLegacyPath)

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[C-1] dpkg present with no lock files must not read as absent")
	assert.Equal(t, "dpkg is installed but its lock files are missing or hidden", got.Cause, "[C-1] cause")
	assert.Empty(t, h.opened, "[C-1] nothing under /proc is read once the lock files are missing")
}

func TestDpkgProbe_RD_AC010_DpkgBinaryPresentVarLibHiddenIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	delete(h.stats, lockFrontendPath)
	delete(h.stats, lockLegacyPath)
	delete(h.stats, "/var/lib/dpkg/status") // a mount namespace hides /var/lib

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[C-1] the dpkg binary alone marks a dpkg host")
	assert.Equal(t, "dpkg is installed but its lock files are missing or hidden", got.Cause, "[C-1] cause")
}

func TestDpkgProbe_RD_AC010_MarkerStatErrorIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	delete(h.stats, lockFrontendPath)
	delete(h.stats, lockLegacyPath)
	delete(h.stats, "/var/lib/dpkg/status")
	h.statErrs["/usr/bin/dpkg"] = &fs.PathError{Op: "stat", Path: "/usr/bin/dpkg", Err: fs.ErrPermission}

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[C-1] a marker that cannot be checked must be unknown")
	assert.Contains(t, got.Cause, "cannot stat /usr/bin/dpkg", "[C-1] cause")
}

func TestDpkgProbe_RD_AC010_CauseIsBounded(t *testing.T) {
	h := newFakeDpkgHost()
	h.statErrs[lockFrontendPath] = errors.New(strings.Repeat("x", 5000))

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-010] unknown")
	assert.LessOrEqual(t, len(got.Cause), dpkgCauseMaxBytes, "[AC-010] causes are at most 200 bytes")
}

// ---------------------------------------------------------------------------
// AC-011: bounded reads
// ---------------------------------------------------------------------------

// locksOfSize returns valid, non-matching /proc/locks text of exactly n bytes.
func locksOfSize(t *testing.T, n int) string {
	t.Helper()
	const prefix = "1: POSIX  ADVISORY  WRITE 1 08:01:99 0 "
	unit := len(prefix) + len("EOF") + 1
	full := (n - len(prefix) - 2) / unit
	last := n - full*unit - len(prefix) - 1
	require.GreaterOrEqual(t, last, 1, "fixture arithmetic")
	var b strings.Builder
	for i := 0; i < full; i++ {
		b.WriteString(prefix + "EOF\n")
	}
	b.WriteString(prefix + strings.Repeat("9", last) + "\n")
	require.Equal(t, n, b.Len(), "fixture size")
	return b.String()
}

func TestDpkgProbe_RD_AC011_OverOneMiBIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.files["/proc/locks"] = locksOfSize(t, dpkgProbeMaxBytes+1)

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-011] lock information over 1 MiB must be unknown")
	assert.Equal(t, "lock information exceeds 1 MiB", got.Cause, "[AC-011] cause")
}

func TestDpkgProbe_RD_AC011_ExactlyOneMiBParses(t *testing.T) {
	h := newFakeDpkgHost()
	h.files["/proc/locks"] = locksOfSize(t, dpkgProbeMaxBytes)

	got := h.probe()

	assert.Equal(t, dpkgLockFree, got.State, "[AC-011] exactly 1 MiB is within the bound")
}

// endlessLocks serves valid lock lines without end and counts the bytes served.
type endlessLocks struct {
	served int64
	limit  int64
	buf    []byte
}

func (e *endlessLocks) Read(p []byte) (int, error) {
	if e.served >= e.limit {
		return 0, io.EOF
	}
	const line = "1: POSIX  ADVISORY  WRITE 1 08:01:99 0 EOF\n"
	n := 0
	for n < len(p) && e.served < e.limit {
		if len(e.buf) == 0 {
			e.buf = []byte(line)
		}
		c := copy(p[n:], e.buf)
		e.buf = e.buf[c:]
		n += c
		e.served += int64(c)
	}
	return n, nil
}

func TestDpkgProbe_RD_AC011_ReaderStopsAtCap(t *testing.T) {
	h := newFakeDpkgHost()
	src := &endlessLocks{limit: 5 << 20}
	h.readers["/proc/locks"] = func() io.Reader { return src }

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-011] a 5 MiB source must be unknown")
	assert.LessOrEqual(t, src.served, int64(dpkgProbeMaxBytes+1),
		"[AC-011] the cap is at the reader: the probe must not read the rest of a 5 MiB source")
}

func TestDpkgProbe_RD_AC011_LineOver4KiBIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks("1: POSIX  ADVISORY  WRITE 5 08:01:99 0 EOF", "2: POSIX ADVISORY WRITE 5 08:01:99 0 "+strings.Repeat("9", 5000))

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-011] a line over 4 KiB must be unknown")
	assert.Contains(t, got.Cause, "cannot read /proc/locks", "[AC-011] cause")
}

func TestDpkgProbe_RD_AC011_SlowReadOverOneSecondIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.tick = 400 * time.Millisecond
	h.setLocks(
		"1: POSIX  ADVISORY  WRITE 5 08:01:90 0 EOF",
		"2: POSIX  ADVISORY  WRITE 5 08:01:91 0 EOF",
		"3: POSIX  ADVISORY  WRITE 5 08:01:92 0 EOF",
		"4: POSIX  ADVISORY  WRITE 5 08:01:93 0 EOF",
	)

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-011] a check slower than 1s must be unknown")
	assert.Equal(t, "lock check exceeded 1s", got.Cause, "[AC-011] cause")
}

func TestDpkgProbe_RD_AC011_FastReadIsNotSlow(t *testing.T) {
	h := newFakeDpkgHost()
	h.tick = time.Millisecond
	h.setLocks("1: POSIX  ADVISORY  WRITE 5 08:01:90 0 EOF")

	got := h.probe()

	assert.Equal(t, dpkgLockFree, got.State, "[AC-011] a fast check is not slow")
}

func TestDpkgProbe_RD_AC011_SlowHolderReadIsUnknown(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine)
	h.setHolder(1234, "dpkg", 0)
	h.onOpen = func(path string) {
		if path == "/proc/1234/comm" {
			h.clock = h.clock.Add(2 * time.Second)
		}
	}

	got := h.probe()

	assert.Equal(t, dpkgLockUnknown, got.State, "[AC-011] the holder reads count toward the 1s budget")
	assert.Equal(t, "lock check exceeded 1s", got.Cause, "[AC-011] cause")
}

// ---------------------------------------------------------------------------
// AC-012: no dpkg, no wait
// ---------------------------------------------------------------------------

func TestDpkgProbe_RD_AC012_NoDpkgNoLockFilesIsAbsentWithoutReadingProc(t *testing.T) {
	h := newFakeDpkgHost()
	h.stats = map[string]fileID{} // a dnf host: nothing under /var/lib/dpkg, no /usr/bin/dpkg

	got := h.probe()

	assert.Equal(t, dpkgLockAbsent, got.State, "[AC-012] a host without dpkg is absent")
	assert.Empty(t, h.opened, "[AC-012] neither self/status nor /proc/locks is read on a host without dpkg")
}

// ---------------------------------------------------------------------------
// AC-018: the probe never takes the lock
// ---------------------------------------------------------------------------

func TestDpkgProbe_RD_AC018_NeverOpensLockFiles(t *testing.T) {
	h := newFakeDpkgHost()
	h.setLocks(hawaiiFrontendLine)
	h.setHolder(1234, "dpkg", 0)

	h.probe()

	allowed := map[string]bool{
		"/proc/self/status": true, "/proc/locks": true, "/proc/1234/comm": true, "/proc/1234/status": true,
	}
	for _, p := range h.opened {
		assert.True(t, allowed[p], "[AC-018] the probe opened %s; only /proc/self/status, /proc/locks and /proc/<pid>/{comm,status} are allowed", p)
		assert.NotContains(t, p, "/var/lib/dpkg", "[AC-018] a dpkg lock file must never be opened")
	}
}

// bannedLockingSelectors are identifiers whose use would let the probe take, test
// or contend for a lock (FR-007).
var bannedLockingSelectors = map[string]bool{
	"FcntlFlock": true, "Flock": true, "F_SETLK": true, "F_SETLKW": true, "F_GETLK": true,
	"F_OFD_SETLK": true, "F_OFD_GETLK": true, "OpenFile": true,
}

func TestDpkgLock_RD_AC018_NoLockingCallsInSource(t *testing.T) {
	files, err := filepath.Glob("dpkglock*.go")
	require.NoError(t, err)
	files = append(files, "reboot_clearance.go")
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err, "parse %s", name)
		checked++
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pos := fset.Position(sel.Pos())
			assert.False(t, bannedLockingSelectors[sel.Sel.Name], "[AC-018] %s uses %s, which can take or test a lock", pos, sel.Sel.Name)
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "syscall" && sel.Sel.Name == "Open" {
				t.Errorf("[AC-018] %s uses syscall.Open", pos)
			}
			return true
		})
	}
	assert.GreaterOrEqual(t, checked, 4, "[AC-018] the source check must cover the probe files and the gate")
}

// ---------------------------------------------------------------------------
// C-1: the shipped units must not hide the host's pid namespace
// ---------------------------------------------------------------------------

func TestAgentUnit_RD_C1_NoPrivatePIDs(t *testing.T) {
	data, err := os.ReadFile("../../scripts/riot-agent.service")
	require.NoError(t, err)
	units := map[string]string{
		"scripts/riot-agent.service": string(data),
		"the unit install.sh writes": installerUnit(t),
	}
	for name, unit := range units {
		for _, directive := range []string{"PrivatePIDs=", "ProtectProc=", "ProcSubset="} {
			assert.NotContains(t, unit, directive,
				"[C-1] %s sets %s. The dpkg lock probe reads /proc/locks and /proc/self/status; with a narrowed /proc every reboot on a dpkg host would skip as lock state unknown. Adding it needs an ADD change", name, directive)
		}
	}
}

func TestParseNSpid_RD_C1_CountsNamespaceLevels(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		levels int
		ok     bool
	}{
		{"initial namespace", "Name:\tx\nNSpid:\t4242\nNSsid:\t1\n", 1, true},
		{"nested namespace", "NSpid:\t4242\t1\n", 2, true},
		{"three levels", "NSpid:\t4242\t77\t1\n", 3, true},
		{"line missing", "Name:\tx\nUid:\t0\t0\t0\t0\n", 0, false},
		{"empty line", "NSpid:\n", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			levels, ok := parseNSpid(tc.text)
			assert.Equal(t, tc.ok, ok, "[C-1] ok")
			assert.Equal(t, tc.levels, levels, "[C-1] levels")
		})
	}
}

func TestReadSmallFile_RD_C1_CutNSpidLineIsNeverTrusted(t *testing.T) {
	// An 8 KiB cap that ends inside the NSpid line must not read as one level.
	const cutLine = "NSpid:\t4242\t"
	head := "Groups:\t" + strings.Repeat("1", dpkgSmallReadMax-len(cutLine)-len("Groups:\t")-1) + "\n"
	require.Equal(t, dpkgSmallReadMax-len(cutLine), len(head), "fixture: the cap must fall inside the NSpid line")
	text := head + cutLine + "1\n"
	h := newFakeDpkgHost()
	h.files["/proc/self/status"] = text

	got, err := readSmallFile(h.env(), "/proc/self/status")

	require.NoError(t, err)
	_, ok := parseNSpid(got)
	assert.False(t, ok, "[C-1] a cut-off status text must not yield an NSpid line, or a cut 'NSpid: 4242<TAB>' would read as one level")
	assert.True(t, strings.HasSuffix(got, "\n") || got == "", "[C-1] only complete lines are returned")
}

// ---------------------------------------------------------------------------
// AC-021: no exec, no sudo
// ---------------------------------------------------------------------------

func TestDpkgProbe_RD_AC021_NoExecNoSudo(t *testing.T) {
	data, err := os.ReadFile("dpkglock.go")
	require.NoError(t, err)
	src := string(data)
	for _, banned := range []string{"os/exec", "exec.Command", "sudo", "runCommand"} {
		assert.NotContains(t, src, banned, "[AC-021] the probe must not run a process or use sudo (%s)", banned)
	}
	other, err := os.ReadFile("dpkglock_linux.go")
	require.NoError(t, err)
	for _, banned := range []string{"os/exec", "exec.Command", "sudo", "runCommand"} {
		assert.NotContains(t, string(other), banned, "[AC-021] the Linux probe must not run a process or use sudo (%s)", banned)
	}
}

func TestProbeDpkg_RD_AC021_SeamDefaultsToPlatformProbe(t *testing.T) {
	scripted := dpkgLockStatus{State: dpkgLockHeld, PID: 7, UID: -1}
	a := &Agent{dpkgLockProbe: func() dpkgLockStatus { return scripted }}

	assert.Equal(t, scripted, a.probeDpkg(), "[AC-021] a test probe replaces the platform probe")
}
