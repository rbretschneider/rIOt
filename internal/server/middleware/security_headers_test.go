package middleware

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseCSP splits a policy into directive name -> source list.
func parseCSP(t *testing.T, policy string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, part := range strings.Split(policy, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		_, dup := out[fields[0]]
		require.False(t, dup, "directive %q appears twice in %q", fields[0], policy)
		out[fields[0]] = fields[1:]
	}
	return out
}

// cspHostSourceMatches implements the subset of CSP source matching that the
// tests need: an explicit scheme://host[:port] source matches a URL with the
// same scheme and host.
func cspHostSourceMatches(sources []string, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for _, s := range sources {
		if strings.HasPrefix(s, "'") {
			continue
		}
		if s == u.Scheme+"://"+u.Host {
			return true
		}
	}
	return false
}

func serveThroughSecurityHeaders(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, r)
	return rec
}

func TestHARDEN002_AC011_CSPDirectiveContent(t *testing.T) {
	rec := serveThroughSecurityHeaders(httptest.NewRequest("GET", "/", nil))
	policy := rec.Header().Get("Content-Security-Policy")
	require.NotEmpty(t, policy, "CSP header must be present")
	d := parseCSP(t, policy)

	assert.Equal(t, []string{"'self'"}, d["default-src"], "default-src")
	assert.Equal(t, []string{"'none'"}, d["object-src"], "object-src")
	assert.Equal(t, []string{"'self'"}, d["base-uri"], "base-uri")
	assert.Equal(t, []string{"'none'"}, d["frame-ancestors"], "frame-ancestors")
	assert.Equal(t, []string{"'self'", "data:"}, d["img-src"], "img-src")
	assert.Equal(t, []string{"'self'", "data:"}, d["font-src"], "font-src")
	assert.Contains(t, d["style-src"], "'self'", "style-src must include 'self'")
	assert.Equal(t, []string{"'self'"}, d["script-src"], "script-src must be exactly 'self'")
	assert.NotContains(t, d["script-src"], "'unsafe-inline'", "script-src must not allow inline")
	assert.NotContains(t, d["script-src"], "'unsafe-eval'", "script-src must not allow eval")
	assert.NotContains(t, policy, "upgrade-insecure-requests", "setup wizard runs over plain HTTP")
	assert.Equal(t, []string{"'self'"}, d["form-action"], "SEC-008: form-action 'self' replaces the FRD's no-form-action clause")
}

func TestHARDEN002_AC012_CSPPermitsSameHostWebSockets(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "rIOt.lan:7331"
	rec := serveThroughSecurityHeaders(req)
	d := parseCSP(t, rec.Header().Get("Content-Security-Policy"))

	connect := d["connect-src"]
	assert.Contains(t, connect, "'self'", "connect-src must keep 'self'")
	assert.True(t, cspHostSourceMatches(connect, "ws://rIOt.lan:7331/ws"), "ws://<host>/ws must match connect-src %v", connect)
	assert.True(t, cspHostSourceMatches(connect, "wss://rIOt.lan:7331/ws/terminal/x/y"), "wss://<host>/ws/terminal must match connect-src %v", connect)
	assert.NotContains(t, connect, "ws:", "bare ws: scheme would allow WebSockets to any host")
	assert.NotContains(t, connect, "wss:", "bare wss: scheme would allow WebSockets to any host")
	assert.False(t, cspHostSourceMatches(connect, "wss://evil.example/ws"), "another host must not match")
}

