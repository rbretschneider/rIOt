package collectors

import (
	"bufio"
	"encoding/binary"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// ReadDockerLogs streams bounded logical lines without allocating Docker frame sizes.
// Returning false from emit stops the read immediately. Raw TTY streams have no headers.
func ReadDockerLogs(r io.Reader, tty bool, emit func(models.ContainerLogEntry) bool) error {
	return readDockerLogs(r, tty, true, emit)
}

func readDockerLogs(r io.Reader, tty, truncateTime bool, emit func(models.ContainerLogEntry) bool) error {
	br := bufio.NewReaderSize(r, 4096)
	readLines := func(r io.Reader, stream string) bool {
		lr := bufio.NewReaderSize(r, 4096)
		var line []byte
		overflow := false
		for {
			part, err := lr.ReadSlice('\n')
			if len(line) < 16384 {
				n := min(len(part), 16384-len(line))
				line = append(line, part[:n]...)
				if n < len(part) {
					overflow = true
				}
			} else if len(part) > 0 {
				overflow = true
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if len(line) > 0 {
				s := strings.TrimSuffix(string(line), "\n")
				for !utf8.ValidString(s) {
					s = s[:len(s)-1]
				}
				ts := time.Now().UTC()
				if i := strings.IndexByte(s, ' '); i > 0 {
					if t, e := time.Parse(time.RFC3339Nano, s[:i]); e == nil {
						ts = t
						s = s[i+1:]
					}
				}
				if overflow {
					s += " [truncated]"
				}
				if truncateTime {
					ts = ts.Truncate(time.Microsecond)
				}
				if !emit(models.ContainerLogEntry{Timestamp: ts, Stream: stream, Line: s}) {
					return false
				}
			}
			line = nil
			overflow = false
			if err != nil {
				return true
			}
		}
	}
	if tty {
		readLines(br, "stdout")
		return nil
	}
	for {
		var h [8]byte
		if _, e := io.ReadFull(br, h[:]); e != nil {
			return e
		}
		size := binary.BigEndian.Uint32(h[4:])
		stream := "stdout"
		if h[0] == 2 {
			stream = "stderr"
		}
		if h[0] != 1 && h[0] != 2 {
			return io.ErrUnexpectedEOF
		}
		if !readLines(io.LimitReader(br, int64(size)), stream) {
			return nil
		}
	}
}

type containerLogCapRing struct {
	entries                   []models.ContainerLogEntry
	bytes, maxLines, maxBytes int
	dropped                   int64
	droppedAt                 time.Time
}

func (r *containerLogCapRing) add(e models.ContainerLogEntry) {
	if len(e.Line) > r.maxBytes {
		r.dropped++
		r.droppedAt = e.Timestamp
		return
	}
	r.entries = append(r.entries, e)
	r.bytes += len(e.Line)
	for len(r.entries) > r.maxLines || r.bytes > r.maxBytes {
		old := r.entries[0]
		r.entries = r.entries[1:]
		r.bytes -= len(old.Line)
		r.dropped++
		r.droppedAt = old.Timestamp
	}
}
func MaskLogEntries(entries []models.ContainerLogEntry) {
	for i := range entries {
		entries[i].Timestamp = entries[i].Timestamp.Truncate(time.Microsecond)
		entries[i].Line = CanonicalLogLine(entries[i].Line, 16<<10)
	}
}

// CanonicalLogLine applies the identical configured line limit and masking to
// stored and live logs, preserving their boundary identity after truncation.
func CanonicalLogLine(line string, maxBytes int) string {
	if maxBytes < 1024 || maxBytes > 16<<10 {
		maxBytes = 16 << 10
	}
	line = TruncateLogLine(line, maxBytes)
	if len(line) > maxBytes {
		line = TruncateLogLine(line, maxBytes-len(truncationMarker))
	}
	line, _ = redact.MaskLine(line)
	if len(line) > maxBytes {
		return TruncateLogLine(line, maxBytes-len(truncationMarker))
	}
	return line
}

type ContainerLogBatch struct {
	Entries []models.ContainerLogEntry
	Gaps    []models.ContainerLogGap
}
