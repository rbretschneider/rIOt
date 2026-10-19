package collectors

import (
	"context"
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

// ContainerLogCollector fetches recent log lines from Docker containers.
type ContainerLogCollector struct {
	SocketPath     string
	MaxLines       int
	MaxBytes       int
	lastCollection time.Time
	TailLines      int // number of lines per container (default 50)

	mu       sync.Mutex
	lastSeen map[string]time.Time // containerID -> last seen timestamp
	tty      map[string]bool
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
	maxLines, maxBytes := c.MaxLines, c.MaxBytes
	if maxLines < 1 || maxLines > 1000 {
		maxLines = 50
	}
	if maxBytes < 1024 || maxBytes > 262144 {
		maxBytes = 16384
	}
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
		opts := container.LogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true}
		if ctr.State != "running" {
			if inspect.State == nil {
				cancel()
				continue
			}
			finished, e := time.Parse(time.RFC3339Nano, inspect.State.FinishedAt)
			if e != nil || (!c.lastCollection.IsZero() && !finished.After(c.lastCollection)) {
				delete(c.lastSeen, ctr.ID)
				cancel()
				continue
			}
			opts.Until = finished.Add(time.Second).Format(time.RFC3339Nano)
			if since.IsZero() {
				since = c.lastCollection
			}
		}
		if since.IsZero() {
			opts.Tail = fmt.Sprint(maxLines)
		} else {
			opts.Since = since.Format(time.RFC3339Nano)
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
		ring := capRing{maxLines: maxLines, maxBytes: maxBytes}
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
		MaskLogEntries(ring.entries)
		result.Entries = append(result.Entries, ring.entries...)
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
	c.lastCollection = now
	return result, nil
}
func (c *ContainerLogCollector) newClient() (*client.Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if c.SocketPath != "" {
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
