package collectors

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLogLine is one line a fake container "wrote".
type fakeLogLine struct {
	ts   time.Time
	text string
}

// fakeContainer is a running container on the fake daemon.
type fakeContainer struct {
	id    string
	name  string
	lines []fakeLogLine
}

// fakeDocker is a minimal Docker daemon: ping, container list and logs. Its
// logs endpoint honours Tail and Since like the real one unless ignoreTail is
// set, and records every request's query.
type fakeDocker struct {
	srv *httptest.Server

	mu         sync.Mutex
	containers []fakeContainer
	ignoreTail bool
	queries    []map[string]string // one per logs request, in order
	written    atomic.Int64        // body bytes the daemon managed to write
}

func newFakeDocker(t *testing.T, containers ...fakeContainer) *fakeDocker {
	t.Helper()
	fd := &fakeDocker{containers: containers}
	fd.srv = httptest.NewServer(http.HandlerFunc(fd.serve))
	t.Cleanup(fd.srv.Close)
	return fd
}

func (fd *fakeDocker) host() string { return "tcp://" + strings.TrimPrefix(fd.srv.URL, "http://") }

func (fd *fakeDocker) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/_ping"):
		w.Header().Set("API-Version", "1.43")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("OK"))
	case strings.HasSuffix(path, "/containers/json"):
		type ctr struct {
			ID    string   `json:"Id"`
			Names []string `json:"Names"`
			State string   `json:"State"`
		}
		var out []ctr
		fd.mu.Lock()
		for _, c := range fd.containers {
			out = append(out, ctr{ID: c.id, Names: []string{"/" + c.name}, State: "running"})
		}
		fd.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	case strings.HasSuffix(path, "/json"):
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"Config": map[string]interface{}{"Tty": false}, "State": map[string]interface{}{"Running": true}})
	case strings.HasSuffix(path, "/logs"):
		fd.serveLogs(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (fd *fakeDocker) serveLogs(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	id := parts[len(parts)-2]
	q := map[string]string{"tail": r.URL.Query().Get("tail"), "since": r.URL.Query().Get("since"), "id": id}

	fd.mu.Lock()
	fd.queries = append(fd.queries, q)
	var lines []fakeLogLine
	for _, c := range fd.containers {
		if c.id == id {
			lines = c.lines
		}
	}
	ignoreTail := fd.ignoreTail
	fd.mu.Unlock()

	// The Docker client sends Since as "<unix seconds>.<nanoseconds>". Like the
	// real daemon, the filter is inclusive of the boundary line.
	if since := q["since"]; since != "" {
		secs, nanos, _ := strings.Cut(since, ".")
		s, serr := strconv.ParseInt(secs, 10, 64)
		n, nerr := strconv.ParseInt(nanos, 10, 64)
		if serr == nil && nerr == nil {
			st := time.Unix(s, n)
			var kept []fakeLogLine
			for _, l := range lines {
				if !l.ts.Before(st) {
					kept = append(kept, l)
				}
			}
			lines = kept
		}
	}
	if tail, err := strconv.Atoi(q["tail"]); err == nil && !ignoreTail && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}

	w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
	flusher, _ := w.(http.Flusher)
	for _, l := range lines {
		payload := l.ts.UTC().Format("2006-01-02T15:04:05.000000000Z") + " " + l.text + "\n"
		var header [8]byte
		header[0] = 1
		binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
		if _, err := w.Write(header[:]); err != nil {
			return
		}
		n, err := w.Write([]byte(payload))
		fd.written.Add(int64(8 + n))
		if err != nil || r.Context().Err() != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (fd *fakeDocker) logQueries() []map[string]string {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	return append([]map[string]string(nil), fd.queries...)
}

// numberedLines makes n lines "line-0000001 <padding>" one microsecond apart,
// each about size bytes long.
func numberedLines(n, size int, start time.Time) []fakeLogLine {
	out := make([]fakeLogLine, n)
	for i := range out {
		prefix := fmt.Sprintf("line-%07d ", i+1)
		pad := ""
		if size > len(prefix) {
			pad = strings.Repeat("p", size-len(prefix))
		}
		out[i] = fakeLogLine{ts: start.Add(time.Duration(i) * time.Microsecond), text: prefix + pad}
	}
	return out
}

var logStart = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func collectContainerLogs(t *testing.T, c *ContainerLogCollector) []containerLine {
	t.Helper()
	got, err := c.Collect(context.Background())
	require.NoError(t, err)
	return toLines(t, got)
}

type containerLine struct {
	ID, Name, Stream, Line string
	TS                     time.Time
}

func toLines(t *testing.T, got interface{}) []containerLine {
	t.Helper()
	batch, ok := got.(ContainerLogBatch)
	entries := batch.Entries
	require.True(t, ok, "Collect must return []models.ContainerLogEntry, got %T", got)
	out := make([]containerLine, len(entries))
	for i, e := range entries {
		out[i] = containerLine{ID: e.ContainerID, Name: e.ContainerName, Stream: e.Stream, Line: e.Line, TS: e.Timestamp}
	}
	return out
}

func TestGROWTH_AC056_NewestThousandLinesBoundedRead(t *testing.T) {
	t.Run("a container that wrote 50,000 lines yields the newest 1,000 and the daemon is asked for only 1,000", func(t *testing.T) {
		fd := newFakeDocker(t, fakeContainer{id: "aaaaaaaaaaaa1111", name: "web", lines: numberedLines(50_000, 100, logStart)})
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10}

		lines := collectContainerLogs(t, c)

		require.Len(t, lines, 1000, "[AC-056] exactly the newest 1,000 lines")
		assert.Equal(t, "line-0049001", strings.Fields(lines[0].Line)[0], "[AC-056] the oldest kept line is #49,001")
		assert.Equal(t, "line-0050000", strings.Fields(lines[999].Line)[0], "[AC-056] the newest line is last")
		q := fd.logQueries()
		require.Len(t, q, 1)
		assert.Equal(t, "1000", q[0]["tail"], "[AC-056] the daemon must be asked for Tail = max_lines")
		assert.Less(t, fd.written.Load(), int64(1000*160), "[AC-056] the bytes read stay within the bound, not the 5 MB the container wrote")
		assert.Equal(t, "web", lines[0].Name)
		assert.Equal(t, "aaaaaaaaaaaa", lines[0].ID, "the short ID is 12 characters")
	})

	t.Run("the next pass asks for Since the last line seen and the same Tail", func(t *testing.T) {
		all := numberedLines(3000, 100, logStart)
		fd := newFakeDocker(t, fakeContainer{id: "bbbbbbbbbbbb2222", name: "api", lines: all[:2000]})
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10}
		first := collectContainerLogs(t, c)
		require.Len(t, first, 1000)

		fd.mu.Lock()
		fd.containers[0].lines = all
		fd.mu.Unlock()
		second := collectContainerLogs(t, c)

		q := fd.logQueries()
		require.Len(t, q, 2)
		assert.Equal(t, "20001", q[1]["tail"], "[AC-056] every pass is bounded by Tail")
		last := first[len(first)-1].TS
		assert.Equal(t, fmt.Sprintf("%d.%09d", last.Unix(), last.Nanosecond()), q[1]["since"],
			"[AC-056] Since is the last line seen (the Docker client sends unix seconds.nanoseconds)")
		assert.LessOrEqual(t, len(second), 1000)
	})

	t.Run("max_lines from the config lowers the Tail", func(t *testing.T) {
		fd := newFakeDocker(t, fakeContainer{id: "cccccccccccc3333", name: "db", lines: numberedLines(500, 50, logStart)})
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 100}

		lines := collectContainerLogs(t, c)

		assert.Len(t, lines, 100)
		assert.Equal(t, "100", fd.logQueries()[0]["tail"])
	})

	t.Run("a daemon that ignores Tail is cut off at the scan bound and the newest lines of what was read are kept", func(t *testing.T) {
		fd := newFakeDocker(t, fakeContainer{id: "dddddddddddd4444", name: "noisy", lines: numberedLines(50_000, 1000, logStart)})
		fd.ignoreTail = true
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10}

		lines := collectContainerLogs(t, c)

		require.NotEmpty(t, lines)
		assert.LessOrEqual(t, len(lines), 1000, "[AC-056] never more than max_lines")
		assert.Less(t, fd.written.Load(), int64(24<<20), "[AC-056] the scan must stop near 8 MiB, not read all 50 MB")
		last, err := strconv.Atoi(strings.TrimPrefix(strings.Fields(lines[len(lines)-1].Line)[0], "line-"))
		require.NoError(t, err)
		assert.Less(t, last, 12_000, "[AC-056] the scan stopped after about 8 MiB of 1 KiB lines (about 8,000), not at line 50,000")
	})
}

