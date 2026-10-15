//go:build linux

// Command riot-agent-apply is the root-owned, privileged verifier that
// installs a staged rIOt agent update (AGENT-HARDEN-001 AD-005).
//
// Threat model: the unprivileged riot user can stage arbitrary files and can
// invoke this binary directly through the fixed sudoers rule, so this binary
// assumes the caller verified NOTHING. It therefore authenticates the exact
// bytes it installs, on the privileged side of the boundary:
//
//  1. each staged input is opened O_NOFOLLOW and must be a bounded regular file;
//  2. the signed checksums.txt is verified against the compiled-in ed25519 key;
//  3. the staged binary is read ONCE into memory, hashed in memory, and matched
//     against the exact-basename entry of the verified checksums.txt;
//  4. those SAME in-memory bytes are written to the hardcoded target through an
//     O_EXCL|O_NOFOLLOW temp file and an atomic rename.
//
// The staging files are never re-read after verification, the install target
// is a compile-time constant, and no argument or environment variable can
// influence the trust anchor or the target.
//
// Modes:
//
//	riot-agent-apply          stop service, verify+install, start service
//	                          (run detached via sudo systemd-run)
//	riot-agent-apply --boot   verify+install a pending staged update only
//	                          (run by the unit's ExecStartPre as root)
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/releasekey"
)

// Fixed paths. They must match byte-for-byte the constants in
// internal/agent/updater.go, the sudoers rule, and the systemd unit (V-005).
const (
	// targetBin is HARDCODED and never taken from argv or the environment.
	targetBin = "/usr/local/bin/riot-agent"

	stagingBinaryPath    = "/var/lib/riot/riot-agent.update"
	stagingChecksumsPath = "/var/lib/riot/riot-agent.update.checksums"
	stagingSigPath       = "/var/lib/riot/riot-agent.update.sig"

	serviceName = "riot-agent"
)

const (
	// maxMetaFileSize caps checksums.txt and checksums.txt.sig reads (SEC-002).
	maxMetaFileSize int64 = 1 << 20
	// maxBinarySize caps the in-memory binary read so a riot-planted huge
	// staging file is refused instead of exhausting memory (C3). The real
	// agent is tens of MiB.
	maxBinarySize int64 = 128 << 20

	// applyStopDelay lets the agent finish reporting its update event
	// before the service is stopped.
	applyStopDelay = 3 * time.Second

	bootFlag = "--boot"

	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2

	installedFileMode fs.FileMode = 0o755
	tempSuffixBytes               = 8
	tempFilePrefix                = ".riot-agent.apply."
)

// targetAssetName is the release asset name whose checksums.txt entry
// authenticates the binary installed at targetBin, e.g. "riot-agent-linux-arm64".
// It is baked per platform at build time:
//
//	-ldflags "-X main.targetAssetName=riot-agent-<suffix>"
//
// An unset value matches no checksums.txt line, so a verifier built without it
// fails closed.
var targetAssetName = ""

// stagedInputs are the three riot-writable files the agent stages.
type stagedInputs struct {
	binary    string
	checksums string
	signature string
}

// installer holds the verifier's inputs. Production wiring is
// newProductionInstaller; tests inject temp paths and a test key.
type installer struct {
	staged          stagedInputs
	targetBin       string
	targetAssetName string
	publicKey       func() (ed25519.PublicKey, error)
	maxBinarySize   int64
	maxMetaSize     int64
	logger          *slog.Logger

	// betweenVerifyAndWrite is a test seam invoked after the bytes are
	// verified and before they are written. Production leaves it nil.
	betweenVerifyAndWrite func()
}

// serviceController starts and stops the agent service.
type serviceController interface {
	Stop() error
	Start() error
}

func newProductionInstaller(logger *slog.Logger) *installer {
	return &installer{
		staged: stagedInputs{
			binary:    stagingBinaryPath,
			checksums: stagingChecksumsPath,
			signature: stagingSigPath,
		},
		targetBin:       targetBin,
		targetAssetName: targetAssetName,
		publicKey:       releasekey.PublicKey,
		maxBinarySize:   maxBinarySize,
		maxMetaSize:     maxMetaFileSize,
		logger:          logger,
	}
}