func TestHARDEN002_AC012_C6_HostInjectionFallsBackToSelf(t *testing.T) {
	const wantConnect = "connect-src 'self'"

	connectDirective := func(policy string) string {
		for _, part := range strings.Split(policy, ";") {
			p := strings.TrimSpace(part)
			if strings.HasPrefix(p, "connect-src") {
				return p
			}
		}
		return ""
	}

	for _, b := range []string{";", ",", "'", "*", "(", ")", "!", "$", "&", "+", "=", "%", " ", "\"", "\\", "/", "<"} {
		b := b
		t.Run("byte "+b, func(t *testing.T) {
			policy := ContentSecurityPolicy("a" + b + "b.example")
			assert.Equal(t, wantConnect, connectDirective(policy), "host containing %q must fall back to connect-src 'self'", b)
			assert.Equal(t, 1, strings.Count(policy, "default-src"), "no second policy may be injected")
			assert.NotContains(t, policy, "unsafe-eval", "no directive may be injected")
		})
	}

	t.Run("empty host", func(t *testing.T) {
		assert.Equal(t, wantConnect, connectDirective(ContentSecurityPolicy("")))
	})
	t.Run("zoned IPv6 literal", func(t *testing.T) {
		assert.Equal(t, wantConnect, connectDirective(ContentSecurityPolicy("[fe80::1%25eth0]:7331")))
	})
	t.Run("263-byte host falls back", func(t *testing.T) {
		host := strings.Repeat("a", 263)
		assert.Equal(t, wantConnect, connectDirective(ContentSecurityPolicy(host)))
	})
	t.Run("262-byte host is accepted (boundary)", func(t *testing.T) {
		host := strings.Repeat("a", 262)
		got := connectDirective(ContentSecurityPolicy(host))
		assert.Equal(t, "connect-src 'self' ws://"+host+" wss://"+host, got)
	})
	t.Run("through the middleware with a spoofed Host", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "x.example; script-src *"
		rec := serveThroughSecurityHeaders(req)
		d := parseCSP(t, rec.Header().Get("Content-Security-Policy"))
		assert.Equal(t, []string{"'self'"}, d["script-src"], "spoofed Host must not alter script-src")
		assert.Equal(t, []string{"'self'"}, d["connect-src"], "spoofed Host must fall back to 'self'")
	})
}

func TestHARDEN002_AC013_HSTSOnDirectTLS(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := serveThroughSecurityHeaders(req)

	hsts := rec.Header().Get("Strict-Transport-Security")
	assert.Equal(t, "max-age=31536000", hsts, "HSTS must be exactly max-age=31536000 on direct TLS")
	assert.NotContains(t, hsts, "includeSubDomains")
	assert.NotContains(t, hsts, "preload")
}

func TestHARDEN002_AC014_HSTSBehindTrustedProxyMatchesIsHTTPS(t *testing.T) {
	tp := ParseTrustedProxies("10.0.0.5/32")
	var isHTTPS bool
	chain := RealIP(tp)(SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isHTTPS = IsHTTPS(r)
	})))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:4444"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, "max-age=31536000", rec.Header().Get("Strict-Transport-Security"), "trusted proxy + X-Forwarded-Proto https must yield HSTS")
	assert.True(t, isHTTPS, "the same request must satisfy IsHTTPS (the cookie Secure predicate)")
}

func TestHARDEN002_AC015_NoHSTSOnPlainHTTPOrUntrustedPeer(t *testing.T) {
	tp := ParseTrustedProxies("10.0.0.5/32")

	t.Run("plain HTTP, no forwarding headers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RealIP(tp)(SecurityHeaders()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))).
			ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		assert.Empty(t, rec.Header().Get("Strict-Transport-Security"), "no HSTS on plain HTTP")
	})

	t.Run("untrusted peer supplies X-Forwarded-Proto https", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.9:4444"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		RealIP(tp)(SecurityHeaders()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))).ServeHTTP(rec, req)
		assert.Empty(t, rec.Header().Get("Strict-Transport-Security"), "an untrusted peer must not be able to turn HSTS on")
	})

	t.Run("trusted peer says http over a TLS connection", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.5:4444"
		req.TLS = &tls.ConnectionState{}
		req.Header.Set("X-Forwarded-Proto", "http")
		rec := httptest.NewRecorder()
		RealIP(tp)(SecurityHeaders()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))).ServeHTTP(rec, req)
		assert.Empty(t, rec.Header().Get("Strict-Transport-Security"), "HSTS must follow the resolved scheme, same as the cookie")
	})
}

func TestHARDEN002_AC009_BaselineHeadersSetBeforeHandler(t *testing.T) {
	rec := serveThroughSecurityHeaders(httptest.NewRequest("GET", "/", nil))
	assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"), "CSP")
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
}

// hijackableRecorder is a ResponseRecorder that also implements http.Hijacker.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack not supported in test")
}

func TestHARDEN002_AC017_SecurityHeadersDoNotWrapWriter(t *testing.T) {
	var sawHijacker, sawFlusher bool
	h := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, sawHijacker = w.(http.Hijacker)
		_, sawFlusher = w.(http.Flusher)
	}))
	h.ServeHTTP(hijackableRecorder{httptest.NewRecorder()}, httptest.NewRequest("GET", "/", nil))
	assert.True(t, sawHijacker, "Hijacker must survive the middleware so WebSocket upgrades keep working")
	assert.True(t, sawFlusher, "Flusher must survive the middleware")
}
