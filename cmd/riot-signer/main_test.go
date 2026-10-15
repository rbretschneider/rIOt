package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/releasekey"
)

// signerFixture is a tool wired to an in-memory environment and a test-owned
// keypair, plus a temp dir holding a checksums.txt.
type signerFixture struct {
	tool         *tool
	stdout       *bytes.Buffer
	stderr       *bytes.Buffer
	pub          ed25519.PublicKey
	privB64      string
	checksumsTxt string
	sigPath      string
}

func newSignerFixture(t *testing.T) *signerFixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	dir := t.TempDir()
	f := &signerFixture{
		stdout:       &bytes.Buffer{},
		stderr:       &bytes.Buffer{},
		pub:          pub,
		privB64:      base64.StdEncoding.EncodeToString(priv),
		checksumsTxt: filepath.Join(dir, "checksums.txt"),
		sigPath:      filepath.Join(dir, "checksums.txt.sig"),
	}
	require.NoError(t, os.WriteFile(f.checksumsTxt, []byte(strings.Repeat("a", 64)+"  riot-agent-linux-amd64\n"), 0o644))
	f.tool = &tool{
		getenv: func(name string) string {
			if name == privateKeyEnvVar {
				return f.privB64
			}
			return ""
		},
		publicKey: func() (ed25519.PublicKey, error) { return f.pub, nil },
		stdout:    f.stdout,
		stderr:    f.stderr,
	}
	return f
}

func TestAC017_SignThenVerify_RoundTripSucceeds(t *testing.T) {
	f := newSignerFixture(t)

	require.Equal(t, exitOK, f.tool.run([]string{"sign", f.checksumsTxt, f.sigPath}), f.stderr.String())
	require.Equal(t, exitOK, f.tool.run([]string{"verify", f.checksumsTxt, f.sigPath}), f.stderr.String())

	raw, err := os.ReadFile(f.sigPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "\n", "the .sig must be a single line with no trailing newline")
	decoded, err := base64.StdEncoding.DecodeString(string(raw))
	require.NoError(t, err)
	assert.Len(t, decoded, ed25519.SignatureSize)
}

func TestAC017_Sign_NeverPrintsKeyMaterial(t *testing.T) {
	f := newSignerFixture(t)

	require.Equal(t, exitOK, f.tool.run([]string{"sign", f.checksumsTxt, f.sigPath}))

	assert.NotContains(t, f.stdout.String()+f.stderr.String(), f.privB64, "private key must never appear in tool output")
	assert.Empty(t, f.stdout.String(), "sign must print nothing derived from the key")
}

func TestAC017_Sign_RefusesBadPrivateKeySource(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"unset", ""},
		{"not base64", "%%%"},
		{"32 byte seed instead of 64 byte key", base64.StdEncoding.EncodeToString(make([]byte, 32))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSignerFixture(t)
			f.privB64 = tc.value

			code := f.tool.run([]string{"sign", f.checksumsTxt, f.sigPath})

			assert.Equal(t, exitFailure, code, "a bad key must be a non-zero exit")
			_, statErr := os.Stat(f.sigPath)
			assert.True(t, os.IsNotExist(statErr), "no .sig may be written when the key is bad")
			if tc.value != "" {
				assert.NotContains(t, f.stderr.String(), tc.value, "error text must not echo the key")
			}
		})
	}
}

func TestSEC003_Verify_FailsOnMismatch(t *testing.T) {
	otherPub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(t *testing.T, f *signerFixture)
	}{
		{"tampered signature", func(t *testing.T, f *signerFixture) {
			sig, err := os.ReadFile(f.sigPath)
			require.NoError(t, err)
			raw, err := base64.StdEncoding.DecodeString(string(sig))
			require.NoError(t, err)
			raw[0] ^= 0xff
			require.NoError(t, os.WriteFile(f.sigPath, []byte(base64.StdEncoding.EncodeToString(raw)), 0o644))
		}},
		{"tampered checksums", func(t *testing.T, f *signerFixture) {
			require.NoError(t, os.WriteFile(f.checksumsTxt, []byte("evil\n"), 0o644))
		}},
		{"committed key differs from the signing key", func(t *testing.T, f *signerFixture) {
			f.pub = otherPub
		}},
		{"missing sig file", func(t *testing.T, f *signerFixture) {
			require.NoError(t, os.Remove(f.sigPath))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSignerFixture(t)
			require.Equal(t, exitOK, f.tool.run([]string{"sign", f.checksumsTxt, f.sigPath}))
			tc.mutate(t, f)

			assert.Equal(t, exitFailure, f.tool.run([]string{"verify", f.checksumsTxt, f.sigPath}),
				"verify must exit non-zero so the release job fails")
		})
	}
}

func TestSEC003_Verify_FailsOnUnusableCommittedKey(t *testing.T) {
	f := newSignerFixture(t)
	require.Equal(t, exitOK, f.tool.run([]string{"sign", f.checksumsTxt, f.sigPath}))

	// Wire the real releasekey decoder with a placeholder, as shipped.
	original := releasekey.PublicKeyBase64
	t.Cleanup(func() { releasekey.PublicKeyBase64 = original })
	releasekey.PublicKeyBase64 = "REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY"
	f.tool.publicKey = releasekey.PublicKey

	assert.Equal(t, exitFailure, f.tool.run([]string{"verify", f.checksumsTxt, f.sigPath}),
		"a placeholder or non-32-byte committed key must fail the release gate")
}

func TestAC017_Genkey_EmitsLabelledUsableKeypair(t *testing.T) {
	f := newSignerFixture(t)

	require.Equal(t, exitOK, f.tool.run([]string{"genkey"}))

	lines := strings.Split(strings.TrimSpace(f.stdout.String()), "\n")
	require.Len(t, lines, 2)
	require.True(t, strings.HasPrefix(lines[0], "PUBLIC_KEY="))
	require.True(t, strings.HasPrefix(lines[1], "PRIVATE_KEY="))

	pub, err := releasekey.ParsePublicKey(strings.TrimPrefix(lines[0], "PUBLIC_KEY="))
	require.NoError(t, err, "genkey public half must pass the agent's own key check")
	f.pub = pub
	f.privB64 = strings.TrimPrefix(lines[1], "PRIVATE_KEY=")
	require.Equal(t, exitOK, f.tool.run([]string{"sign", f.checksumsTxt, f.sigPath}))
	assert.Equal(t, exitOK, f.tool.run([]string{"verify", f.checksumsTxt, f.sigPath}),
		"a freshly generated pair must sign and verify")
}

func TestAC017_Usage_RejectsUnknownAndMalformedInvocations(t *testing.T) {
	tests := [][]string{
		{},
		{"bogus"},
		{"sign", "only-one-arg"},
		{"verify"},
		{"genkey", "extra"},
	}
	for _, args := range tests {
		f := newSignerFixture(t)
		assert.Equal(t, exitUsage, f.tool.run(args), "args %v must be a usage error", args)
	}
}
