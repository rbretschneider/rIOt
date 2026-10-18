package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const spaMarker = "<!--riot-test-spa-index-->"

func testFrontendFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":        {Data: []byte("<html>" + spaMarker + "</html>")},
		"assets/app.js":     {Data: []byte("console.log('app')")},
		"assets/app.css":    {Data: []byte("body{color:red}")},
		"site.webmanifest":  {Data: []byte(`{"name":"rIOt"}`)},
		"favicon.ico":       {Data: []byte{0, 0, 1, 0}},
		"assets/font.woff2": {Data: []byte("wOF2")},
	}
}

// newRouterHarness builds the real setupRouter with a test FrontendFS and nil
// repositories. It can serve only routes that need no database (HARDEN-002
// AD-013 harness a).
func newRouterHarness(t *testing.T, mutate func(*Config)) (*Server, http.Handler) {
	t.Helper()
	cfg := &Config{Port: 7331}
	if mutate != nil {
		mutate(cfg)
	}
	s := New(cfg)
	s.FrontendFS = testFrontendFS()
	s.JWTSecret = []byte("router-test-secret")
	s.SetupComplete.Store(true)
	return s, s.setupRouter()
}

func doRequest(h http.Handler, method, target string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertBaselineHeaders(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"), "%s: Content-Security-Policy missing", what)
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), "%s: X-Frame-Options", what)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), "%s: X-Content-Type-Options", what)
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"), "%s: Referrer-Policy", what)
}

// pprofMarkers are strings that only real pprof output contains.
var pprofMarkers = []string{"goroutine profile", "heap profile", "Types of profiles available"}

func assertNoPprofContent(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	body := rec.Body.String()
	for _, m := range pprofMarkers {
		assert.NotContains(t, body, m, "%s: response must contain no pprof output", what)
	}
}

func TestHARDEN002_AC001_PprofNotServedOnMainRouter(t *testing.T) {
	_, h := newRouterHarness(t, nil)

	for _, path := range []string{
		"/debug/pprof/",
		"/debug/pprof/heap",
		"/debug/pprof/cmdline",
		"/debug/pprof/goroutine?debug=2",
		"/debug/pprof",
	} {
		rec := doRequest(h, "GET", path, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, "GET %s must be an explicit 404, not the SPA's 200", path)
		assertNoPprofContent(t, rec, path)
		assert.NotContains(t, rec.Body.String(), spaMarker, "GET %s must not return the SPA index", path)
		assertBaselineHeaders(t, rec, path)
	}

	for _, method := range []string{"POST", "PUT", "DELETE"} {
		rec := doRequest(h, method, "/debug/pprof/heap", nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, "%s /debug/pprof/heap must be 404", method)
	}
}

func TestHARDEN002_AC001_C5_PprofPathVariantsServeNoProfile(t *testing.T) {
	_, h := newRouterHarness(t, nil)

	cases := []struct {
		name   string
		target string
		raw    string
	}{
		{"upper-case", "/DEBUG/PPROF/", ""},
		{"doubled leading slash", "//debug/pprof/", ""},
		{"doubled inner slash", "/debug//pprof/heap", ""},
		{"dot segments", "/debug/pprof/../debug/pprof/heap", ""},
		{"encoded separator", "/debug%2Fpprof/heap", "/debug%2Fpprof/heap"},
		{"encoded slash after pprof", "/debug/pprof%2Fheap", "/debug/pprof%2Fheap"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(h, "GET", tc.target, func(r *http.Request) {
				if tc.raw != "" {
					r.URL.RawPath = tc.raw
				}
			})
			assertNoPprofContent(t, rec, tc.target)
		})
	}
}

func TestHARDEN002_AC006_LoopbackEnabledMainRouterStill404(t *testing.T) {
	log, _ := newCapturedLogger()
	p, err := listenPprof("127.0.0.1:0", log)
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	// Sanity: the loopback listener really is serving profiles.
	resp, err := http.Get("http://" + p.Addr() + "/debug/pprof/")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "loopback listener must be serving pprof for this test to mean anything")

	_, h := newRouterHarness(t, func(c *Config) { c.PprofAddr = p.Addr() })
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/goroutine?debug=2"} {
		rec := doRequest(h, "GET", path, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, "main router GET %s must stay 404 while the loopback listener is up", path)
		assertNoPprofContent(t, rec, path)
	}
}

