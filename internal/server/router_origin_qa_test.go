package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Added by QA Engineer (GROWTH-001 PR-D).

// Covers AC D-72: safe methods on every state-changing admin path are never
// answered by the origin check. HEAD and OPTIONS must keep working (CORS
// preflight), and a GET WebSocket upgrade from a foreign Origin is left to the
// existing WebSocket origin check and AdminAuth, not to this middleware.
func TestGROWTH_D72_SafeMethodsAndWebSocketUpgradeAreNotOriginRefused(t *testing.T) {
	for _, mtls := range []bool{false, true} {
		h := originHarness(t, mtls)
		for _, rt := range walkRoutes(t, h) {
			if !isStateChangingMethod(rt.method) || !(rt.cookieAuth || isPreAuthRoute(rt.pattern)) {
				continue
			}
			for _, m := range []string{http.MethodHead, http.MethodOptions} {
				rec := serve(h, m, concretePath(rt.pattern), func(r *http.Request) {
					r.Header.Set("Origin", "https://evil.example")
					r.Header.Set("Sec-Fetch-Site", "cross-site")
				})
				assert.False(t, isOriginRefused(rec), "%s %s must not be answered by the origin check; got %d", m, rt.pattern, rec.Code)
			}
		}

		for _, ws := range []string{"/ws", "/ws/terminal/dev-1/ctr-1"} {
			rec := serve(h, http.MethodGet, ws, func(r *http.Request) {
				r.Header.Set("Origin", "https://evil.example")
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				r.Header.Set("Connection", "Upgrade")
				r.Header.Set("Upgrade", "websocket")
				r.Header.Set("Sec-WebSocket-Version", "13")
				r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			})
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s without a session is refused by AdminAuth, not the origin check", ws)
			assert.False(t, isOriginRefused(rec), ws)
		}
	}
}

// Covers AC D-72: the agent WebSocket is a GET with no Origin and is outside
// the origin check, with mTLS off and on.
func TestGROWTH_D72_AgentWebSocketUpgradeUnaffected(t *testing.T) {
	h := originHarness(t, false)
	rec := serve(h, http.MethodGet, "/ws/agent", func(r *http.Request) {
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
	})
	assert.False(t, isOriginRefused(rec), "got %d %q", rec.Code, rec.Body.String())
}

// Covers AC D-72, log and abuse bound: a refused request is answered before
// the login limiter, so a cross-origin flood cannot burn the operator's login
// allowance (a lock-out vector if the check ran after the limiter).
func TestGROWTH_D72_RefusedLoginDoesNotConsumeLoginRateLimit(t *testing.T) {
	h := originHarness(t, false)
	for i := 0; i < 20; i++ {
		rec := serve(h, http.MethodPost, "/api/v1/auth/login", func(r *http.Request) {
			r.Header.Set("Origin", "https://evil.example")
			r.RemoteAddr = "192.0.2.77:4000"
		})
		if !isOriginRefused(rec) {
			t.Fatalf("request %d: want origin_refused, got %d %q", i, rec.Code, rec.Body.String())
		}
	}
}

// Covers AC D-72: bodies other than JSON are refused on a real admin route
// through the whole router (multipart, form, text, chunked via unknown length),
// each with a correct Origin, and none reaches a handler (the harness has no
// database, so a handler reaching the repo would panic and be a 500).
func TestGROWTH_D72_NonJSONBodiesRefusedThroughRouter(t *testing.T) {
	h := originHarness(t, false)
	for _, ct := range []string{
		"text/plain",
		"application/x-www-form-urlencoded",
		"multipart/form-data; boundary=x",
		"text/plain;application/json",
		"",
	} {
		for _, target := range []string{
			"/api/v1/fleet/bulk-update",
			"/api/v1/devices/dev-1/rotate-key",
			"/api/v1/auth/login",
			"/api/v1/setup/complete",
		} {
			req := httptest.NewRequest(http.MethodPost, target, strings.NewReader("password=x"))
			req.Header.Set("Origin", "http://"+req.Host)
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.True(t, isOriginRefused(rec), "%s with Content-Type %q: got %d %q", target, ct, rec.Code, rec.Body.String())
		}
	}
}
