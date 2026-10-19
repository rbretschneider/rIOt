package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SEC C-7: the device and command IDs are path-escaped, so a hostile command ID
// cannot re-target the authenticated POST to another endpoint.
func TestCL_SEC007_ReportURLPathEscaped(t *testing.T) {
	var gotURI, gotKey, gotKind string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI, gotKey = r.RequestURI, r.Header.Get("X-rIOt-Key")
		var rep models.CommandReport
		_ = json.NewDecoder(r.Body).Decode(&rep)
		gotKind = string(rep.Kind)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"command_id":"x","result":"accepted","status":"succeeded"}`))
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, "secret-key")

	reply, err := c.PostCommandReport(context.Background(), "dev/1", "../../telemetry",
		models.CommandReport{Kind: models.ReportKindAck, CommandID: "../../telemetry"})

	require.NoError(t, err)
	assert.Equal(t, "/api/v1/devices/dev%2F1/commands/..%2F..%2Ftelemetry/report", gotURI,
		"[SEC-007] slashes in either ID must be escaped so the request path keeps its shape")
	assert.Equal(t, "secret-key", gotKey, "[SEC-007] the device key header must be sent")
	assert.Equal(t, "ack", gotKind)
	assert.Equal(t, 200, reply.StatusCode)
	assert.Equal(t, models.ReportAccepted, reply.Response.Result)
}

// AC-025: a legacy server's plain 404 page yields a reply with no code, which
// the outbox reads as "no report endpoint".
func TestCL_AC025_NonJSONReplyYieldsEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, "k")

	reply, err := c.PostCommandReport(context.Background(), idA, idB, models.CommandReport{Kind: models.ReportKindResult})

	require.NoError(t, err)
	assert.Equal(t, 404, reply.StatusCode)
	assert.Empty(t, reply.Response.Code, "[AC-025] a legacy 404 carries no error code")
	assert.True(t, isLegacyReply(reply), "[AC-025] 404 without a code is the legacy-server signature")
}

// scriptedTelemetryServer answers /telemetry with the queued responses in order
// (repeating the last), counting every request that reaches it.
type scriptedTelemetryServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits int
	// script entries are "<status>" or "<status> <Retry-After value>".
	script []string
	other  int // requests to any other path
}

func newScriptedTelemetryServer(t *testing.T, script ...string) *scriptedTelemetryServer {
	t.Helper()
	s := &scriptedTelemetryServer{script: script}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/telemetry") {
			s.other++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		entry := s.script[min(s.hits, len(s.script)-1)]
		s.hits++
		status, retry, _ := strings.Cut(entry, " ")
		code, _ := strconv.Atoi(status)
		if retry != "" {
			w.Header().Set("Retry-After", retry)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"scripted"}`))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *scriptedTelemetryServer) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// manualNow is an injectable clock.
type manualNow struct{ t time.Time }

func (m *manualNow) now() time.Time          { return m.t }
func (m *manualNow) advance(d time.Duration) { m.t = m.t.Add(d) }

func newBackoffClient(t *testing.T, srv *scriptedTelemetryServer) (*HTTPClient, *manualNow) {
	t.Helper()
	clk := &manualNow{t: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	c := NewHTTPClient(srv.srv.URL, "k")
	c.now = clk.now
	return c, clk
}

func sendOne(c *HTTPClient) error {
	return c.SendTelemetry(context.Background(), "dev", &models.TelemetrySnapshot{DeviceID: "dev"})
}

func TestGROWTH_AC063_RetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cases := []struct {
		name, header string
		want         time.Duration
	}{
		{"seconds", "30", 30 * time.Second},
		{"http date", now.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second},
		{"date in the past", now.Add(-time.Minute).UTC().Format(http.TimeFormat), 0},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"garbage", "soon", 0},
		{"absent", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseRetryAfter(tc.header, now), "[AC-063] Retry-After %q", tc.header)
		})
	}
	assert.Greater(t, parseRetryAfter("9999999999999", now), retryAfterMax,
		"[AC-063] an absurd value must come back above the cap so the clamp applies, without overflowing")
}

