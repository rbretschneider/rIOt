package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/expect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/v1/activity sits in the admin session group (EXPECT-001 ADD 6.1).

func sessionFor(t *testing.T, s *Server) func(*http.Request) {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}).SignedString(s.JWTSecret)
	require.NoError(t, err)
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "riot_session", Value: tok}) }
}

func TestEX_Activity_RouteRequiresAnAdminSession(t *testing.T) {
	_, h := newRouterHarness(t, nil)

	rec := doRequest(h, http.MethodGet, "/api/v1/activity", nil)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "no session: refused")
	rec = doRequest(h, http.MethodGet, "/api/v1/activity", func(r *http.Request) { r.Header.Set("X-rIOt-Key", "a-device-key") })
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "a device key is not an admin session")
}

func TestEX_Activity_RouteWithoutATrackerIs503(t *testing.T) {
	s, h := newRouterHarness(t, nil)

	rec := doRequest(h, http.MethodGet, "/api/v1/activity", sessionFor(t, s))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"error":"activity unavailable","code":"activity_unavailable"}`, rec.Body.String())
}

func TestEX_Activity_RouteServesTheTrackersActivity(t *testing.T) {
	cfg := &Config{Port: 7331}
	s := New(cfg)
	s.FrontendFS = testFrontendFS()
	s.JWTSecret = []byte("router-test-secret")
	s.SetupComplete.Store(true)
	s.Expect = expect.New(expect.Options{NewestAgentVersion: func() string { return "2.78.0" }})
	h := s.setupRouter()

	rec := doRequest(h, http.MethodGet, "/api/v1/activity", sessionFor(t, s))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp models.ActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "2.78.0", resp.NewestAgentVersion)
	assert.NotNil(t, resp.Activities, "an empty fleet is an empty list")
	assert.Contains(t, rec.Body.String(), `"activities":[]`)
}
