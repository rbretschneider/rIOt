package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testIDP is a test-only stub OIDC issuer: an httptest.Server serving
// discovery, token exchange, and a JWKS endpoint, with in-test RSA key(s) and
// controllable ID-token claims per authorization code. No test using this
// stub makes an outbound network connection (§12 note 21).
type testIDP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string
	seq    int

	mu    sync.Mutex
	codes map[string]*codeResponse
}

// codeResponse describes what the stub's /token endpoint should return for a
// specific authorization code.
type codeResponse struct {
	tokenError string // if non-empty, /token responds 400 {"error": tokenError}
	claims     idTokenTestClaims
}

type idTokenTestClaims struct {
	Issuer        string
	Audience      string
	Subject       string
	Nonce         string
	Email         string
	HasEmail      bool
	EmailVerified bool
	HasVerified   bool
	ExpiresIn     time.Duration
	SigningKey    *rsa.PrivateKey
	KID           string
	// OmitIDToken, when true, causes /token to succeed but omit id_token entirely.
	OmitIDToken bool
	// MalformedIDToken, when true, causes /token to return a syntactically
	// invalid id_token string instead of a signed JWT.
	MalformedIDToken bool
}

// newTestIDP starts a stub issuer and returns it. Call t.Cleanup is handled
// internally; the caller does not need to close it explicitly unless
// simulating an IdP that goes down mid-test (see close()).
func newTestIDP(t *testing.T) *testIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	idp := &testIDP{
		t:     t,
		key:   key,
		kid:   "test-key-1",
		codes: make(map[string]*codeResponse),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/token", idp.handleToken)
	mux.HandleFunc("/keys", idp.handleKeys)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// newClosedTestIDP returns a testIDP whose server is already closed, so any
// discovery attempt against its issuer URL fails with connection refused —
// simulating an unreachable IdP (AC-017, AC-019).
func newClosedTestIDP(t *testing.T) *testIDP {
	t.Helper()
	idp := newTestIDP(t)
	idp.server.Close()
	return idp
}

func (idp *testIDP) issuer() string {
	return idp.server.URL
}

func (idp *testIDP) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	doc := map[string]interface{}{
		"issuer":                                idp.issuer(),
		"authorization_endpoint":                idp.issuer() + "/authorize",
		"token_endpoint":                        idp.issuer() + "/token",
		"jwks_uri":                              idp.issuer() + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

func (idp *testIDP) handleKeys(w http.ResponseWriter, r *http.Request) {
	pub := idp.key.PublicKey
	jwk := map[string]interface{}{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": idp.kid,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{jwk}})
}

func (idp *testIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code := r.Form.Get("code")

	idp.mu.Lock()
	resp, ok := idp.codes[code]
	idp.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	if resp.tokenError != "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": resp.tokenError})
		return
	}

	body := map[string]interface{}{
		"access_token": "test-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	}

	if !resp.claims.OmitIDToken {
		if resp.claims.MalformedIDToken {
			body["id_token"] = "not-a-valid-jwt"
		} else {
			idToken, err := idp.signIDToken(resp.claims)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			body["id_token"] = idToken
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (idp *testIDP) signIDToken(c idTokenTestClaims) (string, error) {
	now := time.Now()
	expiresIn := c.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 5 * time.Minute
	}
	issuer := c.Issuer
	if issuer == "" {
		issuer = idp.issuer()
	}

	claims := jwt.MapClaims{
		"iss": issuer,
		"sub": c.Subject,
		"aud": c.Audience,
		"exp": now.Add(expiresIn).Unix(),
		"iat": now.Unix(),
	}
	if c.Nonce != "" {
		claims["nonce"] = c.Nonce
	}
	if c.HasEmail {
		claims["email"] = c.Email
	}
	if c.HasVerified {
		claims["email_verified"] = c.EmailVerified
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	key := c.SigningKey
	if key == nil {
		key = idp.key
	}
	kid := c.KID
	if kid == "" {
		kid = idp.kid
	}
	token.Header["kid"] = kid

	return token.SignedString(key)
}

// issueCode registers an authorization code that /token will exchange for a
// valid, signed ID token built from the given claims. clientID becomes the
// token's "aud" unless claims.Audience is already set.
func (idp *testIDP) issueCode(clientID string, claims idTokenTestClaims) string {
	if claims.Audience == "" {
		claims.Audience = clientID
	}
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.seq++
	code := "test-code-" + strconv.Itoa(idp.seq)
	idp.codes[code] = &codeResponse{claims: claims}
	return code
}

// issueErrorCode registers an authorization code that causes /token to
// respond with an OAuth error (simulating *oauth2.RetrieveError).
func (idp *testIDP) issueErrorCode(oauthError string) string {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.seq++
	code := "test-code-" + strconv.Itoa(idp.seq)
	idp.codes[code] = &codeResponse{tokenError: oauthError}
	return code
}
