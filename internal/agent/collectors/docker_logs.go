package collectors

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Bounds on one container-log collection pass (GROWTH-001 AD-017). The first
// three are the defaults of the configurable max_lines, max_bytes and
// max_line_bytes keys; the rest are fixed.
const (
	defaultContainerLogMaxLines     = 1000
	defaultContainerLogMaxBytes     = 256 << 10
	defaultContainerLogMaxLineBytes = 4 << 10

	// A single container is never scanned past 20,000 lines or 8 MiB, whatever
	// the daemon sends (DUX D-41).
	containerLogScanMaxLines = 20000
	containerLogScanMaxBytes = 8 << 20

	// One whole pass keeps at most 5,000 lines and 4 MiB across all containers.
	containerLogPassMaxLines = 5000
	containerLogPassMaxBytes = 4 << 20

	// dockerMaxFrame is the largest single log frame accepted from the daemon.
	dockerMaxFrame = 1 << 20

	// truncationMarker is appended to a line that was cut.
	truncationMarker = " [truncated]"
)

// ContainerLogCollector fetches recent log lines from Docker containers.
type ContainerLogCollector struct {
	SocketPath string
	// Host overrides the Docker daemon address (for example tcp://127.0.0.1:2375).
	// It exists so tests can point the collector at a fake daemon.
	Host string

	// Per-container limits per collection pass. Zero means the default; the agent
	// passes values already clamped to their configured ranges.
	MaxLines     int
	MaxBytes     int
	MaxLineBytes int

	mu       sync.Mutex
	lastSeen map[string]time.Time // containerID -> last seen timestamp
}

func (c *ContainerLogCollector) Name() string { return "container_logs" }

// limits returns the per-container limits with defaults applied.
func (c *ContainerLogCollector) limits() (maxLines, maxBytes, maxLineBytes int) {
	maxLines, maxBytes, maxLineBytes = c.MaxLines, c.MaxBytes, c.MaxLineBytes
	if maxLines <= 0 {
		maxLines = defaultContainerLogMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = defaultContainerLogMaxBytes
	}
	if maxLineBytes <= 0 {
		maxLineBytes = defaultContainerLogMaxLineBytes
	}
	return maxLines, maxBytes, maxLineBytes
}

func (c *ContainerLogCollector) Collect(ctx context.Context) (interface{}, error) {
	c.mu.Lock()
	if c.lastSeen == nil {
		c.lastSeen = make(map[string]time.Time)
	}
	c.mu.Unlock()

	maxLines, maxBytes, maxLineBytes := c.limits()

	cli, err := c.newClient()
	if err != nil {
		return []models.ContainerLogEntry{}, nil
	}
	defer cli.Close()

	if _, err := cli.Ping(ctx); err != nil {
		return []models.ContainerLogEntry{}, nil
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: false}) // running only
	if err != nil {
		return []models.ContainerLogEntry{}, nil
	}

	var (
		allLogs          []models.ContainerLogEntry
		passLinesLeft    = containerLogPassMaxLines
		passBytesLeft    = containerLogPassMaxBytes
		droppedLines     int
		skippedContainer int
	)
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = ctr.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}

		shortID := ctr.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		// The pass budget caps what this container may add. A container left
		// without budget is not read at all, so its cursor stays put and the next
		// pass picks its lines up.
		lines := min(maxLines, passLinesLeft)
		bytes := min(maxBytes, passBytesLeft)
		if lines <= 0 || bytes <= len(truncationMarker) {
			skippedContainer++
			continue
		}

		c.mu.Lock()
		since := c.lastSeen[ctr.ID]
		c.mu.Unlock()

		opts := container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: true,
			// The daemon never sends more than the newest max_lines lines, so a
			// container that wrote 50,000 lines costs one bounded read.
			Tail: strconv.Itoa(maxLines),
		}
		if !since.IsZero() {
			opts.Since = since.Format(time.RFC3339Nano)
		}

		logs, err := cli.ContainerLogs(ctx, ctr.ID, opts)
		if err != nil {
			slog.Debug("container_logs: failed to get logs", "container", name, "error", err)
			continue
		}

		res := readDockerLogStream(logs, shortID, name, dockerScanLimits{
			maxLines:  lines,
			maxBytes:  bytes,
			lineBytes: min(maxLineBytes, bytes-len(truncationMarker)),
		})
		logs.Close()

		droppedLines += res.dropped
		if len(res.entries) > 0 {
			passLinesLeft -= len(res.entries)
			passBytesLeft -= res.keptBytes
			allLogs = append(allLogs, res.entries...)
			c.mu.Lock()
			c.lastSeen[ctr.ID] = res.entries[len(res.entries)-1].Timestamp
			c.mu.Unlock()
		}
	}

	if droppedLines > 0 || skippedContainer > 0 {
		slog.Warn("container_logs: older lines dropped to stay within the per-pass limits",
			"dropped_lines", droppedLines, "containers_deferred", skippedContainer,
			"max_lines", maxLines, "max_bytes", maxBytes)
	}

	// Clean up lastSeen for containers that no longer exist
	c.mu.Lock()
	running := make(map[string]struct{}, len(containers))
	for _, ctr := range containers {
		running[ctr.ID] = struct{}{}
	}
	for id := range c.lastSeen {
		if _, ok := running[id]; !ok {
			delete(c.lastSeen, id)
		}
	}
	c.mu.Unlock()

	return allLogs, nil
}

func (c *ContainerLogCollector) newClient() (*client.Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	switch {
	case c.Host != "":
		opts = append(opts, client.WithHost(c.Host))
	case c.SocketPath != "":
		host := "unix://" + c.SocketPath
		if runtime.GOOS == "windows" {
			host = "npipe://" + c.SocketPath
		}
		opts = append(opts, client.WithHost(host))
	}
	return client.NewClientWithOpts(opts...)
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