func TestHARDEN002_AC009_BaselineHeadersOnSPA(t *testing.T) {
	_, h := newRouterHarness(t, nil)
	rec := doRequest(h, "GET", "/", nil)

	require.Equal(t, http.StatusOK, rec.Code, "SPA index must still be served")
	assert.Contains(t, rec.Body.String(), spaMarker, "SPA body must be index.html")
	assertBaselineHeaders(t, rec, "GET /")
	assert.Empty(t, rec.Header().Get("Strict-Transport-Security"), "no HSTS over plain HTTP")
}

func TestHARDEN002_AC013_HSTSOnDirectTLS(t *testing.T) {
	_, h := newRouterHarness(t, nil)
	for _, path := range []string{"/", "/assets/app.js", "/api/v1/devices"} {
		rec := doRequest(h, "GET", path, func(r *http.Request) { r.TLS = &tls.ConnectionState{} })
		hsts := rec.Header().Get("Strict-Transport-Security")
		assert.Equal(t, "max-age=31536000", hsts, "%s: HSTS on direct TLS", path)
		assert.NotContains(t, hsts, "includeSubDomains", "%s: no includeSubDomains", path)
		assert.NotContains(t, hsts, "preload", "%s: no preload", path)
	}
}

func TestHARDEN002_AC015_NoHSTSOnPlainHTTPOrUntrustedPeer(t *testing.T) {
	_, h := newRouterHarness(t, func(c *Config) { c.TrustedProxies = "10.0.0.5/32" })

	plain := doRequest(h, "GET", "/", nil)
	assert.Empty(t, plain.Header().Get("Strict-Transport-Security"), "plain HTTP must not carry HSTS")

	untrusted := doRequest(h, "GET", "/", func(r *http.Request) {
		r.RemoteAddr = "203.0.113.9:5555"
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	assert.Empty(t, untrusted.Header().Get("Strict-Transport-Security"), "untrusted peer's X-Forwarded-Proto must not enable HSTS")
}

func TestHARDEN002_AC050_HealthEndpointsPublicAndSetupIndependent(t *testing.T) {
	for _, setupComplete := range []bool{false, true} {
		s, h := newRouterHarness(t, nil)
		s.SetupComplete.Store(setupComplete)

		for _, path := range []string{"/health", "/readyz"} {
			rec := doRequest(h, "GET", path, nil)
			what := path
			assert.NotEqual(t, http.StatusUnauthorized, rec.Code, "%s (setup=%v): must not require auth", what, setupComplete)
			assert.NotEqual(t, http.StatusForbidden, rec.Code, "%s (setup=%v): must not require auth", what, setupComplete)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), "%s (setup=%v): must be JSON, not the SPA", what, setupComplete)
			assert.NotContains(t, rec.Body.String(), "setup_required", "%s (setup=%v): must not be the setup guard's response", what, setupComplete)
			assert.NotContains(t, rec.Body.String(), spaMarker, "%s (setup=%v): must not be the SPA catch-all", what, setupComplete)

			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "%s body: %q", what, rec.Body.String())
			assert.Contains(t, body, "status", "%s (setup=%v): must be the health body", what, setupComplete)
			// The harness has no database, so both report the down state.
			assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "%s (setup=%v): nil DB is reported as down", what, setupComplete)
			assertBaselineHeaders(t, rec, what)
		}
	}
}

func TestHARDEN002_AC018_StaticAssetContentTypes(t *testing.T) {
	_, h := newRouterHarness(t, nil)

	cases := []struct {
		path       string
		wantPrefix string
	}{
		{"/", "text/html"},
		{"/assets/app.js", "text/javascript"},
		{"/assets/app.css", "text/css"},
		{"/site.webmanifest", "application/manifest+json"},
		{"/favicon.ico", "image/x-icon"},
		{"/assets/font.woff2", "font/woff2"},
		{"/some/spa/route", "text/html"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			rec := doRequest(h, "GET", tc.path, nil)
			require.Equal(t, http.StatusOK, rec.Code, "GET %s", tc.path)
			ct := rec.Header().Get("Content-Type")
			assert.True(t, strings.HasPrefix(ct, tc.wantPrefix), "GET %s: Content-Type %q must start with %q", tc.path, ct, tc.wantPrefix)
			assert.NotContains(t, ct, "text/plain", "GET %s: nosniff would break a text/plain asset", tc.path)
			assert.NotContains(t, ct, "application/octet-stream", "GET %s: octet-stream is not an acceptable asset type", tc.path)
		})
	}
}
