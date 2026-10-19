package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zeroChunk is streamed repeatedly so a 100 MiB body costs no memory.
var zeroChunk = make([]byte, 64<<10)

// streamZeros writes n zero bytes to w, flushing, and stops early if the client
// goes away. It returns how many bytes were written.
func streamZeros(w http.ResponseWriter, r *http.Request, n int64) int64 {
	flusher, _ := w.(http.Flusher)
	var written int64
	for written < n {
		chunk := zeroChunk
		if remaining := n - written; remaining < int64(len(chunk)) {
			chunk = chunk[:remaining]
		}
		m, err := w.Write(chunk)
		written += int64(m)
		if err != nil || r.Context().Err() != nil {
			break
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return written
}

func newDownloadPipeline(t *testing.T) (*updatePipeline, stagingPaths) {
	t.Helper()
	dir := t.TempDir()
	paths := stagingPaths{
		binary:    filepath.Join(dir, "riot-agent.update"),
		checksums: filepath.Join(dir, "riot-agent.update.checksums"),
		signature: filepath.Join(dir, "riot-agent.update.sig"),
	}
	return &updatePipeline{paths: paths}, paths
}

func TestGROWTH_AC066_DownloadStopsAt64MiBNoPartial(t *testing.T) {
	t.Run("an unannounced 100 MiB stream fails at the limit and leaves no partial file", func(t *testing.T) {
		captureLogs(t)
		var sent atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// No Content-Length: chunked, so only the reader-side cap can stop it.
			sent.Store(streamZeros(w, r, 100<<20))
		}))
		defer srv.Close()
		p, paths := newDownloadPipeline(t)

		_, err := p.downloadBinary(context.Background(), srv.URL)

		require.Error(t, err, "[AC-066] a download past 64 MiB must fail")
		assert.Contains(t, err.Error(), "too large")
		_, statErr := os.Stat(paths.binary)
		assert.True(t, os.IsNotExist(statErr), "[AC-066] the partial file must be deleted, stat error: %v", statErr)
		assert.Less(t, sent.Load(), int64(100<<20), "[AC-066] the agent must stop reading, not drain the whole stream")
	})

	t.Run("an announced oversize body is refused without being read", func(t *testing.T) {
		captureLogs(t)
		var sent atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "104857600")
			sent.Store(streamZeros(w, r, 100<<20))
		}))
		defer srv.Close()
		p, paths := newDownloadPipeline(t)

		_, err := p.downloadBinary(context.Background(), srv.URL)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "announced 104857600")
		_, statErr := os.Stat(paths.binary)
		assert.True(t, os.IsNotExist(statErr), "[AC-066] no partial file may remain")
		assert.Less(t, sent.Load(), int64(16<<20), "[AC-066] an announced oversize body must not be downloaded")
	})

	t.Run("exactly 64 MiB is accepted", func(t *testing.T) {
		captureLogs(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			streamZeros(w, r, maxDownloadBytes)
		}))
		defer srv.Close()
		p, paths := newDownloadPipeline(t)

		sum, err := p.downloadBinary(context.Background(), srv.URL)

		require.NoError(t, err, "[AC-066] the limit itself is allowed")
		assert.NotEmpty(t, sum)
		info, statErr := os.Stat(paths.binary)
		require.NoError(t, statErr)
		assert.Equal(t, maxDownloadBytes, info.Size())
	})

	t.Run("one byte over the limit fails", func(t *testing.T) {
		captureLogs(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			streamZeros(w, r, maxDownloadBytes+1)
		}))
		defer srv.Close()
		p, paths := newDownloadPipeline(t)

		_, err := p.downloadBinary(context.Background(), srv.URL)

		require.Error(t, err, "[AC-066] 64 MiB + 1 must fail")
		_, statErr := os.Stat(paths.binary)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("the pipeline leaves none of the three staging files and reports the failure", func(t *testing.T) {
		captureLogs(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			streamZeros(w, r, 100<<20)
		}))
		defer srv.Close()
		p, paths := newDownloadPipeline(t)

		err := p.run(context.Background(), srv.URL, "http://unused.invalid/checksums.txt", "http://unused.invalid/checksums.txt.sig", "linux-amd64")

		require.Error(t, err, "[AC-066] the update must be reported as failed (runUpdateCheck turns this error into agent_update_failed)")
		for _, path := range []string{paths.binary, paths.checksums, paths.signature} {
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr), "[AC-066] %s must not remain", filepath.Base(path))
		}
	})
}

