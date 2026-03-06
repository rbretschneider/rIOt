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
