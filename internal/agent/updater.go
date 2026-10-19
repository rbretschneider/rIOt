package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/releasekey"
)

// Staging, helper, and unit names. They must match byte-for-byte the
// sudoers rule and systemd unit written by scripts/install.sh, the constants
// in cmd/riot-agent-apply, and the doctor probe (V-005).
const (
	// stagingPath is where the new binary is downloaded before being installed.
	stagingPath = "/var/lib/riot/riot-agent.update"
	// stagingChecksumsPath and stagingSigPath hold the signed checksums.txt and
	// checksums.txt.sig for the privileged verifier, which re-verifies them.
	stagingChecksumsPath = "/var/lib/riot/riot-agent.update.checksums"
	stagingSigPath       = "/var/lib/riot/riot-agent.update.sig"

	// applyBinPath is the root-owned privileged verifier (cmd/riot-agent-apply).
	applyBinPath = "/usr/local/lib/riot/riot-agent-apply"
	// updateUnit is the transient systemd unit the verifier runs under.
	updateUnit = "riot-agent-update"
)

const (
	// maxMetaFileSize caps the in-memory checksums.txt / checksums.txt.sig
	// reads so a hostile server cannot exhaust memory (SEC-002).
	maxMetaFileSize int64 = 1 << 20

	// maxDownloadBytes caps a self-update download (GROWTH-001 FR-072). The real
	// agent is tens of MiB; a longer stream is a fault or an attack.
	maxDownloadBytes int64 = 64 << 20
	// staleStagingAge is how old a staged update file must be before the
	// agent-start sweep removes it.
	staleStagingAge = time.Hour

	metaFetchTimeout      = 30 * time.Second
	binaryDownloadTimeout = 5 * time.Minute

	updateCheckInterval = 6 * time.Hour
	updateStartupDelay  = 30 * time.Second

	releaseAssetPrefix = "riot-agent-"
	windowsAssetExt    = ".exe"
	windowsSuffixStart = "windows-"

	stagedMetaFileMode = 0o600
	stagedBinaryMode   = 0o755

	versionPartCount = 3
)

// stagingPaths are the three files the agent stages for the verifier.
type stagingPaths struct {
	binary    string
	checksums string
	signature string
}

// updateFunc downloads, verifies, and hands off one update (performUpdate).
type updateFunc func(ctx context.Context, downloadURL, checksumURL, signatureURL, suffix string) error

// updatePipeline is the fail-closed download-and-verify pipeline. Its
// collaborators are injected so tests can use temp paths, a test key, and a
// recording command runner.
type updatePipeline struct {
	paths     stagingPaths
	publicKey func() (ed25519.PublicKey, error)
	runner    collectors.CommandRunner
}

func (a *Agent) updateCheckLoop(ctx context.Context) {
	// Wait a bit before first check to let registration complete
	select {
	case <-ctx.Done():
		return
	case <-time.After(updateStartupDelay):
	}

	a.checkAndUpdate(ctx)

	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkAndUpdate(ctx)
		}
	}
}

func (a *Agent) checkAndUpdate(ctx context.Context) {
	a.runUpdateCheck(ctx, a.performUpdate, a.restartSelf)
}

