package collectors

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ContainerLogCollector fetches recent log lines from Docker containers.
type ContainerLogCollector struct {
	SocketPath string
	TailLines  int // number of lines per container (default 50)

	mu       sync.Mutex
	lastSeen map[string]time.Time // containerID -> last seen timestamp
}

func (c *ContainerLogCollector) Name() string { return "container_logs" }

func (c *ContainerLogCollector) Collect(ctx context.Context) (interface{}, error) {
	c.mu.Lock()
	if c.lastSeen == nil {
		c.lastSeen = make(map[string]time.Time)
	}
	c.mu.Unlock()

	tail := c.TailLines
	if tail <= 0 {
		tail = 50
	}

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

	var allLogs []models.ContainerLogEntry
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

		c.mu.Lock()
		since := c.lastSeen[ctr.ID]
		c.mu.Unlock()

		opts := container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: true,
		}

		if since.IsZero() {
			// First collection: tail last N lines
			opts.Tail = fmt.Sprintf("%d", tail)
		} else {
			// Subsequent: only since last seen
			opts.Since = since.Format(time.RFC3339Nano)
		}

		logs, err := cli.ContainerLogs(ctx, ctr.ID, opts)
		if err != nil {
			slog.Debug("container_logs: failed to get logs", "container", name, "error", err)
			continue
		}

		entries := parseDockerLogStream(logs, shortID, name)
		logs.Close()

		if len(entries) > 0 {
			allLogs = append(allLogs, entries...)
			c.mu.Lock()
			c.lastSeen[ctr.ID] = entries[len(entries)-1].Timestamp
			c.mu.Unlock()
		}
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
	br := bufio.NewReader(r)

	for {
		// Read 8-byte header
		header := make([]byte, 8)
		if _, err := io.ReadFull(br, header); err != nil {
			break
		}

		streamType := "stdout"
		if header[0] == 2 {
			streamType = "stderr"
		}

		frameSize := binary.BigEndian.Uint32(header[4:8])
		if frameSize == 0 || frameSize > 1<<20 { // skip empty or >1MB frames
			break
		}

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

		entries = append(entries, models.ContainerLogEntry{
			ContainerID:   containerID,
			ContainerName: containerName,
			Timestamp:     ts,
			Stream:        streamType,
			Line:          msg,
		})
	}

	return entries
}
