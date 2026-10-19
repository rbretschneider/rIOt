package ingest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestDUX_AC144_TelemetryIngestRate(t *testing.T) {
	l := New()
	now := time.Now()
	_, p := Class("/telemetry")
	for i := 0; i < 120; i++ {
		if !l.allow("device", "telemetry", p, now) {
			t.Fatalf("burst rejected at %d", i)
		}
	}
	if l.allow("device", "telemetry", p, now) {
		t.Fatal("rate unbounded")
	}
	if !l.allow("device", "telemetry", p, now.Add(15*time.Second)) {
		t.Fatal("no refill")
	}
}
func TestDUX_AC145_HeartbeatThrottleSkipsHandler(t *testing.T) {
	l := New()
	l.now = func() time.Time { return time.Unix(100, 0) }
	called := 0
	r := chi.NewRouter()
	r.With(l.Middleware).Post("/api/v1/devices/{id}/heartbeat", func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(200) })
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/devices/host/heartbeat", nil))
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
	}
	if called != 6 {
		t.Fatalf("throttled heartbeats performed work: %d", called)
	}
}
func TestDUX_BND_AgentBodyCaps(t *testing.T) {
	l := New()
	r := chi.NewRouter()
	r.With(l.Middleware).Post("/api/v1/devices/{id}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		w.WriteHeader(200)
	})
	request := httptest.NewRequest("POST", "/api/v1/devices/host/heartbeat", strings.NewReader(strings.Repeat("x", 65<<10)))
	request.ContentLength = -1
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, request)
	if rec.Code != 413 {
		t.Fatalf("chunked oversized body returned %d", rec.Code)
	}
}
func TestDUX_BND_LimiterIdleEviction(t *testing.T) {
	l := New()
	now := time.Now()
	_, p := Class("/events")
	l.allow("old", "events", p, now)
	l.allow("new", "events", p, now.Add(11*time.Minute))
	if len(l.buckets) != 1 {
		t.Fatal("idle rate state retained")
	}
}