// runUpdateCheck is checkAndUpdate with its side effects injected: apply
// performs the update and restart ends the process after a successful one.
func (a *Agent) runUpdateCheck(ctx context.Context, apply updateFunc, restart func()) {
	if a.version == "dev" {
		return // Don't auto-update dev builds
	}

	if a.config.Agent.AutoUpdate != nil && !*a.config.Agent.AutoUpdate {
		slog.Debug("auto-update disabled by config")
		return
	}

	goarm := ""
	if runtime.GOARCH == "arm" {
		goarm = goarmVersion()
	}

	resp, err := a.client.CheckForUpdate(ctx, a.version, runtime.GOOS, runtime.GOARCH, goarm)
	if err != nil {
		slog.Warn("update check failed", "error", err)
		return
	}

	if resp.LatestVersion == "" {
		slog.Debug("update check: server has no release info yet, will retry next cycle")
		return
	}

	if !resp.UpdateAvail {
		slog.Debug("agent is up to date", "version", a.version)
		return
	}

	// AD-010: best-effort downgrade guard. It rejects a server that honestly
	// advertises a latest_version that is not newer than the running one. It
	// CANNOT stop a forged server that advertises a high latest_version while
	// serving an older, genuinely signed binary: the version is not part of
	// the signed material (checksums.txt filenames carry none), so the agent
	// cannot cryptographically learn the real version of the bytes. Robust
	// anti-rollback needs a signed version signal (recorded follow-up).
	if !isNewerVersion(resp.LatestVersion, a.version) {
		slog.Debug("ignoring update: advertised version is not newer", "current", a.version, "advertised", resp.LatestVersion)
		return
	}

	slog.Info("update available", "current", a.version, "latest", resp.LatestVersion)
	a.reportUpdateEvent(ctx, models.EventAgentUpdateAvail, models.SeverityInfo,
		fmt.Sprintf("Agent update available: %s → %s", a.version, resp.LatestVersion))

	// Find the download URL for our platform
	suffix := platformSuffix(goarm)
	downloadURL, ok := resp.Assets[suffix]
	if !ok {
		slog.Warn("no binary available for this platform", "platform", suffix)
		return
	}

	a.reportUpdateEvent(ctx, models.EventAgentUpdateStarted, models.SeverityInfo,
		fmt.Sprintf("Agent update started: %s → %s", a.version, resp.LatestVersion))

	if err := apply(ctx, downloadURL, resp.ChecksumURL, resp.SignatureURL, suffix); err != nil {
		slog.Error("update failed", "error", err)
		a.reportUpdateEvent(ctx, models.EventAgentUpdateFailed, models.SeverityWarning,
			fmt.Sprintf("Agent update failed: %v", err))
		return
	}

	a.reportUpdateEvent(ctx, models.EventAgentUpdateCompleted, models.SeverityInfo,
		fmt.Sprintf("Agent updated to %s", resp.LatestVersion))
	slog.Info("update applied, restarting")
	restart()
}

// performUpdate downloads and verifies an update and hands it to the
// privileged verifier. It fails closed: every verification problem is an error
// and the live binary is never touched by the agent.
func (a *Agent) performUpdate(ctx context.Context, downloadURL, checksumURL, signatureURL, suffix string) error {
	return a.newUpdatePipeline().run(ctx, downloadURL, checksumURL, signatureURL, suffix)
}

func (a *Agent) newUpdatePipeline() *updatePipeline {
	runner := a.runner
	if runner == nil {
		runner = execRunner
	}
	return &updatePipeline{
		paths: stagingPaths{
			binary:    stagingPath,
			checksums: stagingChecksumsPath,
			signature: stagingSigPath,
		},
		publicKey: releasekey.PublicKey,
		runner:    runner,
	}
}

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// run executes the AD-004 pipeline in strict order. The agent's checks are a
// non-authoritative pre-flight: the agent runs as an unprivileged user, so the
// root verifier re-verifies everything over the bytes it installs.
func (p *updatePipeline) run(ctx context.Context, downloadURL, checksumURL, signatureURL, suffix string) error {
	staged := false
	defer func() {
		if !staged {
			p.discardStaging()
		}
	}()

	actualSum, err := p.downloadBinary(ctx, downloadURL)
	if err != nil {
		return err
	}
	checksums, sig, err := fetchSignedChecksums(ctx, checksumURL, signatureURL)
	if err != nil {
		return err
	}
	if err := p.verifyDownload(checksums, sig, suffix, actualSum); err != nil {
		return err
	}
	if err := p.stageSignedMaterial(checksums, sig); err != nil {
		return err
	}

	staged = true // keep the three staged files for the verifier
	p.applyStaged(ctx)
	return nil
}

