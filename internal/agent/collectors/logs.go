package collectors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Bounds on one journal collection pass (GROWTH-001 AD-017, SEC-022).
const (
	defaultJournalMaxEntries   = 5000
	defaultJournalMaxLineBytes = 4 << 10

	// journalRingMaxBytes bounds the kept entries by size as well as by count.
	journalRingMaxBytes = 2 << 20

	// The scan itself stops at 500,000 lines or 256 MiB, so a journal with years
	// of backlog costs a bounded read. The cursor advances to what was read, and
	// the next pass continues from there.
	journalScanMaxLines = 500_000
	journalScanMaxBytes = 256 << 20

	// journalMaxLineBytes: one journal JSON line larger than this is skipped and
	// counted, never held in memory.
	journalMaxLineBytes = 1 << 20

	// journalStderrBytes bounds the stderr kept for diagnostics.
	journalStderrBytes = 4 << 10
)

// LogsCollector gathers recent journal entries (info and above).
// Linux-only; returns empty on other platforms.
type LogsCollector struct {
	mu          sync.Mutex
	lastSeen    time.Time
	authCounter *authFailureCounter

	// Per-pass limits. Zero means the default; the agent passes values already
	// clamped to their configured ranges.
	MaxEntries   int
	MaxLineBytes int

	// openJournal starts journalctl; nil means the real process. Tests inject a
	// stream. finish waits for the process, killing it first when stop is set.
	openJournal func(ctx context.Context, args []string) (stream io.Reader, finish func(stop bool) error, err error)
}

func (c *LogsCollector) Name() string { return "logs" }

func (c *LogsCollector) limits() (maxEntries, maxLineBytes int) {
	maxEntries, maxLineBytes = c.MaxEntries, c.MaxLineBytes
	if maxEntries <= 0 {
		maxEntries = defaultJournalMaxEntries
	}
	if maxLineBytes <= 0 {
		maxLineBytes = defaultJournalMaxLineBytes
	}
	return maxEntries, maxLineBytes
}

func (c *LogsCollector) Collect(ctx context.Context) (interface{}, error) {
	if runtime.GOOS != "linux" {
		return []models.LogEntry{}, nil
	}
	return c.collectPass(ctx)
}

// collectPass is one journal pass. It is separate from Collect so tests can run
// it on any platform with an injected journal stream.
func (c *LogsCollector) collectPass(ctx context.Context) (interface{}, error) {
	c.mu.Lock()
	since := c.lastSeen
	c.mu.Unlock()

	// Record whether this is the first-ever scan (cursor was zero on entry).
	// We read this BEFORE the exec so that a subsequent mutation of c.lastSeen
	// cannot affect the first-interval gate decision (AD-001, note 3).
	firstScan := since.IsZero()

	if since.IsZero() {
		since = time.Now().Add(-5 * time.Minute)
	}

	// No -n: the output is streamed through a bounded ring, so the newest
	// entries win and the count of skipped ones is exact.
	args := []string{
		"--since", since.Format("2006-01-02 15:04:05"),
		"--priority=0..6",
		"-o", "json",
		"--no-pager",
	}
	open := c.openJournal
	if open == nil {
		open = execJournal
	}
	stream, finish, err := open(ctx, args)
	if err != nil {
		return c.collectFromOutput(nil, err, firstScan)
	}
	res := c.scanJournal(stream)
	waitErr := finish(res.stopped)
	if waitErr != nil && res.lines == 0 {
		// Nothing was read and the process failed: fail open (FR-006).
		return c.collectFromOutput(nil, waitErr, firstScan)
	}
	if waitErr != nil {
		slog.Warn("journalctl exited with an error after producing output", "error", waitErr)
	}
	c.reportScan(res)

	// After a real cursor-based scan (not the first-ever scan), mark the
	// counter ready so SecurityCollector reports the real count next tick.
	if !firstScan {
		c.authCounter.MarkReady()
	}
	return res.entries, nil
}

// collectFromOutput handles the post-exec logic: on exec error it logs a
// warning and returns the fail-open empty slice (FR-006, ADD Section 9); on
// success it parses the output and conditionally marks the counter ready.
// Extracted as a named helper so unit tests can inject a pre-computed
// (output, error) tuple without spawning a real journalctl process.
func (c *LogsCollector) collectFromOutput(out []byte, err error, firstScan bool) (interface{}, error) {
	if err != nil {
		// Fail-open (FR-006): return empty log slice; do not call MarkReady;
		// SecurityCollector will drain 0 and report 0.
		slog.Warn("journalctl exec failed", "error", err)
		return []models.LogEntry{}, nil
	}

	entries, _ := c.parseAndCount(out)

	// After a real cursor-based scan (not the first-ever scan), mark the
	// counter ready so SecurityCollector reports the real count next tick.
	if !firstScan {
		c.authCounter.MarkReady()
	}

	return entries, nil
}

// parseAndCount parses the raw journalctl JSON output, builds the LogEntry
// slice, and increments the authFailureCounter for each line that passes the
// trusted-origin + content filters. It also advances c.lastSeen to the
// timestamp of the latest parsed entry.
//
// Extracted as a named helper so unit tests can feed raw JSON without
// spawning a real journalctl process (ADD Section 12, note 2). It applies the
// same bounds as a live pass.
func (c *LogsCollector) parseAndCount(out []byte) ([]models.LogEntry, time.Time) {
	res := c.scanJournal(bytes.NewReader(out))
	c.reportScan(res)
	return res.entries, res.latest
}