func TestGROWTH_AC063_429HonoursRetryAfter(t *testing.T) {
	t.Run("no request is sent until Retry-After has passed", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "429 30", "200")
		c, clk := newBackoffClient(t, srv)

		err := sendOne(c)
		var se *StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, 429, se.StatusCode)
		assert.Equal(t, 30*time.Second, se.RetryAfter)

		clk.advance(29 * time.Second)
		err = sendOne(c)
		require.ErrorIs(t, err, ErrBackoff, "[AC-063] 29 s after a 30 s Retry-After the client must still hold back")
		assert.Equal(t, 1, srv.requests(), "[AC-063] a held-back send must not reach the network")

		clk.advance(time.Second)
		require.NoError(t, sendOne(c), "[AC-063] at 30 s the retry is allowed")
		assert.Equal(t, 2, srv.requests())
	})

	t.Run("Retry-After is clamped to [1 s, 1 h]", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "429 999999")
		c, clk := newBackoffClient(t, srv)
		_ = sendOne(c)
		clk.advance(time.Hour - time.Second)
		require.ErrorIs(t, sendOne(c), ErrBackoff, "an absurd Retry-After still holds back")
		clk.advance(time.Second)
		_ = sendOne(c)
		assert.Equal(t, 2, srv.requests(), "[AC-063] the hold must end after at most one hour")
	})

	t.Run("without Retry-After it waits 30 s and doubles up to 10 min", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "429")
		c, clk := newBackoffClient(t, srv)
		want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
		for i, step := range want {
			_ = sendOne(c) // reaches the server and is refused
			hits := srv.requests()
			clk.advance(step - time.Nanosecond)
			require.ErrorIs(t, sendOne(c), ErrBackoff, "[AC-063] step %d: still held back just before %s", i, step)
			clk.advance(time.Nanosecond)
			assert.Equal(t, hits, srv.requests(), "[AC-063] step %d: nothing was sent while held back", i)
		}
	})

	t.Run("a success resets the doubling", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "429", "429", "200", "429")
		c, clk := newBackoffClient(t, srv)
		_ = sendOne(c) // 30 s
		clk.advance(30 * time.Second)
		_ = sendOne(c) // 60 s
		clk.advance(60 * time.Second)
		require.NoError(t, sendOne(c)) // success
		_ = sendOne(c)                 // 429 again: back to 30 s
		clk.advance(30 * time.Second)
		require.NotErrorIs(t, sendOne(c), ErrBackoff, "[AC-063] after a success the wait starts again at 30 s")
	})
}

func TestGROWTH_AC062_401And403BackOffLikeA429(t *testing.T) {
	for _, status := range []string{"401", "403"} {
		t.Run(status, func(t *testing.T) {
			srv := newScriptedTelemetryServer(t, status, "200")
			c, clk := newBackoffClient(t, srv)

			require.Error(t, sendOne(c))
			require.ErrorIs(t, sendOne(c), ErrBackoff, "[AC-062] %s must back telemetry off", status)
			clk.advance(30 * time.Second)
			require.NoError(t, sendOne(c))
		})
	}
}

func TestGROWTH_AC063_OtherStatusesDoNotBackOff(t *testing.T) {
	t.Run("a 400 never holds anything back", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "400")
		c, _ := newBackoffClient(t, srv)
		_ = sendOne(c)
		err := sendOne(c)
		require.NotErrorIs(t, err, ErrBackoff)
		assert.Equal(t, 2, srv.requests())
	})
	t.Run("a 5xx without Retry-After retries on the next tick", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "503")
		c, _ := newBackoffClient(t, srv)
		_ = sendOne(c)
		require.NotErrorIs(t, sendOne(c), ErrBackoff)
		assert.Equal(t, 2, srv.requests())
	})
	t.Run("a 5xx with Retry-After honours it", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "503 20", "200")
		c, clk := newBackoffClient(t, srv)
		_ = sendOne(c)
		require.ErrorIs(t, sendOne(c), ErrBackoff)
		clk.advance(20 * time.Second)
		require.NoError(t, sendOne(c))
	})
	t.Run("heartbeats are not gated by the telemetry back-off", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "429 600")
		c, _ := newBackoffClient(t, srv)
		_ = sendOne(c)
		require.ErrorIs(t, sendOne(c), ErrBackoff)

		_, err := c.SendHeartbeat(context.Background(), "dev", &models.Heartbeat{})

		require.NoError(t, err, "[AC-063] liveness must keep flowing while telemetry is throttled")
		assert.Equal(t, 1, srv.other)
	})
}