// downloadBinary streams the binary to the staging path and returns its
// SHA-256. The stream digest is pre-flight only (SEC-006): the verifier
// re-hashes the bytes it installs.
func (p *updatePipeline) downloadBinary(ctx context.Context, downloadURL string) (sum string, err error) {
	// Any failure leaves no partial file behind (GROWTH-001 FR-072). This defer
	// is registered first so it runs after the file handle is closed.
	defer func() {
		if err != nil {
			os.Remove(p.paths.binary)
		}
	}()

	// Stage under the riot-owned data dir, not /tmp: PrivateTmp isolates /tmp
	// and a rename across filesystems would fail.
	stagingFile, err := os.OpenFile(p.paths.binary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, stagedBinaryMode)
	if err != nil {
		return "", fmt.Errorf("create staging file: %w", err)
	}
	defer stagingFile.Close()

	slog.Info("downloading update", "url", downloadURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	client := &http.Client{Timeout: binaryDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Refuse an announced oversize body without reading it, and cap an
	// unannounced (chunked) or lying one: read at most the limit plus one byte,
	// so "more than the limit arrived" is detectable.
	if resp.ContentLength > maxDownloadBytes {
		return "", fmt.Errorf("download too large: the server announced %d bytes, the limit is %d", resp.ContentLength, maxDownloadBytes)
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(stagingFile, hash), io.LimitReader(resp.Body, maxDownloadBytes+1))
	if copyErr != nil {
		return "", fmt.Errorf("write binary: %w", copyErr)
	}
	if n > maxDownloadBytes {
		return "", fmt.Errorf("download too large: more than %d bytes arrived", maxDownloadBytes)
	}
	if err := stagingFile.Close(); err != nil {
		return "", fmt.Errorf("close staging file: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// sweepStaleStaging removes the agent's own staged update files that are older
// than maxAge, left behind by an interrupted or abandoned update. Only regular
// files are removed (a directory or link planted at the path is left alone), and
// a young file is kept because an update may be in flight (GROWTH-001 FR-073).
func sweepStaleStaging(paths stagingPaths, now time.Time, maxAge time.Duration) {
	for _, path := range []string{paths.binary, paths.checksums, paths.signature} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if err := os.Remove(path); err != nil {
			slog.Warn("could not remove a stale update staging file", "path", path, "error", err)
			continue
		}
		slog.Info("removed a stale update staging file", "path", path)
	}
}

// fetchSignedChecksums requires BOTH locators (an unsigned or pre-signing
// release advertises no signature_url and is refused, FR-012/FR-026), then
// fetches checksums.txt and checksums.txt.sig under the size cap.
func fetchSignedChecksums(ctx context.Context, checksumURL, signatureURL string) (checksums, sig []byte, err error) {
	if checksumURL == "" {
		return nil, nil, fmt.Errorf("release has no checksums.txt: refusing unverifiable update")
	}
	if signatureURL == "" {
		return nil, nil, fmt.Errorf("release is unsigned (no checksums.txt.sig): refusing unverifiable update")
	}
	checksums, err = fetchBytes(ctx, checksumURL, maxMetaFileSize)
	if err != nil {
		return nil, nil, fmt.Errorf("checksum fetch failed: %w", err)
	}
	sig, err = fetchBytes(ctx, signatureURL, maxMetaFileSize)
	if err != nil {
		return nil, nil, fmt.Errorf("signature fetch failed: %w", err)
	}
	return checksums, sig, nil
}

// verifyDownload runs the pre-flight checks in order: signature over the
// exact checksums bytes, exact-basename entry, then hash equality.
func (p *updatePipeline) verifyDownload(checksums, sig []byte, suffix, actualSum string) error {
	if err := p.verifyReleaseSignature(checksums, sig); err != nil {
		return fmt.Errorf("signature invalid: %w", err)
	}
	expectedSum, err := parseExpectedChecksum(checksums, suffix)
	if err != nil {
		return fmt.Errorf("checksum unavailable: %w", err)
	}
	if !strings.EqualFold(expectedSum, actualSum) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSum, actualSum)
	}
	slog.Info("release signature and checksum verified")
	return nil
}

// verifyReleaseSignature checks the detached signature over the exact
// checksums.txt bytes against the compiled-in key. No server-supplied value
// can select or replace that key (FR-005, FR-006).
func (p *updatePipeline) verifyReleaseSignature(checksums, sigB64 []byte) error {
	pub, err := p.publicKey()
	if err != nil {
		return err
	}
	return releasekey.VerifySignature(pub, checksums, sigB64)
}

// stageSignedMaterial persists the verified checksums.txt and .sig next to the
// binary for the privileged verifier to re-verify.
func (p *updatePipeline) stageSignedMaterial(checksums, sig []byte) error {
	if err := os.WriteFile(p.paths.checksums, checksums, stagedMetaFileMode); err != nil {
		return fmt.Errorf("stage checksums.txt: %w", err)
	}
	if err := os.WriteFile(p.paths.signature, sig, stagedMetaFileMode); err != nil {
		return fmt.Errorf("stage checksums.txt.sig: %w", err)
	}
	return nil
}

// discardStaging removes all staged files after a refusal.
func (p *updatePipeline) discardStaging() {
	for _, path := range []string{p.paths.binary, p.paths.checksums, p.paths.signature} {
		os.Remove(path)
	}
}

// applyStaged asks the privileged verifier to install the staged update. The
// only sudo commands are the fixed reset-failed rule and the fixed
// `systemd-run --unit=riot-agent-update <verifier>` rule; no shell is ever
// built. If the verifier cannot be started, the staged files are left for the
// unit's ExecStartPre (`riot-agent-apply --boot`) to verify on next restart.
//
// It does not return an error in either case. The caller reports the update as
// completed and exits so systemd restarts the agent; on the fallback path that
// restart is what applies the staged update. A verifier refusal happens after
// this returns, in the root process, so it surfaces in the journal, not as an
// agent_update_failed event.
func (p *updatePipeline) applyStaged(ctx context.Context) {
	_, _ = p.runner(ctx, "sudo", "systemctl", "reset-failed", updateUnit)

	out, err := p.runner(ctx, "sudo", "systemd-run", "--unit="+updateUnit, applyBinPath)
	if err == nil {
		slog.Info("update scheduled via privileged verifier")
		return
	}
	slog.Debug("privileged verifier unavailable",
		"error", fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))))
	slog.Warn("could not start the privileged update verifier; update staged for next restart — re-run install.sh to enable automatic updates")
}

