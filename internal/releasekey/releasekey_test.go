package releasekey

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return pub, priv
}

func sha256Hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func TestAC007_PublicKey_FailsClosedOnBadEmbeddedKey(t *testing.T) {
	validPub, _ := testKeyPair(t)
	tests := []struct {
		name    string
		encoded string
		wantErr bool
	}{
		{"empty constant", "", true},
		{"shipped placeholder", "REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY", true},
		{"not base64", "!!!not-base64!!!", true},
		{"31 bytes", base64.StdEncoding.EncodeToString(make([]byte, 31)), true},
		{"33 bytes", base64.StdEncoding.EncodeToString(make([]byte, 33)), true},
		{"64 byte private key pasted by mistake", base64.StdEncoding.EncodeToString(make([]byte, 64)), true},
		{"valid 32 byte key", base64.StdEncoding.EncodeToString(validPub), false},
		{"valid key with trailing newline", base64.StdEncoding.EncodeToString(validPub) + "\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePublicKey(tc.encoded)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrBadPublicKey, "bad embedded key must fail closed")
				assert.Nil(t, got, "no key may be returned alongside an error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, validPub, got, "decoded key must equal the source key")
		})
	}
}

func TestAC007_PublicKey_ReadsOnlyTheCompiledInVar(t *testing.T) {
	original := PublicKeyBase64
	t.Cleanup(func() { PublicKeyBase64 = original })

	pub, _ := testKeyPair(t)
	PublicKeyBase64 = base64.StdEncoding.EncodeToString(pub)
	got, err := PublicKey()
	require.NoError(t, err)
	assert.Equal(t, pub, got, "PublicKey must decode the -X-overridable var")

	PublicKeyBase64 = "garbage"
	_, err = PublicKey()
	assert.ErrorIs(t, err, ErrBadPublicKey, "a garbage override must fail closed")
}

func TestAC006_VerifySignature_FailClosedTable(t *testing.T) {
	pub, priv := testKeyPair(t)
	otherPub, _ := testKeyPair(t)
	checksums := []byte(sha256Hex("bin") + "  riot-agent-linux-amd64\n")
	goodSig := SignChecksums(priv, checksums)

	tests := []struct {
		name      string
		pub       ed25519.PublicKey
		checksums []byte
		sig       []byte
		wantErr   error
	}{
		{"valid signature", pub, checksums, []byte(goodSig), nil},
		{"valid signature with trailing newline", pub, checksums, []byte(goodSig + "\n"), nil},
		{"checksums altered after signing", pub, append([]byte("x"), checksums...), []byte(goodSig), ErrSignatureInvalid},
		{"signed by a different key", otherPub, checksums, []byte(goodSig), ErrSignatureInvalid},
		{"malformed base64 signature", pub, checksums, []byte("%%%not base64%%%"), ErrMalformedSignature},
		{"empty signature", pub, checksums, []byte(""), ErrMalformedSignature},
		{"wrong-length signature (63 bytes)", pub, checksums, []byte(base64.StdEncoding.EncodeToString(make([]byte, 63))), ErrMalformedSignature},
		{"wrong-length signature (65 bytes)", pub, checksums, []byte(base64.StdEncoding.EncodeToString(make([]byte, 65))), ErrMalformedSignature},
		{"all-zero 64 byte signature", pub, checksums, []byte(base64.StdEncoding.EncodeToString(make([]byte, 64))), ErrSignatureInvalid},
		{"nil public key", nil, checksums, []byte(goodSig), ErrBadPublicKey},
		{"short public key", ed25519.PublicKey(make([]byte, 31)), checksums, []byte(goodSig), ErrBadPublicKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifySignature(tc.pub, tc.checksums, tc.sig)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestAC017_SignChecksums_ProducesSingleLineBase64Of64Bytes(t *testing.T) {
	_, priv := testKeyPair(t)
	sig := SignChecksums(priv, []byte("abc"))

	assert.NotContains(t, sig, "\n", "signature must be a single line with no trailing newline")
	raw, err := base64.StdEncoding.DecodeString(sig)
	require.NoError(t, err, "signature must be std base64")
	assert.Len(t, raw, ed25519.SignatureSize)
}

func TestAC017_GenerateKeyPair_RoundTripsThroughSignAndVerify(t *testing.T) {
	pubB64, privB64, err := GenerateKeyPair()
	require.NoError(t, err)

	pub, err := ParsePublicKey(pubB64)
	require.NoError(t, err)
	privRaw, err := base64.StdEncoding.DecodeString(privB64)
	require.NoError(t, err)
	require.Len(t, privRaw, ed25519.PrivateKeySize)

	data := []byte("checksums")
	sig := SignChecksums(ed25519.PrivateKey(privRaw), data)
	require.NoError(t, VerifySignature(pub, data, []byte(sig)))
}

func TestSEC005_ExpectedChecksum_ExactBasenameOnly(t *testing.T) {
	amd64Hash := sha256Hex("amd64")
	armHash := sha256Hex("arm")
	applyHash := sha256Hex("apply")
	manifest := strings.Join([]string{
		sha256Hex("install") + "  install.sh",
		applyHash + "  riot-agent-apply-linux-amd64",
		amd64Hash + "  riot-agent-linux-amd64",
		armHash + "  riot-agent-linux-armv7",
		"",
	}, "\n")

	tests := []struct {
		name     string
		manifest string
		asset    string
		want     string
		wantErr  error
	}{
		{"selects the exact asset, not the apply binary that contains it", manifest, "riot-agent-linux-amd64", amd64Hash, nil},
		{"selects the verifier asset by its own exact name", manifest, "riot-agent-apply-linux-amd64", applyHash, nil},
		{"selects armv7", manifest, "riot-agent-linux-armv7", armHash, nil},
		{"bare suffix is not an asset name (substring rejected)", manifest, "linux-amd64", "", ErrNoChecksumEntry},
		{"platform absent", manifest, "riot-agent-linux-arm64", "", ErrNoChecksumEntry},
		{"only a substring-matching line exists", amd64Hash + "  riot-agent-linux-amd64-fips\n", "riot-agent-linux-amd64", "", ErrNoChecksumEntry},
		{"empty manifest", "", "riot-agent-linux-amd64", "", ErrNoChecksumEntry},
		{"duplicate entry is ambiguous", amd64Hash + "  riot-agent-linux-amd64\n" + armHash + "  riot-agent-linux-amd64\n", "riot-agent-linux-amd64", "", ErrAmbiguousChecksumEntry},
		{"digest too short", "abc123  riot-agent-linux-amd64\n", "riot-agent-linux-amd64", "", ErrMalformedChecksum},
		{"digest not hex", strings.Repeat("z", 64) + "  riot-agent-linux-amd64\n", "riot-agent-linux-amd64", "", ErrMalformedChecksum},
		{"line with extra fields is skipped", amd64Hash + "  riot-agent-linux-amd64 extra\n", "riot-agent-linux-amd64", "", ErrNoChecksumEntry},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpectedChecksum([]byte(tc.manifest), tc.asset)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, got, "no digest may be returned alongside an error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