// journalScan is the outcome of reading one journal stream.
type journalScan struct {
	entries  []models.LogEntry // newest kept, oldest first; nil when none
	lines    int               // lines read
	evicted  int               // older entries pushed out of the ring
	oversize int               // lines too long to hold, skipped
	stopped  bool              // the scan bound ended the read before the stream did
	latest   time.Time         // newest timestamp among every parsed entry
}

// skipped is the exact number of entries not returned because of a bound.
func (s journalScan) skipped() int { return s.evicted + s.oversize }

// scanJournal reads journalctl JSON lines from r. Every parsed line counts
// toward the auth-failure counter and the cursor, so auth counting stays exact
// even for entries the ring evicts; only the newest entries that fit the ring
// are returned.
func (c *LogsCollector) scanJournal(r io.Reader) journalScan {
	maxEntries, maxLineBytes := c.limits()
	ring := newCapRing(maxEntries, journalRingMaxBytes, func(e models.LogEntry) int {
		return len(e.Message) + len(e.Unit)
	})
	var res journalScan
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	scannedBytes := 0

	for {
		if res.lines >= journalScanMaxLines || scannedBytes >= journalScanMaxBytes {
			res.stopped = true
			break
		}
		line, read, tooLong, err := readBoundedLine(br, journalMaxLineBytes, buf)
		buf = line[:0]
		scannedBytes += read
		if len(line) > 0 || tooLong {
			res.lines++
			switch {
			case tooLong:
				res.oversize++
			case len(bytes.TrimSpace(line)) > 0:
				c.addJournalLine(line, maxLineBytes, ring, &res)
			}
		}
		if err != nil {
			break
		}
	}

	res.evicted = ring.dropped()
	if items := ring.items(); len(items) > 0 {
		res.entries = items
	}
	if !res.latest.IsZero() {
		c.mu.Lock()
		c.lastSeen = res.latest.Add(time.Microsecond)
		c.mu.Unlock()
	}
	return res
}

// addJournalLine parses one journal JSON line into the ring.
func (c *LogsCollector) addJournalLine(line []byte, maxLineBytes int, ring *capRing[models.LogEntry], res *journalScan) {
	var raw map[string]interface{}
	if err := json.Unmarshal(line, &raw); err != nil {
		return
	}

	var ts time.Time
	if usecStr, ok := raw["__REALTIME_TIMESTAMP"].(string); ok {
		if usec, err := strconv.ParseInt(usecStr, 10, 64); err == nil {
			ts = time.Unix(usec/1_000_000, (usec%1_000_000)*1000)
		}
	}
	if ts.IsZero() {
		ts = time.Now()
	}

	priority := 4
	if p, ok := raw["PRIORITY"].(string); ok {
		if v, err := strconv.Atoi(p); err == nil {
			priority = v
		}
	}

	unit, _ := raw["_SYSTEMD_UNIT"].(string)
	if unit == "" {
		unit, _ = raw["SYSLOG_IDENTIFIER"].(string)
	}

	message, _ := raw["MESSAGE"].(string)

	ring.add(models.LogEntry{
		Timestamp: ts,
		Priority:  priority,
		Unit:      TruncateLogLine(unit, 256),
		Message:   TruncateLogLine(message, maxLineBytes),
	})

	if ts.After(res.latest) {
		res.latest = ts
	}

	// Increment the shared auth-failure counter for lines that pass
	// the trusted-origin filter AND a content pattern match (AD-001, AD-004).
	if matchesAuthFailure(raw) {
		c.authCounter.Add(1)
	}
}

// reportScan logs what the bounds cost this pass (one line per pass at most).
func (c *LogsCollector) reportScan(res journalScan) {
	if res.skipped() > 0 || res.stopped {
		slog.Warn("logs: journal entries skipped to stay within the per-pass limits",
			"skipped", res.skipped(), "kept", len(res.entries), "lines_read", res.lines,
			"scan_stopped_at_limit", res.stopped)
	}
}

// readBoundedLine reads one line (including its newline) of at most max bytes
// into buf. A longer line is read to its end and discarded, with tooLong set,
// so the returned slice never exceeds max. read is the number of bytes consumed.
func readBoundedLine(br *bufio.Reader, max int, buf []byte) (line []byte, read int, tooLong bool, err error) {
	buf = buf[:0]
	for {
		chunk, rerr := br.ReadSlice('\n')
		read += len(chunk)
		if !tooLong && len(buf)+len(chunk) <= max {
			buf = append(buf, chunk...)
		} else {
			tooLong = true
			buf = buf[:0]
		}
		if rerr == bufio.ErrBufferFull {
			continue
		}
		return buf, read, tooLong, rerr
	}
}

// execJournal runs journalctl and streams its stdout.
func execJournal(ctx context.Context, args []string) (io.Reader, func(stop bool) error, error) {
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return stdout, func(stop bool) error {
		if stop {
			_ = cmd.Process.Kill()
		}
		err := cmd.Wait()
		if stop {
			return nil // the kill is ours; its exit status is not a failure
		}
		if err != nil && stderr.Len() > 0 {
			return &journalError{err: err, stderr: stderr.String()}
		}
		return err
	}, nil
}

type journalError struct {
	err    error
	stderr string
}

func (e *journalError) Error() string { return e.err.Error() + ": " + e.stderr }
func (e *journalError) Unwrap() error { return e.err }

// limitedBuffer keeps the first journalStderrBytes written to it.
type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := journalStderrBytes - len(l.b); room > 0 {
		l.b = append(l.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}
func (l *limitedBuffer) Len() int       { return len(l.b) }
func (l *limitedBuffer) String() string { return string(bytes.TrimSpace(l.b)) }
