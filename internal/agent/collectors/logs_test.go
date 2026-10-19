package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeJSONLine builds a single journald JSON line with the given fields.
func makeJSONLine(uid, unit, syslogID, message string, ts time.Time) string {
	m := map[string]interface{}{
		"PRIORITY": "4",
		"MESSAGE":  message,
		"__REALTIME_TIMESTAMP": fmt.Sprintf("%d",
			ts.UnixNano()/int64(time.Microsecond)),
	}
	if uid != "" {
		m["_UID"] = uid
	}
	if unit != "" {
		m["_SYSTEMD_UNIT"] = unit
	}
	if syslogID != "" {
		m["SYSLOG_IDENTIFIER"] = syslogID
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// newTestLogsCollector returns a LogsCollector with a fresh counter for unit tests.
func newTestLogsCollector() (*LogsCollector, *authFailureCounter) {
	counter := &authFailureCounter{}
	lc := &LogsCollector{authCounter: counter}
	return lc, counter
}

// [AC-002] Four real-origin sshd/sudo lines each increment the counter.
func TestAC002_ParseAndCount_FourAuthPatterns_CounterIsFour(t *testing.T) {
	lc, counter := newTestLogsCollector()

	base := time.Now()
	lines := []string{
		makeJSONLine("0", "ssh.service", "sshd", "Failed password for root from 1.2.3.4 port 22 ssh2", base),
		makeJSONLine("0", "systemd-logind.service", "login", "authentication failure; logname=LOGIN uid=0 euid=0 tty=/dev/tty1 ruser= rhost=  user=baduser", base.Add(time.Second)),
		makeJSONLine("0", "ssh.service", "sshd", "Invalid user hacker from 203.0.113.42 port 12345", base.Add(2*time.Second)),
		makeJSONLine("0", "sudo.service", "sudo", "pam_unix(sudo:auth): authentication failure; logname=alice uid=1000 euid=0 tty=/dev/pts/0 ruser=alice rhost=  user=alice", base.Add(3*time.Second)),
	}

	input := []byte(joinLines(lines))
	entries, _ := lc.parseAndCount(input)

	require.Len(t, entries, 4, "[AC-002] all four lines must produce log entries")
	assert.Equal(t, 4, counter.Drain(),
		"[AC-002] counter must equal 4 — one increment per matching line")
}

// [AC-003, SEC-001] Forged logger entry with non-root UID does NOT increment the counter.
func TestAC003_ParseAndCount_ForgedLoggerEntry_NotCounted(t *testing.T) {
	lc, counter := newTestLogsCollector()

	line := makeJSONLine("1000", "", "logger", "Failed password for root from 1.2.3.4 port 22 ssh2", time.Now())
	lc.parseAndCount([]byte(line))

	assert.Equal(t, 0, counter.Drain(),
		"[SEC-001] forged logger entry must NOT increment the counter")
}

// [AC-003] Non-matching message content does NOT increment the counter.
func TestAC003_ParseAndCount_NonMatchingMessage_NotCounted(t *testing.T) {
	lc, counter := newTestLogsCollector()

	line := makeJSONLine("0", "ssh.service", "sshd", "Accepted publickey for alice from 10.0.0.5 port 22 ssh2", time.Now())
	lc.parseAndCount([]byte(line))

	assert.Equal(t, 0, counter.Drain(),
		"[AC-003] non-matching message must NOT increment the counter")
}

// [AC-007] Ten identical sshd-origin Failed password lines increment the counter to 10.
func TestAC007_ParseAndCount_TenIdenticalLines_CounterIsTen(t *testing.T) {
	lc, counter := newTestLogsCollector()

	base := time.Now()
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = makeJSONLine("0", "ssh.service", "sshd",
			"Failed password for root from 10.0.0.1 port 22 ssh2",
			base.Add(time.Duration(i)*time.Second))
	}

	lc.parseAndCount([]byte(joinLines(lines)))

	assert.Equal(t, 10, counter.Drain(),
		"[AC-007] 10 identical sshd-origin lines must produce counter = 10 (no de-duplication)")
}

// [AC-005] MarkReady is NOT called when parseAndCount runs (that is the caller's job).
// This verifies that counter stays not-ready after parseAndCount alone.
func TestAC005_ParseAndCount_DoesNotCallMarkReady(t *testing.T) {
	lc, counter := newTestLogsCollector()

	line := makeJSONLine("0", "ssh.service", "sshd", "Failed password for root from 1.2.3.4 port 22", time.Now())
	lc.parseAndCount([]byte(line))

	// parseAndCount itself does not call MarkReady — that is Collect's responsibility.
	assert.False(t, counter.IsReady(),
		"[AC-005] parseAndCount must not call MarkReady — Collect is responsible")
}

// [AC-006] When given an error (empty output), no entries are returned and counter stays 0.
func TestAC006_ParseAndCount_EmptyOutput_NoEntriesNoCount(t *testing.T) {
	lc, counter := newTestLogsCollector()

	entries, _ := lc.parseAndCount([]byte(""))
	assert.Empty(t, entries, "[AC-006] empty output must produce no log entries")
	assert.Equal(t, 0, counter.Drain(), "[AC-006] empty output must not increment counter")
}

// [AC-006] When journalctl exec fails, the error is logged at Warn level and
// an empty slice is returned (fail-open, FR-006 / ADD Section 9).
func TestAC006_CollectExecError_LogsWarning(t *testing.T) {
	lc, counter := newTestLogsCollector()

	// Capture slog output so we can assert on the warning message.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	original := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(original) })

	execErr := errors.New("exec: journalctl not found")
	result, err := lc.collectFromOutput(nil, execErr, true)

	// Fail-open: nil error returned to the caller.
	assert.NoError(t, err, "[AC-006] exec error must not propagate to caller")

	// Empty log slice returned.
	entries, ok := result.([]models.LogEntry)
	require.True(t, ok, "[AC-006] result must be []models.LogEntry")
	assert.Empty(t, entries, "[AC-006] exec error must produce no log entries")

	// Counter must not be incremented.
	assert.Equal(t, 0, counter.Drain(), "[AC-006] exec error must not increment auth counter")

	// Counter must not be marked ready (MarkReady is only called on success).
	assert.False(t, counter.IsReady(), "[AC-006] exec error must not mark counter ready")

	// Warning must be logged with the correct message key.
	assert.Contains(t, buf.String(), "journalctl exec failed",
		"[AC-006] slog.Warn must emit 'journalctl exec failed' on exec error")
}

// ParseAndCount updates lastSeen to the latest timestamp in the output.
func TestParseAndCount_UpdatesLastSeen(t *testing.T) {
	lc, _ := newTestLogsCollector()

	base := time.Now().Truncate(time.Second)
	later := base.Add(5 * time.Second)

	lines := []string{
		makeJSONLine("0", "sshd.service", "sshd", "some message", base),
		makeJSONLine("0", "sshd.service", "sshd", "another message", later),
	}

	lc.parseAndCount([]byte(joinLines(lines)))

	lc.mu.Lock()
	lastSeen := lc.lastSeen
	lc.mu.Unlock()

	// lastSeen should be later + 1µs
	assert.True(t, lastSeen.After(later),
		"lastSeen must be advanced beyond the latest timestamp in output")
}

// ParseAndCount handles malformed JSON lines gracefully.
func TestParseAndCount_MalformedJSON_Skipped(t *testing.T) {
	lc, counter := newTestLogsCollector()

	input := "{not valid json}\n" + makeJSONLine("0", "ssh.service", "sshd", "Failed password for root", time.Now())
	entries, _ := lc.parseAndCount([]byte(input))

	require.Len(t, entries, 1, "valid line after malformed one must still be parsed")
	assert.Equal(t, 1, counter.Drain(), "valid auth failure after malformed line must be counted")
}

// journalStream feeds n numbered journal lines, one microsecond apart, to a
// collector through its injected journal seam.
type journalStream struct {
	lines    string
	err      error
	finished []bool // the stop flag of each finish call
}

func (j *journalStream) open(context.Context, []string) (io.Reader, func(bool) error, error) {
	return strings.NewReader(j.lines), func(stop bool) error {
		j.finished = append(j.finished, stop)
		return j.err
	}, nil
}

// numberedJournal builds n lines "msg-0000001" with the given uid/unit/identifier.
func numberedJournal(n int, uid, unit, ident, text string, start time.Time) string {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		sb.WriteString(makeJSONLine(uid, unit, ident, fmt.Sprintf("msg-%07d %s", i, text), start.Add(time.Duration(i)*time.Microsecond)))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &buf
}

func collectJournal(t *testing.T, lc *LogsCollector, js *journalStream, firstScan bool) []models.LogEntry {
	t.Helper()
	lc.openJournal = js.open
	if !firstScan {
		lc.mu.Lock()
		lc.lastSeen = time.Now()
		lc.mu.Unlock()
	}
	got, err := lc.collectPass(context.Background())
	require.NoError(t, err)
	entries, ok := got.([]models.LogEntry)
	require.True(t, ok, "collect must return []models.LogEntry, got %T", got)
	return entries
}

func TestGROWTH_AC058_JournalKeepsNewest5000Logs2000Skipped(t *testing.T) {
	logs := captureWarnings(t)
	lc, _ := newTestLogsCollector()
	js := &journalStream{lines: numberedJournal(7000, "1000", "app.service", "app", "x", time.Now())}

	entries := collectJournal(t, lc, js, false)

	require.Len(t, entries, 5000, "[AC-058] 5,000 entries are sent")
	assert.Contains(t, entries[0].Message, "msg-0002001", "[AC-058] the oldest 2,000 are the ones skipped")
	assert.Contains(t, entries[4999].Message, "msg-0007000", "[AC-058] the newest entry is last")
	assert.Contains(t, logs.String(), "skipped=2000", "[AC-058] the 2,000 skipped entries are logged, exactly")
	assert.Equal(t, []bool{false}, js.finished, "a stream read to its end is not killed")
}

func TestGROWTH_AC058_SkippedCountIsExact(t *testing.T) {
	lc, _ := newTestLogsCollector()
	res := lc.scanJournal(strings.NewReader(numberedJournal(5123, "1000", "app.service", "app", "x", time.Now())))
	assert.Equal(t, 123, res.skipped(), "[AC-058] skipped = entries read - entries kept")
	assert.Len(t, res.entries, 5000)
	assert.Equal(t, 5123, res.lines)
}

func TestGROWTH_AC058_MaxEntriesFromConfigIsHonoured(t *testing.T) {
	captureWarnings(t)
	lc, _ := newTestLogsCollector()
	lc.MaxEntries = 500
	js := &journalStream{lines: numberedJournal(700, "1000", "app.service", "app", "x", time.Now())}

	entries := collectJournal(t, lc, js, false)

	assert.Len(t, entries, 500)
	assert.Contains(t, entries[0].Message, "msg-0000201")
}

func TestGROWTH_AC058_AuthCountingStaysExactForEvictedEntries(t *testing.T) {
	captureWarnings(t)
	lc, counter := newTestLogsCollector()
	// The first 2,000 lines are real sshd failures and are evicted from the
	// ring by the 5,000 later lines; every one of them must still be counted.
	failures := numberedJournal(2000, "0", "ssh.service", "sshd", "Failed password for root from 1.2.3.4 port 22", time.Now())
	noise := numberedJournal(5000, "1000", "app.service", "app", "fine", time.Now().Add(time.Hour))
	js := &journalStream{lines: failures + noise}

	entries := collectJournal(t, lc, js, false)

	require.Len(t, entries, 5000)
	assert.Equal(t, 2000, counter.Drain(), "[AC-058] auth counting stays exact even when the entries are skipped")
}

func TestGROWTH_AC058_CursorAdvancesPastSkippedEntries(t *testing.T) {
	captureWarnings(t)
	lc, _ := newTestLogsCollector()
	base := time.Now().Truncate(time.Second)
	js := &journalStream{lines: numberedJournal(6000, "1000", "app.service", "app", "x", base)}

	collectJournal(t, lc, js, false)

	lc.mu.Lock()
	defer lc.mu.Unlock()
	assert.True(t, lc.lastSeen.After(base.Add(6000*time.Microsecond)),
		"the cursor must move past every line read, or the skipped entries would be re-read forever")
}

func TestGROWTH_AC057_JournalMessagesTruncatedAtRuneBoundary(t *testing.T) {
	captureWarnings(t)
	lc, _ := newTestLogsCollector()
	long := makeJSONLine("1000", "app.service", "app", strings.Repeat("a", 10<<10), time.Now()) + "\n" +
		makeJSONLine("1000", "app.service", "app", strings.Repeat("€", 5000), time.Now()) + "\n"
	js := &journalStream{lines: long}

	entries := collectJournal(t, lc, js, false)

	require.Len(t, entries, 2)
	assert.Equal(t, strings.Repeat("a", 4096)+" [truncated]", entries[0].Message, "[AC-057] 10 KiB journal message: 4 KiB plus the marker")
	body := strings.TrimSuffix(entries[1].Message, " [truncated]")
	assert.True(t, utf8.ValidString(entries[1].Message), "[AC-057] valid UTF-8")
	assert.Equal(t, 4095, len(body), "[AC-057] cut back to a rune boundary")
	assert.True(t, strings.HasSuffix(entries[1].Message, " [truncated]"))
}

func TestGROWTH_SEC022_JournalRingBoundedInBytesAsWellAsEntries(t *testing.T) {
	captureWarnings(t)
	lc, _ := newTestLogsCollector()
	// 5,000 entries of about 3 KiB each: 15 MiB, far over the 2 MiB ring.
	res := lc.scanJournal(strings.NewReader(numberedJournal(5000, "1000", "app.service", "app", strings.Repeat("m", 3000), time.Now())))

	total := 0
	for _, e := range res.entries {
		total += len(e.Message) + len(e.Unit)
	}
	assert.LessOrEqual(t, total, 2<<20, "[SEC-022] the ring must hold at most 2 MiB")
	assert.Greater(t, total, 1<<20, "[SEC-022] and use most of it")
	assert.Less(t, len(res.entries), 5000, "[SEC-022] the byte bound, not the entry bound, applies here")
	assert.Equal(t, 5000-len(res.entries), res.skipped(), "[SEC-022] the skipped count stays exact")
	assert.Contains(t, res.entries[len(res.entries)-1].Message, "msg-0005000", "the newest entry is kept")
}

func TestGROWTH_SEC022_OversizeJournalLineIsSkippedAndCounted(t *testing.T) {
	captureWarnings(t)
	lc, _ := newTestLogsCollector()
	huge := `{"MESSAGE":"` + strings.Repeat("h", 2<<20) + `"}` + "\n"
	input := makeJSONLine("1000", "app.service", "app", "before", time.Now()) + "\n" + huge +
		makeJSONLine("1000", "app.service", "app", "after", time.Now()) + "\n"

	res := lc.scanJournal(strings.NewReader(input))

	require.Len(t, res.entries, 2, "lines either side of the huge one are still read")
	assert.Equal(t, 1, res.oversize)
	assert.Equal(t, 1, res.skipped(), "[SEC-022] a line too large to hold is counted as skipped")
}

// endlessReader returns the same chunk forever and counts what was read.
type endlessReader struct {
	chunk []byte
	pos   int
	read  int64
}

func (e *endlessReader) Read(p []byte) (int, error) {
	n := copy(p, e.chunk[e.pos:])
	e.pos = (e.pos + n) % len(e.chunk)
	e.read += int64(n)
	return n, nil
}

func TestGROWTH_SEC022_JournalScanStopsAtItsBounds(t *testing.T) {
	captureWarnings(t)

	t.Run("500,000 lines", func(t *testing.T) {
		lc, _ := newTestLogsCollector()
		line := makeJSONLine("1000", "app.service", "app", "tick", time.Now()) + "\n"
		src := &endlessReader{chunk: []byte(line)}

		res := lc.scanJournal(src)

		assert.True(t, res.stopped, "an endless journal must be cut off")
		assert.Equal(t, 500_000, res.lines)
		assert.Len(t, res.entries, 5000)
		assert.Less(t, src.read, int64(500_000*len(line)+128<<10), "the reader is not drained past the bound (plus bufio read-ahead)")
	})

	t.Run("256 MiB", func(t *testing.T) {
		lc, _ := newTestLogsCollector()
		// 60 KiB junk lines: cheap to skip, so the byte bound is what ends the scan.
		line := append([]byte(strings.Repeat("j", 60<<10)), '\n')
		src := &endlessReader{chunk: line}

		res := lc.scanJournal(src)

		assert.True(t, res.stopped)
		assert.GreaterOrEqual(t, src.read, int64(256<<20))
		assert.Less(t, src.read, int64(256<<20+1<<20), "the scan must not run past 256 MiB by more than read-ahead")
		assert.Less(t, res.lines, 500_000)
	})
}

func TestGROWTH_SEC022_StoppedScanKillsTheProcessAndNormalEndDoesNot(t *testing.T) {
	captureWarnings(t)

	t.Run("a scan stopped at its bound kills journalctl", func(t *testing.T) {
		lc, _ := newTestLogsCollector()
		lc.openJournal = func(context.Context, []string) (io.Reader, func(bool) error, error) {
			line := makeJSONLine("1000", "app.service", "app", "tick", time.Now()) + "\n"
			return &endlessReader{chunk: []byte(line)}, func(stop bool) error {
				assert.True(t, stop, "[SEC-022] the process must be killed when the scan bound is hit")
				return nil
			}, nil
		}
		got, err := lc.collectPass(context.Background())
		require.NoError(t, err)
		assert.Len(t, got.([]models.LogEntry), 5000)
	})

	t.Run("a failing process with no output fails open and does not mark the counter ready", func(t *testing.T) {
		lc, counter := newTestLogsCollector()
		js := &journalStream{err: errors.New("exit status 1")}
		entries := collectJournal(t, lc, js, false)
		assert.Empty(t, entries)
		assert.False(t, counter.IsReady(), "[FR-006] fail open: no MarkReady on a failed exec")
	})

	t.Run("a successful non-first pass marks the counter ready", func(t *testing.T) {
		lc, counter := newTestLogsCollector()
		js := &journalStream{lines: numberedJournal(3, "1000", "app.service", "app", "x", time.Now())}
		entries := collectJournal(t, lc, js, false)
		assert.Len(t, entries, 3)
		assert.True(t, counter.IsReady())
	})

	t.Run("the first scan does not mark the counter ready", func(t *testing.T) {
		lc, counter := newTestLogsCollector()
		js := &journalStream{lines: numberedJournal(3, "1000", "app.service", "app", "x", time.Now())}
		collectJournal(t, lc, js, true)
		assert.False(t, counter.IsReady(), "the first interval reports zero regardless (AD-001)")
	})
}

// joinLines joins a slice of JSON strings with newlines.
func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		result += l
		if i < len(lines)-1 {
			result += "\n"
		}
	}
	return result
}
