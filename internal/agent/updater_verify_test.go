package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/releasekey"
)

const (
	testBinaryBytes   = "NEW-SIGNED-AGENT-BINARY"
	testDeviceID      = "device-1"
	testCurrentVer    = "1.0.0"
	testLatestVer     = "1.1.0"
	testSuffix        = "linux-amd64"
	verifierPathInADD = "/usr/local/lib/riot/riot-agent-apply"
)

func sha256HexString(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func newTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return pub, priv
}

// recordingRunner is a fake sudo/command runner that records every call.
type recordingRunner struct {
	mu       sync.Mutex
	calls    [][]string
	failWhen func(call []string) error
}

func (r *recordingRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	if r.failWhen != nil {
		if err := r.failWhen(call); err != nil {
			return []byte("scripted failure"), err
		}
	}
	return nil, nil
}

func (r *recordingRunner) recorded() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

// releaseFixture is a fake release host (binary, checksums.txt, checksums.txt.sig)
// plus a pipeline wired to temp staging paths and a test-generated key. The
// real signing key is never involved. Fields are tweaked by mods BEFORE the
// server starts, so handlers read immutable state.
type releaseFixture struct {
	t        *testing.T
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
	suffix   string
	binary   string
	manifest string
	sigBody  string

	binaryStatus    int
	checksumsStatus int
	sigStatus       int

	srv      *httptest.Server
	pipeline *updatePipeline
	runner   *recordingRunner
	paths    stagingPaths
}

type fixtureMod func(fx *releaseFixture)

func newReleaseFixture(t *testing.T, mods ...fixtureMod) *releaseFixture {
	t.Helper()
	pub, priv := newTestKeyPair(t)
	dir := t.TempDir()
	fx := &releaseFixture{
		t:      t,
		pub:    pub,
		priv:   priv,
		suffix: testSuffix,
		binary: testBinaryBytes,
		paths: stagingPaths{
			binary:    filepath.Join(dir, "riot-agent.update"),
			checksums: filepath.Join(dir, "riot-agent.update.checksums"),
			signature: filepath.Join(dir, "riot-agent.update.sig"),
		},
		runner: &recordingRunner{},
	}
	fx.setManifest(fx.binary)
	fx.pipeline = &updatePipeline{
		paths:     fx.paths,
		publicKey: func() (ed25519.PublicKey, error) { return fx.pub, nil },
		runner:    fx.runner.run,
	}
	for _, mod := range mods {
		mod(fx)
	}
	fx.srv = httptest.NewServer(http.HandlerFunc(fx.serve))
	t.Cleanup(fx.srv.Close)
	return fx
}

// setManifest rebuilds a correctly signed manifest listing binary under this
// platform's exact asset name, alongside decoy lines.
func (fx *releaseFixture) setManifest(binary string) {
	fx.manifest = sha256HexString("installer") + "  install.sh\n" +
		sha256HexString("verifier") + "  riot-agent-apply-" + fx.suffix + "\n" +
		sha256HexString(binary) + "  " + releaseAssetName(fx.suffix) + "\n"
	fx.sigBody = releasekey.SignChecksums(fx.priv, []byte(fx.manifest))
}

func (fx *releaseFixture) serve(w http.ResponseWriter, r *http.Request) {
	body, status := "", http.StatusNotFound
	switch r.URL.Path {
	case "/bin":
		body, status = fx.binary, statusOrOK(fx.binaryStatus)
	case "/checksums.txt":
		body, status = fx.manifest, statusOrOK(fx.checksumsStatus)
	case "/checksums.txt.sig":
		body, status = fx.sigBody, statusOrOK(fx.sigStatus)
	}
	if status != http.StatusOK {
		http.Error(w, "scripted", status)
		return
	}
	_, _ = w.Write([]byte(body))
}

func statusOrOK(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}

func (fx *releaseFixture) binaryURL() string    { return fx.srv.URL + "/bin" }
func (fx *releaseFixture) checksumsURL() string { return fx.srv.URL + "/checksums.txt" }
func (fx *releaseFixture) sigURL() string       { return fx.srv.URL + "/checksums.txt.sig" }

