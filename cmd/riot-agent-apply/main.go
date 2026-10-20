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

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
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

	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	exitDeferred = 75

	installedFileMode fs.FileMode = 0o755
	tempSuffixBytes               = 8
	tempFilePrefix                = ".riot-agent.apply."

	// tempNameHexLen is the number of hex characters tempPathBeside appends. It
	// is derived from tempSuffixBytes so the sweep's name match can never drift
	// from the name the installer produces (GROWTH-001 SEC-023).
	tempNameHexLen = 2 * tempSuffixBytes

	// staleTempAge is how old a leftover temp file must be before the sweep
	// removes it, so a concurrent install's file is never touched.
	staleTempAge = time.Hour
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
	// Production always wires these; fixtures inject harmless lock/status seams.
	acquireMaintenance func() (io.Closer, error)
	maintenanceIdle    func() error
	writeUpdateStatus  func(maintenance.UpdateStatus) error
	verifiedSHA256     string
	stagedSHA256       string
	staged             stagedInputs
	targetBin          string
	targetAssetName    string
	publicKey          func() (ed25519.PublicKey, error)
	maxBinarySize      int64
	maxMetaSize        int64
	logger             *slog.Logger

	// betweenVerifyAndWrite is a test seam invoked after the bytes are
	// verified and before they are written. Production leaves it nil.
	betweenVerifyAndWrite func()

	// dirOwnerUID is the owner the target directory must have for the stale
	// temp sweep to run. The zero value is root, which is what production uses;
	// tests set it to the test user.
	dirOwnerUID uint32
	// now is the sweep's clock; nil means time.Now.
	now func() time.Time
}

// serviceController starts and stops the agent service.
type serviceController interface {
	Stop() error
	Start() error
}

func newProductionInstaller(logger *slog.Logger) *installer {
	return &installer{
		acquireMaintenance: func() (io.Closer, error) { return maintenance.AcquireLock() },
		maintenanceIdle:    requireMaintenanceIdle,
		writeUpdateStatus:  maintenance.WriteUpdateStatus,
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
	case len(args) == 1 && args[0] == "--maintenance-protocol":
		fmt.Fprintln(os.Stdout, maintenance.ProtocolVersion)
		return exitOK
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
	unlock, err := in.maintenanceExclusion()
	if err != nil {
		in.noteUpdateStatus("deferred", "", err.Error())
		in.logger.Warn("agent replacement deferred", "error", err)
		return exitDeferred
	}
	defer unlock()
	if err := in.updateStatus("accepted", "", "signed updater accepted handoff"); err != nil {
		in.logger.Error("cannot persist update handoff", "error", err)
		return exitFailure
	}
	in.sweepStaleTemps()
	sleep(applyStopDelay)
	if err := svc.Stop(); err != nil {
		in.logger.Error("could not stop service; refusing replacement", "error", err)
		in.noteUpdateStatus("error", "", "cannot stop agent before replacement")
		if startErr := svc.Start(); startErr != nil {
			in.logger.Error("could not restore current agent service", "error", startErr)
		}
		return exitFailure
	}

	code := exitOK
	if err := in.verifyAndInstall(); err != nil {
		in.logger.Error("update refused", "error", err)
		in.removeStaging()
		code = exitFailure
		in.noteUpdateStatus("error", "", err.Error())
	} else {
		if err := in.updateStatus("installed", in.verifiedSHA256, "signed agent installed"); err != nil {
			in.logger.Error("cannot persist installed outcome", "error", err)
			code = exitFailure
		}
		in.logger.Info("update installed", "target", in.targetBin)
	}

	if err := svc.Start(); err != nil {
		in.logger.Error("could not start service", "error", err)
		in.noteUpdateStatus("error", "", "agent installed but service start failed")
		code = exitFailure
	}
	return code
}

// runBoot applies a pending staged update at service start. It never blocks
// the current agent from starting: every outcome returns exitOK (NFR-005).
func runBoot(in *installer) int {
	unlock, err := in.maintenanceExclusion()
	if err != nil {
		in.logger.Info("boot update deferred while maintenance is reserved", "error", err)
		return exitOK
	}
	defer unlock()
	in.sweepStaleTemps()
	if _, err := os.Lstat(in.staged.binary); errors.Is(err, fs.ErrNotExist) {
		return exitOK
	}
	if err := in.verifyAndInstall(); err != nil {
		in.logger.Error("staged update refused at boot", "error", err)
		in.noteUpdateStatus("error", "", err.Error())
		in.removeStaging()
		return exitOK
	}
	if err := in.updateStatus("installed", in.verifiedSHA256, "signed agent installed at boot"); err != nil {
		in.logger.Error("cannot persist boot update outcome", "error", err)
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
	in.verifiedSHA256 = strings.ToLower(expected)
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

// isTempName reports whether name is exactly what tempPathBeside produces:
// tempFilePrefix followed by tempNameHexLen lowercase hex characters.
func isTempName(name string) bool {
	suffix, ok := strings.CutPrefix(name, tempFilePrefix)
	if !ok || len(suffix) != tempNameHexLen {
		return false
	}
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// sweepStaleTemps removes temp files an interrupted install left beside the
// target (GROWTH-001 FR-073, SEC-023). It is defence in depth for a root
// process, so it removes only when everything is as expected:
//
//   - the target directory is owned by root and not writable by group or
//     others, so no unprivileged user can plant or swap entries in it;
//   - the entry name matches tempPathBeside's output exactly;
//   - Lstat says it is a regular file (a symlink or directory is left alone);
//   - it is more than staleTempAge old.
//
// Removal is os.Remove, which never follows a link. Every problem is logged and
// skipped: the sweep must never get in the way of an update or of a start.
func (in *installer) sweepStaleTemps() {
	dir := filepath.Dir(in.targetBin)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != in.dirOwnerUID || info.Mode().Perm()&0o022 != 0 {
		in.logger.Warn("skipping the stale temp sweep: the directory is not owned by root or is writable by others", "dir", dir)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		in.logger.Warn("could not list the target directory for the stale temp sweep", "dir", dir, "error", err)
		return
	}
	now := time.Now
	if in.now != nil {
		now = in.now
	}
	for _, entry := range entries {
		if !isTempName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if now().Sub(fi.ModTime()) <= staleTempAge {
			continue
		}
		if err := os.Remove(path); err != nil {
			in.logger.Warn("could not remove a stale temp file", "path", path, "error", err)
			continue
		}
		in.logger.Info("removed a stale temp file", "path", path)
	}
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
