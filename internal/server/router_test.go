package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
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

// mwHarness is HARDEN-002 AD-013 harness (b): the production global middleware
// chain in front of mock-backed handlers, for routes that need a database in
// the real router (setupRouter holds concrete repository types).
type mwHarness struct {
	t         *testing.T
	handler   http.Handler
	devices   *testutil.MockDeviceRepo
	hub       *websocket.Hub
	jwtSecret []byte
}

const (
	harnessRegKey   = "K-abc123"
	harnessDeviceID = "dev-1"
	harnessDevKey   = "device-key-1"
	harnessPassword = "correct-password"
)

func newMiddlewareHarness(t *testing.T, mutate func(*Config)) *mwHarness {
	t.Helper()
	cfg := &Config{Port: 7331, AllowedOrigins: []string{"https://app.example"}}
	if mutate != nil {
		mutate(cfg)
	}
	s := New(cfg)
	s.SetupComplete.Store(true)

	pwHash, err := bcrypt.GenerateFromPassword([]byte(harnessPassword), bcrypt.MinCost)
	require.NoError(t, err)
	adminRepo := testutil.NewMockAdminRepo(string(pwHash))
	adminRepo.Config["registration_key"] = harnessRegKey
	devices := testutil.NewMockDeviceRepo()
	devices.Devices[harnessDeviceID] = &models.Device{ID: harnessDeviceID, Hostname: "node-1"}
	devices.APIKeys[harnessDevKey] = harnessDeviceID
	hub := websocket.NewHub()
	go hub.Run()
	secret := []byte("middleware-harness-secret")
	gen := events.NewGenerator(testutil.NewMockEventRepo(), hub, testutil.NewMockAlertRuleRepo(),
		testutil.NewMockDispatcher(), testutil.NewMockCommandRepo())

	h := handlers.New(handlers.HandlerDeps{
		Devices:       devices,
		Telemetry:     testutil.NewMockTelemetryRepo(),
		Events:        testutil.NewMockEventRepo(),
		Hub:           hub,
		EventGen:      gen,
		AdminRepo:     adminRepo,
		JWTSecret:     secret,
		SetupComplete: &s.SetupComplete,
	})

	r := chi.NewRouter()
	r.Use(s.globalMiddleware()...)
	r.Post("/api/v1/devices/register", h.RegisterDevice)
	r.With(middleware.DeviceAuth(devices)).Post("/api/v1/devices/{id}/heartbeat", h.Heartbeat)
	r.Get("/api/v1/auth/check", h.AuthCheck)
	r.Post("/api/v1/auth/login", h.Login)
	r.With(middleware.AdminAuth(secret)).Get("/ws", h.WebSocket)
	r.Get("/ws/agent", h.HandleAgentWS)
	r.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })

	return &mwHarness{t: t, handler: r, devices: devices, hub: hub, jwtSecret: secret}
}

func (m *mwHarness) session(t *testing.T) *http.Cookie {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "admin", "exp": time.Now().Add(time.Hour).Unix()})
	signed, err := tok.SignedString(m.jwtSecret)
	require.NoError(t, err)
	return &http.Cookie{Name: "riot_session", Value: signed}
}

func (m *mwHarness) do(method, target, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rdr)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	m.handler.ServeHTTP(rec, req)
	return rec
}

