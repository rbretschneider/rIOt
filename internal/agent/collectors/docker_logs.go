package collectors

import (
	"bufio"
	"context"
	"encoding/binary"
	"strings"

	"fmt"
	"io"

	"runtime"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

const (
	defaultContainerLogMaxLines     = 50
	defaultContainerLogMaxBytes     = 16 << 10
	defaultContainerLogMaxLineBytes = 16 << 10
	containerLogScanMaxLines        = 20000
	containerLogScanMaxBytes        = 8 << 20
	containerLogPassMaxLines        = 5000
	containerLogPassMaxBytes        = 4 << 20
	dockerMaxFrame                  = 1 << 20
	truncationMarker                = " [truncated]"
)

func (c *ContainerLogCollector) limits() (int, int, int) {
	lines, bytes, line := c.MaxLines, c.MaxBytes, c.MaxLineBytes
	if lines <= 0 || lines > 1000 {
		lines = defaultContainerLogMaxLines
	}
	if bytes < 1024 || bytes > 262144 {
		bytes = defaultContainerLogMaxBytes
	}
	if line < 1024 || line > 16<<10 {
		line = defaultContainerLogMaxLineBytes
	}
	return lines, bytes, line
}

// ContainerLogCollector fetches recent log lines from Docker containers.
type ContainerLogCollector struct {
	SocketPath     string
	Host           string
	MaxLineBytes   int
	MaxLines       int
	MaxBytes       int
	lastCollection time.Time
	TailLines      int // number of lines per container (default 50)

	mu            sync.Mutex
	lastSeen      map[string]time.Time // containerID -> last seen timestamp
	tty           map[string]bool
	deferredSince map[string]time.Time
}

func (c *ContainerLogCollector) Name() string { return "container_logs" }

func (c *ContainerLogCollector) Collect(ctx context.Context) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastSeen == nil {
		c.lastSeen = map[string]time.Time{}
	}
	if c.tty == nil {
		c.tty = map[string]bool{}
	}
	if c.deferredSince == nil {
		c.deferredSince = map[string]time.Time{}
	}
	maxLines, maxBytes, maxLineBytes := c.limits()
	linesLeft, bytesLeft := containerLogPassMaxLines, containerLogPassMaxBytes
	result := ContainerLogBatch{}
	cli, err := c.newClient()
	if err != nil {
		return result, nil
	}
	defer cli.Close()
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return result, nil
	}
	now := time.Now().UTC()
	existing := map[string]bool{}
	for _, ctr := range containers {
		existing[ctr.ID] = true
		if linesLeft <= 0 || bytesLeft <= len(truncationMarker) {
			if _, ok := c.deferredSince[ctr.ID]; !ok {
				since := c.lastSeen[ctr.ID]
				if since.IsZero() {
					since = c.lastCollection
				}
				c.deferredSince[ctr.ID] = since
			}
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		tty, known := c.tty[ctr.ID]
		var inspect container.InspectResponse
		if !known || ctr.State != "running" {
			inspect, err = cli.ContainerInspect(callCtx, ctr.ID)
			if err != nil {
				cancel()
				continue
			}
			tty = inspect.Config != nil && inspect.Config.Tty
			c.tty[ctr.ID] = tty
		}
		since := c.lastSeen[ctr.ID]
		deferred, deferredBefore := c.deferredSince[ctr.ID]
		if deferredBefore {
			since = deferred
		}
		opts := container.LogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true}
		if ctr.State != "running" {
			if inspect.State == nil {
				cancel()
				continue
			}
			finished, e := time.Parse(time.RFC3339Nano, inspect.State.FinishedAt)
			if e != nil || (!deferredBefore && !c.lastCollection.IsZero() && !finished.After(c.lastCollection)) {
				delete(c.lastSeen, ctr.ID)
				cancel()
				continue
			}
			opts.Until = finished.Add(time.Second).Format(time.RFC3339Nano)
			if since.IsZero() && !deferredBefore {
				since = c.lastCollection
			}
		}
		if since.IsZero() {
			opts.Tail = fmt.Sprint(maxLines)
		} else {
			opts.Since = since.Format(time.RFC3339Nano)
			// A bounded look-back counts capped-away lines, rather than silently
			// hiding them behind a daemon tail equal to the retained ring size.
			opts.Tail = fmt.Sprint(containerLogScanMaxLines + 1)
		}
		logs, err := cli.ContainerLogs(callCtx, ctr.ID, opts)
		if err != nil {
			cancel()
			continue
		}
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}
		id := ctr.ID
		if len(id) > 12 {
			id = id[:12]
		}
		ring := containerLogCapRing{maxLines: min(maxLines, linesLeft), maxBytes: min(maxBytes, bytesLeft)}
		scanned, bytes := 0, 0
		lower := false
		newest := since
		limited := &io.LimitedReader{R: logs, N: 8 << 20}
		readDockerLogs(limited, tty, false, func(e models.ContainerLogEntry) bool {
			scanned++
			bytes += len(e.Line)
			if e.Timestamp.After(newest) {
				newest = e.Timestamp
			}
			if !since.IsZero() && !e.Timestamp.After(since) {
				return true
			}
			e.ContainerID = id
			e.ContainerName = name
			e.Line = CanonicalLogLine(e.Line, maxLineBytes)
			ring.add(e)
			if scanned >= 20000 || bytes >= 8<<20 {
				lower = true
				return false
			}
			return true
		})
		logs.Close()
		cancel()
		if limited.N == 0 {
			lower = true
		}
		for i := range ring.entries {
			ring.entries[i].Timestamp = ring.entries[i].Timestamp.Truncate(time.Microsecond)
		}
		result.Entries = append(result.Entries, ring.entries...)
		linesLeft -= len(ring.entries)
		bytesLeft -= ring.bytes
		delete(c.deferredSince, ctr.ID)
		if ring.dropped > 0 || lower {
			count := ring.dropped
			if lower {
				count++
			}
			stamp := ring.droppedAt
			if stamp.IsZero() {
				stamp = newest
			}
			result.Gaps = append(result.Gaps, models.ContainerLogGap{ContainerID: id, ContainerName: name, Timestamp: stamp, Count: count, LowerBound: lower, Reason: "agent_cap"})
		}
		if lower {
			newest = now
		}
		if ctr.State == "running" {
			c.lastSeen[ctr.ID] = newest
		} else {
			delete(c.lastSeen, ctr.ID)
		}
	}
	for id := range c.lastSeen {
		if !existing[id] {
			delete(c.lastSeen, id)
		}
	}
	for id := range c.tty {
		if !existing[id] {
			delete(c.tty, id)
		}
	}
	for id := range c.deferredSince {
		if !existing[id] {
			delete(c.deferredSince, id)
		}
	}
	c.lastCollection = now
	return result, nil
}
func (c *ContainerLogCollector) newClient() (*client.Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if c.Host != "" {
		opts = append(opts, client.WithHost(c.Host))
	} else if c.SocketPath != "" {
		host := "unix://" + c.SocketPath
		if runtime.GOOS == "windows" {
			host = "npipe://" + c.SocketPath
		}
		opts = append(opts, client.WithHost(host))
	}
	return client.NewClientWithOpts(opts...)
}

