package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)

	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/v1/server/time-display sits in the admin session group
// (TIME-12H-001 AD-006, security condition C-4 gate 1).

const timeDisplayPath = "/api/v1/server/time-display"

func newYorkZone(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	return loc
}

func timeDisplayRouter(t *testing.T, zone *time.Location) (*Server, http.Handler) {
	t.Helper()
	s, _ := newRouterHarness(t, nil)
	s.DisplayZone = zone
	return s, s.setupRouter()
}

func signedSession(t *testing.T, secret []byte, exp time.Time) func(*http.Request) {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"exp": exp.Unix()}).SignedString(secret)
	require.NoError(t, err)
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "riot_session", Value: tok}) }
}

func TestTIME12H_AC050_TimeDisplayRequiresAdminSession(t *testing.T) {
	s, h := timeDisplayRouter(t, newYorkZone(t))

	cases := map[string]func(*http.Request){
		"no session": nil,
		"an X-rIOt-Key header only": func(r *http.Request) {
			r.Header.Set("X-rIOt-Key", "a-device-key")
		},
		"an expired JWT":      signedSession(t, s.JWTSecret, time.Now().Add(-time.Hour)),
		"a bad-signature JWT": signedSession(t, []byte("not-the-server-secret"), time.Now().Add(time.Hour)),
		"a garbage session":   func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "riot_session", Value: "not.a.jwt"}) },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rec := doRequest(h, http.MethodGet, timeDisplayPath, mutate)

			assert.Equal(t, http.StatusUnauthorized, rec.Code, "[AC-050] %s must be refused", name)
			assert.JSONEq(t, `{"error":"unauthorized"}`, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "America/New_York", "[FR-011] the zone must not be disclosed")
		})
	}
}

func TestTIME12H_AC050_TimeDisplayReportsZone(t *testing.T) {
	s, h := timeDisplayRouter(t, newYorkZone(t))

	rec := doRequest(h, http.MethodGet, timeDisplayPath, sessionFor(t, s))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.JSONEq(t, `{"display_zone":"America/New_York"}`, rec.Body.String(), "[AC-050] exact body")
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "[AC-050] the zone must not be cached")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestTIME12H_AC050_TimeDisplayRouteIsGetOnly(t *testing.T) {
	s, h := timeDisplayRouter(t, newYorkZone(t))

	rec := doRequest(h, http.MethodPost, timeDisplayPath, sessionFor(t, s))

	assert.NotEqual(t, http.StatusOK, rec.Code, "the endpoint only answers GET")
}

// FR-012 and DV-8: the endpoint is new, so the unchanged-response check is made
// on GET /api/v1/auth/check, built with the display zone set.
func TestTIME12H_AC050_AuthCheckResponseUnchanged(t *testing.T) {
	secret := []byte("auth-check-secret")
	s := New(&Config{Port: 7331})
	s.SetupComplete.Store(true)
	h := handlers.New(handlers.HandlerDeps{
		AdminRepo:     testutil.NewMockAdminRepo("hash"),
		JWTSecret:     secret,
		SetupComplete: &s.SetupComplete,
		DisplayZone:   newYorkZone(t),
	})
	r := chi.NewRouter()
	r.Get("/api/v1/auth/check", h.AuthCheck)

	call := func(mutate func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
		if mutate != nil {
			mutate(req)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	anon := call(nil)
	assert.Equal(t, http.StatusOK, anon.Code)
	assert.JSONEq(t, `{"authenticated":false,"needs_setup":false}`, anon.Body.String())

	authed := call(signedSession(t, secret, time.Now().Add(time.Hour)))
	assert.Equal(t, http.StatusOK, authed.Code)
	assert.JSONEq(t, `{"authenticated":true,"needs_setup":false}`, authed.Body.String(),
		"[FR-012] auth/check keeps its fields and gains none")
}

func TestTIME12H_AC051_FallbackZoneReportedAsUTC(t *testing.T) {
	zone := ResolveDisplayZone("Mars/Olympus", "", slog.New(&logRecorder{}))
	s, h := timeDisplayRouter(t, zone.Location)

	rec := doRequest(h, http.MethodGet, timeDisplayPath, sessionFor(t, s))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.JSONEq(t, `{"display_zone":"UTC"}`, rec.Body.String(), "[AC-051] an invalid zone is reported as UTC")
}

func TestTIME12H_AC051_UnsetZoneReportedAsUTC(t *testing.T) {
	s, h := timeDisplayRouter(t, nil)

	rec := doRequest(h, http.MethodGet, timeDisplayPath, sessionFor(t, s))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"display_zone":"UTC"}`, rec.Body.String(), "a Server that never resolved a zone reports UTC")
}