func (fx *releaseFixture) update() error {
	return fx.pipeline.run(context.Background(), fx.binaryURL(), fx.checksumsURL(), fx.sigURL(), fx.suffix)
}

func (fx *releaseFixture) stagedFiles() []string {
	return []string{fx.paths.binary, fx.paths.checksums, fx.paths.signature}
}

func (fx *releaseFixture) assertNoStagingLeft() {
	fx.t.Helper()
	for _, path := range fx.stagedFiles() {
		_, err := os.Stat(path)
		assert.True(fx.t, os.IsNotExist(err), "staging file %s must be removed after a refusal", path)
	}
}

// assertRefusedWithoutPrivilege checks FR-010 / AC-001: refusal, staging
// removed, and not a single sudo or helper invocation.
func (fx *releaseFixture) assertRefusedWithoutPrivilege(err error, wantInError string) {
	fx.t.Helper()
	require.Error(fx.t, err, "update must be refused (fail closed)")
	assert.ErrorContains(fx.t, err, wantInError, "refusal must name its failure class")
	assert.Empty(fx.t, fx.runner.recorded(), "no sudo command or helper may run for a refused update")
	fx.assertNoStagingLeft()
}

func TestAC001_AC006_PerformUpdate_RefusalsFailClosed(t *testing.T) {
	_, attackerPriv := newTestKeyPair(t)
	tests := []struct {
		name        string
		mod         fixtureMod
		wantInError string
	}{
		{"AC-001 tampered binary (hash mismatch)", func(fx *releaseFixture) {
			fx.binary = testBinaryBytes + "-tampered"
		}, "checksum mismatch"},
		{"AC-004 checksums.txt returns 404", func(fx *releaseFixture) {
			fx.checksumsStatus = http.StatusNotFound
		}, "checksum fetch failed"},
		{"AC-004 checksums.txt returns 500", func(fx *releaseFixture) {
			fx.checksumsStatus = http.StatusInternalServerError
		}, "checksum fetch failed"},
		{"AC-003 signature returns 404", func(fx *releaseFixture) {
			fx.sigStatus = http.StatusNotFound
		}, "signature fetch failed"},
		{"AC-003 signature returns 403", func(fx *releaseFixture) {
			fx.sigStatus = http.StatusForbidden
		}, "signature fetch failed"},
		{"AC-006 malformed base64 signature", func(fx *releaseFixture) {
			fx.sigBody = "%%%not base64%%%"
		}, "signature invalid"},
		{"AC-006 empty signature body", func(fx *releaseFixture) {
			fx.sigBody = ""
		}, "signature invalid"},
		{"AC-006 wrong-length signature", func(fx *releaseFixture) {
			fx.sigBody = base64.StdEncoding.EncodeToString(make([]byte, 63))
		}, "signature invalid"},
		{"AC-006 ed25519 verify false: zeroed 64-byte signature", func(fx *releaseFixture) {
			fx.sigBody = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		}, "signature invalid"},
		{"AC-006 checksums altered after signing", func(fx *releaseFixture) {
			fx.manifest += sha256HexString("late") + "  extra-line\n"
		}, "signature invalid"},
		{"AC-006 signed by a key other than the embedded one", func(fx *releaseFixture) {
			fx.priv = attackerPriv
			fx.setManifest(fx.binary)
		}, "signature invalid"},
		{"V-002 platform entry absent from the signed manifest", func(fx *releaseFixture) {
			fx.manifest = sha256HexString(testBinaryBytes) + "  riot-agent-linux-arm64\n"
			fx.sigBody = releasekey.SignChecksums(fx.priv, []byte(fx.manifest))
		}, "checksum unavailable"},
		{"SEC-005 only a substring-matching entry is signed", func(fx *releaseFixture) {
			fx.manifest = sha256HexString(testBinaryBytes) + "  riot-agent-apply-" + fx.suffix + "\n"
			fx.sigBody = releasekey.SignChecksums(fx.priv, []byte(fx.manifest))
		}, "checksum unavailable"},
		{"AC-007 empty embedded key", func(fx *releaseFixture) {
			fx.pipeline.publicKey = func() (ed25519.PublicKey, error) { return releasekey.ParsePublicKey("") }
		}, "signature invalid"},
		{"AC-007 garbage embedded key", func(fx *releaseFixture) {
			fx.pipeline.publicKey = func() (ed25519.PublicKey, error) { return releasekey.ParsePublicKey("garbage!!") }
		}, "signature invalid"},
		{"AC-007 shipped placeholder embedded key", func(fx *releaseFixture) {
			fx.pipeline.publicKey = func() (ed25519.PublicKey, error) {
				return releasekey.ParsePublicKey("REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY")
			}
		}, "signature invalid"},
		{"SEC-002 oversized checksums.txt body", func(fx *releaseFixture) {
			fx.manifest = strings.Repeat("A", int(maxMetaFileSize)+1)
		}, "exceeds"},
		{"SEC-002 oversized signature body", func(fx *releaseFixture) {
			fx.sigBody = strings.Repeat("A", int(maxMetaFileSize)+1)
		}, "exceeds"},
		{"binary download returns 404", func(fx *releaseFixture) {
			fx.binaryStatus = http.StatusNotFound
		}, "download failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newReleaseFixture(t, tc.mod)

			fx.assertRefusedWithoutPrivilege(fx.update(), tc.wantInError)
		})
	}
}