func TestGROWTH_AC056_ByteAndPassBoundsHold(t *testing.T) {
	t.Run("max_bytes keeps the newest lines within 256 KiB per container", func(t *testing.T) {
		fd := newFakeDocker(t, fakeContainer{id: "eeeeeeeeeeee5555", name: "fat", lines: numberedLines(1000, 1000, logStart)})
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10}

		lines := collectContainerLogs(t, c)

		total := 0
		for _, l := range lines {
			total += len(l.Line)
		}
		assert.LessOrEqual(t, total, 256<<10, "[AC-056] a container may not keep more than max_bytes")
		assert.Greater(t, total, 200<<10, "[AC-056] but it should keep close to that")
		assert.Equal(t, "line-0001000", strings.Fields(lines[len(lines)-1].Line)[0], "the newest line is kept")
	})

	t.Run("a pass keeps at most 5,000 lines and defers the containers it has no budget for", func(t *testing.T) {
		var ctrs []fakeContainer
		for i := 0; i < 8; i++ {
			ctrs = append(ctrs, fakeContainer{
				id: fmt.Sprintf("%012d%04d", i, i), name: fmt.Sprintf("c%d", i),
				lines: numberedLines(1000, 60, logStart),
			})
		}
		fd := newFakeDocker(t, ctrs...)
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10}

		first := collectContainerLogs(t, c)
		assert.Len(t, first, 5000, "[AC-056] the pass cap is 5,000 lines")
		assert.Len(t, fd.logQueries(), 5, "[AC-056] containers past the budget are not even read")

		// The deferred containers keep their cursor, so the next pass reads them.
		second := collectContainerLogs(t, c)
		names := map[string]bool{}
		for _, l := range second {
			names[l.Name] = true
		}
		assert.True(t, names["c5"] && names["c6"] && names["c7"], "[AC-056] deferred containers are served on the next pass, got %v", names)
	})

	t.Run("a pass keeps at most 4 MiB", func(t *testing.T) {
		var ctrs []fakeContainer
		for i := 0; i < 20; i++ {
			ctrs = append(ctrs, fakeContainer{
				id: fmt.Sprintf("%012d%04d", i, i), name: fmt.Sprintf("c%d", i),
				lines: numberedLines(300, 1000, logStart),
			})
		}
		fd := newFakeDocker(t, ctrs...)
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10}

		lines := collectContainerLogs(t, c)

		total := 0
		for _, l := range lines {
			total += len(l.Line)
		}
		assert.LessOrEqual(t, total, 4<<20, "[AC-056] the pass byte cap is 4 MiB")
		assert.Greater(t, total, 3<<20, "[AC-056] and most of it is used")
	})
}