// parseDockerLogStream reads Docker multiplexed log output.
// Docker log stream format: 8-byte header per frame:
//
//	[0]   = stream type (1=stdout, 2=stderr)
//	[1-3] = padding
//	[4-7] = uint32 frame size (big-endian)
func parseDockerLogStream(r io.Reader, containerID, containerName string) []models.ContainerLogEntry {
	var entries []models.ContainerLogEntry
	ReadDockerLogs(io.LimitReader(r, 8<<20), false, func(e models.ContainerLogEntry) bool {
		e.ContainerID = containerID
		e.ContainerName = containerName
		entries = append(entries, e)
		return len(entries) < 20000
	})
	return entries
}

// dockerScanLimits bound the reading of one container's log stream.
type dockerScanLimits struct {
	maxLines  int // newest lines kept
	maxBytes  int // newest bytes kept (sum of line lengths)
	lineBytes int // longer lines are truncated to this, plus the marker
}

// dockerScanResult is what one container's stream produced.
type dockerScanResult struct {
	entries   []models.ContainerLogEntry // oldest first
	keptBytes int
	scanned   int  // lines read from the stream
	dropped   int  // older lines evicted by the ring
	stopped   bool // the scan bound ended the read before the stream did
}

// readDockerLogStream reads Docker multiplexed log output, keeping only the
// newest lines that fit lim in a capRing, and never scanning more than
// containerLogScanMaxLines lines or containerLogScanMaxBytes bytes.
//
// Docker log stream format: 8-byte header per frame:
//
//	[0]   = stream type (1=stdout, 2=stderr)
//	[1-3] = padding
//	[4-7] = uint32 frame size (big-endian)
func readDockerLogStream(r io.Reader, containerID, containerName string, lim dockerScanLimits) dockerScanResult {
	ring := newCapRing(lim.maxLines, lim.maxBytes, func(e models.ContainerLogEntry) int { return len(e.Line) })
	var res dockerScanResult
	br := bufio.NewReader(r)
	var header [8]byte
	scannedBytes := 0

	for {
		if res.scanned >= containerLogScanMaxLines || scannedBytes >= containerLogScanMaxBytes {
			res.stopped = true
			break
		}
		if _, err := io.ReadFull(br, header[:]); err != nil {
			break
		}

		streamType := "stdout"
		if header[0] == 2 {
			streamType = "stderr"
		}

		frameSize := binary.BigEndian.Uint32(header[4:8])
		if frameSize == 0 || frameSize > dockerMaxFrame { // skip empty or >1MB frames
			break
		}
		scannedBytes += len(header) + int(frameSize)

		frame := make([]byte, frameSize)
		if _, err := io.ReadFull(br, frame); err != nil {
			break
		}

		line := string(frame)
		// Docker timestamps format: 2006-01-02T15:04:05.999999999Z <message>
		var ts time.Time
		var msg string
		if len(line) > 31 && line[10] == 'T' {
			if t, err := time.Parse(time.RFC3339Nano, line[:30]); err == nil {
				ts = t
				msg = line[31:] // skip timestamp + space
			} else {
				ts = time.Now().UTC()
				msg = line
			}
		} else {
			ts = time.Now().UTC()
			msg = line
		}

		// Trim trailing newline
		if len(msg) > 0 && msg[len(msg)-1] == '\n' {
			msg = msg[:len(msg)-1]
		}

		if msg == "" {
			continue
		}
		res.scanned++

		ring.add(models.ContainerLogEntry{
			ContainerID:   containerID,
			ContainerName: containerName,
			Timestamp:     ts,
			Stream:        streamType,
			Line:          TruncateLogLine(msg, lim.lineBytes),
		})
	}

	res.entries = ring.items()
	res.keptBytes = ring.size()
	res.dropped = ring.dropped()
	return res
}

