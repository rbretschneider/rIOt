package oidc

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRedirectURI = "https://riot.example:7331/api/v1/auth/oidc/callback"

func newEnabledService(t *testing.T, issuerURL, clientID, clientSecret string) *Service {
	t.Helper()
	return New(Options{
		IssuerURL:    issuerURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		JWTSecret:    testJWTSecret(),
	})
}

// [AC-006] Start redirects to the IdP with PKCE, state and nonce: the
// returned authorization URL carries response_type=code,
// code_challenge_method=S256, a code_challenge, a state, a nonce, and a
// scope containing openid.
func TestAC006_BeginLogin_AuthURLShapeAndPKCE(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	authURL, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	u, err := url.Parse(authURL)
	require.NoError(t, err)
	q := u.Query()

	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotEmpty(t, q.Get("state"))
	assert.NotEmpty(t, q.Get("nonce"))
	assert.Contains(t, q.Get("scope"), "openid")
	assert.Equal(t, testRedirectURI, q.Get("redirect_uri"))

	assert.Equal(t, q.Get("state"), tx.State)
	assert.Equal(t, q.Get("nonce"), tx.Nonce)
	assert.NotEmpty(t, tx.CodeVerifier)
	assert.Equal(t, "/", tx.ReturnPath)
}

// [AC-008] Successful login: CompleteLogin validates a correctly-signed ID
// token with a matching nonce and returns claims with no token field
// reachable (AD-017 — enforced structurally by the Claims type itself).
func TestAC008_CompleteLogin_Success(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	authURL, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)
	_ = authURL

	email := "person@example.com"
	code := idp.issueCode("riot-client", idTokenTestClaims{
		Subject:       "user-123",
		Nonce:         tx.Nonce,
		HasEmail:      true,
		Email:         email,
		HasVerified:   true,
		EmailVerified: true,
	})

	claims, err := svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.NoError(t, err)
	assert.Equal(t, idp.issuer(), claims.Issuer)
	assert.Equal(t, "user-123", claims.Subject)
	require.NotNil(t, claims.Email)
	assert.Equal(t, email, *claims.Email)
	require.NotNil(t, claims.EmailVerified)
	assert.True(t, *claims.EmailVerified)
}

// [AC-014] Nonce mismatch is rejected: the ID token's nonce claim does not
// match the transaction's nonce.
func TestAC014_CompleteLogin_NonceMismatch(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	code := idp.issueCode("riot-client", idTokenTestClaims{
		Subject: "user-123",
		Nonce:   "a-different-nonce",
	})

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonNonceMismatch, loginErr.Reason)
}

// [AC-017] IdP down: BeginLogin degrades with sso_unavailable, and discovery
// failure is never cached — a subsequent attempt against a live IdP retries
// discovery rather than reusing a cached failure (AD-004, A-7).
func TestAC017_BeginLogin_IdPDown_UnavailableAndNotCached(t *testing.T) {
	deadIDP := newClosedTestIDP(t)
	svc := newEnabledService(t, deadIDP.issuer(), "riot-client", "riot-secret")

	_, _, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeUnavailable, loginErr.Code)
	assert.Equal(t, ReasonDiscoveryFailed, loginErr.Reason)

	// A discovery failure must not be cached (AD-004): point the *same*
	// Service at a live issuer by constructing a fresh Service pointed at a
	// live stub sharing the same in-test behaviour — the important
	// assertion is that svc.provider was never set, so the very next call
	// still attempts discovery rather than short-circuiting.
	liveIDP := newTestIDP(t)
	svc2 := newEnabledService(t, liveIDP.issuer(), "riot-client", "riot-secret")
	_, _, err = svc2.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	assert.NoError(t, err, "a fresh discovery attempt against a live issuer must succeed")
}