func TestGROWTH_AC057_LongLinesTruncatedAtRuneBoundary(t *testing.T) {
	t.Run("a 10 KiB container-log line is 4 KiB plus the marker", func(t *testing.T) {
		fd := newFakeDocker(t, fakeContainer{id: "ffffffffffff6666", name: "long", lines: []fakeLogLine{{ts: logStart, text: strings.Repeat("a", 10<<10)}}})
		c := &ContainerLogCollector{Host: fd.host(), MaxLines: 1000, MaxBytes: 256 << 10, MaxLineBytes: 4096}

		lines := collectContainerLogs(t, c)

		require.Len(t, lines, 1)
		assert.Equal(t, strings.Repeat("a", 4096-len(" [truncated]"))+" [truncated]", lines[0].Line, "[AC-057] 4 KiB plus the visible marker")
	})

	t.Run("the cut never splits a multi-byte character", func(t *testing.T) {
		// 4096 is not a multiple of 3, so a byte cut would land inside a euro sign.
		euro := strings.Repeat("€", 5000)

		got := TruncateLogLine(euro, 4096)

		assert.True(t, utf8.ValidString(got), "[AC-057] the result must be valid UTF-8")
		assert.True(t, strings.HasSuffix(got, " [truncated]"))
		body := strings.TrimSuffix(got, " [truncated]")
		assert.Equal(t, 4095, len(body), "[AC-057] cut back to the rune boundary at 4,095 bytes")
		assert.Equal(t, 1365, utf8.RuneCountInString(body))
	})

	t.Run("a line within the limit is untouched", func(t *testing.T) {
		assert.Equal(t, "short line", TruncateLogLine("short line", 4096))
		exact := strings.Repeat("b", 4096)
		assert.Equal(t, exact, TruncateLogLine(exact, 4096), "[AC-057] exactly at the limit is not truncated")
	})

	t.Run("NUL bytes and invalid UTF-8 are made safe before storage", func(t *testing.T) {
		got := TruncateLogLine("a\x00b\xffc", 4096)
		assert.True(t, utf8.ValidString(got))
		assert.NotContains(t, got, "\x00", "Postgres TEXT cannot hold NUL")
	})

	t.Run("max_line_bytes from the config lowers the cut", func(t *testing.T) {
		fd := newFakeDocker(t, fakeContainer{id: "999999999999aaaa", name: "cfg", lines: []fakeLogLine{{ts: logStart, text: strings.Repeat("z", 3000)}}})
		c := &ContainerLogCollector{Host: fd.host(), MaxLineBytes: 1024}

		lines := collectContainerLogs(t, c)

		require.Len(t, lines, 1)
		assert.Equal(t, 1024, len(lines[0].Line))
	})
}