func TestAC002_AC004_PerformUpdate_MissingLocatorsAreRefused(t *testing.T) {
	tests := []struct {
		name         string
		checksumsURL func(fx *releaseFixture) string
		sigURL       func(fx *releaseFixture) string
		wantInError  string
	}{
		{"AC-002 no signature_url (unsigned release)",
			func(fx *releaseFixture) string { return fx.checksumsURL() },
			func(fx *releaseFixture) string { return "" },
			"unsigned"},
		{"AC-004 no checksum_url (legacy skip-verification path is gone)",
			func(fx *releaseFixture) string { return "" },
			func(fx *releaseFixture) string { return fx.sigURL() },
			"no checksums.txt"},
		{"neither locator advertised",
			func(fx *releaseFixture) string { return "" },
			func(fx *releaseFixture) string { return "" },
			"no checksums.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newReleaseFixture(t)

			err := fx.pipeline.run(context.Background(), fx.binaryURL(), tc.checksumsURL(fx), tc.sigURL(fx), fx.suffix)

			fx.assertRefusedWithoutPrivilege(err, tc.wantInError)
		})
	}
}

func TestAC003_AC004_PerformUpdate_TransportErrorsAreRefused(t *testing.T) {
	tests := []struct {
		name        string
		urls        func(fx *releaseFixture, dead string) (checksums, sig string)
		wantInError string
	}{
		{"AC-004 checksums host unreachable", func(fx *releaseFixture, dead string) (string, string) {
			return dead + "/checksums.txt", fx.sigURL()
		}, "checksum fetch failed"},
		{"AC-003 signature host unreachable", func(fx *releaseFixture, dead string) (string, string) {
			return fx.checksumsURL(), dead + "/checksums.txt.sig"
		}, "signature fetch failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newReleaseFixture(t)
			deadServer := httptest.NewServer(http.NotFoundHandler())
			dead := deadServer.URL
			deadServer.Close()
			checksumsURL, sigURL := tc.urls(fx, dead)

			err := fx.pipeline.run(context.Background(), fx.binaryURL(), checksumsURL, sigURL, fx.suffix)

			fx.assertRefusedWithoutPrivilege(err, tc.wantInError)
		})
	}
}

