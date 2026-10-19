package server

// Added by QA Engineer (HARDEN-002). Probes through the real setupRouter and the
// pprof listener lifecycle.

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func qaConnectSrc(t *testing.T, policy string) []string {
	t.Helper()
	for _, part := range strings.Split(policy, ";") {
		f := strings.Fields(part)
		if len(f) > 0 && f[0] == "connect-src" {
			return f[1:]
		}
	}
	t.Fatalf("no connect-src in %q", policy)
	return nil
}

// Covers AC-012: the CSP built from the real request Host keeps the ws/wss
// same-host sources for the Host shapes a LAN deployment actually sees (IPv4 with port,
// bare hostname, bracketed IPv6 with port, mixed case) and, through the full
// router, a spoofed Host cannot alter any other directive.
func TestHARDEN002_QA_AC012_CSPHostShapesThroughRouter(t *testing.T) {
	_, h := newRouterHarness(t, nil)

	cases := []struct {
		host string
		want []string // exact connect-src source list
	}{
		{"192.0.2.10:7331", []string{"'self'", "ws://192.0.2.10:7331", "wss://192.0.2.10:7331"}},
		{"[::1]:7331", []string{"'self'", "ws://[::1]:7331", "wss://[::1]:7331"}},
		{"riot.lan", []string{"'self'", "ws://riot.lan", "wss://riot.lan"}},
		{"RIOT.Lan:443", []string{"'self'", "ws://RIOT.Lan:443", "wss://RIOT.Lan:443"}},
		{"[fe80::1%25eth0]:7331", []string{"'self'"}},
		{"evil.example;script-src", []string{"'self'"}},
		{"a.example,b.example", []string{"'self'"}},
		{"a.example *", []string{"'self'"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.host, func(t *testing.T) {
			rec := doRequest(h, "GET", "/", func(r *http.Request) { r.Host = tc.host })
			require.Equal(t, http.StatusOK, rec.Code)
			policy := rec.Header().Get("Content-Security-Policy")
			assert.Equal(t, tc.want, qaConnectSrc(t, policy), "policy: %s", policy)
			assert.Equal(t, 1, strings.Count(policy, "script-src"), "script-src must appear exactly once: %s", policy)
			assert.Contains(t, policy, "script-src 'self';")
			assert.Contains(t, policy, "frame-ancestors 'none'")
		})
	}
}

// Covers AC-018: Content-Type for every extension the embedded build can emit,
// including .svg and .json which the shipped test only implied, and an unknown
// extension that must still get a non-sniffable, non-octet-stream answer.
func TestHARDEN002_QA_AC018_StaticContentTypesAllExtensions(t *testing.T) {
	s, h := newRouterHarness(t, nil)
	s.FrontendFS = fstest.MapFS{
		"index.html":           {Data: []byte("<html>" + spaMarker + "</html>")},
		"assets/a.js":          {Data: []byte("1")},
		"assets/a.mjs":         {Data: []byte("1")},
		"assets/a.css":         {Data: []byte("a{}")},
		"assets/a.woff2":       {Data: []byte("wOF2")},
		"assets/a.woff":        {Data: []byte("wOFF")},
		"assets/a.ttf":         {Data: []byte("ttf")},
		"assets/icon.svg":      {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"assets/a.png":         {Data: []byte("\x89PNG\r\n\x1a\n")},
		"assets/a.webp":        {Data: []byte("RIFF")},
		"manifest.webmanifest": {Data: []byte(`{"name":"x"}`)},
		"assets/a.js.map":      {Data: []byte("{}")},
		"data.json":            {Data: []byte("{}")},
	}
	h = s.setupRouter()

	cases := map[string]string{
		"/assets/a.js":          "text/javascript",
		"/assets/a.mjs":         "text/javascript",
		"/assets/a.css":         "text/css",
		"/assets/a.woff2":       "font/woff2",
		"/assets/a.woff":        "font/woff",
		"/assets/a.ttf":         "font/ttf",
		"/assets/icon.svg":      "image/svg+xml",
		"/assets/a.png":         "image/png",
		"/assets/a.webp":        "image/webp",
		"/manifest.webmanifest": "application/manifest+json",
		"/assets/a.js.map":      "application/json",
		"/data.json":            "application/json",
	}
	for path, want := range cases {
		path, want := path, want
		t.Run(path, func(t *testing.T) {
			rec := doRequest(h, "GET", path, nil)
			require.Equal(t, http.StatusOK, rec.Code, "GET %s", path)
			ct := rec.Header().Get("Content-Type")
			assert.True(t, strings.HasPrefix(ct, want), "GET %s: Content-Type %q must start with %q", path, ct, want)
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		})
	}

	t.Run("missing asset falls back to the SPA as html, not as the asset's type", func(t *testing.T) {
		rec := doRequest(h, "GET", "/assets/missing.js", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html"), "got %q", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), spaMarker)
	})
}

// Covers AC-007: a pprof listener with a profile request in flight is still
// released when the shutdown deadline expires (Deviation 2: Close fallback), so
// the loopback port is never left bound.
func TestHARDEN002_QA_AC007_ShutdownWithStreamingProfileReleasesPort(t *testing.T) {
	log, _ := newCapturedLogger()
	p, err := listenPprof("127.0.0.1:0", log)
	require.NoError(t, err)
	addr := p.Addr()

	// A 10 s CPU profile blocks the response for its whole duration.
	client := &http.Client{Timeout: 15 * time.Second}
	reqDone := make(chan struct{})
	go func() {
		defer close(reqDone)
		resp, err := client.Get("http://" + addr + "/debug/pprof/profile?seconds=10")
		if err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(300 * time.Millisecond) // let the request reach the handler

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = p.Shutdown(ctx)
	assert.Less(t, time.Since(start), 3*time.Second, "shutdown must not wait for the 10 s profile")

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
	}
	assert.Error(t, err, "%s must be released after shutdown even with a profile in flight", addr)

	select {
	case <-reqDone:
	case <-time.After(15 * time.Second):
		t.Fatal("the in-flight profile request never ended")
	}
}

// Covers AC-004, AC-007: shutting a listener down twice, and shutting down a nil
// server, is harmless (server.go shuts pprof down unconditionally).
func TestHARDEN002_QA_AC007_ShutdownIsIdempotent(t *testing.T) {
	log, _ := newCapturedLogger()
	p, err := listenPprof("127.0.0.1:0", log)
	require.NoError(t, err)

	require.NoError(t, p.Shutdown(context.Background()))
	assert.NotPanics(t, func() { _ = p.Shutdown(context.Background()) }, "a second Shutdown must not panic")
	var nilServer *pprofServer
	assert.NoError(t, nilServer.Shutdown(context.Background()))
}