// AC-010: every response class carries the baseline headers, and status codes
// and bodies are exactly what they were before this story.
func TestHARDEN002_AC010_HeadersOnEveryResponseClass(t *testing.T) {
	type tc struct {
		name       string
		call       func() *httptest.ResponseRecorder
		wantStatus int
		wantBody   string // exact body; "" skips the body assertion
	}

	_, realRouter := newRouterHarness(t, func(c *Config) { c.AllowedOrigins = []string{"https://app.example"} })
	mw := newMiddlewareHarness(t, nil)

	cases := []tc{
		{"a: SPA index", func() *httptest.ResponseRecorder { return doRequest(realRouter, "GET", "/", nil) }, http.StatusOK, ""},
		{"a: static asset", func() *httptest.ResponseRecorder { return doRequest(realRouter, "GET", "/assets/app.js", nil) }, http.StatusOK, "console.log('app')"},
		{"a: admin API unauthenticated 401", func() *httptest.ResponseRecorder { return doRequest(realRouter, "GET", "/api/v1/devices", nil) }, http.StatusUnauthorized, `{"error":"unauthorized"}` + "\n"},
		{"a: CORS preflight 204", func() *httptest.ResponseRecorder {
			return doRequest(realRouter, "OPTIONS", "/api/v1/devices", func(r *http.Request) {
				r.Header.Set("Origin", "https://app.example")
				r.Header.Set("Access-Control-Request-Method", "GET")
			})
		}, http.StatusNoContent, ""},
		{"a: /health with the DB down 503", func() *httptest.ResponseRecorder { return doRequest(realRouter, "GET", "/health", nil) }, http.StatusServiceUnavailable, ""},
		{"a: /readyz", func() *httptest.ResponseRecorder { return doRequest(realRouter, "GET", "/readyz", nil) }, http.StatusServiceUnavailable, ""},
		{"a: pprof 404", func() *httptest.ResponseRecorder { return doRequest(realRouter, "GET", "/debug/pprof/", nil) }, http.StatusNotFound, `{"error":"not found"}` + "\n"},
		{"b: auth check", func() *httptest.ResponseRecorder { return mw.do("GET", "/api/v1/auth/check", "", nil) }, http.StatusOK, `{"authenticated":false,"needs_setup":false}` + "\n"},
		{"b: register with a wrong key 401", func() *httptest.ResponseRecorder {
			return mw.do("POST", "/api/v1/devices/register", `{"hostname":"h"}`, func(r *http.Request) { r.Header.Set("X-rIOt-Key", "wrong") })
		}, http.StatusUnauthorized, `{"error":"invalid registration key"}` + "\n"},
		{"b: device route without a key 401", func() *httptest.ResponseRecorder {
			return mw.do("POST", "/api/v1/devices/"+harnessDeviceID+"/heartbeat", `{}`, nil)
		}, http.StatusUnauthorized, `{"error":"missing X-rIOt-Key header"}` + "\n"},
		{"b: valid agent heartbeat 200", func() *httptest.ResponseRecorder {
			return mw.do("POST", "/api/v1/devices/"+harnessDeviceID+"/heartbeat",
				`{"device_id":"dev-1","data":{"uptime":1,"cpu_percent":1,"mem_percent":1,"load_avg_1m":0.1,"disk_root_percent":1}}`,
				func(r *http.Request) { r.Header.Set("X-rIOt-Key", harnessDevKey) })
		}, http.StatusOK, `{"status":"ok"}` + "\n"},
		{"b: panic recovered as 500", func() *httptest.ResponseRecorder { return mw.do("GET", "/panic", "", nil) }, http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			rec := c.call()
			assert.Equal(t, c.wantStatus, rec.Code, "status code must be unchanged")
			if c.wantBody != "" {
				assert.Equal(t, c.wantBody, rec.Body.String(), "body must be unchanged")
			}
			assertBaselineHeaders(t, rec, c.name)
		})
	}
}

