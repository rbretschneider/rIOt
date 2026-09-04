package oidc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func testJWTSecret() []byte { return []byte("test-jwt-secret-for-oidc-config") }

// [AC-003] Partial configuration stays dormant: any one of the three
// required vars empty leaves SSO dormant.
func TestAC003_New_PartialConfig_Dormant(t *testing.T) {
	cases := []Options{
		{IssuerURL: "", ClientID: "client", ClientSecret: "secret", JWTSecret: testJWTSecret()},
		{IssuerURL: "https://auth.example/", ClientID: "", ClientSecret: "secret", JWTSecret: testJWTSecret()},
		{IssuerURL: "https://auth.example/", ClientID: "client", ClientSecret: "", JWTSecret: testJWTSecret()},
		{IssuerURL: "   ", ClientID: "client", ClientSecret: "secret", JWTSecret: testJWTSecret()},
	}
	for i, o := range cases {
		s := New(o)
		assert.Falsef(t, s.Enabled(), "case %d should be dormant", i)
		assert.Emptyf(t, s.ButtonLabel(), "case %d dormant label must be empty", i)
	}
}

// [AC-004] Configured: availability and default label — when all three vars
// are set and the label is unset, the effective label defaults to
// "Sign in with SSO".
func TestAC004_New_AllConfigured_DefaultLabel(t *testing.T) {
	s := New(Options{
		IssuerURL:    "https://auth.example/application/o/riot/",
		ClientID:     "riot-client",
		ClientSecret: "riot-secret",
		JWTSecret:    testJWTSecret(),
	})
	assert.True(t, s.Enabled())
	assert.Equal(t, "Sign in with SSO", s.ButtonLabel())
}

// Custom label is echoed verbatim when under the length limit.
func TestNew_CustomLabel(t *testing.T) {
	s := New(Options{
		IssuerURL:    "https://auth.example/application/o/riot/",
		ClientID:     "riot-client",
		ClientSecret: "riot-secret",
		ButtonLabel:  "Sign in with authentik",
		JWTSecret:    testJWTSecret(),
	})
	assert.Equal(t, "Sign in with authentik", s.ButtonLabel())
}

// [V-005] A malformed issuer URL leaves SSO dormant without aborting
// construction (boot).
func TestV005_New_MalformedIssuerURL_Dormant(t *testing.T) {
	cases := []string{
		"not-a-url",
		"ftp://auth.example/",
		"://missing-scheme",
		"https://",
	}
	for _, issuer := range cases {
		s := New(Options{
			IssuerURL:    issuer,
			ClientID:     "client",
			ClientSecret: "secret",
			JWTSecret:    testJWTSecret(),
		})
		assert.Falsef(t, s.Enabled(), "issuer %q should leave SSO dormant", issuer)
	}
}

// [OQ-8] RIOT_OIDC_BUTTON_LABEL longer than 64 runes is truncated, not rejected outright.
func TestOQ8_New_LongLabel_Truncated(t *testing.T) {
	long := strings.Repeat("x", 100)
	s := New(Options{
		IssuerURL:    "https://auth.example/application/o/riot/",
		ClientID:     "client",
		ClientSecret: "secret",
		ButtonLabel:  long,
		JWTSecret:    testJWTSecret(),
	})
	label := s.ButtonLabel()
	assert.Len(t, []rune(label), maxButtonLabelRunes)
	assert.Equal(t, strings.Repeat("x", maxButtonLabelRunes), label)
}

// A nil *Service (mis-wired dependency) is nil-safe.
func TestService_NilSafe(t *testing.T) {
	var s *Service
	assert.False(t, s.Enabled())
	assert.Empty(t, s.ButtonLabel())
}