func TestGROWTH_AC066_FailedDownloadIsReportedAsAgentUpdateFailed(t *testing.T) {
	captureLogs(t)
	var (
		eventsMu sync.Mutex
		events   []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/update/check"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"latest_version":"9.9.9","update_available":true,"assets":{"` +
				platformSuffix("") + `":"http://` + r.Host + `/big"}}`))
		case strings.HasSuffix(r.URL.Path, "/events"):
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			eventsMu.Lock()
			events = append(events, string(buf[:n]))
			eventsMu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/big":
			streamZeros(w, r, 100<<20)
		}
	}))
	defer srv.Close()

	a := &Agent{config: &Config{}, version: "1.0.0"}
	a.config.Agent.DeviceID = "dev"
	a.client = NewHTTPClient(srv.URL, "k")
	p, _ := newDownloadPipeline(t)
	apply := func(ctx context.Context, downloadURL, checksumURL, signatureURL, suffix string) error {
		return p.run(ctx, downloadURL, checksumURL, signatureURL, suffix)
	}
	restarted := false

	a.runUpdateCheck(context.Background(), apply, func() { restarted = true })

	assert.False(t, restarted, "[AC-066] a failed update must not restart the agent")
	eventsMu.Lock()
	joined := strings.Join(events, "\n")
	eventsMu.Unlock()
	assert.Contains(t, joined, "agent_update_failed", "[AC-066] the failure must be reported to the server")
}

func TestGROWTH_AC067_AgentStartSweepsStaleStaging(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	setup := func(t *testing.T, age time.Duration) stagingPaths {
		t.Helper()
		dir := t.TempDir()
		paths := stagingPaths{
			binary:    filepath.Join(dir, "riot-agent.update"),
			checksums: filepath.Join(dir, "riot-agent.update.checksums"),
			signature: filepath.Join(dir, "riot-agent.update.sig"),
		}
		for _, p := range []string{paths.binary, paths.checksums, paths.signature} {
			require.NoError(t, os.WriteFile(p, []byte("staged"), 0o600))
			require.NoError(t, os.Chtimes(p, now.Add(-age), now.Add(-age)))
		}
		return paths
	}
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }

	t.Run("files older than 1 h are deleted", func(t *testing.T) {
		captureLogs(t)
		paths := setup(t, 2*time.Hour)

		sweepStaleStaging(paths, now, staleStagingAge)

		for _, p := range []string{paths.binary, paths.checksums, paths.signature} {
			assert.False(t, exists(p), "[AC-067] the 2 h old %s must be deleted", filepath.Base(p))
		}
	})

	t.Run("files 10 minutes old remain", func(t *testing.T) {
		captureLogs(t)
		paths := setup(t, 10*time.Minute)

		sweepStaleStaging(paths, now, staleStagingAge)

		for _, p := range []string{paths.binary, paths.checksums, paths.signature} {
			assert.True(t, exists(p), "[AC-067] the 10 min old %s may belong to an update in flight", filepath.Base(p))
		}
	})

	t.Run("a file exactly 1 h old is kept and one second older is deleted", func(t *testing.T) {
		captureLogs(t)
		paths := setup(t, time.Hour)
		sweepStaleStaging(paths, now, staleStagingAge)
		assert.True(t, exists(paths.binary), "[AC-067] the boundary itself is not stale")

		sweepStaleStaging(paths, now.Add(time.Second), staleStagingAge)
		assert.False(t, exists(paths.binary), "[AC-067] one second past the boundary it is stale")
	})

	t.Run("a directory at a staging path is left alone", func(t *testing.T) {
		captureLogs(t)
		dir := t.TempDir()
		paths := stagingPaths{binary: filepath.Join(dir, "riot-agent.update")}
		require.NoError(t, os.Mkdir(paths.binary, 0o700))
		require.NoError(t, os.Chtimes(paths.binary, now.Add(-3*time.Hour), now.Add(-3*time.Hour)))

		sweepStaleStaging(paths, now, staleStagingAge)

		assert.True(t, exists(paths.binary), "[AC-067] only regular files are swept")
	})

	t.Run("a symlink at a staging path is left alone and its target survives", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("creating symlinks needs elevated rights on Windows")
		}
		captureLogs(t)
		dir := t.TempDir()
		target := filepath.Join(dir, "precious")
		require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
		require.NoError(t, os.Chtimes(target, now.Add(-3*time.Hour), now.Add(-3*time.Hour)))
		paths := stagingPaths{binary: filepath.Join(dir, "riot-agent.update")}
		require.NoError(t, os.Symlink(target, paths.binary))

		sweepStaleStaging(paths, now, staleStagingAge)

		assert.True(t, exists(target), "[AC-067] the link target must never be touched")
		assert.True(t, exists(paths.binary))
	})

	t.Run("missing files are not an error", func(t *testing.T) {
		captureLogs(t)
		sweepStaleStaging(stagingPaths{binary: filepath.Join(t.TempDir(), "nope")}, now, staleStagingAge)
	})
}
