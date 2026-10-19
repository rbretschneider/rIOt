package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/server/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withSecurityHeaders wraps an OIDC handler in the same two outer middlewares
// production applies first: RealIP, then SecurityHeaders (HARDEN-002 T-3).
func withSecurityHeaders(h http.HandlerFunc) http.Handler {
	return middleware.RealIP(middleware.ParseTrustedProxies(""))(middleware.SecurityHeaders()(h))
}

func assertHeadersPresent(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	csp := rec.Header().Get("Content-Security-Policy")
	assert.NotEmpty(t, csp, "%s: Content-Security-Policy", what)
	assert.Contains(t, csp, "form-action 'self'", "%s: the CSP carries form-action 'self'; anchor navigation to the IdP is not governed by it", what)
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), "%s: X-Frame-Options", what)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), "%s: X-Content-Type-Options", what)
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"), "%s: Referrer-Policy", what)
}

// [HARDEN-002][AC-016] The OIDC start redirect and the callback redirect are
// byte-for-byte what they were, while now also carrying the baseline headers.
func TestHARDEN002_AC016_OIDCRedirectsUnaffectedByHeaders(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	// /start
	startRec := httptest.NewRecorder()
	withSecurityHeaders(setup.h.OIDCStart).ServeHTTP(startRec, newOIDCRequest("GET", "/api/v1/auth/oidc/start"))

	require.Equal(t, http.StatusFound, startRec.Code, "/start must still redirect with 302")
	loc, err := url.Parse(startRec.Header().Get("Location"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(loc.String(), idp.issuer()), "Location must point at the stub IdP authorization endpoint, got %s", loc)
	assert.Equal(t, "code", loc.Query().Get("response_type"))
	assert.Equal(t, oidcTestRedirectURI, loc.Query().Get("redirect_uri"), "redirect_uri must be exact and unchanged")
	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)
	require.NotNil(t, txCookie, "the transaction cookie must still be set")
	assertHeadersPresent(t, startRec, "/start")
	assert.Equal(t, "max-age=31536000", startRec.Header().Get("Strict-Transport-Security"), "the test request is TLS")

	// /callback
	nonce := loc.Query().Get("nonce")
	code := idp.issueCode(oidcTestClientID, stubIDTokenClaims{
		Subject: "user-abc", Nonce: nonce, HasEmail: true, Email: "person@example.com", HasVerified: true, EmailVerified: true,
	})
	cbReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?code="+code+"&state="+loc.Query().Get("state"))
	cbReq.AddCookie(txCookie)
	cbRec := httptest.NewRecorder()
	withSecurityHeaders(setup.h.OIDCCallback).ServeHTTP(cbRec, cbReq)

	require.Equal(t, http.StatusFound, cbRec.Code, "/callback must still redirect with 302")
	assert.Equal(t, "/", cbRec.Header().Get("Location"), "the callback must redirect to the dashboard")
	session := cookieByName(t, cbRec, "riot_session")
	require.NotNil(t, session, "the session cookie must still be set")
	assert.True(t, session.Secure, "Secure follows the same predicate as HSTS")
	assertHeadersPresent(t, cbRec, "/callback")
}

// [HARDEN-002][AC-016] Failure redirects (IdP error) are redirects too and carry the headers.
func TestHARDEN002_AC016_OIDCFailureRedirectCarriesHeaders(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	rec := httptest.NewRecorder()
	withSecurityHeaders(setup.h.OIDCCallback).ServeHTTP(rec, newOIDCRequest("GET", "/api/v1/auth/oidc/callback?error=access_denied"))

	assert.Equal(t, http.StatusFound, rec.Code, "an IdP error still lands on the login screen via a redirect")
	assertHeadersPresent(t, rec, "failure redirect")
}
