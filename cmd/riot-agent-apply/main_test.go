//go:build linux

package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/releasekey"
)

const (
	testAssetName   = "riot-agent-linux-amd64"
	oldTargetBytes  = "OLD-AGENT-BINARY"
	newBinaryBytes  = "NEW-SIGNED-AGENT-BINARY"
	evilBinaryBytes = "ATTACKER-CHOSEN-BYTES"
	secretBytes     = "ROOT-ONLY-SECRET"
)

// applyFixture is a verifier wired to temp paths and a test-generated keypair.
// The real private key is never involved.
type applyFixture struct {
	t        *testing.T
	dir      string
	in       *installer
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
	logger   *slog.Logger
	svc      *fakeService
	sleeps   []time.Duration
	staged   stagedInputs
	targetAt string
}

type fakeService struct {
	calls []string
}

func (s *fakeService) Stop() error  { s.calls = append(s.calls, "stop"); return nil }
func (s *fakeService) Start() error { s.calls = append(s.calls, "start"); return nil }

func newApplyFixture(t *testing.T) *applyFixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	stageDir := filepath.Join(dir, "stage")
	require.NoError(t, os.Mkdir(binDir, 0o755))
	require.NoError(t, os.Mkdir(stageDir, 0o755))

	fx := &applyFixture{
		t:      t,
		dir:    dir,
		pub:    pub,
		priv:   priv,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		svc:    &fakeService{},
		staged: stagedInputs{
			binary:    filepath.Join(stageDir, "riot-agent.update"),
			checksums: filepath.Join(stageDir, "riot-agent.update.checksums"),
			signature: filepath.Join(stageDir, "riot-agent.update.sig"),
		},
		targetAt: filepath.Join(binDir, "riot-agent"),
	}
	fx.in = &installer{
		staged:          fx.staged,
		targetBin:       fx.targetAt,
		targetAssetName: testAssetName,
		publicKey:       func() (ed25519.PublicKey, error) { return fx.pub, nil },
		maxBinarySize:   maxBinarySize,
		maxMetaSize:     maxMetaFileSize,
		logger:          fx.logger,
	}
	require.NoError(t, os.WriteFile(fx.targetAt, []byte(oldTargetBytes), 0o755))
	return fx
}

func sha256HexOf(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func manifestFor(binary string) string {
	return sha256HexOf("other") + "  install.sh\n" +
		sha256HexOf("apply") + "  riot-agent-apply-linux-amd64\n" +
		sha256HexOf(binary) + "  " + testAssetName + "\n"
}

// stageValid stages a binary plus a checksums.txt/.sig signed by the fixture key.
func (fx *applyFixture) stageValid(binary string) {
	fx.stageSigned(binary, manifestFor(binary), fx.priv)
}

func (fx *applyFixture) stageSigned(binary, manifest string, signer ed25519.PrivateKey) {
	fx.t.Helper()
	require.NoError(fx.t, os.WriteFile(fx.staged.binary, []byte(binary), 0o644))
	require.NoError(fx.t, os.WriteFile(fx.staged.checksums, []byte(manifest), 0o644))
	require.NoError(fx.t, os.WriteFile(fx.staged.signature, []byte(releasekey.SignChecksums(signer, []byte(manifest))), 0o644))
}

// assertRefused requires the install failed and the live binary is untouched
// with no temp debris beside it (AC-020).
func (fx *applyFixture) assertRefused(err error) {
	fx.t.Helper()
	require.Error(fx.t, err, "verification must fail closed")
	fx.assertTargetUntouched()
}

func (fx *applyFixture) assertTargetUntouched() {
	fx.t.Helper()
	got, readErr := os.ReadFile(fx.targetAt)
	require.NoError(fx.t, readErr)
	assert.Equal(fx.t, oldTargetBytes, string(got), "live binary must never be modified on a refusal")
	entries, dirErr := os.ReadDir(filepath.Dir(fx.targetAt))
	require.NoError(fx.t, dirErr)
	assert.Len(fx.t, entries, 1, "no temp file may be left beside the target")
}

func (fx *applyFixture) installedBytes() string {
	fx.t.Helper()
	got, err := os.ReadFile(fx.targetAt)
	require.NoError(fx.t, err)
	return string(got)
}

func (fx *applyFixture) fakeSleep(d time.Duration) { fx.sleeps = append(fx.sleeps, d) }

func TestAC005_VerifyAndInstall_ValidSignatureAndMatchingHashInstalls(t *testing.T) {
	fx := newApplyFixture(t)
	fx.stageValid(newBinaryBytes)

	require.NoError(t, fx.in.verifyAndInstall())

	assert.Equal(t, newBinaryBytes, fx.installedBytes(), "the verified bytes must be what is installed")
	info, err := os.Stat(fx.targetAt)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "installed binary must be mode 0755")
	entries, err := os.ReadDir(filepath.Dir(fx.targetAt))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the temp file must have been renamed away")
}

