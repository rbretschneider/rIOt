// Package ingest bounds authenticated agent requests before decoding them.
package ingest

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type Policy struct {
	Body     int64
	Interval time.Duration
	Burst    int
	Quiet    bool
}

func Class(path string) (string, Policy) {
	switch {
	case strings.HasSuffix(path, "/telemetry"):
		return "telemetry", Policy{25 << 20, 15 * time.Second, 120, false}
	case strings.HasSuffix(path, "/heartbeat"):
		return "heartbeat", Policy{64 << 10, 10 * time.Second, 6, true}
	case strings.HasSuffix(path, "/docker-events") || strings.HasSuffix(path, "/events"):
		return "events", Policy{1 << 20, time.Second / 2, 120, false}
	case strings.HasSuffix(path, "/logs"):
		return "device_logs", Policy{8 << 20, 10 * time.Second, 6, false}
	case strings.HasSuffix(path, "/probe-results"):
		return "probes", Policy{1 << 20, 10 * time.Second, 6, false}
	default:
		return "reports", Policy{1 << 20, time.Second / 2, 120, false}
	}
}

type bucket struct {
	tokens   float64
	at, last time.Time
}
type Limits struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

func New() *Limits { return &Limits{buckets: map[string]*bucket{}, now: time.Now} }

func (l *Limits) AllowFrame(device string, now time.Time) bool {
	return l.allow(device, "ws", Policy{Interval: time.Second / 200, Burst: 400}, now)
}
func (l *Limits) allow(id, class string, p Policy, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, key)
		}
	}
	key := id + ":" + class
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(p.Burst), at: now}
		l.buckets[key] = b
	}
	b.tokens = min(float64(p.Burst), b.tokens+max(0, now.Sub(b.at).Seconds())/p.Interval.Seconds())
	b.at = now
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type cappedBody struct {
	io.ReadCloser
	large bool
}

func (b *cappedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if _, ok := err.(*http.MaxBytesError); ok {
		b.large = true
	}
	return n, err
}

type capWriter struct {
	http.ResponseWriter
	body  *cappedBody
	wrote bool
}

func (w *capWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	if w.body.large {
		w.Header().Set("Content-Type", "application/json")
		status = 413
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *capWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(200)
	}
	if w.body.large {
		_, err := w.ResponseWriter.Write([]byte(`{"error":"payload too large","code":"payload_too_large"}`))
		return len(p), err
	}
	return w.ResponseWriter.Write(p)
}
func (l *Limits) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		class, p := Class(r.URL.Path)
		if !l.allow(chi.URLParam(r, "id"), class, p, l.now()) {
			w.Header().Set("Content-Type", "application/json")
			if p.Quiet {
				w.WriteHeader(200)
				io.WriteString(w, `{"status":"ok","throttled":true}`)
				return
			}
			w.Header().Set("Retry-After", "15")
			w.WriteHeader(429)
			io.WriteString(w, `{"code":"rate_limited"}`)
			return
		}
		if r.ContentLength > p.Body {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(413)
			io.WriteString(w, `{"code":"payload_too_large"}`)
			return
		}
		body := &cappedBody{ReadCloser: http.MaxBytesReader(w, r.Body, p.Body)}
		r.Body = body
		next.ServeHTTP(&capWriter{ResponseWriter: w, body: body}, r)
	})
}
