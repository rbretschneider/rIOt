package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	goidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const discoveryAndExchangeTimeout = 10 * time.Second

// Service owns the Authorization Code + PKCE protocol against a single OIDC
// issuer. Constructed once in setupRouter() and lives for the process
// lifetime, which is what makes the discovery cache (AD-004) useful.
type Service struct {
	mu       sync.Mutex
	provider *goidc.Provider

	httpClient *http.Client

	enabled      bool
	issuerURL    string
	clientID     string
	clientSecret string
	buttonLabel  string

	txKey []byte
}

// discover returns the cached discovery document, fetching it on first use.
// A successful fetch is cached for the process lifetime; a failed fetch is
// never cached, so a recovered IdP works on the next attempt without a
// restart (AD-004, A-7). Never called during Start()/setupRouter() — only
// BeginLogin and CompleteLogin call it.
func (s *Service) discover(ctx context.Context) (*goidc.Provider, error) {
	s.mu.Lock()
	if s.provider != nil {
		p := s.provider
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: discoveryAndExchangeTimeout}
	}
	cctx := goidc.ClientContext(ctx, client)
	p, err := goidc.NewProvider(cctx, s.issuerURL)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.provider = p
	s.mu.Unlock()
	return p, nil
}

func (s *Service) oauth2Config(provider *goidc.Provider, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       []string{goidc.ScopeOpenID, "email", "profile"},
	}
}

// generateRandomValue returns a base64url-encoded value from n bytes of
// crypto/rand — n=32 gives 256 bits of entropy, well over NFR-005's 128-bit
// floor.
func generateRandomValue(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// BeginLogin implements the /start flow: discovery, state/nonce/PKCE
// generation, and authorization-URL construction. It performs no cookie or
// HTTP-response work — that stays in handlers/oidc.go.
func (s *Service) BeginLogin(ctx context.Context, redirectURI, returnPath string, now time.Time) (string, Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryAndExchangeTimeout)
	defer cancel()

	provider, err := s.discover(ctx)
	if err != nil {
		return "", Transaction{}, newLoginError(CodeUnavailable, ReasonDiscoveryFailed, err)
	}

	state, err := generateRandomValue(32)
	if err != nil {
		return "", Transaction{}, newLoginError(CodeUnavailable, ReasonRandFailed, err)
	}
	nonce, err := generateRandomValue(32)
	if err != nil {
		return "", Transaction{}, newLoginError(CodeUnavailable, ReasonRandFailed, err)
	}
	verifier := oauth2.GenerateVerifier()

	cfg := s.oauth2Config(provider, redirectURI)
	authURL := cfg.AuthCodeURL(state, goidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))

	tx := Transaction{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: verifier,
		ReturnPath:   SafeReturnPath(returnPath),
		IssuedAt:     now.Unix(),
	}

	return authURL, tx, nil
}

// idTokenClaims is the subset of ID-token claims decoded beyond what
// go-oidc's IDToken already exposes as fields (Issuer, Subject).
type idTokenClaims struct {
	Email         *string `json:"email"`
	EmailVerified *bool   `json:"email_verified"`
}

// CompleteLogin implements the /callback flow: token exchange and ID-token
// validation via the OIDC library (FR-021 — no hand-written token parsing).
// The returned Claims struct has no token field by construction (AD-017),
// so BR-005/FR-025/AC-023 cannot regress through inattention.
func (s *Service) CompleteLogin(ctx context.Context, redirectURI, code string, tx Transaction) (Claims, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryAndExchangeTimeout)
	defer cancel()

	provider, err := s.discover(ctx)
	if err != nil {
		return Claims{}, newLoginError(CodeUnavailable, ReasonDiscoveryFailed, err)
	}

	if code == "" {
		return Claims{}, newLoginError(CodeFailed, ReasonMissingCode, nil)
	}

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: discoveryAndExchangeTimeout}
	}
	cctx := context.WithValue(ctx, oauth2.HTTPClient, client)

	cfg := s.oauth2Config(provider, redirectURI)
	token, err := cfg.Exchange(cctx, code, oauth2.VerifierOption(tx.CodeVerifier))
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if isRetrieveError(err, &retrieveErr) {
			return Claims{}, newLoginError(CodeFailed, ReasonTokenExchangeRejected, err)
		}
		return Claims{}, newLoginError(CodeUnavailable, ReasonIdPUnreachable, err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Claims{}, newLoginError(CodeFailed, ReasonMissingIDToken, nil)
	}

	verifier := provider.Verifier(&goidc.Config{ClientID: s.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, newLoginError(CodeFailed, ReasonTokenInvalid, err)
	}

	if idToken.Nonce != tx.Nonce {
		return Claims{}, newLoginError(CodeFailed, ReasonNonceMismatch, nil)
	}

	var extra idTokenClaims
	_ = idToken.Claims(&extra) // best-effort: absent/malformed extra claims are not fatal

	claims := Claims{
		Issuer:        idToken.Issuer,
		Subject:       idToken.Subject,
		Email:         extra.Email,
		EmailVerified: extra.EmailVerified,
	}

	if err := ValidateClaims(claims); err != nil {
		return Claims{}, err
	}

	return claims, nil
}

// isRetrieveError reports whether err is (or wraps) an *oauth2.RetrieveError
// — i.e. the IdP responded with an OAuth error, as opposed to a transport
// failure (timeout/DNS/connection refused).
func isRetrieveError(err error, target **oauth2.RetrieveError) bool {
	for err != nil {
		if re, ok := err.(*oauth2.RetrieveError); ok {
			*target = re
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