func TestAC009_ProductionInstaller_TargetIsHardcoded(t *testing.T) {
	assert.Equal(t, "/usr/local/bin/riot-agent", targetBin, "target must be the hardcoded system path")
	prod := newProductionInstaller(slog.New(slog.NewTextHandler(io.Discard, nil)))
	assert.Equal(t, targetBin, prod.targetBin)
	assert.Equal(t, stagingBinaryPath, prod.staged.binary)
	assert.Equal(t, stagingChecksumsPath, prod.staged.checksums)
	assert.Equal(t, stagingSigPath, prod.staged.signature)
}

func TestV005_StagingPathsMatchTheAgentAndUnitContract(t *testing.T) {
	assert.Equal(t, "/var/lib/riot/riot-agent.update", stagingBinaryPath)
	assert.Equal(t, "/var/lib/riot/riot-agent.update.checksums", stagingChecksumsPath)
	assert.Equal(t, "/var/lib/riot/riot-agent.update.sig", stagingSigPath)
	assert.Equal(t, "riot-agent", serviceName)
}

func TestAC001_VerifyAndInstall_TamperedBinaryIsRefused(t *testing.T) {
	fx := newApplyFixture(t)
	fx.stageValid(newBinaryBytes)
	require.NoError(t, os.WriteFile(fx.staged.binary, []byte(newBinaryBytes+"-tampered"), 0o644))

	err := fx.in.verifyAndInstall()

	fx.assertRefused(err)
	assert.ErrorContains(t, err, "checksum mismatch")
}

func TestSEC001_VerifyAndInstall_ArbitraryUnsignedBytesAreRefused(t *testing.T) {
	_, attackerPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	tests := []struct {
		name  string
		stage func(fx *applyFixture)
	}{
		{"attacker self-signed checksums for their own bytes", func(fx *applyFixture) {
			fx.stageSigned(evilBinaryBytes, manifestFor(evilBinaryBytes), attackerPriv)
		}},
		{"attacker binary with the genuine signed manifest", func(fx *applyFixture) {
			fx.stageValid(newBinaryBytes)
			require.NoError(t, os.WriteFile(fx.staged.binary, []byte(evilBinaryBytes), 0o644))
		}},
		{"attacker binary with no signature file", func(fx *applyFixture) {
			fx.stageValid(evilBinaryBytes)
			require.NoError(t, os.Remove(fx.staged.signature))
		}},
		{"attacker binary with no checksums file", func(fx *applyFixture) {
			fx.stageValid(evilBinaryBytes)
			require.NoError(t, os.Remove(fx.staged.checksums))
		}},
		{"only the binary staged", func(fx *applyFixture) {
			require.NoError(t, os.WriteFile(fx.staged.binary, []byte(evilBinaryBytes), 0o644))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newApplyFixture(t)
			tc.stage(fx)

			fx.assertRefused(fx.in.verifyAndInstall())
		})
	}
}

func TestAC006_VerifyAndInstall_SignatureProblemsAreRefused(t *testing.T) {
	_, otherPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	tests := []struct {
		name  string
		stage func(fx *applyFixture)
	}{
		{"checksums altered after signing", func(fx *applyFixture) {
			fx.stageValid(newBinaryBytes)
			altered := manifestFor(newBinaryBytes) + sha256HexOf("x") + "  extra\n"
			require.NoError(t, os.WriteFile(fx.staged.checksums, []byte(altered), 0o644))
		}},
		{"signature from a different key", func(fx *applyFixture) {
			fx.stageSigned(newBinaryBytes, manifestFor(newBinaryBytes), otherPriv)
		}},
		{"malformed base64 signature", func(fx *applyFixture) {
			fx.stageValid(newBinaryBytes)
			require.NoError(t, os.WriteFile(fx.staged.signature, []byte("%%%not-base64%%%"), 0o644))
		}},
		{"wrong-length signature", func(fx *applyFixture) {
			fx.stageValid(newBinaryBytes)
			short := base64.StdEncoding.EncodeToString(make([]byte, 32))
			require.NoError(t, os.WriteFile(fx.staged.signature, []byte(short), 0o644))
		}},
		{"empty signature file", func(fx *applyFixture) {
			fx.stageValid(newBinaryBytes)
			require.NoError(t, os.WriteFile(fx.staged.signature, nil, 0o644))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newApplyFixture(t)
			tc.stage(fx)

			fx.assertRefused(fx.in.verifyAndInstall())
		})
	}
}

