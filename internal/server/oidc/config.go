package oidc

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultButtonLabel  = "Sign in with SSO"
	maxButtonLabelRunes = 64
)

// Options configures a Service. All string fields are expected to already be
// trimmed by server.LoadConfig (AD-003) — this package re-trims defensively
// but owns no environment reads of its own.
type Options struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	ButtonLabel  string
	JWTSecret    []byte
}

// New applies OIDC-001's dormancy policy (FR-002, FR-003, V-005, OQ-8) and
// returns a *Service that is either enabled or dormant. New performs no
// network I/O and must never be called with the intent of contacting the
// IdP — discovery is lazy (AD-004).
func New(o Options) *Service {
	s := &Service{httpClient: &http.Client{Timeout: discoveryAndExchangeTimeout}}

	issuer := strings.TrimSpace(o.IssuerURL)
	clientID := strings.TrimSpace(o.ClientID)
	clientSecret := strings.TrimSpace(o.ClientSecret)

	if issuer == "" || clientID == "" || clientSecret == "" {
		// Dormant: FR-002. No warning — this is the expected default state.
		s.txKey = DeriveTransactionKey(o.JWTSecret)
		return s
	}

	if !isValidAbsoluteHTTPURL(issuer) {
		slog.Warn("OIDC issuer URL is not a valid absolute http(s) URL — SSO stays dormant", "issuer", issuer)
		s.txKey = DeriveTransactionKey(o.JWTSecret)
		return s
	}

	label := strings.TrimSpace(o.ButtonLabel)
	if label == "" {
		label = defaultButtonLabel
	} else {
		runes := []rune(label)
		if len(runes) > maxButtonLabelRunes {
			label = string(runes[:maxButtonLabelRunes])
			slog.Warn("RIOT_OIDC_BUTTON_LABEL exceeds 64 characters — truncated", "label", label)
		}
	}

	s.enabled = true
	s.issuerURL = issuer
	s.clientID = clientID
	s.clientSecret = clientSecret
	s.buttonLabel = label
	s.txKey = DeriveTransactionKey(o.JWTSecret)
	return s
}

// isValidAbsoluteHTTPURL implements V-005: the issuer URL must parse as an
// absolute http/https URL with a non-empty host.
func isValidAbsoluteHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	return true
}

// Enabled reports whether SSO is configured. Nil-safe: a mis-wired
// dependency yields "SSO off", never "SSO on and ungated" (AD-011).
func (s *Service) Enabled() bool {
	return s != nil && s.enabled
}

// ButtonLabel returns the effective button label, or "" when dormant.
func (s *Service) ButtonLabel() string {
	if s == nil || !s.enabled {
		return ""
	}
	return s.buttonLabel
}

// TransactionKey returns the derived transaction MAC key (AD-005). Nil-safe:
// returns nil when s is nil, in which case DecodeTransaction/Encode will
// simply fail to authenticate anything, never panic.
func (s *Service) TransactionKey() []byte {
	if s == nil {
		return nil
	}
	return s.txKey
}
