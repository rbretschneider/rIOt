package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/server/ca"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// originRoute is one (method, pattern) pair found by walking the real router.
type originRoute struct {
	method  string
	pattern string
	// cookieAuth is true when AdminAuth is in the route's middleware chain.
	// Cookie authentication is identified by the chain, not by the group a
	// route was registered in (GROWTH-001 SEC-032 item 2).
	cookieAuth bool
	// chain is every middleware in front of the endpoint, in order, global
	// ones first; replay rebuilds the real chain around a stub endpoint.
	chain []func(http.Handler) http.Handler
}

// replay serves a request through the route's real middleware chain with the
// endpoint replaced by a stub that answers 204, so the chain can be exercised
// without the database-backed handler behind it.
func (r originRoute) replay(req *http.Request) *httptest.ResponseRecorder {
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for i := len(r.chain) - 1; i >= 0; i-- {
		h = r.chain[i](h)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func (r originRoute) String() string { return r.method + " " + r.pattern }

func isStateChangingMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// chainHasAdminAuth reports whether any middleware in the chain is AdminAuth
// or AdminAuthSameOrigin, by the name of the closure each one returns.
func chainHasAdminAuth(mws []func(http.Handler) http.Handler) bool {
	for _, mw := range mws {
		name := runtime.FuncForPC(reflect.ValueOf(mw).Pointer()).Name()
		if strings.Contains(name, "middleware.AdminAuth") {
			return true
		}
	}
	return false
}

// walkRoutes lists every method and pattern of the router.
func walkRoutes(t *testing.T, h http.Handler) []originRoute {
	t.Helper()
	mux, ok := h.(chi.Routes)
	require.True(t, ok, "the router must be walkable")
	var routes []originRoute
	err := chi.Walk(mux, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		routes = append(routes, originRoute{method: method, pattern: route, cookieAuth: chainHasAdminAuth(mws), chain: mws})
		return nil
	})
	require.NoError(t, err)
	sort.Slice(routes, func(i, j int) bool { return routes[i].String() < routes[j].String() })
	return routes
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// concretePath turns a chi pattern into a requestable path.
func concretePath(pattern string) string {
	p := pathParam.ReplaceAllString(pattern, "x")
	return strings.ReplaceAll(p, "*", "x")
}

// originHarness builds the real setupRouter, optionally with mTLS routes on.
func originHarness(t *testing.T, mtls bool) http.Handler {
	t.Helper()
	// The walk sends hundreds of requests; keep the request log out of the
	// test output.
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	cfg := &Config{Port: 7331, MTLSEnabled: mtls}
	s := New(cfg)
	s.FrontendFS = testFrontendFS()
	s.JWTSecret = []byte("router-test-secret")
	s.SetupComplete.Store(true)
	if mtls {
		authority, err := ca.NewCA()
		require.NoError(t, err)
		s.CA = authority
	}
	return s.setupRouter()
}

// agentRoutes are the routes that authenticate with a device key, a client
// certificate or a bootstrap key, never the session cookie. A route that is
// not cookie-authenticated and not listed here (or under /auth and /setup)
// fails TestGROWTH_D72_EveryCookieStateChangingRouteHasOriginCheck, so a new
// state-changing route must be classified deliberately.
var agentRoutes = map[string]bool{
	"POST /api/v1/devices/{id}/heartbeat":                   true,
	"POST /api/v1/devices/{id}/telemetry":                   true,
	"POST /api/v1/devices/{id}/docker-events":               true,
	"POST /api/v1/devices/{id}/events":                      true,
	"POST /api/v1/devices/{id}/logs":                        true,
	"POST /api/v1/devices/{id}/probe-results":               true,
	"POST /api/v1/devices/{id}/commands/{commandId}/report": true,
	"POST /api/v1/devices/register":                         true,
	"POST /api/v1/enroll":                                   true,
	"POST /api/v1/renew":                                    true,
	"GET /ws/agent":                                         true,
	"GET /api/v1/update/check":                              true,
}

// notServedRoutes answer every method with an explicit 404 and no handler
// logic (HARDEN-002 AD-001), so they have nothing to protect.
func isNotServedRoute(pattern string) bool {
	return pattern == "/debug/pprof" || pattern == "/debug/pprof/*"
}

// isPreAuthRoute: login, logout and setup are cookie-less by nature (they
// create the session or run before any admin exists) and take SameOrigin
// directly instead of through AdminAuth.
func isPreAuthRoute(pattern string) bool {
	return strings.HasPrefix(pattern, "/api/v1/auth/") || strings.HasPrefix(pattern, "/api/v1/setup/")
}

func serve(h http.Handler, method, target string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func isOriginRefused(rec *httptest.ResponseRecorder) bool {
	return rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), "origin_refused")
}

func TestGROWTH_D72_EveryCookieStateChangingRouteHasOriginCheck(t *testing.T) {
	for _, mtls := range []bool{false, true} {
		mtls := mtls
		name := "mTLS off"
		if mtls {
			name = "mTLS on"
		}
		t.Run(name, func(t *testing.T) {
			h := originHarness(t, mtls)
			routes := walkRoutes(t, h)

			var admin, preAuth int
			seen := map[string]originRoute{}
			for _, rt := range routes {
				seen[rt.String()] = rt
				if !isStateChangingMethod(rt.method) {
					continue
				}
				switch {
				case isNotServedRoute(rt.pattern):
					rec := serve(h, rt.method, concretePath(rt.pattern), nil)
					assert.Equal(t, http.StatusNotFound, rec.Code, "%s must stay an explicit 404", rt)
				case agentRoutes[rt.String()]:
					assert.False(t, rt.cookieAuth, "%s is an agent route and must not use the session cookie", rt)
				case rt.cookieAuth || isPreAuthRoute(rt.pattern):
					if rt.cookieAuth {
						admin++
					} else {
						preAuth++
					}
					rec := serve(h, rt.method, concretePath(rt.pattern), nil)
					assert.True(t, isOriginRefused(rec),
						"%s must answer 403 origin_refused to a request with no Origin and no Sec-Fetch-Site; got %d %q",
						rt, rec.Code, rec.Body.String())
					assert.Contains(t, rec.Header().Get("Content-Type"), "application/json", "%s: refusal content type", rt)

					cross := serve(h, rt.method, concretePath(rt.pattern), func(r *http.Request) {
						r.Header.Set("Origin", "https://evil.example")
						r.Header.Set("Sec-Fetch-Site", "cross-site")
					})
					assert.True(t, isOriginRefused(cross), "%s must refuse a cross-site request; got %d %q", rt, cross.Code, cross.Body.String())
				default:
					t.Errorf("%s is state-changing and is neither cookie-authenticated (AdminAuth in its chain), "+
						"under /api/v1/auth or /api/v1/setup, nor a listed agent route; classify it", rt)
				}
			}

			// Guard the detector itself: these admin routes live in the shared
			// /api/v1/devices/{id} route, not in the admin group, and must be
			// recognised as cookie-authenticated (SEC-032 item 2).
			for _, key := range []string{
				"DELETE /api/v1/devices/{id}/",
				"POST /api/v1/devices/{id}/rotate-key",
				"PUT /api/v1/devices/{id}/location",
				"PUT /api/v1/devices/{id}/tags",
				"PUT /api/v1/devices/{id}/auto-patch",
				"PUT /api/v1/devices/{id}/auto-updates",
				"DELETE /api/v1/devices/{id}/auto-updates/{target}",
				"POST /api/v1/devices/{id}/commands",
				"POST /api/v1/devices/{id}/commands/{commandId}/cancel",
				"POST /api/v1/devices/{id}/docker/bulk-update",
				"POST /api/v1/devices/{id}/device-probes",
				"PUT /api/v1/devices/{id}/device-probes/{pid}",
				"DELETE /api/v1/devices/{id}/device-probes/{pid}",
				"POST /api/v1/devices/{id}/device-probes/{pid}/run",
				"POST /api/v1/auth/change-password",
				"POST /api/v1/settings/notification-channels",
				"POST /api/v1/fleet/bulk-update",
			} {
				rt, ok := seen[key]
				if assert.True(t, ok, "route %s must exist; the walk did not find it", key) {
					assert.True(t, rt.cookieAuth, "%s must be detected as cookie-authenticated", key)
				}
			}
			for _, key := range []string{"POST /api/v1/auth/login", "POST /api/v1/auth/logout", "POST /api/v1/setup/complete"} {
				rt, ok := seen[key]
				if assert.True(t, ok, "route %s must exist; the walk did not find it", key) {
					assert.False(t, rt.cookieAuth, "%s carries SameOrigin directly, not AdminAuth", key)
				}
			}
			assert.GreaterOrEqual(t, admin, 35, "the walk must find the admin routes (found %d)", admin)
			assert.Equal(t, 3, preAuth, "login, logout and setup/complete")
		})
	}
}

func TestGROWTH_D72_AgentRoutesUnaffected(t *testing.T) {
	// A hostile-looking request: foreign Origin, cross-site fetch metadata,
	// text/plain body. If any origin check were reachable from an agent route
	// it would answer 403 origin_refused.
	hostile := func(r *http.Request) {
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Content-Type", "text/plain")
	}

	for _, mtls := range []bool{false, true} {
		mtls := mtls
		name := "mTLS off"
		if mtls {
			name = "mTLS on"
		}
		t.Run(name, func(t *testing.T) {
			h := originHarness(t, mtls)
			present := map[string]originRoute{}
			for _, rt := range walkRoutes(t, h) {
				present[rt.String()] = rt
			}

			checked := 0
			for key := range agentRoutes {
				enrollRoute := key == "POST /api/v1/enroll" || key == "POST /api/v1/renew"
				if enrollRoute && !mtls {
					_, exists := present[key]
					assert.False(t, exists, "%s must not exist with mTLS off", key)
					continue
				}
				rt, exists := present[key]
				if !assert.True(t, exists, "agent route %s must be registered", key) {
					continue
				}
				target := concretePath(rt.pattern)
				for _, headers := range []func(*http.Request){nil, hostile} {
					// The route's real middleware chain with a stub endpoint: the
					// database-backed handlers are not under test.
					req := httptest.NewRequest(rt.method, target, nil)
					if headers != nil {
						headers(req)
					}
					rec := rt.replay(req)
					assert.False(t, isOriginRefused(rec),
						"%s is an agent route and must never answer origin_refused; got %d %q", key, rec.Code, rec.Body.String())
					assert.NotEqual(t, http.StatusForbidden, rec.Code,
						"%s must not answer 403 to a request with no cookie; got %q", key, rec.Body.String())
				}
				checked++

				// Device-key routes are also exercised on the real router: with no
				// key the device-key check answers 401, which proves it, not an
				// origin check, is the first gate (and no handler runs).
				if strings.HasPrefix(rt.pattern, "/api/v1/devices/{id}/") && rt.method == http.MethodPost {
					rec := serve(h, rt.method, target, hostile)
					assert.Equal(t, http.StatusUnauthorized, rec.Code,
						"%s with no device key must be 401 from the device-key check; got %q", key, rec.Body.String())
				}
			}
			want := 10
			if mtls {
				want = 12
			}
			assert.Equal(t, want, checked, "every listed agent route must have been exercised")
		})
	}
}

// TestGROWTH_D72_SharedDeviceRouteShape mirrors the structure that makes the
// mount point matter: agent POSTs and admin writes share one
// /api/v1/devices/{id} route. A check mounted on the route would 403 every
// agent; mounted on the AdminAuth chain it must not (SEC-032 item 1).
func TestGROWTH_D72_SharedDeviceRouteShape(t *testing.T) {
	devs := testutil.NewMockDeviceRepo()
	devs.APIKeys["key-1"] = "dev-1"
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	adminAuth := middleware.AdminAuthSameOrigin([]byte(routeSecret), nil)
	router := chi.NewRouter()
	router.Route("/api/v1/devices/{id}", func(r chi.Router) {
		r.With(middleware.DeviceAuth(devs)).Post("/heartbeat", ok.ServeHTTP)
		r.With(adminAuth).Delete("/", ok.ServeHTTP)
		r.With(adminAuth).Put("/location", ok.ServeHTTP)
	})

	t.Run("heartbeat with a valid device key and no Origin is 200", func(t *testing.T) {
		rec := serve(router, "POST", "/api/v1/devices/dev-1/heartbeat", func(r *http.Request) {
			r.Header.Set("X-rIOt-Key", "key-1")
		})
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
	t.Run("heartbeat with a valid device key and a foreign Origin is still 200", func(t *testing.T) {
		rec := serve(router, "POST", "/api/v1/devices/dev-1/heartbeat", func(r *http.Request) {
			r.Header.Set("X-rIOt-Key", "key-1")
			r.Header.Set("Origin", "https://evil.example")
		})
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
	t.Run("DELETE on the same route with no Origin is 403 origin_refused", func(t *testing.T) {
		rec := serve(router, "DELETE", "/api/v1/devices/dev-1/", nil)
		assert.True(t, isOriginRefused(rec), "got %d %q", rec.Code, rec.Body.String())
	})
	t.Run("DELETE with a same-origin Origin and a valid session reaches the handler", func(t *testing.T) {
		rec := serve(router, "DELETE", "/api/v1/devices/dev-1/", func(r *http.Request) {
			r.Header.Set("Origin", "http://example.com")
			r.AddCookie(adminCookie(t))
		})
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
	t.Run("DELETE with a same-origin Origin and no session is 401, not 403", func(t *testing.T) {
		rec := serve(router, "DELETE", "/api/v1/devices/dev-1/", func(r *http.Request) {
			r.Header.Set("Origin", "http://example.com")
		})
		assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})
}