func TestAC007_VerifyAndInstall_UnusableEmbeddedKeyFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		key  func() (ed25519.PublicKey, error)
	}{
		{"placeholder or garbage constant", func() (ed25519.PublicKey, error) {
			return releasekey.ParsePublicKey("REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY")
		}},
		{"empty constant", func() (ed25519.PublicKey, error) { return releasekey.ParsePublicKey("") }},
		{"key lookup error", func() (ed25519.PublicKey, error) { return nil, errors.New("boom") }},
		{"nil key with nil error", func() (ed25519.PublicKey, error) { return nil, nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newApplyFixture(t)
			fx.stageValid(newBinaryBytes)
			fx.in.publicKey = tc.key

			fx.assertRefused(fx.in.verifyAndInstall())
		})
	}
}

func TestSEC005_VerifyAndInstall_RequiresExactBasenameEntry(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		asset    string
	}{
		{
			name:     "only a superstring asset is listed",
			manifest: sha256HexOf(newBinaryBytes) + "  riot-agent-linux-amd64-fips\n",
			asset:    testAssetName,
		},
		{
			name:     "only the verifier asset contains the platform suffix",
			manifest: sha256HexOf(newBinaryBytes) + "  riot-agent-apply-linux-amd64\n",
			asset:    testAssetName,
		},
		{
			name:     "verifier built without a target asset name",
			manifest: manifestFor(newBinaryBytes),
			asset:    "",
		},
		{
			name:     "verifier built for another platform",
			manifest: manifestFor(newBinaryBytes),
			asset:    "riot-agent-linux-arm64",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newApplyFixture(t)
			fx.stageSigned(newBinaryBytes, tc.manifest, fx.priv)
			fx.in.targetAssetName = tc.asset

			fx.assertRefused(fx.in.verifyAndInstall())
		})
	}
}

// TestC1_VerifyAndInstall_PostVerificationStagingChangeCannotAlterInstalledBytes
// proves read-once semantics: the staging file is mutated AFTER verification
// and BEFORE the write, and the installed bytes are still the verified ones.
func TestC1_VerifyAndInstall_PostVerificationStagingChangeCannotAlterInstalledBytes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, fx *applyFixture)
	}{
		{"staging file overwritten in place", func(t *testing.T, fx *applyFixture) {
			require.NoError(t, os.WriteFile(fx.staged.binary, []byte(evilBinaryBytes), 0o644))
		}},
		{"staging file replaced by a symlink to a secret", func(t *testing.T, fx *applyFixture) {
			secret := filepath.Join(fx.dir, "secret")
			require.NoError(t, os.WriteFile(secret, []byte(secretBytes), 0o600))
			require.NoError(t, os.Remove(fx.staged.binary))
			require.NoError(t, os.Symlink(secret, fx.staged.binary))
		}},
		{"staging file deleted", func(t *testing.T, fx *applyFixture) {
			require.NoError(t, os.Remove(fx.staged.binary))
		}},
		{"checksums replaced with attacker content", func(t *testing.T, fx *applyFixture) {
			require.NoError(t, os.WriteFile(fx.staged.checksums, []byte(evilBinaryBytes), 0o644))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newApplyFixture(t)
			fx.stageValid(newBinaryBytes)
			mutated := false
			fx.in.betweenVerifyAndWrite = func() {
				mutated = true
				tc.mutate(t, fx)
			}

			require.NoError(t, fx.in.verifyAndInstall())

			require.True(t, mutated, "the seam must have run between verification and the write")
			assert.Equal(t, newBinaryBytes, fx.installedBytes(),
				"installed bytes must be exactly the verified in-memory bytes, never a re-read of staging")
		})
	}
}

