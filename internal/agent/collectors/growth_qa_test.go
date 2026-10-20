package collectors

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer: adversarial probes of the GROWTH-001 PR-C collector bounds.

// Covers AC-056 / DUX D-41: the 20,000-line scan bound on its own. The existing
// scan-bound test uses 1 KiB lines, so the 8 MiB byte bound ends it first; with
// tiny lines only the line bound can stop a daemon that ignores Tail.
func TestGROWTH_QA_AC056_ScanLineBoundStopsTinyLines(t *testing.T) {
	fd := newFakeDocker(t, fakeContainer{id: "ffffffffffff6666", name: "chatty", lines: numberedLines(60_000, 1, logStart)})
	fd.ignoreTail = true
	c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10}

	lines := collectContainerLogs(t, c)

	require.Len(t, lines, 1000, "[AC-056] still never more than max_lines")
	assert.Equal(t, "line-0020000", strings.Fields(lines[len(lines)-1].Line)[0],
		"[AC-056] the scan must stop at 20,000 lines even though 8 MiB was not reached")
	assert.Less(t, fd.written.Load(), int64(8<<20), "[AC-056] the daemon was cut off by the line bound, not the byte bound")
}

// Covers AC-057 / FR-062: one huge line with no newline, multi-byte and invalid
// UTF-8 inputs, and the exact boundary.
func TestGROWTH_QA_AC057_HugeAndHostileLinesStayBounded(t *testing.T) {
	const max = 4096
	marker := truncationMarker

	t.Run("a 16 MiB line without a newline", func(t *testing.T) {
		got := TruncateLogLine(strings.Repeat("é", 8<<20), max)
		assert.LessOrEqual(t, len(got), max+len(marker))
		assert.True(t, utf8.ValidString(got), "[AC-057] cut at a character boundary")
		assert.True(t, strings.HasSuffix(got, marker))
	})
	t.Run("invalid UTF-8 cannot expand past the bound", func(t *testing.T) {
		// Every invalid byte becomes a 3-byte U+FFFD when sanitized.
		got := TruncateLogLine(string(bytes.Repeat([]byte{0xff}, 1<<20)), max)
		assert.LessOrEqual(t, len(got), max+len(marker), "[AC-057] replacement characters must not break the bound")
		assert.True(t, utf8.ValidString(got))
	})
	t.Run("NUL and control bytes", func(t *testing.T) {
		got := TruncateLogLine(strings.Repeat("a\x00b\x1b[31m", 100_000), max)
		assert.LessOrEqual(t, len(got), max+len(marker))
		assert.NotContains(t, got, "\x00", "[AC-057] NUL bytes must not reach storage")
	})
	t.Run("the exact boundary", func(t *testing.T) {
		assert.Equal(t, strings.Repeat("a", max), TruncateLogLine(strings.Repeat("a", max), max), "exactly max is not marked")
		assert.Equal(t, strings.Repeat("a", max)+marker, TruncateLogLine(strings.Repeat("a", max+1), max), "max+1 is cut and marked")
		assert.Equal(t, "", TruncateLogLine("", max))
	})
	t.Run("a multi-byte rune straddling the boundary", func(t *testing.T) {
		in := strings.Repeat("a", max-1) + "€" + "tail" // the euro sign spans bytes max-1 .. max+1
		got := TruncateLogLine(in, max)
		assert.Equal(t, strings.Repeat("a", max-1)+marker, got, "[AC-057] the straddling rune is dropped whole")
	})
}

// Covers AC-056 / AC-057: one 1 MiB Docker frame with no newline is cut to the
// line bound, and does not stop the lines behind it.
func TestGROWTH_QA_AC057_OneMegabyteFrameIsTruncatedNotKeptWhole(t *testing.T) {
	frame := func(payload string) []byte {
		var h [8]byte
		h[0] = 1
		binary.BigEndian.PutUint32(h[4:], uint32(len(payload)))
		return append(h[:], payload...)
	}
	var stream bytes.Buffer
	stream.Write(frame("2026-01-02T03:04:05.000000001Z " + strings.Repeat("z", dockerMaxFrame-64)))
	stream.Write(frame("2026-01-02T03:04:05.000000002Z after\n"))

	res := readDockerLogStream(&stream, "abc", "web", dockerScanLimits{maxLines: 1000, maxBytes: 256 << 10, lineBytes: 4096})

	require.Len(t, res.entries, 2)
	assert.LessOrEqual(t, len(res.entries[0].Line), 4096+len(truncationMarker), "[AC-057] the huge frame is truncated")
	assert.Equal(t, "after", res.entries[1].Line, "the line behind the huge frame is still read")
	assert.LessOrEqual(t, res.keptBytes, 256<<10)
}
