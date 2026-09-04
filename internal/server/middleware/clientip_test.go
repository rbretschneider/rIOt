package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRealIPHandler(tp *TrustedProxies) (http.Handler, *capturedRequest) {
	captured := &capturedRequest{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.remoteAddr = r.RemoteAddr
		captured.clientIP = ClientIP(r)
		captured.scheme = RequestScheme(r)
	})
	return RealIP(tp)(next), captured
}

type capturedRequest struct {
	remoteAddr string
	clientIP   string
	scheme     string
}

// [SEC-002] The throttle key / logged IP cannot be influenced by any header from an untrusted peer.
func TestSEC002_UntrustedPeer_ForwardingHeadersIgnored(t *testing.T) {
	tp := ParseTrustedProxies("") // trust nobody
	handler, captured := newRealIPHandler(tp)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Real-IP", "10.0.0.2")
	req.Header.Set("True-Client-IP", "10.0.0.3")
	req.Header.Set("X-Forwarded-Proto", "https")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "203.0.113.9", captured.clientIP, "untrusted peer's forwarding headers must be ignored")
	assert.Equal(t, "http", captured.scheme, "untrusted peer's X-Forwarded-Proto must be ignored")
}

// [SEC-002] A trusted peer's X-Forwarded-For is honoured.
func TestSEC002_TrustedPeer_XForwardedForHonoured(t *testing.T) {
	tp := ParseTrustedProxies("203.0.113.0/24")
	handler, captured := newRealIPHandler(tp)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	req.Header.Set("X-Forwarded-Proto", "https")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "198.51.100.20", captured.clientIP, "trusted peer's X-Forwarded-For must be honoured")
	assert.Equal(t, "https", captured.scheme, "trusted peer's X-Forwarded-Proto must be honoured")
}

// [SEC-002] Rightmost-untrusted walk: skip trusted hops, return the first untrusted entry from the right.
func TestSEC002_RightmostUntrustedWalk(t *testing.T) {
	tp := ParseTrustedProxies("203.0.113.0/24,198.51.100.0/24")
	handler, captured := newRealIPHandler(tp)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	// Chain: real-client, trusted-hop-1, trusted-hop-2 (rightmost)
	req.Header.Set("X-Forwarded-For", "192.0.2.55, 203.0.113.5, 198.51.100.10")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "192.0.2.55", captured.clientIP, "must walk right-to-left, skip trusted hops, and return the first untrusted address")
}

// [C5] When every entry in the X-Forwarded-For chain is trusted, fall back to
// the TCP peer address rather than an unspecified value.
func TestC5_AllTrustedXFFChain_FallsBackToPeer(t *testing.T) {
	tp := ParseTrustedProxies("203.0.113.0/24,198.51.100.0/24")
	handler, captured := newRealIPHandler(tp)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 198.51.100.10")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "203.0.113.9", captured.clientIP, "an all-trusted XFF chain must fall back to the TCP peer, not an unspecified value")
}

// [SEC-002] A malformed CIDR entry is dropped; the remaining valid entries in
// the list still work.
func TestSEC002_MalformedCIDR_DroppedRestStillWorks(t *testing.T) {
	tp := ParseTrustedProxies("not-a-cidr, 203.0.113.0/24, also-bad")
	require.NotNil(t, tp)

	handler, captured := newRealIPHandler(tp)
	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "198.51.100.20", captured.clientIP, "valid entries in the list must still work after a malformed entry is dropped")
}

// [SEC-002] Every entry malformed results in a trust-nobody set (fails closed).
func TestSEC002_AllMalformedCIDR_TrustsNobody(t *testing.T) {
	tp := ParseTrustedProxies("not-a-cidr, also-bad")
	handler, captured := newRealIPHandler(tp)
	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "203.0.113.9", captured.clientIP, "an entirely malformed trusted-proxy list must fail closed to trust-nobody")
}

// [SEC-002] A bare IP (no CIDR suffix) is accepted as a /32.
func TestSEC002_BareIPTreatedAsSingleHost(t *testing.T) {
	tp := ParseTrustedProxies("203.0.113.9")
	handler, captured := newRealIPHandler(tp)
	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "198.51.100.20", captured.clientIP)

	// A different peer not covered by the bare-IP entry is untrusted.
	handler2, captured2 := newRealIPHandler(tp)
	req2 := httptest.NewRequest("GET", "/api/v1/auth/oidc/start", nil)
	req2.RemoteAddr = "203.0.113.10:5555"
	req2.Header.Set("X-Forwarded-For", "198.51.100.20")
	handler2.ServeHTTP(httptest.NewRecorder(), req2)
	assert.Equal(t, "203.0.113.10", captured2.clientIP)
}

// RequestScheme falls back to r.TLS when no trusted proxy header is present.
func TestRequestScheme_FallsBackToTLS(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	assert.Equal(t, "http", RequestScheme(req))

	req.TLS = &tls.ConnectionState{}
	assert.Equal(t, "https", RequestScheme(req))
}

// ClientIP falls back to the raw RemoteAddr when it has no port.
func TestClientIP_FallsBackToRawRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "not-a-host-port"
	assert.Equal(t, "not-a-host-port", ClientIP(req))
}

// A nil TrustedProxies (mis-wired dependency) trusts nobody.
func TestParseTrustedProxies_NilSafe(t *testing.T) {
	var tp *TrustedProxies
	assert.False(t, tp.trusts(mustParseAddr("203.0.113.9")))
}

func mustParseAddr(s string) (addr netip.Addr) {
	addr, _ = netip.ParseAddr(s)
	return addr
}
