package agent

import (
	"context"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"io"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var logIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var dockerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)

type logRequest struct {
	StreamID  string   `json:"stream_id"`
	RequestID string   `json:"request_id"`
	Container string   `json:"container"`
	After     string   `json:"after"`
	Limit     int      `json:"limit"`
	StreamIDs []string `json:"stream_ids"`
}
type liveLine struct {
	Stream    string    `json:"s"`
	Time      time.Time `json:"t"`
	Line      string    `json:"l"`
	Truncated bool      `json:"x,omitempty"`
}
type agentFollow struct {
	cancel context.CancelFunc
	kept   time.Time
	name   string
}
type agentLogState struct {
	mu             sync.Mutex
	streams        map[string]*agentFollow
	backfills      int
	refilledAt     time.Time
	tokens         float64
	backfillTokens float64
	liveTokens     float64
}

func (c *agentWSClient) logEvent(id string, backfill bool, kind string, n int, reason string) {
	d := map[string]interface{}{"kind": kind}
	if backfill {
		d["request_id"] = id
	} else {
		d["stream_id"] = id
	}
	if n > 0 {
		d["n"] = n
	}
	if reason != "" {
		d["reason"] = reason
	}
	b, _ := marshalLogJSON(d)
	c.send(AgentWSMessage{Type: "logs_event", SessionID: id, Data: b})
}
func (c *agentWSClient) handleLogs(ctx context.Context, msg AgentWSMessage) {
	var req logRequest
	if len(msg.Data) > 4096 || json.Unmarshal(msg.Data, &req) != nil {
		return
	}
	s := &c.logs
	s.mu.Lock()
	if s.streams == nil {
		s.streams = map[string]*agentFollow{}
	}
	switch msg.Type {
	case "logs_keep":
		if len(req.StreamIDs) <= 8 {
			for _, id := range req.StreamIDs {
				if f := s.streams[id]; f != nil {
					f.kept = time.Now()
				}
			}
		}
		s.mu.Unlock()
		return
	case "logs_unfollow":
		if f := s.streams[req.StreamID]; f != nil {
			f.cancel()
			delete(s.streams, req.StreamID)
		}
		s.mu.Unlock()
		return
	case "logs_follow":
		if !logIDPattern.MatchString(req.StreamID) || !dockerNamePattern.MatchString(req.Container) {
			s.mu.Unlock()
			return
		}
		if f := s.streams[req.StreamID]; f != nil {
			f.kept = time.Now()
			s.mu.Unlock()
			return
		}
		for _, f := range s.streams {
			if f.name == req.Container {
				s.mu.Unlock()
				c.logEvent(req.StreamID, false, "unavailable", 0, "too_many_streams")
				return
			}
		}
		if len(s.streams) >= 8 {
			s.mu.Unlock()
			c.logEvent(req.StreamID, false, "unavailable", 0, "too_many_streams")
			return
		}
		streamCtx, cancel := context.WithCancel(ctx)
		f := &agentFollow{cancel: cancel, kept: time.Now(), name: req.Container}
		s.streams[req.StreamID] = f
		s.mu.Unlock()
		go func() {
			defer func() {
				s.mu.Lock()
				if s.streams[req.StreamID] == f {
					delete(s.streams, req.StreamID)
				}
				s.mu.Unlock()
				cancel()
			}()
			go func() {
				t := time.NewTicker(time.Minute)
				defer t.Stop()
				for {
					select {
					case <-streamCtx.Done():
						return
					case <-t.C:
						s.mu.Lock()
						stale := time.Since(f.kept) >= 5*time.Minute
						s.mu.Unlock()
						if stale {
							cancel()
							return
						}
					}
				}
			}()
			events, unsubscribe, err := c.agent.dockerEvents.Subscribe()
			if err == nil {
				defer unsubscribe()
			}
			for {
				c.streamLogs(streamCtx, req, false)
				if streamCtx.Err() != nil {
					return
				}
				if events == nil {
					return
				}
				waiting := true
				for waiting {
					select {
					case <-streamCtx.Done():
						return
					case event := <-events:
						if string(event.Action) == "start" && event.Actor.Attributes["name"] == req.Container {
							c.logEvent(req.StreamID, false, "restarted", 0, "")
							waiting = false
						}
					}
				}
			}
		}()
	case "logs_backfill":
		if !logIDPattern.MatchString(req.RequestID) || !dockerNamePattern.MatchString(req.Container) {
			s.mu.Unlock()
			return
		}
		if s.backfills >= 2 {
			s.mu.Unlock()
			c.logEvent(req.RequestID, true, "unavailable", 0, "too_many_streams")
			return
		}
		s.backfills++
		s.mu.Unlock()
		go func() {
			defer func() { s.mu.Lock(); s.backfills--; s.mu.Unlock() }()
			readCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			c.streamLogs(readCtx, req, true)
		}()
	default:
		s.mu.Unlock()
	}
}
func (c *agentWSClient) allowLogBytes(n int, backfill bool) bool {
	return c.allowLogBytesAt(n, backfill, time.Now())
}
func (c *agentWSClient) allowLogBytesAt(n int, backfill bool, now time.Time) bool {
	s := &c.logs
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refilledAt.IsZero() {
		s.refilledAt = now
		s.tokens = 2 << 20
		s.backfillTokens = 1 << 20
		s.liveTokens = 1 << 20
	}
	elapsed := max(now.Sub(s.refilledAt).Seconds(), 0)
	s.tokens = min(float64(2<<20), s.tokens+elapsed*float64(2<<20))
	s.backfillTokens = min(float64(1<<20), s.backfillTokens+elapsed*float64(1<<20))
	s.liveTokens = min(float64(1<<20), s.liveTokens+elapsed*float64(1<<20))
	s.refilledAt = now
	if n < 0 || float64(n) > s.tokens {
		return false
	}
	if backfill {
		if float64(n) > s.backfillTokens {
			return false
		}
		s.backfillTokens -= float64(n)
	} else {
		if s.backfills > 0 && float64(n) > s.liveTokens {
			return false
		}
		s.liveTokens = max(0, s.liveTokens-float64(n))
	}
	s.tokens -= float64(n)
	return true
}
func (c *agentWSClient) streamLogs(ctx context.Context, req logRequest, backfill bool) {
	id := req.StreamID
	if backfill {
		id = req.RequestID
	}
	cli, err := newDockerClient(c.agent.config.Docker.SocketPath)
	if err != nil {
		c.logEvent(id, backfill, "unavailable", 0, "docker_unavailable")
		return
	}
	defer cli.Close()
	insp, err := cli.ContainerInspect(ctx, req.Container)
	if err != nil {
		c.logEvent(id, backfill, "unavailable", 0, "not_found")
		return
	}
	tty := insp.Config != nil && insp.Config.Tty
	opts := container.LogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Follow: !backfill, Tail: "0", Since: time.Now().UTC().Format(time.RFC3339Nano)}
	if backfill {
		opts.Tail = "100"
		opts.Since = ""
		if t, e := time.Parse(time.RFC3339Nano, req.After); e == nil {
			opts.Since = t.Format(time.RFC3339Nano)
			opts.Tail = "5000"
		}
	}
	r, err := cli.ContainerLogs(ctx, insp.ID, opts)
	if err != nil {
		c.logEvent(id, backfill, "unavailable", 0, "unsupported")
		return
	}
	defer r.Close()
	readCtx, stopRead := context.WithCancel(ctx)
	defer stopRead()
	go func() { <-readCtx.Done(); r.Close() }()
	lines := make(chan liveLine, 4)
	done := make(chan struct{})
	var skip atomic.Int64
	go func() {
		defer close(done)
		defer close(lines)
		refilled := time.Now()
		lineTokens, byteTokens := float64(1000), float64(512<<10)
		delivered := 0
		var reader io.Reader = r
		if backfill {
			reader = io.LimitReader(r, 96<<20)
		}
		collectors.ReadDockerLogs(reader, tty, func(e models.ContainerLogEntry) bool {
			if ctx.Err() != nil {
				return false
			}
			if backfill && req.After != "" {
				if t, x := time.Parse(time.RFC3339Nano, req.After); x == nil && e.Timestamp.Before(t) {
					return true
				}
			}
			now := time.Now()
			elapsed := max(now.Sub(refilled).Seconds(), 0)
			lineTokens = min(1000, lineTokens+elapsed*1000)
			byteTokens = min(float64(512<<10), byteTokens+elapsed*float64(512<<10))
			refilled = now
			n := len(e.Line)
			if backfill {
				if delivered >= 5000 {
					return false
				}
				for !c.allowLogBytes(n, true) {
					if !sleepContext(ctx, 20*time.Millisecond) {
						return false
					}
				}
			} else if lineTokens < 1 || byteTokens < float64(n) || !c.allowLogBytes(n, false) {
				skip.Add(1)
				return true
			}
			lineTokens--
			byteTokens -= float64(n)
			if backfill {
				select {
				case <-ctx.Done():
					return false
				default:
				}
			} else if len(lines) == cap(lines) {
				skip.Add(1)
				return true
			}
			limit := 16 << 10
			if c.agent != nil && c.agent.config != nil {
				limit = c.agent.config.Limits().ContainerLogMaxLineBytes
			}
			line := prepareLiveLineWithLimit(e, limit)
			if backfill {
				select {
				case lines <- line:
					delivered++
				case <-ctx.Done():
					return false
				}
				return true
			}
			select {
			case lines <- line:
			default:
				skip.Add(1)
			}
			return true
		})

	}()
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	batch := make([]liveLine, 0, 128)
	batchBytes, total := 0, 0
	flush := func() {
		if len(batch) == 0 {
			return
		}
		d := map[string]interface{}{"lines": batch}
		if backfill {
			d["request_id"] = id
		} else {
			d["stream_id"] = id
		}
		b, _ := marshalLogJSON(d)
		if c.send(AgentWSMessage{Type: "logs_data", SessionID: id, Data: b}) != nil {
			r.Close()
		}
		total += len(batch)
		batch = batch[:0]
		batchBytes = 0
	}
	if !backfill {
		c.logEvent(id, false, "live", 0, "")
	}
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				flush()
				dropped := int(skip.Swap(0))
				if dropped > 0 {
					c.logEvent(id, backfill, "dropped", dropped, "")
				}
				if backfill {
					skipped, lower := countBackfillSkipped(ctx, cli, insp.ID, req.After, tty, total)
					data, _ := json.Marshal(map[string]interface{}{"request_id": id, "kind": "backfill_end", "n": total, "skipped": skipped, "skipped_lb": lower})
					c.send(AgentWSMessage{Type: "logs_event", SessionID: id, Data: data})
				} else {
					code := 0
					if state, err := cli.ContainerInspect(ctx, req.Container); err == nil && state.State != nil {
						code = state.State.ExitCode
					}
					data, _ := json.Marshal(map[string]interface{}{"stream_id": id, "kind": "exited", "code": code})
					c.send(AgentWSMessage{Type: "logs_event", SessionID: id, Data: data})
				}
				return
			}
			encoded, _ := marshalLogJSON(line)
			n := len(encoded) + 1
			if batchBytes+n > 60<<10 {
				flush()
			}
			batch = append(batch, line)
			batchBytes += n
			if len(batch) >= 256 {
				flush()
			}
		case <-t.C:
			flush()
			if !backfill {
				if n := int(skip.Swap(0)); n > 0 {
					c.logEvent(id, false, "dropped", n, "")
				}
			}
		}
	}
}

func prepareLiveLine(e models.ContainerLogEntry) liveLine { return prepareLiveLineWithLimit(e, 16<<10) }
func prepareLiveLineWithLimit(e models.ContainerLogEntry, limit int) liveLine {
	text := collectors.CanonicalLogLine(e.Line, limit)
	line := liveLine{Stream: "o", Time: e.Timestamp.Truncate(time.Microsecond), Line: text, Truncated: strings.HasSuffix(text, " [truncated]")}
	if e.Stream == "stderr" {
		line.Stream = "e"
	}
	return line
}

// The independent skip scan counts only, so even very long backfills never
// retain the history in memory. Its line and byte bounds make loss explicit.
func countBackfillSkipped(ctx context.Context, cli *client.Client, id, after string, tty bool, delivered int) (int, bool) {
	if after == "" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, err := cli.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Since: after, Tail: "all"})
	if err != nil {
		return 0, true
	}
	defer r.Close()
	limited := &io.LimitedReader{R: r, N: 96 << 20}
	n := 0
	collectors.ReadDockerLogs(limited, tty, func(e models.ContainerLogEntry) bool { n++; return n < 20000 })
	return max(n-delivered, 0), n >= 20000 || limited.N == 0
}
