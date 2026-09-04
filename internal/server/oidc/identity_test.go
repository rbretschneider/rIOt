package oidc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// [AC-026] Open redirect is refused: SafeReturnPath rejects everything except
// a single-leading-slash same-origin path.
func TestAC026_SafeReturnPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"absolute foreign URL", "https://evil.example", "/"},
		{"protocol-relative", "//evil.example", "/"},
		{"backslash bypass", "/\\evil.example", "/"},
		{"no leading slash", "evil", "/"},
		{"empty string", "", "/"},
		{"valid path with query", "/ok?a=b", "/ok?a=b"},
		{"valid root", "/", "/"},
		{"control character", "/ok\x00path", "/"},
		{"CR/LF injection", "/ok\r\nX-Injected: 1", "/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeReturnPath(tc.in)
			assert.Equal(t, tc.want, got, "input %q", tc.in)
		})
	}
}

// [V-001] Issuer and subject must both be non-empty; a validated token
// missing either is a claims_incomplete rejection.
func TestV001_ValidateClaims(t *testing.T) {
	email := "user@example.com"
	verified := true

	t.Run("valid claims pass", func(t *testing.T) {
		err := ValidateClaims(Claims{Issuer: "https://auth.example/", Subject: "abc123", Email: &email, EmailVerified: &verified})
		assert.NoError(t, err)
	})

	t.Run("missing issuer is rejected", func(t *testing.T) {
		err := ValidateClaims(Claims{Issuer: "", Subject: "abc123"})
		assertClaimsIncomplete(t, err)
	})

	t.Run("missing subject is rejected", func(t *testing.T) {
		err := ValidateClaims(Claims{Issuer: "https://auth.example/", Subject: ""})
		assertClaimsIncomplete(t, err)
	})

	t.Run("whitespace-only issuer is rejected", func(t *testing.T) {
		err := ValidateClaims(Claims{Issuer: "   ", Subject: "abc123"})
		assertClaimsIncomplete(t, err)
	})

	t.Run("nil email and email_verified do not block validation (BR-007)", func(t *testing.T) {
		err := ValidateClaims(Claims{Issuer: "https://auth.example/", Subject: "abc123"})
		assert.NoError(t, err)
	})

	t.Run("email_verified=false does not block validation (BR-007)", func(t *testing.T) {
		unverified := false
		err := ValidateClaims(Claims{Issuer: "https://auth.example/", Subject: "abc123", EmailVerified: &unverified})
		assert.NoError(t, err)
	})
}

func assertClaimsIncomplete(t *testing.T, err error) {
	t.Helper()
	var loginErr *LoginError
	if assertAsLoginError(t, err, &loginErr) {
		assert.Equal(t, CodeFailed, loginErr.Code)
		assert.Equal(t, ReasonClaimsIncomplete, loginErr.Reason)
	}
}

func assertAsLoginError(t *testing.T, err error, target **LoginError) bool {
	t.Helper()
	le, ok := err.(*LoginError)
	if !ok {
		t.Fatalf("expected *LoginError, got %T: %v", err, err)
		return false
	}
	*target = le
	return true
}
