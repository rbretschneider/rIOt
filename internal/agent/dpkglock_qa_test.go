package agent

import (
	"io"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (REBOOT-DPKG-001). Each test closes a gap that a mutation
// of the probe survived: the developer tests passed with the mutant in place.

// qaEndlessBytes serves one repeated byte without end (until limit) and counts
// what was served, so a test can prove the reader, not the parser, stops the read.
type qaEndlessBytes struct {
	b      byte
	served int64
	limit  int64
}

func (e *qaEndlessBytes) Read(p []byte) (int, error) {
	if e.served >= e.limit {
		return 0, io.EOF
	}
	n := len(p)
	if remaining := e.limit - e.served; int64(n) > remaining {
		n = int(remaining)
	}
	for i := 0; i < n; i++ {
		p[i] = e.b
	}
	e.served += int64(n)
	return n, nil
}

// Covers AC-002 (mutant: the inode-only leniency widened to "target major 0").
// The leniency needs major 0 on BOTH sides (ADD AD-001 step 7). A lock line on a
// real block device is a different file even when the inode number is equal.
func TestProcLocks_QA_AC002_AnonymousTargetDoesNotMatchBlockDeviceLine(t *testing.T) {
	h := newFakeDpkgHost()
	h.stats[lockFrontendPath] = fileID{Major: 0, Minor: 0x2e, Ino: 262338} // stat on btrfs: anonymous dev
	h.stats[lockLegacyPath] = fileID{Major: 0, Minor: 0x2e, Ino: 262339}
	h.setLocks("1: POSIX  ADVISORY  WRITE 812 08:01:262338 0 EOF") // same inode number, on /dev/sda1
	h.setHolder(812, "someone", 1000)

	got := h.probe()

	assert.Equal(t, dpkgLockFree, got.State, "[AC-002] an anonymous-device target must not match a lock on a block device, even with an equal inode number")
	assert.False(t, got.InodeOnly, "[AC-002] no inode-only match was made")
}

// Covers AC-011 / NFR-003 (mutant: the 1 second bound read as ">=" instead of ">").
// "Finish within 1 second" means exactly 1s is still within the bound; 1s plus a
// bit is not.
func TestDpkgProbe_QA_AC011_ExactlyOneSecondIsWithinTheBound(t *testing.T) {
	// The probe reads the clock three times for one non-matching line: at the start,
	// after the line and at the end. The last reading is therefore 2 * tick.
	cases := []struct {
		name string
		tick time.Duration
		want dpkgLockState
	}{
		{"exactly 1s is within the bound", 500 * time.Millisecond, dpkgLockFree},
		{"1s and 2ms is over the bound", 501 * time.Millisecond, dpkgLockUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFakeDpkgHost()
			h.tick = tc.tick
			h.setLocks("1: POSIX  ADVISORY  WRITE 5 08:01:90 0 EOF")

			got := h.probe()

			assert.Equal(t, tc.want, got.State, "[AC-011] the 1 second boundary (%s per clock read)", tc.tick)
			if tc.want == dpkgLockUnknown {
				assert.Equal(t, "lock check exceeded 1s", got.Cause, "[AC-011] cause")
			}
		})
	}
}

// Covers AC-011 / NFR-002 (mutant: the 8 KiB cap on /proc/<pid>/{comm,status} and
// /proc/self/status removed). The finite fixtures of the developer tests still pass
// without the reader cap because the parser trims afterwards; only an endless
// source shows whether the read itself is bounded.
func TestDpkgProbe_QA_AC011_SmallReadsStopAtTheReaderCap(t *testing.T) {
	t.Run("holder comm and status", func(t *testing.T) {
		h := newFakeDpkgHost()
		h.setLocks(hawaiiFrontendLine)
		comm := &qaEndlessBytes{b: 'a', limit: 4 << 20}
		status := &qaEndlessBytes{b: 'u', limit: 4 << 20}
		h.readers["/proc/1234/comm"] = func() io.Reader { return comm }
		h.readers["/proc/1234/status"] = func() io.Reader { return status }

		got := h.probe()

		assert.Equal(t, dpkgLockHeld, got.State, "[AC-011] an unreadable-looking holder is still a held lock")
		assert.LessOrEqual(t, comm.served, int64(dpkgSmallReadMax+1), "[AC-011] /proc/<pid>/comm is read at most 8 KiB + 1 byte")
		assert.LessOrEqual(t, status.served, int64(dpkgSmallReadMax+1), "[AC-011] /proc/<pid>/status is read at most 8 KiB + 1 byte")
		assert.LessOrEqual(t, len(got.Comm), dpkgHolderNameMax, "[AC-011] the holder name stays within 16 bytes")
	})
	t.Run("self status", func(t *testing.T) {
		h := newFakeDpkgHost()
		self := &qaEndlessBytes{b: 'x', limit: 4 << 20}
		h.readers["/proc/self/status"] = func() io.Reader { return self }
		h.setLocks("1: POSIX  ADVISORY  WRITE 5 08:01:99 0 EOF")

		got := h.probe()

		assert.Equal(t, dpkgLockUnknown, got.State, "[AC-011] a self/status with no complete line cannot prove the namespace")
		assert.LessOrEqual(t, self.served, int64(dpkgSmallReadMax+1), "[AC-011] /proc/self/status is read at most 8 KiB + 1 byte")
	})
}