func TestGROWTH_AC056_CapRingKeepsNewestAndCountsEvictions(t *testing.T) {
	size := func(s string) int { return len(s) }

	t.Run("item cap", func(t *testing.T) {
		r := newCapRing(3, 1<<20, size)
		for i := 1; i <= 7; i++ {
			r.add(fmt.Sprintf("v%d", i))
		}
		assert.Equal(t, []string{"v5", "v6", "v7"}, r.items())
		assert.Equal(t, 4, r.dropped())
		assert.Equal(t, 6, r.size())
	})

	t.Run("byte cap", func(t *testing.T) {
		r := newCapRing(100, 10, size)
		for _, s := range []string{"aaaa", "bbbb", "cccc", "dd"} {
			r.add(s)
		}
		assert.Equal(t, []string{"bbbb", "cccc", "dd"}, r.items())
		assert.LessOrEqual(t, r.size(), 10)
		assert.Equal(t, 1, r.dropped())
	})

	t.Run("memory stays bounded over a long stream", func(t *testing.T) {
		r := newCapRing(10, 1<<20, size)
		for i := 0; i < 100_000; i++ {
			r.add("x")
		}
		assert.Equal(t, 10, len(r.items()))
		assert.LessOrEqual(t, cap(r.buf), 64, "[AC-056] the backing array must not grow with the stream, cap=%d", cap(r.buf))
	})

	t.Run("an item that alone exceeds the byte cap is still kept as the newest", func(t *testing.T) {
		r := newCapRing(5, 3, size)
		r.add("abcdefgh")
		assert.Equal(t, []string{"abcdefgh"}, r.items())
	})
}
