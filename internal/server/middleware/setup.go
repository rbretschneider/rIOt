package middleware

import (
	"net/http"
	"strings"
	"sync/atomic"
)

// SetupGuard blocks all API routes (except setup, auth/check, and health)
// when the server is in setup mode. Frontend static assets are always allowed.
func SetupGuard(setupComplete *atomic.Bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if setupComplete.Load() {
				next.ServeHTTP(w, r)
				return
			}

			path := r.URL.Path

			// Always allow setup endpoints
			if strings.HasPrefix(path, "/api/v1/setup/") {
				next.ServeHTTP(w, r)
				return
			}

			// Allow health check and server cert (TOFU)
			if path == "/health" || path == "/api/v1/server-cert" {
				next.ServeHTTP(w, r)
				return
			}

			// Allow auth check (frontend needs this to detect setup state)
			if path == "/api/v1/auth/check" {
				next.ServeHTTP(w, r)
				return
			}

			// Allow the three OIDC endpoints through by exact path match (not
			// prefix — OIDC-001 AD-019, SEC-011). The handlers themselves gate
			// on isSetupComplete() and refuse to mint a session while setup is
			// incomplete (AC-020); this entry exists only so the availability
			// endpoint can answer {"available": false} and /start, /callback
			// can answer 404 instead of the 503 setup_required body below.
			if path == "/api/v1/auth/oidc" || path == "/api/v1/auth/oidc/start" || path == "/api/v1/auth/oidc/callback" {
				next.ServeHTTP(w, r)
				return
			}

			// Block all other API routes
			if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"error":"setup_required"}`))
				return
			}

			// Allow frontend static assets
			next.ServeHTTP(w, r)
		})
	}
}
