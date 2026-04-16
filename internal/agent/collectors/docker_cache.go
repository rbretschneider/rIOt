package collectors

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// FreshnessCachePath returns the OS-specific path for the Docker image
// freshness cache file. Mirrors the path helper pattern established by
// offsetFilePath() in webservers_nginx_access.go and config.go helpers.
func FreshnessCachePath() string {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "riot", "docker-freshness-cache.json")
	}
	return "/etc/riot/docker-freshness-cache.json"
}

// loadFreshnessCache reads the persisted freshness cache from CachePath and
// populates c.digestCache with entries that are still within the TTL (FR-013,
// FR-014). If the file is absent, unreadable, or corrupt the collector starts
// with an empty cache and logs accordingly (FR-016).
func (c *DockerCollector) loadFreshnessCache() {
	if c.CachePath == "" {
		return
	}

	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("docker: freshness cache file not found, starting empty", "path", c.CachePath)
		} else {
			slog.Warn("docker: could not read freshness cache file, starting empty", "path", c.CachePath, "error", err)
		}
		return
	}

	var loaded map[string]*imageFreshness
	if err := json.Unmarshal(data, &loaded); err != nil {
		slog.Warn("docker: freshness cache file is corrupt, starting empty", "path", c.CachePath, "error", err)
		return
	}

	// Prune entries that have exceeded the TTL (FR-014).
	pruned := make(map[string]*imageFreshness, len(loaded))
	for ref, entry := range loaded {
		if time.Since(entry.CheckedAt) < freshnessCacheTTL {
			pruned[ref] = entry
		}
	}

	c.cacheMu.Lock()
	c.digestCache = pruned
	c.cacheMu.Unlock()

	slog.Info("docker: freshness cache loaded from disk", "entries", len(pruned))
}

// saveFreshnessCache persists the in-memory freshness cache to CachePath using
// an atomic temp-file-then-rename to prevent corruption (FR-015). Entries older
// than the TTL are pruned before writing (NFR-006). All errors are logged as
// warnings — cache persistence is best-effort (AD-003).
func (c *DockerCollector) saveFreshnessCache() {
	if c.CachePath == "" {
		return
	}

	// Build a pruned snapshot under read lock.
	c.cacheMu.RLock()
	snapshot := make(map[string]*imageFreshness, len(c.digestCache))
	for ref, entry := range c.digestCache {
		if time.Since(entry.CheckedAt) < freshnessCacheTTL {
			snapshot[ref] = entry
		}
	}
	c.cacheMu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		slog.Warn("docker: failed to marshal freshness cache", "error", err)
		return
	}

	dir := filepath.Dir(c.CachePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Warn("docker: failed to create cache directory", "dir", dir, "error", err)
		return
	}

	// Atomic write: write to temp file, then rename (FR-015).
	tmp := c.CachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		slog.Warn("docker: failed to write freshness cache temp file", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, c.CachePath); err != nil {
		slog.Warn("docker: failed to rename freshness cache temp file", "path", c.CachePath, "error", err)
		// Best-effort cleanup of the orphaned temp file.
		_ = os.Remove(tmp)
	}
}

// removeFreshnessCacheFile deletes the on-disk cache file. Called by
// ClearFreshnessCache so a subsequent agent restart does not reload stale
// entries for a just-updated image (AD-006).
func removeFreshnessCacheFile(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
