package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newIncompleteSetupGuard() func(http.Handler) http.Handler {
	var complete atomic.Bool
	complete.Store(false)
	return SetupGuard(&complete)
}

func passthroughHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// [AC-020] The three exact OIDC paths pass the guard while setup is incomplete.
func TestAC020_SetupGuard_OIDCPathsPassThrough(t *testing.T) {
	guard := newIncompleteSetupGuard()
	handler := guard(passthroughHandler())

	for _, path := range []string{
		"/api/v1/auth/oidc",
		"/api/v1/auth/oidc/start",
		"/api/v1/auth/oidc/callback",
	} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "path %s must pass the setup guard", path)
	}
}

// [AC-020] Other /api/ paths, including POST /api/v1/auth/login, are still blocked.
func TestAC020_SetupGuard_OtherAPIPathsStillBlocked(t *testing.T) {
	guard := newIncompleteSetupGuard()
	handler := guard(passthroughHandler())

	for _, path := range []string{
		"/api/v1/auth/login",
		"/api/v1/devices",
	} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "path %s must still be blocked during setup", path)
	}
}

// [SEC-011] SetupGuard matches the three OIDC paths exactly; a path merely
// prefixed by /api/v1/auth/oidc does not pass and falls through to the
// generic /api/ block instead.
func TestSEC011_SetupGuard_ExactMatchNotPrefix(t *testing.T) {
	guard := newIncompleteSetupGuard()
	handler := guard(passthroughHandler())

	req := httptest.NewRequest("GET", "/api/v1/auth/oidcanything", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "a path merely prefixed by /api/v1/auth/oidc must not bypass the guard")
}

// SetupGuard passes every request through once setup is complete.
func TestSetupGuard_CompleteAllowsEverything(t *testing.T) {
	var complete atomic.Bool
	complete.Store(true)
	handler := SetupGuard(&complete)(passthroughHandler())

	req := httptest.NewRequest("GET", "/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
