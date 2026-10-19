package middleware

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type wrappedWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *wrappedWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

type agentRequestSummary struct {
	at, last                time.Time
	count, bytes, throttled int
	routes                  map[string]int
}

var agentRequestSummaries = struct {
	sync.Mutex
	devices map[string]*agentRequestSummary
}{devices: map[string]*agentRequestSummary{}}

func summarizeAgentRequest(r *http.Request, status, bytes int, now time.Time) bool {
	if r.Method != "POST" {
		return false
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "devices" {
		return false
	}
	class := parts[4]
	switch class {
	case "telemetry", "heartbeat", "docker-events", "events", "logs", "probe-results":
	default:
		return false
	}
	if status == 401 || status == 403 {
		return true
	}
	agentRequestSummaries.Lock()
	defer agentRequestSummaries.Unlock()
	for id, s := range agentRequestSummaries.devices {
		if now.Sub(s.last) > 10*time.Minute {
			delete(agentRequestSummaries.devices, id)
		}
	}
	s := agentRequestSummaries.devices[parts[3]]
	if s == nil {
		if len(agentRequestSummaries.devices) >= 4096 {
			return true
		}
		s = &agentRequestSummary{at: now, routes: map[string]int{}}
		agentRequestSummaries.devices[parts[3]] = s
	}
	s.last = now
	s.count++
	s.bytes += bytes
	s.routes[class]++
	if status == 429 {
		s.throttled++
	}
	interval := 10 * time.Minute
	if status >= 400 {
		interval = time.Minute
	}
	if now.Sub(s.at) >= interval {
		slog.Info("agent ingest summary", "device_id", parts[3], "requests", s.count, "response_bytes", s.bytes, "throttled", s.throttled, "routes", s.routes)
		s.at = now
		s.count = 0
		s.bytes = 0
		s.throttled = 0
		s.routes = map[string]int{}
	}
	return true
}

func (w *wrappedWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through the logger.
func (w *wrappedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support hijacking")
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &wrappedWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		if summarizeAgentRequest(r, ww.status, ww.bytes, time.Now()) {
			return
		}
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.status,
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr,
		)
	})
}
