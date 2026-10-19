package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// sanitizedComposeName is the fixed name of the sanitized copy. It stays in the
// compose working directory because relative `extends`, `include` and
// `env_file` paths resolve against the compose file's own location.
const sanitizedComposeName = ".compose.riot-sanitized.yml"

// sanitizeComposeFile creates a copy of the first validated compose file in the
// validated directory with problematic keys removed (version, deploy sections)
// so that Docker Compose doesn't reject files with Swarm-only config in
// non-Swarm environments. It accepts only a trustedDir, so the copy is never
// written into a directory that failed validation (AUTOUPD-HARDEN-001 AD-010).
// The copy is created exclusively with mode 0600 (SEC C-5). The caller records
// the path in the in-flight marker first and defers removeSanitized.
func sanitizeComposeFile(td trustedDir) (string, error) {
	if !td.valid() {
		return "", errors.New("compose directory has not been validated")
	}
	composeFile := td.Files()[0]
	workDir := td.Path()

	data, err := os.ReadFile(composeFile)
	if err != nil {
		return "", err
	}

	var compose map[string]interface{}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return "", fmt.Errorf("parse compose file: %w", err)
	}

	// Remove version key — without it, Docker Compose uses compose-spec which is more lenient
	delete(compose, "version")

	// Remove deploy sections from all services — these are Swarm-only and cause
	// validation errors in non-Swarm Docker Compose environments
	if services, ok := compose["services"].(map[string]interface{}); ok {
		for svcName, svc := range services {
			if svcMap, ok := svc.(map[string]interface{}); ok {
				delete(svcMap, "deploy")
				services[svcName] = svcMap
			}
		}
	}

	out, err := yaml.Marshal(compose)
	if err != nil {
		return "", fmt.Errorf("marshal sanitized compose: %w", err)
	}

	path, err := writeSanitizedCompose(workDir, out)
	if err != nil {
		return "", err
	}
	slog.Info("docker_update: created sanitized compose file", "original", composeFile, "sanitized", path)
	return path, nil
}

// openFunc opens a file; os.OpenFile in production, a recorder in tests.
type openFunc func(name string, flag int, perm os.FileMode) (*os.File, error)

// writeSanitizedCompose writes data to <workDir>/.compose.riot-sanitized.yml
// (SEC C-5). A leftover regular file owned by the agent (a crash remnant) is
// removed; anything else at that path, in particular a symlink, fails the step.
// The file is then created with O_EXCL, which never follows a symlink, and mode
// 0600.
func writeSanitizedCompose(workDir string, data []byte) (string, error) {
	return writeSanitizedComposeWith(os.OpenFile, workDir, data)
}

func writeSanitizedComposeWith(open openFunc, workDir string, data []byte) (string, error) {
	path := filepath.Join(workDir, sanitizedComposeName)
	refusal := fmt.Errorf("refusing to write sanitized compose file: %s exists and is not an agent-owned regular file", path)

	fi, err := os.Lstat(path)
	switch {
	case err == nil:
		if !fi.Mode().IsRegular() || !ownedByEUID(fi) {
			return "", refusal
		}
		if err := os.Remove(path); err != nil {
			return "", fmt.Errorf("remove stale sanitized compose file: %w", err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}

	f, err := open(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create sanitized compose file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		removeSanitized(path)
		return "", fmt.Errorf("write sanitized compose file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		removeSanitized(path)
		return "", fmt.Errorf("sync sanitized compose file: %w", err)
	}
	if err := f.Close(); err != nil {
		removeSanitized(path)
		return "", fmt.Errorf("close sanitized compose file: %w", err)
	}
	return path, nil
}

// removeSanitized deletes the sanitized copy. A symlink at the path is unlinked
// and its target is never touched. A missing file is not an error.
func removeSanitized(path string) {
	if path == "" {
		return
	}
	fi, err := os.Lstat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("docker_update: cannot inspect sanitized compose file", "path", path, "error", err)
		}
		return
	}
	if !fi.Mode().IsRegular() && fi.Mode()&os.ModeSymlink == 0 {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("docker_update: cannot remove sanitized compose file", "path", path, "error", err)
	}
}

// removeLeftoverSanitized is the startup-cleanup variant: it removes an
// agent-owned regular file or unlinks a symlink at <dir>/.compose.riot-sanitized.yml
// and leaves anything else alone.
func removeLeftoverSanitized(dir string) bool {
	path := filepath.Join(dir, sanitizedComposeName)
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0, fi.Mode().IsRegular() && ownedByEUID(fi):
		if err := os.Remove(path); err != nil {
			slog.Warn("recovery: cannot remove leftover sanitized compose file", "path", path, "error", err)
			return false
		}
		return true
	}
	return false
}
