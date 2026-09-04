package handlers

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

// stubIDP is a minimal test-only OIDC issuer for exercising the OIDC-001
// handlers end to end without any outbound network connection (§12 note 21).
// It mirrors internal/server/oidc/testidp_test.go's shape but lives in this
// package since Go test files are not importable across packages.
type stubIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string

	mu    sync.Mutex
	seq   int
	codes map[string]*stubCodeResponse
}

type stubCodeResponse struct {
	tokenError string
	claims     stubIDTokenClaims
}

type stubIDTokenClaims struct {
	Audience         string
	Subject          string
	Nonce            string
	Email            string
	HasEmail         bool
	EmailVerified    bool
	HasVerified      bool
	ExpiresIn        time.Duration
	OmitIDToken      bool
	MalformedIDToken bool
}

func newStubIDP(t *testing.T) *stubIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	idp := &stubIDP{key: key, kid: "test-key-1", codes: make(map[string]*stubCodeResponse)}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/token", idp.handleToken)
	mux.HandleFunc("/keys", idp.handleKeys)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func newClosedStubIDP(t *testing.T) *stubIDP {
	t.Helper()
	idp := newStubIDP(t)
	idp.server.Close()
	return idp
}

func (idp *stubIDP) issuer() string { return idp.server.URL }

func (idp *stubIDP) handleDiscovery(w http.ResponseWriter, r *http.Request) {
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

func (idp *stubIDP) handleKeys(w http.ResponseWriter, r *http.Request) {
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

func (idp *stubIDP) handleToken(w http.ResponseWriter, r *http.Request) {
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

func (idp *stubIDP) signIDToken(c stubIDTokenClaims) (string, error) {
	now := time.Now()
	expiresIn := c.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 5 * time.Minute
	}
	claims := jwt.MapClaims{
		"iss": idp.issuer(),
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
	token.Header["kid"] = idp.kid
	return token.SignedString(idp.key)
}

func (idp *stubIDP) issueCode(clientID string, claims stubIDTokenClaims) string {
	if claims.Audience == "" {
		claims.Audience = clientID
	}
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.seq++
	code := "stub-code-" + strconv.Itoa(idp.seq)
	idp.codes[code] = &stubCodeResponse{claims: claims}
	return code
}

func (idp *stubIDP) issueErrorCode(oauthError string) string {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.seq++
	code := "stub-code-" + strconv.Itoa(idp.seq)
	idp.codes[code] = &stubCodeResponse{tokenError: oauthError}
	return code
}