// Covers C-5 (end to end, not only the helper): a hostile comm must not reach the
// holder text in a form the notifiers would render. The unit test of
// sanitizeHolderName alone passes even if probeDpkgLock stopped calling it.
func TestDpkgProbe_QA_C5_HostileCommIsNeutralizedInTheHolderText(t *testing.T) {
	hostile := []string{
		"[ok](http://evil.example/x)",
		"unattended-upgr\n@everyone",
		"a\x00b\x1b[31mred",
		"<script>alert(1)</script>",
		"caf\xc3\xa9",
		strings.Repeat("A", 200),
	}
	safe := regexp.MustCompile(`^[A-Za-z0-9._+:?-]{0,16}$`)
	for _, comm := range hostile {
		t.Run(comm, func(t *testing.T) {
			h := newFakeDpkgHost()
			h.setLocks(hawaiiFrontendLine)
			h.files["/proc/1234/comm"] = comm + "\n"
			h.files["/proc/1234/status"] = "Name:\tx\nUid:\t1000\t1000\t1000\t1000\n"

			got := h.probe()

			require.Equal(t, dpkgLockHeld, got.State)
			assert.Regexp(t, safe, got.Comm, "[C-5] comm is limited to [A-Za-z0-9._+:-] (everything else '?') and 16 bytes")
			text := got.holderText()
			assert.NotContains(t, text, "\n", "[C-5] the holder text is one line")
			assert.NotContains(t, text, "[", "[C-5] no markdown link can be built from the holder text")
			assert.NotContains(t, text, "<", "[C-5] no markup can be built from the holder text")
			assert.NotContains(t, text, "@", "[C-5] no mention can be built from the holder text")
		})
	}
}

// Covers AC-003 (mixed lock / lock-frontend absence). dpkg older than 1.19.1 has
// no lock-frontend; some images have no legacy lock. Whichever single file exists
// is the one that counts, and a single stat failure still fails closed.
func TestDpkgProbe_QA_AC003_SingleLockFileHosts(t *testing.T) {
	t.Run("only lock-frontend exists and is held", func(t *testing.T) {
		h := newFakeDpkgHost()
		delete(h.stats, lockLegacyPath)
		h.setLocks(hawaiiFrontendLine)
		h.setHolder(1234, "apt-get", 0)

		assert.Equal(t, dpkgLockHeld, h.probe().State, "[AC-003] a held lock-frontend counts when the legacy lock is missing")
	})
	t.Run("only the legacy lock exists and is free", func(t *testing.T) {
		h := newFakeDpkgHost()
		delete(h.stats, lockFrontendPath)
		h.setLocks("1: POSIX  ADVISORY  WRITE 5 08:01:99 0 EOF")

		got := h.probe()

		assert.Equal(t, dpkgLockFree, got.State, "[AC-003] one lock file present and unheld is free, neither absent nor unknown")
		assert.NotEmpty(t, h.readlinks, "[AC-003] the host is verified before it is called free")
	})
	t.Run("only lock-frontend exists and is free", func(t *testing.T) {
		h := newFakeDpkgHost()
		delete(h.stats, lockLegacyPath)

		assert.Equal(t, dpkgLockFree, h.probe().State, "[AC-003] one lock file present and unheld is free")
	})
	t.Run("lock-frontend missing and the legacy lock cannot be checked", func(t *testing.T) {
		h := newFakeDpkgHost()
		delete(h.stats, lockFrontendPath)
		h.statErrs[lockLegacyPath] = &fs.PathError{Op: "stat", Path: lockLegacyPath, Err: fs.ErrPermission}

		got := h.probe()

		assert.Equal(t, dpkgLockUnknown, got.State, "[AC-010] a lock file that cannot be checked is unknown even when the other is missing")
		assert.Contains(t, got.Cause, "cannot stat "+lockLegacyPath, "[AC-010] cause names the path")
	})
	t.Run("both files held: the first line in file order names the holder", func(t *testing.T) {
		h := newFakeDpkgHost()
		h.setLocks("1: POSIX  ADVISORY  WRITE 11 103:02:262339 0 EOF", "2: POSIX  ADVISORY  WRITE 22 103:02:262338 0 EOF")
		h.setHolder(11, "dpkg", 0)
		h.setHolder(22, "apt", 0)

		got := h.probe()

		assert.Equal(t, dpkgLockHeld, got.State, "[AC-002] held")
		assert.Equal(t, int64(11), got.PID, "[AC-018] the first matching line in file order is the reported holder")
	})
}