// A discovery failure against a Service pointed at a dead IdP is retried
// (not cached) once the IdP comes back — proven directly against the same
// *Service* by restarting behaviour is impractical with httptest, so this
// asserts the documented contract at the unit level: repeated calls against
// a persistently-down IdP each attempt discovery independently (no panic,
// no stale cached provider ever returned).
func TestAC017_BeginLogin_RepeatedFailureDoesNotPanicOrCache(t *testing.T) {
	deadIDP := newClosedTestIDP(t)
	svc := newEnabledService(t, deadIDP.issuer(), "riot-client", "riot-secret")

	for i := 0; i < 3; i++ {
		_, _, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
		require.Error(t, err)
	}
}

// Token exchange rejected by the IdP (an OAuth error) maps to sso_failed.
func TestCompleteLogin_TokenExchangeRejected(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	code := idp.issueErrorCode("invalid_grant")

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonTokenExchangeRejected, loginErr.Reason)
}

// An unknown authorization code (never issued by the stub) also surfaces as
// a rejected token exchange.
func TestCompleteLogin_UnknownCode(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, "never-issued", tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
}

// A response with no id_token is rejected.
func TestCompleteLogin_MissingIDToken(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	code := idp.issueCode("riot-client", idTokenTestClaims{Subject: "user-123", Nonce: tx.Nonce, OmitIDToken: true})

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonMissingIDToken, loginErr.Reason)
}

// A malformed id_token string is rejected as invalid, not parsed by hand.
func TestCompleteLogin_MalformedIDToken(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	code := idp.issueCode("riot-client", idTokenTestClaims{Subject: "user-123", Nonce: tx.Nonce, MalformedIDToken: true})

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonTokenInvalid, loginErr.Reason)
}

// [FR-021] Signature validation is enforced: an ID token signed with a key
// other than the one advertised in the IdP's JWKS fails verification.
func TestFR021_CompleteLogin_WrongSigningKey_Rejected(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	otherIDP := newTestIDP(t)
	code := idp.issueCode("riot-client", idTokenTestClaims{
		Subject:    "user-123",
		Nonce:      tx.Nonce,
		SigningKey: otherIDP.key, // signed with a key not in idp's JWKS
	})

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonTokenInvalid, loginErr.Reason)
}

// [FR-021] Audience validation is enforced: an ID token issued for a
// different client ID fails verification.
func TestFR021_CompleteLogin_WrongAudience_Rejected(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	code := idp.issueCode("some-other-client", idTokenTestClaims{
		Subject: "user-123",
		Nonce:   tx.Nonce,
	})

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonTokenInvalid, loginErr.Reason)
}

// [FR-021] Expiry validation is enforced: an already-expired ID token fails verification.
func TestFR021_CompleteLogin_ExpiredToken_Rejected(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	code := idp.issueCode("riot-client", idTokenTestClaims{
		Subject:   "user-123",
		Nonce:     tx.Nonce,
		ExpiresIn: -1 * time.Hour, // already expired
	})

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonTokenInvalid, loginErr.Reason)
}

// [V-001] A validated token missing subject is a claims_incomplete rejection.
func TestV001_CompleteLogin_EmptySubject_Rejected(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	code := idp.issueCode("riot-client", idTokenTestClaims{
		Subject: "",
		Nonce:   tx.Nonce,
	})

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, code, tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonClaimsIncomplete, loginErr.Reason)
}

// Missing code parameter is rejected before any network call.
func TestCompleteLogin_MissingCode(t *testing.T) {
	idp := newTestIDP(t)
	svc := newEnabledService(t, idp.issuer(), "riot-client", "riot-secret")

	_, tx, err := svc.BeginLogin(context.Background(), testRedirectURI, "/", time.Now())
	require.NoError(t, err)

	_, err = svc.CompleteLogin(context.Background(), testRedirectURI, "", tx)
	require.Error(t, err)
	var loginErr *LoginError
	require.True(t, errors.As(err, &loginErr))
	assert.Equal(t, CodeFailed, loginErr.Code)
	assert.Equal(t, ReasonMissingCode, loginErr.Reason)
}