func TestAC005_AC010_PerformUpdate_ValidReleaseInvokesOnlyTheFixedVerifier(t *testing.T) {
	fx := newReleaseFixture(t)

	require.NoError(t, fx.update())

	want := [][]string{
		{"sudo", "systemctl", "reset-failed", "riot-agent-update"},
		{"sudo", "systemd-run", "--unit=riot-agent-update", verifierPathInADD},
	}
	assert.Equal(t, want, fx.runner.recorded(),
		"the only privileged commands must be the fixed reset-failed rule and the fixed systemd-run verifier rule")
	for _, call := range fx.runner.recorded() {
		for _, arg := range call {
			assert.NotEqual(t, "sh", arg, "no shell may be invoked (call %v)", call)
			assert.NotEqual(t, "-c", arg, "no sh -c script may be built (call %v)", call)
			assert.NotContains(t, arg, "*", "no wildcard argument may be passed (call %v)", call)
		}
	}
}

func TestAC005_PerformUpdate_StagesTheSignedMaterialForTheVerifier(t *testing.T) {
	fx := newReleaseFixture(t)

	require.NoError(t, fx.update())

	staged := map[string]string{
		fx.paths.binary:    fx.binary,
		fx.paths.checksums: fx.manifest,
		fx.paths.signature: fx.sigBody,
	}
	for path, want := range staged {
		got, err := os.ReadFile(path)
		require.NoError(t, err, "staged file %s must exist for the verifier", path)
		assert.Equal(t, want, string(got), "staged file %s must hold the exact verified bytes", path)
	}
}

func TestAC011_PerformUpdate_VerifierUnavailableLeavesVerifiedStagingForBoot(t *testing.T) {
	fx := newReleaseFixture(t)
	fx.runner.failWhen = func(call []string) error {
		if len(call) > 1 && call[1] == "systemd-run" {
			return errors.New("sudo: a password is required")
		}
		return nil
	}

	require.NoError(t, fx.update(), "falling back to the next-restart apply is not an update failure")

	for _, path := range fx.stagedFiles() {
		_, err := os.Stat(path)
		assert.NoError(t, err, "staged file %s must be kept for ExecStartPre --boot", path)
	}
	for _, call := range fx.runner.recorded() {
		assert.NotContains(t, call, "sh", "the fallback must not shell out (call %v)", call)
	}
}

func TestAC005_PerformUpdate_ResetFailedFailureIsIgnored(t *testing.T) {
	fx := newReleaseFixture(t)
	fx.runner.failWhen = func(call []string) error {
		if len(call) > 1 && call[1] == "systemctl" {
			return errors.New("unit not failed")
		}
		return nil
	}

	require.NoError(t, fx.update())

	assert.Len(t, fx.runner.recorded(), 2, "reset-failed is best effort; systemd-run must still run")
}

func TestAC007_PerformUpdate_SelfConsistentAttackerReleaseIsRefused(t *testing.T) {
	// A forged server controls the binary, checksums.txt, .sig, and every URL.
	// Only the embedded key may decide authenticity.
	_, attackerPriv := newTestKeyPair(t)
	fx := newReleaseFixture(t, func(fx *releaseFixture) {
		fx.priv = attackerPriv
		fx.binary = "ATTACKER-BINARY"
		fx.setManifest(fx.binary)
	})

	fx.assertRefusedWithoutPrivilege(fx.update(), "signature invalid")
}

func TestAC007_NewUpdatePipeline_UsesOnlyTheCompiledInReleaseKey(t *testing.T) {
	original := releasekey.PublicKeyBase64
	t.Cleanup(func() { releasekey.PublicKeyBase64 = original })
	pub, _ := newTestKeyPair(t)
	releasekey.PublicKeyBase64 = base64.StdEncoding.EncodeToString(pub)

	a := &Agent{}
	got, err := a.newUpdatePipeline().publicKey()

	require.NoError(t, err)
	assert.Equal(t, pub, got, "the pipeline must take its key from internal/releasekey")
}

func TestV005_UpdateConstants_MatchTheInstallerAndVerifierContract(t *testing.T) {
	assert.Equal(t, "/var/lib/riot/riot-agent.update", stagingPath)
	assert.Equal(t, "/var/lib/riot/riot-agent.update.checksums", stagingChecksumsPath)
	assert.Equal(t, "/var/lib/riot/riot-agent.update.sig", stagingSigPath)
	assert.Equal(t, verifierPathInADD, applyBinPath)
	assert.Equal(t, "riot-agent-update", updateUnit)
}

