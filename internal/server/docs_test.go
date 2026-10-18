package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Paths are relative to internal/server, where go test runs.
const (
	readmePath    = "../../README.md"
	changelogPath = "../../CHANGELOG.md"
)

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "reading %s", path)
	return string(raw)
}

// [HARDEN-002][AC-054] I-2: the README must not describe notification retry
// behaviour the code does not perform. Option B removed the claim.
func TestHARDEN002_AC054_ReadmeHasNoRetryClaim(t *testing.T) {
	readme := strings.ToLower(readRepoFile(t, readmePath))

	assert.NotContains(t, readme, "retry queue", "the README must not mention a notification retry queue")
	assert.NotContains(t, readme, "retried automatically", "the README must not claim automatic retries")
	assert.NotContains(t, readme, "automatic retry", "the README must not claim automatic retries")
}

// [HARDEN-002][AC-055] Option B: the dead retry queue is gone and the rest of
// internal/resilient is intact.
func TestHARDEN002_AC055_RetryQueueRemoved(t *testing.T) {
	_, err := os.Stat("../resilient/retry_queue.go")
	assert.True(t, os.IsNotExist(err), "retry_queue.go must be deleted")
	_, err = os.Stat("../resilient/retry_queue_test.go")
	assert.True(t, os.IsNotExist(err), "retry_queue_test.go must be deleted")

	for _, keep := range []string{"dns_cache.go", "dns_cache_test.go", "http_client.go", "http_client_test.go"} {
		_, err := os.Stat(filepath.Join("../resilient", keep))
		assert.NoError(t, err, "%s must be left intact (D-20)", keep)
	}

	// No symbol of the removed queue may remain anywhere in the Go tree.
	needle := "Retry" + "Queue"
	var scanned int
	err = filepath.WalkDir("../..", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++
		if d.Name() == "docs_test.go" {
			// This file's own test name (mandated by the ADD) contains the symbol.
			return nil
		}
		raw, readErr := os.ReadFile(path)
		require.NoError(t, readErr, path)
		assert.NotContains(t, string(raw), needle, "%s still references the removed queue", path)
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, scanned, 50, "the scan must cover the Go tree, scanned %d files", scanned)
}