// TestC2_VerifyAndInstall_NonRegularOrSymlinkedInputsAreRefused covers the
// SEC-001 symlink arbitrary-read: a planted link must never be followed.
func TestC2_VerifyAndInstall_NonRegularOrSymlinkedInputsAreRefused(t *testing.T) {
	inputs := []struct {
		name string
		path func(fx *applyFixture) string
	}{
		{"binary", func(fx *applyFixture) string { return fx.staged.binary }},
		{"checksums", func(fx *applyFixture) string { return fx.staged.checksums }},
		{"signature", func(fx *applyFixture) string { return fx.staged.signature }},
	}
	replacements := []struct {
		name    string
		replace func(t *testing.T, fx *applyFixture, path string)
	}{
		{"symlink to a root-only secret", func(t *testing.T, fx *applyFixture, path string) {
			secret := filepath.Join(fx.dir, "secret")
			require.NoError(t, os.WriteFile(secret, []byte(secretBytes), 0o600))
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Symlink(secret, path))
		}},
		{"symlink to the genuine file elsewhere", func(t *testing.T, fx *applyFixture, path string) {
			real := filepath.Join(fx.dir, "real-copy")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(real, data, 0o644))
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Symlink(real, path))
		}},
		{"directory", func(t *testing.T, fx *applyFixture, path string) {
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Mkdir(path, 0o755))
		}},
		{"dangling symlink", func(t *testing.T, fx *applyFixture, path string) {
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Symlink(filepath.Join(fx.dir, "nowhere"), path))
		}},
	}
	for _, input := range inputs {
		for _, repl := range replacements {
			t.Run(input.name+"/"+repl.name, func(t *testing.T) {
				fx := newApplyFixture(t)
				fx.stageValid(newBinaryBytes)
				repl.replace(t, fx, input.path(fx))

				err := fx.in.verifyAndInstall()

				fx.assertRefused(err)
				assert.NotContains(t, fx.installedBytes(), secretBytes, "a symlinked source must never be read into the target")
			})
		}
	}
}

func TestC2_VerifyAndInstall_FIFOInputIsRefusedWithoutBlocking(t *testing.T) {
	fx := newApplyFixture(t)
	fx.stageValid(newBinaryBytes)
	require.NoError(t, os.Remove(fx.staged.binary))
	require.NoError(t, syscall.Mkfifo(fx.staged.binary, 0o644))

	done := make(chan error, 1)
	go func() { done <- fx.in.verifyAndInstall() }()

	select {
	case err := <-done:
		fx.assertRefused(err)
	case <-time.After(5 * time.Second):
		t.Fatal("verifier blocked on a FIFO staging file; it must open non-blocking and refuse non-regular files")
	}
}

func TestC3_VerifyAndInstall_OversizedInputsAreRefused(t *testing.T) {
	const smallLimit int64 = 1024
	tests := []struct {
		name  string
		stage func(fx *applyFixture)
	}{
		{"oversized binary", func(fx *applyFixture) {
			big := strings.Repeat("A", int(smallLimit)+1)
			fx.stageSigned(big, manifestFor(big), fx.priv)
			fx.in.maxBinarySize = smallLimit
		}},
		{"oversized checksums.txt", func(fx *applyFixture) {
			fx.stageValid(newBinaryBytes)
			fx.in.maxMetaSize = 16
		}},
		{"oversized checksums.txt.sig", func(fx *applyFixture) {
			fx.stageValid(newBinaryBytes)
			require.NoError(t, os.WriteFile(fx.staged.signature, []byte(strings.Repeat("A", int(smallLimit)+1)), 0o644))
			fx.in.maxMetaSize = smallLimit
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newApplyFixture(t)
			tc.stage(fx)

			err := fx.in.verifyAndInstall()

			fx.assertRefused(err)
			assert.ErrorContains(t, err, "exceeds", "the refusal must name the size limit")
		})
	}
}

func TestC3_VerifyAndInstall_BinaryAtExactLimitIsAccepted(t *testing.T) {
	fx := newApplyFixture(t)
	binary := strings.Repeat("B", 2048)
	fx.stageSigned(binary, manifestFor(binary), fx.priv)
	fx.in.maxBinarySize = int64(len(binary))

	require.NoError(t, fx.in.verifyAndInstall())

	assert.Equal(t, binary, fx.installedBytes(), "a binary exactly at the limit is within bounds")
}

func TestC3_Limits_AreBoundedAndLeaveRoomForARealAgent(t *testing.T) {
	assert.Equal(t, int64(1<<20), maxMetaFileSize, "metadata cap must be 1 MiB (SEC-002)")
	assert.GreaterOrEqual(t, maxBinarySize, int64(64<<20), "cap must comfortably fit a real agent binary")
	assert.LessOrEqual(t, maxBinarySize, int64(256<<20), "cap must stay bounded for small devices")
}

