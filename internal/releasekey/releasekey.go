// Package releasekey is the single compile-time trust anchor for rIOt agent
// updates, plus the one implementation of the signed-checksums encoding
// contract (AD-001). The agent pre-flight, the privileged verifier
// (cmd/riot-agent-apply), and the CI signer (cmd/riot-signer) all import it so
// the producer and every consumer can never drift apart.
//
// It is a leaf package: it imports only the standard library.
package releasekey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// PublicKeyBase64 is the base64 (std encoding) of the 32-byte ed25519 public
// key that authenticates release checksums. It is non-secret. It is a var, not
// a const, so a rotation build can override it:
//
//	-ldflags "-X github.com/DesyncTheThird/rIOt/internal/releasekey.PublicKeyBase64=<base64pub>"
//
// It is never read from the server, the config file, the environment, or any
// path the riot user can write. The placeholder below is deliberately not a
// valid key: until the maintainer runs `riot-signer genkey` and commits the
// real public key, every verification refuses (fail closed) and the release
// workflow's `riot-signer verify` gate fails the build.
var PublicKeyBase64 = "REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY"

const (
	// checksumFieldCount is the number of whitespace-separated fields on a
	// checksums.txt line: "<sha256-hex>  <filename>".
	checksumFieldCount = 2
	// sha256HexLength is the length of a hex-encoded SHA-256 digest.
	sha256HexLength = 64
)

var (
	// ErrBadPublicKey means the embedded public key is empty, not base64, or
	// not exactly ed25519.PublicKeySize bytes.
	ErrBadPublicKey = errors.New("release public key is not a valid base64 32-byte ed25519 key")
	// ErrMalformedSignature means the .sig is not base64 of exactly
	// ed25519.SignatureSize bytes.
	ErrMalformedSignature = errors.New("release signature is malformed")
	// ErrSignatureInvalid means the signature is well formed but does not
	// verify over the received checksums bytes under the embedded key.
	ErrSignatureInvalid = errors.New("release signature does not verify against the embedded public key")
	// ErrNoChecksumEntry means checksums.txt has no line for the asset.
	ErrNoChecksumEntry = errors.New("checksums.txt has no entry for this platform")
	// ErrAmbiguousChecksumEntry means checksums.txt lists the asset more than once.
	ErrAmbiguousChecksumEntry = errors.New("checksums.txt has more than one entry for this platform")
	// ErrMalformedChecksum means the matching line does not carry a SHA-256 hex digest.
	ErrMalformedChecksum = errors.New("checksums.txt entry is not a valid sha256 digest")
)

// PublicKey decodes the compiled-in PublicKeyBase64. A bad, empty, or
// placeholder value returns an error so callers fail CLOSED.
func PublicKey() (ed25519.PublicKey, error) {
	return ParsePublicKey(PublicKeyBase64)
}

// ParsePublicKey decodes a base64 (std encoding) ed25519 public key and
// requires exactly ed25519.PublicKeySize bytes.
func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPublicKey, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrBadPublicKey, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// GenerateKeyPair returns a fresh ed25519 keypair, both halves base64 (std).
func GenerateKeyPair() (publicB64, privateB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate ed25519 key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}

// SignChecksums signs the exact bytes of checksums.txt and returns the
// detached signature in the on-disk format: base64 (std) of the raw 64-byte
// signature, single line, no trailing newline (AD-001).
func SignChecksums(priv ed25519.PrivateKey, checksums []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, checksums))
}

// VerifySignature checks the detached checksums.txt.sig contents (base64, any
// surrounding whitespace tolerated) over the exact received checksums bytes.
// It never inspects the key's origin: the caller supplies the trust anchor.
func VerifySignature(pub ed25519.PublicKey, checksums, sigB64 []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrBadPublicKey
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedSignature, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: got %d bytes", ErrMalformedSignature, len(sig))
	}
	if !ed25519.Verify(pub, checksums, sig) {
		return ErrSignatureInvalid
	}
	return nil
}

// ExpectedChecksum returns the SHA-256 hex digest for assetName from verified
// checksums.txt bytes. The filename field must EQUAL assetName; a substring
// match is never accepted (SEC-005). Zero matches, more than one match, or a
// non-SHA-256 digest are all errors.
func ExpectedChecksum(checksums []byte, assetName string) (string, error) {
	var found string
	matches := 0
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != checksumFieldCount || fields[1] != assetName {
			continue
		}
		matches++
		found = fields[0]
	}
	switch {
	case matches == 0:
		return "", fmt.Errorf("%w: %s", ErrNoChecksumEntry, assetName)
	case matches > 1:
		return "", fmt.Errorf("%w: %s", ErrAmbiguousChecksumEntry, assetName)
	}
	if _, err := hex.DecodeString(found); err != nil || len(found) != sha256HexLength {
		return "", fmt.Errorf("%w: %s", ErrMalformedChecksum, assetName)
	}
	return found, nil
}
