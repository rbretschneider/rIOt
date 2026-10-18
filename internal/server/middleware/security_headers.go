package middleware

import "net/http"

// HARDEN-002 AD-004: the CSP is a static prefix and suffix around a
// connect-src source list that may carry the validated request Host. The
// constants make the per-request cost one string concatenation.
const (
	cspPrefix = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'"
	cspSuffix = "; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

	// maxCSPHostLen bounds the reflected Host. 262 bytes is the longest value
	// accepted; anything longer falls back to connect-src 'self'.
	maxCSPHostLen = 262
)

// validCSPHost reports whether host is safe to reflect into a CSP source
// expression. The byte set [A-Za-z0-9.-:[]] excludes every byte that can alter
// a policy even though Go's Host grammar accepts it: ';' (new directive), ','
// (second policy), whitespace (new source), a single quote (keyword), '*'
// (wildcard), the remaining sub-delimiters and '%'.
func validCSPHost(host string) bool {
	if len(host) < 1 || len(host) > maxCSPHostLen {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '[', c == ']':
		default:
			return false
		}
	}
	return true
}

// ContentSecurityPolicy returns the dashboard CSP. connect-src lists same-host
// ws:// and wss:// sources explicitly because older browsers do not treat
// 'self' as covering WebSocket schemes; a bare ws:/wss: scheme would allow
// WebSockets to any host and is deliberately not used. An invalid Host yields
// exactly "connect-src 'self'".
func ContentSecurityPolicy(host string) string {
	if !validCSPHost(host) {
		return cspPrefix + cspSuffix
	}
	return cspPrefix + " ws://" + host + " wss://" + host + cspSuffix
}

// SecurityHeaders sets the HARDEN-002 response headers before the next handler
// runs. It never wraps the ResponseWriter, so Hijacker and Flusher keep
// working for the WebSocket and terminal routes. It must run after RealIP:
// HSTS depends on the trusted-proxy scheme RealIP records.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", ContentSecurityPolicy(r.Host))
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if IsHTTPS(r) {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}