func TestSEC002_FetchBytes_EnforcesStatusAndSizeCap(t *testing.T) {
	const limit int64 = 16
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exact":
			_, _ = w.Write([]byte(strings.Repeat("a", int(limit))))
		case "/over":
			_, _ = w.Write([]byte(strings.Repeat("a", int(limit)+1)))
		case "/empty":
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name    string
		path    string
		wantLen int
		wantErr string
	}{
		{"body exactly at the cap is accepted", "/exact", int(limit), ""},
		{"empty body is returned as empty", "/empty", 0, ""},
		{"body one byte over the cap is refused", "/over", 0, "exceeds"},
		{"non-200 is refused", "/missing", 0, "status 404"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fetchBytes(context.Background(), srv.URL+tc.path, limit)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, got, "no partial body may be returned with an error")
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tc.wantLen)
		})
	}
}

func TestSEC005_ParseExpectedChecksum_ExactAssetNames(t *testing.T) {
	amd64Sum := sha256HexString("amd64")
	armSum := sha256HexString("arm")
	winSum := sha256HexString("windows")
	manifest := strings.Join([]string{
		sha256HexString("apply") + "  riot-agent-apply-linux-amd64",
		amd64Sum + "  riot-agent-linux-amd64",
		armSum + "  riot-agent-linux-armv7",
		winSum + "  riot-agent-windows-amd64.exe",
		"",
	}, "\n")

	tests := []struct {
		name    string
		suffix  string
		want    string
		wantErr error
	}{
		{"linux amd64 skips the verifier line that contains its suffix", "linux-amd64", amd64Sum, nil},
		{"linux armv7", "linux-armv7", armSum, nil},
		{"C4 windows asset carries .exe", "windows-amd64", winSum, nil},
		{"platform absent", "linux-arm64", "", releasekey.ErrNoChecksumEntry},
		{"partial suffix is not a match", "amd64", "", releasekey.ErrNoChecksumEntry},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseExpectedChecksum([]byte(manifest), tc.suffix)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSEC004_IsNewerVersion_FailsClosed(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"newer patch", "1.0.1", "1.0.0", true},
		{"newer minor", "1.2.0", "1.1.9", true},
		{"newer major", "2.0.0", "1.9.9", true},
		{"v prefix tolerated", "v1.1.0", "1.0.0", true},
		{"numeric not lexical (10 > 9)", "1.10.0", "1.9.0", true},
		{"equal is not newer", "1.0.0", "1.0.0", false},
		{"older patch", "1.0.0", "1.0.1", false},
		{"older major", "1.9.9", "2.0.0", false},
		{"pre-release suffix ignored for ordering", "1.1.0-rc1", "1.0.0", true},
		{"dev current is never updated", "9.9.9", "dev", false},
		{"unparseable latest", "latest", "1.0.0", false},
		{"too few parts", "1.1", "1.0.0", false},
		{"too many parts", "1.1.0.1", "1.0.0", false},
		{"negative part", "1.-1.0", "1.0.0", false},
		{"empty latest", "", "1.0.0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isNewerVersion(tc.latest, tc.current))
		})
	}
}

func TestC4_ReleaseAssetName_MatchesPublishedAssets(t *testing.T) {
	assert.Equal(t, "riot-agent-linux-amd64", releaseAssetName("linux-amd64"))
	assert.Equal(t, "riot-agent-linux-armv6", releaseAssetName("linux-armv6"))
	assert.Equal(t, "riot-agent-darwin-arm64", releaseAssetName("darwin-arm64"))
	assert.Equal(t, "riot-agent-windows-amd64.exe", releaseAssetName("windows-amd64"))
}

