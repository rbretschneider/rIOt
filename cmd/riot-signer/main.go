// Command riot-signer is the build-time release signing tool (AGENT-HARDEN-001
// AD-009). It is used by the release workflow and the maintainer; it is not
// shipped to devices.
//
//	riot-signer genkey
//	riot-signer sign   <checksums.txt> <checksums.txt.sig>   (key from $AGENT_SIGNING_PRIVATE_KEY)
//	riot-signer verify <checksums.txt> <checksums.txt.sig>   (key from internal/releasekey)
//
// Nothing derived from the private key is ever printed.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/releasekey"
)

const (
	// privateKeyEnvVar names the environment variable that carries the
	// base64 ed25519 private key. It is the only way to supply the key.
	privateKeyEnvVar = "AGENT_SIGNING_PRIVATE_KEY"

	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2

	signatureFileMode = 0o644
)

const usageText = `usage:
  riot-signer genkey
  riot-signer sign   <checksums.txt> <checksums.txt.sig>
  riot-signer verify <checksums.txt> <checksums.txt.sig>
`

var errUsage = errors.New("usage")

// tool carries the tool's dependencies so tests can inject a key source, an
// environment, and output streams.
type tool struct {
	getenv    func(string) string
	publicKey func() (ed25519.PublicKey, error)
	stdout    io.Writer
	stderr    io.Writer
}

func main() {
	t := &tool{
		getenv:    os.Getenv,
		publicKey: releasekey.PublicKey,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
	}
	os.Exit(t.run(os.Args[1:]))
}

// run dispatches a subcommand and returns the process exit code.
func (t *tool) run(args []string) int {
	err := t.dispatch(args)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage):
		fmt.Fprint(t.stderr, usageText)
		return exitUsage
	default:
		fmt.Fprintf(t.stderr, "riot-signer: %v\n", err)
		return exitFailure
	}
}

func (t *tool) dispatch(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "genkey":
		return t.genkey(args[1:])
	case "sign":
		return t.sign(args[1:])
	case "verify":
		return t.verify(args[1:])
	default:
		return errUsage
	}
}

func (t *tool) genkey(args []string) error {
	if len(args) != 0 {
		return errUsage
	}
	publicB64, privateB64, err := releasekey.GenerateKeyPair()
	if err != nil {
		return err
	}
	fmt.Fprintf(t.stdout, "PUBLIC_KEY=%s\n", publicB64)
	fmt.Fprintf(t.stdout, "PRIVATE_KEY=%s\n", privateB64)
	return nil
}

func (t *tool) sign(args []string) error {
	if len(args) != 2 {
		return errUsage
	}
	priv, err := t.privateKeyFromEnv()
	if err != nil {
		return err
	}
	checksums, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}
	sig := releasekey.SignChecksums(priv, checksums)
	if err := os.WriteFile(args[1], []byte(sig), signatureFileMode); err != nil {
		return fmt.Errorf("write %s: %w", args[1], err)
	}
	return nil
}

func (t *tool) verify(args []string) error {
	if len(args) != 2 {
		return errUsage
	}
	pub, err := t.publicKey()
	if err != nil {
		return fmt.Errorf("committed release public key is unusable: %w", err)
	}
	checksums, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}
	sig, err := os.ReadFile(args[1])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[1], err)
	}
	if err := releasekey.VerifySignature(pub, checksums, sig); err != nil {
		return fmt.Errorf("%s does not verify against the committed public key: %w", args[1], err)
	}
	fmt.Fprintf(t.stdout, "OK: %s verifies against the committed release public key\n", args[1])
	return nil
}

// privateKeyFromEnv decodes the signing key. Error text deliberately omits
// the underlying decode error and any key material.
func (t *tool) privateKeyFromEnv() (ed25519.PrivateKey, error) {
	encoded := strings.TrimSpace(t.getenv(privateKeyEnvVar))
	if encoded == "" {
		return nil, fmt.Errorf("%s is not set", privateKeyEnvVar)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s is not a base64 %d-byte ed25519 private key", privateKeyEnvVar, ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}
