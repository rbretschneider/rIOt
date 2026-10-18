package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCapturedLogger returns a logger writing JSON records to the buffer.
func newCapturedLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line must be JSON: %q", line)
		out = append(out, rec)
	}
	return out
}

// freeLoopbackPort returns a port that was free on 127.0.0.1 a moment ago.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestHARDEN002_AC002_PprofDisabledByDefault(t *testing.T) {
	addr, err := ParsePprofAddr("")
	assert.NoError(t, err, "empty value is not an error")
	assert.Equal(t, "", addr, "empty value means disabled")

	log, buf := newCapturedLogger()
	p := startPprof("", log)
	assert.Nil(t, p, "no listener may start when RIOT_PPROF_ADDR is empty")
	assert.Equal(t, "", p.Addr(), "nil server reports no address")
	assert.Empty(t, strings.TrimSpace(buf.String()), "disabled pprof must log nothing")
	assert.NoError(t, p.Shutdown(context.Background()), "Shutdown on a nil server is a no-op")
}

func TestHARDEN002_AC003_LoopbackAddrsAccepted(t *testing.T) {
	for _, raw := range []string{"127.0.0.1:6060", "127.0.0.2:6061", "[::1]:6060"} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			addr, err := ParsePprofAddr(raw)
			require.NoError(t, err, "%s must be accepted", raw)
			assert.NotEmpty(t, addr, "an accepted value returns a bind address")
		})
	}
}

func TestHARDEN002_AC003_C5_BindAddrBuiltFromParsedValues(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:6060":  net.JoinHostPort("127.0.0.1", "6060"),
		"127.0.0.2:6061":  net.JoinHostPort("127.0.0.2", "6061"),
		"[::1]:6060":      net.JoinHostPort("::1", "6060"),
		"127.0.0.1:1":     net.JoinHostPort("127.0.0.1", "1"),
		"127.0.0.1:65535": net.JoinHostPort("127.0.0.1", "65535"),
	}
	for raw, want := range cases {
		addr, err := ParsePprofAddr(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, addr, "bind address for %q must be rebuilt from parsed values", raw)
	}

	t.Run("startPprof logs WARN with the exact bound addr", func(t *testing.T) {
		port := freeLoopbackPort(t)
		raw := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		log, buf := newCapturedLogger()
		p := startPprof(raw, log)
		require.NotNil(t, p, "listener should start on a free loopback port")
		defer p.Shutdown(context.Background())

		recs := logRecords(t, buf)
		require.Len(t, recs, 1, "exactly one record expected, got %s", buf.String())
		assert.Equal(t, "WARN", recs[0]["level"], "enablement is logged at WARN")
		assert.Equal(t, raw, recs[0]["addr"], "WARN record must name the bound address")
		assert.Equal(t, raw, p.Addr(), "Addr() reports the bound address")
	})
}

func TestHARDEN002_AC004_NonLoopbackAddrsRejected(t *testing.T) {
	cases := []struct {
		raw    string
		reason string
	}{
		{":6060", "host must be a literal IP"},
		{"0.0.0.0:6060", "host is not loopback"},
		{"[::]:6060", "host is not loopback"},
		{"192.168.1.5:6060", "host is not loopback"},
		{"localhost:6060", "host must be a literal IP"},
		{"127.0.0.1", "missing port"},
		{"127.0.0.1:", "missing port"},
		{"127.0.0.1:0", "port not canonical decimal"},
		{"127.0.0.1:70000", "port out of range"},
		{"garbage", "missing port"},
		// C5(c)
		{"[::ffff:127.0.0.1]:6060", "host is not loopback"},
		{"[::1%lo]:6060", "host has a zone"},
		{"127.1:6060", "host must be a literal IP"},
		{"0177.0.0.1:6060", "host must be a literal IP"},
		{"127.0.0.1:+6060", "port not canonical decimal"},
		{"127.0.0.1:06060", "port not canonical decimal"},
		{"127.0.0.1: 6060", "port not canonical decimal"},
		{"127.0.0.1:60 60", "port not canonical decimal"},
		{"127.0.0.1:-1", "port not canonical decimal"},
		{"127.0.0.1:http", "port not canonical decimal"},
		{"127.0.0.1:123456", "port not canonical decimal"},
		{"10.0.0.1:6060", "host is not loopback"},
		{"[fe80::1]:6060", "host is not loopback"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			addr, err := ParsePprofAddr(tc.raw)
			require.Error(t, err, "%q must be rejected", tc.raw)
			assert.Equal(t, "", addr, "a rejected value returns no bind address")
			assert.Equal(t, tc.reason, err.Error(), "rejection reason for %q", tc.raw)

			log, buf := newCapturedLogger()
			assert.NotPanics(t, func() {
				assert.Nil(t, startPprof(tc.raw, log), "startPprof must not start a listener for %q", tc.raw)
			})
			recs := logRecords(t, buf)
			require.Len(t, recs, 1, "one ERROR record expected for %q, got %s", tc.raw, buf.String())
			assert.Equal(t, "ERROR", recs[0]["level"])
			assert.Equal(t, tc.raw, recs[0]["value"], "ERROR record must name the rejected value")
			assert.Equal(t, tc.reason, recs[0]["reason"], "ERROR record must carry the reason")
		})
	}
}

func TestHARDEN002_AC004_BindFailureDoesNotAbort(t *testing.T) {
	// Occupy a port, then ask pprof to bind it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	raw := ln.Addr().String()

	log, buf := newCapturedLogger()
	var p *pprofServer
	assert.NotPanics(t, func() { p = startPprof(raw, log) })
	assert.Nil(t, p, "a bind failure must disable pprof, not abort startup")
	recs := logRecords(t, buf)
	require.Len(t, recs, 1, "got %s", buf.String())
	assert.Equal(t, "ERROR", recs[0]["level"])
	assert.Equal(t, raw, recs[0]["addr"])
}

func TestHARDEN002_AC005_LoopbackListenerServesPprof(t *testing.T) {
	log, _ := newCapturedLogger()
	p, err := listenPprof("127.0.0.1:0", log)
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	get := func(path string) (int, string) {
		resp, err := http.Get("http://" + p.Addr() + path)
		require.NoError(t, err, "GET %s", path)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	code, body := get("/debug/pprof/")
	assert.Equal(t, http.StatusOK, code, "pprof index")
	assert.Contains(t, body, "Types of profiles available", "index must list profiles")

	code, body = get("/debug/pprof/heap?debug=1")
	assert.Equal(t, http.StatusOK, code, "heap profile")
	assert.Contains(t, body, "heap profile", "heap profile text output")

	code, body = get("/debug/pprof/goroutine?debug=1")
	assert.Equal(t, http.StatusOK, code, "goroutine profile")
	assert.Contains(t, body, "goroutine profile", "goroutine profile text output")
}

func TestHARDEN002_AC007_PprofEnableLoggedAndShutdownCloses(t *testing.T) {
	log, buf := newCapturedLogger()
	p, err := listenPprof("127.0.0.1:0", log)
	require.NoError(t, err)
	addr := p.Addr()

	recs := logRecords(t, buf)
	require.Len(t, recs, 1, "got %s", buf.String())
	assert.Equal(t, "WARN", recs[0]["level"], "enablement is logged at WARN")
	assert.Equal(t, addr, recs[0]["addr"], "WARN record must name the bound address")

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	require.NoError(t, err, "listener must accept connections before shutdown")
	conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(ctx))

	conn, err = net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
	}
	assert.Error(t, err, "after shutdown %s must no longer accept connections", addr)
}