// updateCycleFixture drives runUpdateCheck against a fake rIOt server that
// serves /api/v1/update/check and records posted events.
type updateCycleFixture struct {
	t         *testing.T
	agent     *Agent
	checkHits int
	mu        sync.Mutex
	events    []models.EventType
	applied   int
	restarted int
	gotURLs   [3]string
}

func newUpdateCycleFixture(t *testing.T, version string, autoUpdate *bool, resp func(suffix string) UpdateCheckResponse) *updateCycleFixture {
	t.Helper()
	fx := &updateCycleFixture{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fx.mu.Lock()
		defer fx.mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/update/check":
			fx.checkHits++
			_ = json.NewEncoder(w).Encode(resp(platformSuffix(testGoarm())))
		case strings.HasSuffix(r.URL.Path, "/events"):
			var evt models.AgentEvent
			_ = json.NewDecoder(r.Body).Decode(&evt)
			fx.events = append(fx.events, evt.Type)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{}
	cfg.Agent.DeviceID = testDeviceID
	cfg.Agent.AutoUpdate = autoUpdate
	fx.agent = &Agent{config: cfg, version: version, client: NewHTTPClient(srv.URL, "test-key")}
	return fx
}

// testGoarm mirrors runUpdateCheck's arm handling so asset keys line up.
func testGoarm() string {
	if runtime.GOARCH == "arm" {
		return goarmVersion()
	}
	return ""
}

func (fx *updateCycleFixture) apply(err error) updateFunc {
	return func(_ context.Context, downloadURL, checksumURL, signatureURL, suffix string) error {
		fx.mu.Lock()
		defer fx.mu.Unlock()
		fx.applied++
		fx.gotURLs = [3]string{downloadURL, checksumURL, signatureURL}
		return err
	}
}

func (fx *updateCycleFixture) restart() {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.restarted++
}

func (fx *updateCycleFixture) eventCount(evt models.EventType) int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	count := 0
	for _, got := range fx.events {
		if got == evt {
			count++
		}
	}
	return count
}

func availableUpdate(latest string) func(suffix string) UpdateCheckResponse {
	return func(suffix string) UpdateCheckResponse {
		return UpdateCheckResponse{
			CurrentVersion: testCurrentVer,
			LatestVersion:  latest,
			UpdateAvail:    true,
			Assets:         map[string]string{suffix: "https://example.invalid/bin"},
			ChecksumURL:    "https://example.invalid/checksums.txt",
			SignatureURL:   "https://example.invalid/checksums.txt.sig",
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func TestAC015_UpdateCycle_DevBuildNeverChecksOrUpdates(t *testing.T) {
	fx := newUpdateCycleFixture(t, "dev", nil, availableUpdate(testLatestVer))

	fx.agent.runUpdateCheck(context.Background(), fx.apply(nil), fx.restart)

	assert.Zero(t, fx.hits(), "a dev build must not even query for updates")
	assert.Zero(t, fx.appliedCount(), "a dev build must never download or install")
}

func TestAC016_UpdateCycle_AutoUpdateDefaultOnAndExplicitFalseSuppresses(t *testing.T) {
	tests := []struct {
		name        string
		autoUpdate  *bool
		wantChecked bool
		wantApplied int
	}{
		{"nil means enabled (default-on preserved)", nil, true, 1},
		{"explicit true is enabled", boolPtr(true), true, 1},
		{"explicit false suppresses exactly as before", boolPtr(false), false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newUpdateCycleFixture(t, testCurrentVer, tc.autoUpdate, availableUpdate(testLatestVer))

			fx.agent.runUpdateCheck(context.Background(), fx.apply(nil), fx.restart)

			assert.Equal(t, tc.wantChecked, fx.hits() > 0)
			assert.Equal(t, tc.wantApplied, fx.appliedCount())
		})
	}
}

func TestSEC004_UpdateCycle_NonNewerAdvertisedVersionIsIgnoredSilently(t *testing.T) {
	for _, advertised := range []string{testCurrentVer, "0.9.0", "garbage"} {
		t.Run(advertised, func(t *testing.T) {
			fx := newUpdateCycleFixture(t, testCurrentVer, nil, availableUpdate(advertised))

			fx.agent.runUpdateCheck(context.Background(), fx.apply(nil), fx.restart)

			assert.Zero(t, fx.appliedCount(), "a non-newer latest_version must never reach performUpdate")
			assert.Zero(t, fx.restartCount())
			fx.mu.Lock()
			defer fx.mu.Unlock()
			assert.Empty(t, fx.events, "the downgrade guard logs at debug and emits no events")
		})
	}
}

func TestAC002_UpdateCycle_PassesSignatureURLToPerformUpdate(t *testing.T) {
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, availableUpdate(testLatestVer))

	fx.agent.runUpdateCheck(context.Background(), fx.apply(nil), fx.restart)

	assert.Equal(t, [3]string{
		"https://example.invalid/bin",
		"https://example.invalid/checksums.txt",
		"https://example.invalid/checksums.txt.sig",
	}, fx.urls(), "download, checksum, and signature locators must all be threaded through")
}

func TestAC019_UpdateCycle_RefusalEmitsExactlyOneFailedEventAndNeverRestarts(t *testing.T) {
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, availableUpdate(testLatestVer))

	fx.agent.runUpdateCheck(context.Background(), fx.apply(errors.New("signature invalid")), fx.restart)

	assert.Equal(t, 1, fx.eventCount(models.EventAgentUpdateFailed), "exactly one update-failed event")
	assert.Zero(t, fx.eventCount(models.EventAgentUpdateCompleted), "a refusal must not emit update-completed")
	assert.Zero(t, fx.restartCount(), "a refused update must leave the agent running")
}

