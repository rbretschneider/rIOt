package oidc

import "strings"

// Claims holds the ID-token claims this package cares about, after
// signature/issuer/audience/expiry validation has already been performed by
// the OIDC library (FR-021). Email and EmailVerified are pointers so
// "absent" and "present and false/empty" stay distinguishable in the audit
// row (FRD §7.1, A-5).
type Claims struct {
	Issuer        string
	Subject       string
	Email         *string
	EmailVerified *bool
}

// ValidateClaims implements V-001: a validated token missing Issuer or
// Subject is a claims_incomplete rejection. email_verified is never
// consulted here — it is recorded, never gated on (BR-007).
func ValidateClaims(c Claims) error {
	if strings.TrimSpace(c.Issuer) == "" || strings.TrimSpace(c.Subject) == "" {
		return newLoginError(CodeFailed, ReasonClaimsIncomplete, nil)
	}
	return nil
}

// SafeReturnPath implements V-004 / AC-026: returns raw only when it is a
// same-origin absolute path — exactly one leading '/', not a protocol-relative
// "//host", contains no backslash (some browsers normalise '\' to '/', making
// "/\evil.example" a protocol-relative bypass), and contains no control
// characters. Any other value is replaced with "/".
func SafeReturnPath(raw string) string {
	const fallback = "/"
	if raw == "" {
		return fallback
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return fallback
	}
	if strings.ContainsRune(raw, '\\') {
		return fallback
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return fallback
		}
	}
	return raw
}
