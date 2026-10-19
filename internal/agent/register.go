package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

const (
	// identityProbeBytes is how much the pre-flight writes and syncs in each
	// directory. It must exceed what the real writes need (a 36-byte ID and a
	// config of a few hundred bytes), so a nearly full file system fails the
	// pre-flight and not the registration (GROWTH-001 SEC-016).
	identityProbeBytes = 4 << 10
	identityProbeName  = ".riot-preflight"

	// The pre-flight retries in-process: 5 s, 10 s, 20 s ... capped at 10 min.
	preflightInitialDelay = 5 * time.Second
	preflightMaxDelay     = 10 * time.Minute

	// identityExitDelay is how long the agent waits before exiting when it
	// cannot persist a new identity, so a supervisor restart loop is slowed
	// further than the unit's own RestartSec.
	identityExitDelay = 60 * time.Second
)

// errIdentityNotPersistable marks a registration whose new identity could not be
// written to disk. The agent must not keep running under an identity it cannot
// remember: after a restart it would register again and leave a ghost device.
var errIdentityNotPersistable = errors.New("device identity could not be persisted")

// identityPersistError carries the path that could not be written.
type identityPersistError struct {
	Path string
	Err  error
}

func (e *identityPersistError) Error() string {
	return fmt.Sprintf("%v: %s: %v", errIdentityNotPersistable, e.Path, e.Err)
}
func (e *identityPersistError) Unwrap() error        { return e.Err }
func (e *identityPersistError) Is(target error) bool { return target == errIdentityNotPersistable }

// stateFS returns the file system identity writes go through.
func (a *Agent) stateFS() stateFS {
	if a.fsys != nil {
		return a.fsys
	}
	return osStateFS{}
}

// identityPath is the device ID file.
func (a *Agent) identityPath() string {
	if a.idPath != "" {
		return a.idPath
	}
	return IDPath()
}

// identityPreflightOnce proves the agent can durably write in the ID directory
// and in the config directory, through the same atomic helper the real writes
// use: it writes and syncs identityProbeBytes, then removes the file.
func (a *Agent) identityPreflightOnce() error {
	payload := bytes.Repeat([]byte{'#'}, identityProbeBytes)
	for _, target := range []string{a.identityPath(), a.configPath} {
		probe := filepath.Join(filepath.Dir(target), identityProbeName)
		if err := newAtomicJSONFile(probe, a.stateFS()).SaveBytes(payload); err != nil {
			return err
		}
		if err := a.stateFS().Remove(probe); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", probe, err)
		}
	}
	return nil
}

// waitForWritableIdentity blocks until the pre-flight passes. Until then the
// agent never calls Register: each registration would create a device the agent
// then loses. It returns only a context error.
func (a *Agent) waitForWritableIdentity(ctx context.Context) error {
	delay := preflightInitialDelay
	for {
		err := a.identityPreflightOnce()
		if err == nil {
			return nil
		}
		slog.Error("cannot write the device identity; not registering",
			"error", err, "retry_in", delay.String())
		if !a.pauseFor(ctx, delay) {
			return ctx.Err()
		}
		delay = min(delay*2, preflightMaxDelay)
	}
}

func (a *Agent) register(ctx context.Context) error {
	// The persisted identity: the config's device_id, else the ID file.
	fileID := ""
	if data, err := os.ReadFile(a.identityPath()); err == nil {
		fileID = strings.TrimSpace(string(data))
	}
	persistedID := strings.TrimSpace(a.config.Agent.DeviceID)
	if persistedID == "" {
		persistedID = fileID
	}

	if persistedID == "" {
		// First registration: be sure the identity can be written before asking
		// the server to create one.
		if err := a.waitForWritableIdentity(ctx); err != nil {
			return err
		}
	}

	// Collect hardware info for registration
	sysCollector := &collectors.SystemCollector{}
	sysData, err := sysCollector.Collect(ctx)
	if err != nil {
		slog.Warn("failed to collect system info for registration", "error", err)
	}

	hostname := a.config.Agent.DeviceName
	if hostname == "" {
		hostname, _ = os.Hostname()
	}

	var hwProfile *models.HardwareProfile
	if sysInfo, ok := sysData.(*models.SystemInfo); ok && sysInfo != nil {
		hwProfile = &models.HardwareProfile{
			CPUModel:       sysInfo.CPUModel,
			CPUCores:       sysInfo.CPUCores,
			CPUThreads:     sysInfo.CPUThreads,
			TotalRAMMB:     sysInfo.TotalRAMMB,
			BoardModel:     sysInfo.BoardModel,
			SerialNumber:   sysInfo.SerialNumber,
			BIOSVersion:    sysInfo.BIOSVersion,
			BIOSDate:       sysInfo.BIOSDate,
			Virtualization: sysInfo.Virtualization,
		}
		if hostname == "" {
			hostname = sysInfo.Hostname
		}
	}

	reg := &models.DeviceRegistration{
		Hostname:        hostname,
		Arch:            sysCollector.GetArch(),
		AgentVersion:    a.version,
		Tags:            a.config.Agent.Tags,
		DeviceID:        persistedID,
		HardwareProfile: hwProfile,
	}

	resp, err := a.client.Register(ctx, reg)
	if err != nil {
		return err
	}

	if err := a.persistRegistration(persistedID, fileID, resp); err != nil {
		return err
	}

	slog.Info("registered with server",
		"device_id", resp.DeviceID,
		"short_id", resp.ShortID,
	)
	return nil
}

// persistRegistration writes what registration returned. The ID goes first: if
// the key is then lost, the next start re-registers the same device (bounded),
// whereas a lost ID would create a new device on every start.
//
// A failure is fatal only when the identity changed (a new ID, or one that
// differs from the persisted one): the agent must not run under an identity it
// cannot remember. A same-ID re-registration only warns, since the agent keeps
// the identity it already has.
func (a *Agent) persistRegistration(persistedID, fileID string, resp *models.DeviceRegistrationResponse) error {
	changed := resp.DeviceID != "" && resp.DeviceID != persistedID

	if resp.DeviceID != "" {
		a.config.Agent.DeviceID = resp.DeviceID
		if resp.DeviceID != fileID {
			if err := a.writeIdentity(resp.DeviceID); err != nil {
				if changed {
					return &identityPersistError{Path: a.identityPath(), Err: err}
				}
				slog.Warn("could not rewrite the device ID file; the device ID is unchanged",
					"path", a.identityPath(), "error", err)
			}
		}
	}

	if resp.APIKey != "" {
		a.config.Server.APIKey = resp.APIKey
		a.client.apiKey = resp.APIKey
		if err := a.config.SaveWith(a.stateFS(), a.configPath); err != nil {
			if changed {
				return &identityPersistError{Path: a.configPath, Err: err}
			}
			slog.Warn("could not save the new API key to the config; it is kept in memory only",
				"path", a.configPath, "error", err)
		}
	}
	return nil
}

// writeIdentity writes the ID file atomically (temp file, fsync, rename).
func (a *Agent) writeIdentity(id string) error {
	path := a.identityPath()
	if err := newAtomicJSONFile(path, a.stateFS()).SaveBytes([]byte(id)); err != nil {
		return err
	}
	if a.fsys == nil && runtime.GOOS != "windows" {
		// The ID is not a secret and `riot-agent doctor` reads it as whichever
		// user runs it; keep the mode the file always had.
		_ = os.Chmod(path, 0o644)
	}
	return nil
}