func TestAC005_UpdateCycle_SuccessEmitsCompletedAndRestarts(t *testing.T) {
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, availableUpdate(testLatestVer))

	fx.agent.runUpdateCheck(context.Background(), fx.apply(nil), fx.restart)

	assert.Equal(t, 1, fx.eventCount(models.EventAgentUpdateCompleted))
	assert.Zero(t, fx.eventCount(models.EventAgentUpdateFailed))
	assert.Equal(t, 1, fx.restartCount())
}

func TestAC002_UpdateCycle_UnsignedReleaseEndToEndIsASafeNoOp(t *testing.T) {
	rel := newReleaseFixture(t)
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, func(suffix string) UpdateCheckResponse {
		return UpdateCheckResponse{
			LatestVersion: testLatestVer,
			UpdateAvail:   true,
			Assets:        map[string]string{suffix: rel.binaryURL()},
			ChecksumURL:   rel.checksumsURL(),
			// SignatureURL deliberately absent: a pre-signing release.
		}
	})

	fx.agent.runUpdateCheck(context.Background(), rel.pipeline.run, fx.restart)

	assert.Equal(t, 1, fx.eventCount(models.EventAgentUpdateFailed), "an unsigned release is reported as a failed update")
	assert.Zero(t, fx.restartCount(), "the agent keeps running its current version")
	assert.Empty(t, rel.runner.recorded(), "no sudo command may run")
	rel.assertNoStagingLeft()
}

func TestAC005_UpdateCycle_SignedReleaseEndToEndAppliesAndRestarts(t *testing.T) {
	var rel *releaseFixture
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, func(suffix string) UpdateCheckResponse {
		return UpdateCheckResponse{
			LatestVersion: testLatestVer,
			UpdateAvail:   true,
			Assets:        map[string]string{suffix: rel.binaryURL()},
			ChecksumURL:   rel.checksumsURL(),
			SignatureURL:  rel.sigURL(),
		}
	})
	rel = newReleaseFixture(t, func(r *releaseFixture) {
		r.suffix = platformSuffix(testGoarm())
		r.setManifest(r.binary)
	})

	fx.agent.runUpdateCheck(context.Background(), rel.pipeline.run, fx.restart)

	assert.Equal(t, 1, fx.eventCount(models.EventAgentUpdateCompleted))
	assert.Equal(t, 1, fx.restartCount())
	assert.Len(t, rel.runner.recorded(), 2, "reset-failed then the fixed systemd-run verifier rule")
}