// TruncateLogLine makes s safe for storage and cuts it to at most maxBytes at a
// rune boundary, appending truncationMarker when it cut. The result is at most
// maxBytes + len(truncationMarker) bytes (GROWTH-001 FR-062).
func TruncateLogLine(s string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultContainerLogMaxLineBytes
	}
	if len(s) <= maxBytes {
		return models.SanitizeText(s)
	}
	// Cut first so a huge line is not scanned in full, then sanitize the part
	// that is kept (replacing invalid bytes can lengthen it, so cut again).
	cut := models.TruncateHead(models.SanitizeText(models.TruncateHead(s, maxBytes)), maxBytes)
	return cut + truncationMarker
}

// capRing keeps the newest items within an item count and a byte total. Adding
// to a full ring evicts the oldest items and counts them. Memory is bounded by
// the two caps: no more than maxItems items (and, for a single item that alone
// exceeds maxBytes, one item) are ever held.
type capRing[T any] struct {
	maxItems int
	maxBytes int
	sizeOf   func(T) int

	buf     []T
	head    int // index of the oldest item in buf
	total   int // sum of sizeOf over live items
	evicted int
}

func newCapRing[T any](maxItems, maxBytes int, sizeOf func(T) int) *capRing[T] {
	return &capRing[T]{maxItems: maxItems, maxBytes: maxBytes, sizeOf: sizeOf}
}

func (r *capRing[T]) live() int { return len(r.buf) - r.head }

func (r *capRing[T]) add(v T) {
	r.buf = append(r.buf, v)
	r.total += r.sizeOf(v)
	for r.live() > 1 && (r.live() > r.maxItems || r.total > r.maxBytes) {
		var zero T
		r.total -= r.sizeOf(r.buf[r.head])
		r.buf[r.head] = zero // release the evicted item
		r.head++
		r.evicted++
	}
	// Compact once the dead prefix outweighs the live items, so the backing
	// array stays within about twice the cap.
	if r.head > 0 && r.head >= r.live() {
		n := copy(r.buf, r.buf[r.head:])
		clear(r.buf[n:])
		r.buf = r.buf[:n]
		r.head = 0
	}
}

// items returns the live items, oldest first.
func (r *capRing[T]) items() []T {
	out := make([]T, r.live())
	copy(out, r.buf[r.head:])
	return out
}

// size is the byte total of the live items.
func (r *capRing[T]) size() int { return r.total }

// dropped is how many older items were evicted.
func (r *capRing[T]) dropped() int { return r.evicted }