func TestAC011_RunBoot_NoStagedUpdateIsANoOp(t *testing.T) {
	fx := newApplyFixture(t)

	code := runBoot(fx.in)

	assert.Equal(t, exitOK, code)
	fx.assertTargetUntouched()
}

func TestAC011_RunBoot_AppliesAValidStagedUpdateWithoutServiceControl(t *testing.T) {
	fx := newApplyFixture(t)
	fx.stageValid(newBinaryBytes)

	code := runBoot(fx.in)

	assert.Equal(t, exitOK, code)
	assert.Equal(t, newBinaryBytes, fx.installedBytes())
	assert.Empty(t, fx.svc.calls, "boot mode must not stop or start the service")
	_, err := os.Stat(fx.staged.binary)
	assert.True(t, os.IsNotExist(err), "staging must be consumed after a successful boot apply")
}

func TestNFR005_RunBoot_BadStagedUpdateNeverBlocksStartup(t *testing.T) {
	fx := newApplyFixture(t)
	fx.stageSigned(evilBinaryBytes, manifestFor(evilBinaryBytes), func() ed25519.PrivateKey {
		_, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		return priv
	}())

	code := runBoot(fx.in)

	assert.Equal(t, exitOK, code, "--boot must exit 0 so the current agent still starts")
	fx.assertTargetUntouched()
	for _, path := range []string{fx.staged.binary, fx.staged.checksums, fx.staged.signature} {
		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err), "rejected staging file %s must be removed", path)
	}
}

func TestAC005_RunApply_StopsInstallsAndStartsService(t *testing.T) {
	fx := newApplyFixture(t)
	fx.stageValid(newBinaryBytes)

	code := runApply(fx.in, fx.svc, fx.fakeSleep)

	assert.Equal(t, exitOK, code)
	assert.Equal(t, []string{"stop", "start"}, fx.svc.calls, "service must be stopped then started")
	assert.Equal(t, []time.Duration{applyStopDelay}, fx.sleeps, "apply must wait before stopping the service")
	assert.Equal(t, newBinaryBytes, fx.installedBytes())
}

func TestSEC001_RunApply_RefusedUpdateStillRestoresServiceAndCleansStaging(t *testing.T) {
	fx := newApplyFixture(t)
	fx.stageValid(newBinaryBytes)
	require.NoError(t, os.WriteFile(fx.staged.binary, []byte(evilBinaryBytes), 0o644))

	code := runApply(fx.in, fx.svc, fx.fakeSleep)

	assert.Equal(t, exitFailure, code, "a refusal must be a non-zero exit")
	assert.Equal(t, []string{"stop", "start"}, fx.svc.calls, "the service must be restarted even when the update is refused")
	fx.assertTargetUntouched()
	for _, path := range []string{fx.staged.binary, fx.staged.checksums, fx.staged.signature} {
		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err), "rejected staging file %s must be removed", path)
	}
}

func TestSEC001_RunMode_ArgumentsCannotRedirectTargetOrKey(t *testing.T) {
	tests := [][]string{
		{"/tmp/evil"},
		{"--target=/etc/passwd"},
		{"--boot", "extra"},
		{"--public-key", "AAAA"},
		{"-boot"},
	}
	for _, args := range tests {
		fx := newApplyFixture(t)
		fx.stageValid(newBinaryBytes)

		code := runMode(args, fx.in, fx.svc, fx.fakeSleep)

		assert.Equal(t, exitUsage, code, "args %v must be rejected", args)
		assert.Empty(t, fx.svc.calls, "a rejected invocation must not touch the service (args %v)", args)
		fx.assertTargetUntouched()
	}
}

func TestSEC001_RunMode_NoArgsAppliesAndBootFlagBoots(t *testing.T) {
	apply := newApplyFixture(t)
	apply.stageValid(newBinaryBytes)
	assert.Equal(t, exitOK, runMode(nil, apply.in, apply.svc, apply.fakeSleep))
	assert.Equal(t, []string{"stop", "start"}, apply.svc.calls)

	boot := newApplyFixture(t)
	boot.stageValid(newBinaryBytes)
	assert.Equal(t, exitOK, runMode([]string{bootFlag}, boot.in, boot.svc, boot.fakeSleep))
	assert.Empty(t, boot.svc.calls)
	assert.Equal(t, newBinaryBytes, boot.installedBytes())
}