func TestAC016_RunUpdateCheck_ServerSaysUpToDateDoesNothing(t *testing.T) {
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, func(suffix string) UpdateCheckResponse {
		return UpdateCheckResponse{LatestVersion: testCurrentVer, UpdateAvail: false}
	})

	fx.agent.runUpdateCheck(context.Background(), fx.apply(nil), fx.restart)

	assert.Equal(t, 1, fx.hits())
	assert.Zero(t, fx.appliedCount())
}

func TestNFR007_FetchTimeout_IsNoLongerThanTheLegacyChecksumTimeout(t *testing.T) {
	assert.LessOrEqual(t, metaFetchTimeout, 30*time.Second, "metadata fetches must stay within the existing 30s bound")
}

func (fx *updateCycleFixture) hits() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.checkHits
}

func (fx *updateCycleFixture) appliedCount() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.applied
}

func (fx *updateCycleFixture) restartCount() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.restarted
}

func (fx *updateCycleFixture) urls() [3]string {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.gotURLs
}

// Added by QA Engineer
// Covers AC-001 and AC-019 end to end: a genuinely signed release whose binary
// was tampered with in transit must, through the REAL pipeline and the REAL
// update cycle, emit exactly one update-failed event, never emit
// update-completed, never restart, never run a sudo command, and leave no
// staging files. The existing tampered-binary test stops at performUpdate and
// the cycle tests stub performUpdate out, so neither alone proves this chain.
func TestAC001_AC019_UpdateCycle_TamperedBinaryEndToEndIsRefusedWithoutPrivilege(t *testing.T) {
	var rel *releaseFixture
	fx := newUpdateCycleFixture(t, testCurrentVer, nil, func(suffix string) UpdateCheckResponse {
		return UpdateCheckResponse{
			LatestVersion: testLatestVer,
			UpdateAvail:   true,
			Assets:        map[string]string{suffix: rel.binaryURL()},
			ChecksumURL:   rel.checksumsURL(),
			SignatureURL:  rel.sigURL(),
		}
	})
	rel = newReleaseFixture(t, func(r *releaseFixture) {
		r.suffix = platformSuffix(testGoarm())
		r.setManifest(r.binary)           // sign the genuine binary's hash...
		r.binary = r.binary + "-tampered" // ...then serve different bytes
	})

	fx.agent.runUpdateCheck(context.Background(), rel.pipeline.run, fx.restart)

	assert.Equal(t, 1, fx.eventCount(models.EventAgentUpdateFailed), "exactly one update-failed event")
	assert.Zero(t, fx.eventCount(models.EventAgentUpdateCompleted), "a refused update must not report completion")
	assert.Zero(t, fx.restartCount(), "the agent must keep running its current version")
	assert.Empty(t, rel.runner.recorded(), "no sudo command or verifier may run for a tampered binary")
	rel.assertNoStagingLeft()
}

// Added by QA Engineer
// Covers AC-010 / SEC-004 on the manual (trigger_update command) path: the
// AD-010 downgrade guard must refuse a non-newer advertised version BEFORE any
// download or staging, so a server cannot use the manual trigger to bypass it.
// The implementation added this guard to commands.go but no test covered it.
func TestSEC004_TriggerUpdate_NonNewerAdvertisedVersionIsRefusedBeforeAnyDownload(t *testing.T) {
	for _, advertised := range []string{testCurrentVer, "0.9.0", "garbage"} {
		t.Run(advertised, func(t *testing.T) {
			fx := newUpdateCycleFixture(t, testCurrentVer, nil, availableUpdate(advertised))

			status, msg := fx.agent.handleTriggerUpdate(context.Background())

			assert.Equal(t, "error", status)
			assert.Contains(t, msg, "refusing update", "the refusal must name the downgrade guard")
			assert.Zero(t, fx.eventCount(models.EventAgentUpdateStarted), "no download may start")
			assert.Zero(t, fx.eventCount(models.EventAgentUpdateCompleted))
		})
	}
}