func main() {
	in := newProductionInstaller(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	os.Exit(runMode(os.Args[1:], in, systemdService{unit: serviceName}, time.Sleep))
}

// runMode selects apply or --boot. Nothing else is accepted: no argument may
// influence the trust anchor or the install target.
func runMode(args []string, in *installer, svc serviceController, sleep func(time.Duration)) int {
	switch {
	case len(args) == 0:
		return runApply(in, svc, sleep)
	case len(args) == 1 && args[0] == bootFlag:
		return runBoot(in)
	default:
		fmt.Fprintf(os.Stderr, "usage: riot-agent-apply [%s]\n", bootFlag)
		return exitUsage
	}
}

// runApply stops the service, verifies and installs, and ALWAYS restarts the
// service so a refused update never leaves the agent down.
func runApply(in *installer, svc serviceController, sleep func(time.Duration)) int {
	sleep(applyStopDelay)
	if err := svc.Stop(); err != nil {
		in.logger.Warn("could not stop service; continuing", "error", err)
	}

	code := exitOK
	if err := in.verifyAndInstall(); err != nil {
		in.logger.Error("update refused", "error", err)
		in.removeStaging()
		code = exitFailure
	} else {
		in.logger.Info("update installed", "target", in.targetBin)
	}

	if err := svc.Start(); err != nil {
		in.logger.Error("could not start service", "error", err)
		code = exitFailure
	}
	return code
}

// runBoot applies a pending staged update at service start. It never blocks
// the current agent from starting: every outcome returns exitOK (NFR-005).
func runBoot(in *installer) int {
	if _, err := os.Lstat(in.staged.binary); errors.Is(err, fs.ErrNotExist) {
		return exitOK
	}
	if err := in.verifyAndInstall(); err != nil {
		in.logger.Error("staged update refused at boot", "error", err)
		in.removeStaging()
		return exitOK
	}
	in.logger.Info("staged update installed at boot", "target", in.targetBin)
	return exitOK
}

// verifyAndInstall is the authoritative check (SEC-001, SEC-006): it reads
// each staged input exactly once, verifies in memory, then writes the same
// verified bytes. It never re-opens a staging path after verification (C1).
func (in *installer) verifyAndInstall() error {
	pub, err := in.publicKey()
	if err != nil {
		return fmt.Errorf("embedded release key unusable: %w", err)
	}
	checksums, err := readStagedFile(in.staged.checksums, in.maxMetaSize)
	if err != nil {
		return fmt.Errorf("checksums.txt: %w", err)
	}
	sig, err := readStagedFile(in.staged.signature, in.maxMetaSize)
	if err != nil {
		return fmt.Errorf("checksums.txt.sig: %w", err)
	}
	expected, err := in.authenticatedChecksum(pub, checksums, sig)
	if err != nil {
		return err
	}
	binary, err := readStagedFile(in.staged.binary, in.maxBinarySize)
	if err != nil {
		return fmt.Errorf("staged binary: %w", err)
	}
	if err := requireHashMatch(binary, expected); err != nil {
		return err
	}
	if in.betweenVerifyAndWrite != nil {
		in.betweenVerifyAndWrite()
	}
	if err := writeTargetAtomically(in.targetBin, binary); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	in.removeStaging()
	return nil
}

// authenticatedChecksum verifies the ed25519 signature over the exact
// checksums.txt bytes, then returns this platform's exact-basename entry.
func (in *installer) authenticatedChecksum(pub ed25519.PublicKey, checksums, sig []byte) (string, error) {
	if err := releasekey.VerifySignature(pub, checksums, sig); err != nil {
		return "", err
	}
	if in.targetAssetName == "" {
		return "", errors.New("verifier was built without a target asset name")
	}
	return releasekey.ExpectedChecksum(checksums, in.targetAssetName)
}

// requireHashMatch is the authoritative SHA-256 comparison, over the in-memory
// bytes that will be written.
func requireHashMatch(binary []byte, expectedHex string) error {
	sum := sha256.Sum256(binary)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expectedHex) {
		return fmt.Errorf("checksum mismatch: signed entry %s, staged binary %s", expectedHex, actual)
	}
	return nil
}

// removeStaging deletes the three staged files. unlink never follows a
// symlink, so a planted link is removed, not its target.
func (in *installer) removeStaging() {
	for _, path := range []string{in.staged.binary, in.staged.checksums, in.staged.signature} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			in.logger.Warn("could not remove staged file", "path", path, "error", err)
		}
	}
}

// openRegularNoFollow opens path read-only without following a final symlink
// and without blocking on a FIFO, then requires an fstat'd regular file.
func openRegularNoFollow(path string) (*os.File, fs.FileInfo, error) {
	flags := os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	return f, info, nil
}

// readStagedFile reads a bounded regular file once, fully into memory.
func readStagedFile(path string, limit int64) ([]byte, error) {
	f, info, err := openRegularNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if info.Size() > limit {
		return nil, fmt.Errorf("%s exceeds the %d byte limit", path, limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d byte limit", path, limit)
	}
	return data, nil
}

// writeTargetAtomically writes data to a fresh O_EXCL|O_NOFOLLOW temp file in
// the target's (root-owned) directory, syncs, sets 0755, and renames over the
// target. The caller's bytes are the only content written.
func writeTargetAtomically(target string, data []byte) error {
	tmpPath, err := tempPathBeside(target)
	if err != nil {
		return err
	}
	if err := writeTempFile(tmpPath, data); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

func tempPathBeside(target string) (string, error) {
	suffix := make([]byte, tempSuffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("temp name: %w", err)
	}
	return filepath.Join(filepath.Dir(target), tempFilePrefix+hex.EncodeToString(suffix)), nil
}

func writeTempFile(path string, data []byte) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	f, err := os.OpenFile(path, flags, installedFileMode)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("temp file %s is not a regular file", path)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Chmod(installedFileMode); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	return f.Close()
}

// systemdService drives the real agent unit through systemctl.
type systemdService struct {
	unit string
}

func (s systemdService) Stop() error  { return runSystemctl("stop", s.unit) }
func (s systemdService) Start() error { return runSystemctl("start", s.unit) }

func runSystemctl(action, unit string) error {
	out, err := exec.Command(systemctlBinary(), action, unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w (%s)", action, unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// systemctlBinary prefers the standard absolute locations over PATH lookup.
func systemctlBinary() string {
	for _, candidate := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "systemctl"
}