func (a *Agent) restartSelf() {
	slog.Info("exiting for restart")
	os.Exit(0)
}

// fetchBytes GETs url with the metadata timeout, requires HTTP 200, and
// returns at most limit bytes. A longer body is an error (fail closed).
func fetchBytes(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	client := &http.Client{Timeout: metaFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("body exceeds the %d byte limit", limit)
	}
	return body, nil
}

// parseExpectedChecksum returns the SHA-256 for this platform from already
// verified checksums.txt bytes, matching the exact asset basename (SEC-005).
func parseExpectedChecksum(checksums []byte, suffix string) (string, error) {
	return releasekey.ExpectedChecksum(checksums, releaseAssetName(suffix))
}

// releaseAssetName maps a platform suffix to its release asset basename. Only
// Linux agents auto-update through the systemd verifier; the .exe rule keeps
// the exact-match lookup correct for the Windows asset name (C4).
func releaseAssetName(suffix string) string {
	name := releaseAssetPrefix + suffix
	if strings.HasPrefix(suffix, windowsSuffixStart) {
		name += windowsAssetExt
	}
	return name
}

// isNewerVersion reports whether latest is strictly greater than current,
// comparing major.minor.patch. Either side being unparseable (including
// "dev") is "not newer", so the guard fails closed.
func isNewerVersion(latest, current string) bool {
	latestParts, latestOK := parseVersion(latest)
	currentParts, currentOK := parseVersion(current)
	if !latestOK || !currentOK {
		return false
	}
	for i := range latestParts {
		if latestParts[i] != currentParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	return false
}

// parseVersion parses "v1.2.3", "1.2.3", or "1.2.3-rc1+meta" into numbers.
func parseVersion(version string) ([versionPartCount]int, bool) {
	var out [versionPartCount]int
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version = strings.SplitN(version, "+", 2)[0]
	parts := strings.Split(version, ".")
	if len(parts) != versionPartCount {
		return out, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(strings.SplitN(part, "-", 2)[0])
		if err != nil || number < 0 {
			return out, false
		}
		out[i] = number
	}
	return out, true
}

func platformSuffix(goarm string) string {
	if goarm != "" {
		return fmt.Sprintf("%s-armv%s", runtime.GOOS, goarm)
	}
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

func goarmVersion() string {
	// Default to v7 on ARM — the build matrix uses v6 and v7
	return "7"
}

// reportUpdateEvent sends an agent update event to the server (best-effort).
func (a *Agent) reportUpdateEvent(ctx context.Context, evtType models.EventType, severity models.EventSeverity, message string) {
	deviceID := a.config.Agent.DeviceID
	if deviceID == "" || a.client == nil {
		return
	}
	evt := &models.AgentEvent{
		Type:     evtType,
		Severity: severity,
		Message:  message,
	}
	if err := a.client.ReportEvent(ctx, deviceID, evt); err != nil {
		slog.Debug("failed to report update event", "type", evtType, "error", err)
	}
}