func TestHARDEN002_AC010_CORSHeadersUnchangedByHeaderMiddleware(t *testing.T) {
	_, h := newRouterHarness(t, func(c *Config) { c.AllowedOrigins = []string{"https://app.example"} })
	rec := doRequest(h, "OPTIONS", "/api/v1/devices", func(r *http.Request) { r.Header.Set("Origin", "https://app.example") })

	assert.Equal(t, "https://app.example", rec.Header().Get("Access-Control-Allow-Origin"), "CORS must still be applied")
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestHARDEN002_AC014_HSTSBehindTrustedProxyMatchesSecureCookie(t *testing.T) {
	login := func(m *mwHarness, remote, proto string) *httptest.ResponseRecorder {
		return m.do("POST", "/api/v1/auth/login", `{"password":"`+harnessPassword+`"}`, func(r *http.Request) {
			r.RemoteAddr = remote
			if proto != "" {
				r.Header.Set("X-Forwarded-Proto", proto)
			}
		})
	}
	sessionCookie := func(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
		for _, c := range rec.Result().Cookies() {
			if c.Name == "riot_session" {
				return c
			}
		}
		t.Fatalf("no riot_session cookie in %v", rec.Result().Cookies())
		return nil
	}
	m := newMiddlewareHarness(t, func(c *Config) { c.TrustedProxies = "10.0.0.5/32" })

	t.Run("trusted proxy with https: HSTS and Secure cookie", func(t *testing.T) {
		rec := login(m, "10.0.0.5:4444", "https")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "max-age=31536000", rec.Header().Get("Strict-Transport-Security"))
		assert.True(t, sessionCookie(t, rec).Secure, "the same request must set the Secure cookie (same predicate)")
	})
	t.Run("untrusted peer claiming https: neither", func(t *testing.T) {
		rec := login(m, "203.0.113.9:4444", "https")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
		assert.False(t, sessionCookie(t, rec).Secure)
	})
	t.Run("trusted proxy with http: neither", func(t *testing.T) {
		rec := login(m, "10.0.0.5:4444", "http")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
		assert.False(t, sessionCookie(t, rec).Secure)
	})
}

func TestHARDEN002_AC017_WebSocketUpgradesThroughMiddleware(t *testing.T) {
	m := newMiddlewareHarness(t, nil)
	srv := httptest.NewServer(m.handler)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	t.Run("/ws with a valid admin cookie", func(t *testing.T) {
		hdr := http.Header{}
		hdr.Set("Cookie", m.session(t).String())
		conn, resp, err := gws.DefaultDialer.Dial(wsURL+"/ws", hdr)
		require.NoError(t, err, "the /ws handshake must succeed through the middleware chain")
		defer conn.Close()
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

		received := make(chan []byte, 1)
		go func() {
			if _, msg, err := conn.ReadMessage(); err == nil {
				received <- msg
			}
		}()
		// The client may not be registered with the hub yet; rebroadcast until heard.
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case msg := <-received:
				assert.Contains(t, string(msg), "device_update", "a hub broadcast must reach the client")
				return
			case <-tick.C:
				m.hub.BroadcastDeviceUpdate(&models.Device{ID: harnessDeviceID})
			case <-deadline:
				t.Fatal("no message received over /ws within 3s")
			}
		}
	})

	t.Run("/ws without a cookie is still rejected", func(t *testing.T) {
		_, resp, err := gws.DefaultDialer.Dial(wsURL+"/ws", nil)
		require.Error(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get("Content-Security-Policy"), "a rejected handshake is a normal response and carries the headers")
	})

	t.Run("/ws/agent with valid agent credentials", func(t *testing.T) {
		hdr := http.Header{}
		hdr.Set("X-rIOt-Device", harnessDeviceID)
		hdr.Set("X-rIOt-Key", harnessDevKey)
		conn, resp, err := gws.DefaultDialer.Dial(wsURL+"/ws/agent", hdr)
		require.NoError(t, err, "the /ws/agent handshake must succeed through the middleware chain")
		defer conn.Close()
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

		// Exchange a message: a ping must be answered with a pong by the server.
		pong := make(chan struct{}, 1)
		conn.SetPongHandler(func(string) error {
			pong <- struct{}{}
			return nil
		})
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		require.NoError(t, conn.WriteControl(gws.PingMessage, []byte("hi"), time.Now().Add(time.Second)))
		select {
		case <-pong:
		case <-time.After(3 * time.Second):
			t.Fatal("no pong from /ws/agent within 3s")
		}
	})
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
