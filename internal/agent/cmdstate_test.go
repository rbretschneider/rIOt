package agent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func newTestSeenStore(t *testing.T) (*seenStore, *fakeClock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "seen-commands.json")
	clk := newFakeClock()
	return newSeenStore(path, osStateFS{}, clk), clk, path
}

// AC-067 (FR-057): a command ID is accepted once; the record survives restart.
func TestCL_AC067_SeenStoreRemembersAcrossRestart(t *testing.T) {
	s, clk, path := newTestSeenStore(t)

	dup, err := s.CheckAndAdd(idA)
	require.NoError(t, err)
	assert.False(t, dup, "[AC-067] the first sighting is not a duplicate")
	dup, err = s.CheckAndAdd(idA)
	require.NoError(t, err)
	assert.True(t, dup, "[AC-067] the second sighting is a duplicate")

	restarted := newSeenStore(path, osStateFS{}, clk)
	require.NoError(t, restarted.Load())
	dup, err = restarted.CheckAndAdd(idA)
	require.NoError(t, err)
	assert.True(t, dup, "[AC-067] a restarted agent must still refuse the same ID")
}

// AC-067 (FR-057, NFR-008): at most 1000 entries, oldest dropped first.
func TestCL_AC067_SeenStoreCapsAtThousandEntries(t *testing.T) {
	clk := newFakeClock()
	s := newSeenStore("/state/seen-commands.json", newMemStateFS(), clk)
	for i := 1; i <= models.SeenMaxEntries+1; i++ {
		_, err := s.CheckAndAdd(uuidN(i))
		require.NoError(t, err)
		clk.Advance(time.Second)
	}

	assert.False(t, s.Contains(uuidN(1)), "[AC-067] the oldest ID is dropped at the 1001st entry")
	assert.True(t, s.Contains(uuidN(2)))
	assert.True(t, s.Contains(uuidN(models.SeenMaxEntries+1)))
	assert.Len(t, s.seen, models.SeenMaxEntries, "[AC-067] the store must hold exactly 1000 IDs")
}

// AC-067 (FR-057): entries age out after 48h, on load and at runtime.
func TestCL_AC067_SeenStorePrunesAfterRetention(t *testing.T) {
	s, clk, path := newTestSeenStore(t)
	_, err := s.CheckAndAdd(idA)
	require.NoError(t, err)

	clk.Advance(models.SeenMaxAge)
	assert.True(t, s.Contains(idA), "[AC-067] an ID exactly at the retention limit is still remembered")

	clk.Advance(time.Second)
	reloaded := newSeenStore(path, osStateFS{}, clk)
	require.NoError(t, reloaded.Load())
	assert.False(t, reloaded.Contains(idA), "[AC-067] an ID older than 48h must be dropped on load")

	dup, err := s.CheckAndAdd(idA)
	require.NoError(t, err)
	assert.False(t, dup, "[AC-067] an expired ID may be accepted again")
}

// SEC C-8: a failed save is reported, but the ID is still remembered in memory.
func TestCL_AC067_SeenStoreReportsPersistFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen-commands.json")
	fs := newFaultFS()
	fs.failOn("rename", errors.New("no space left on device"))
	s := newSeenStore(path, fs, newFakeClock())

	dup, err := s.CheckAndAdd(idA)

	require.Error(t, err, "[SEC-008] a persistence failure must be returned to the caller")
	assert.False(t, dup)
	again, _ := s.CheckAndAdd(idA)
	assert.True(t, again, "[SEC-008] the ID must still be remembered in memory so this process never runs it twice")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "[SEC-008] a failed rename must not leave a partial target file")
}

// SEC C-8: file fsync, then rename, then directory fsync, in that order.
func TestCL_SEC008_AtomicWriteOrder(t *testing.T) {
	fs := newFaultFS()
	f := newAtomicJSONFile(filepath.Join(t.TempDir(), "doc.json"), fs)

	require.NoError(t, f.Save(map[string]int{"a": 1}))

	assert.Equal(t, []string{"mkdir", "create", "write", "sync", "close", "rename", "syncdir"}, fs.ops(),
		"[SEC-008] durable write order is temp create, write, fsync, close, rename, directory fsync")
}

// SEC C-8: a failed write leaves the previous document intact and cleans up
// its temp file.
func TestCL_SEC008_FailedWriteKeepsPreviousFileAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	fs := newFaultFS()
	f := newAtomicJSONFile(path, fs)
	require.NoError(t, f.Save(map[string]int{"v": 1}))

	fs.failOn("sync", errors.New("input/output error"))
	require.Error(t, f.Save(map[string]int{"v": 2}), "[SEC-008] a failed fsync must fail the save")

	var got map[string]int
	found, err := newAtomicJSONFile(path, osStateFS{}).Load(&got)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 1, got["v"], "[SEC-008] the previous document must survive a failed save")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "[SEC-008] the failed temp file must be removed")
}

// AC-021 (FR-019): in-flight markers persist, update in place and reload.
func TestCL_AC021_InflightMarkerSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inflight-commands.json")
	s := newInflightStore(path, osStateFS{})
	started := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.Put(idA, inflightMarker{Action: "docker_update", Target: "web", StartedAt: started}))
	require.NoError(t, s.SetStep(idA, "recreate"))
	require.NoError(t, s.UpdateDocker(idA, "web", func(r *dockerRecord) {
		r.OriginalName, r.OldID, r.SetAsideName, r.WasRunning = "web", "old-id", "web.riot-old-11111111", true
		r.StoppedDependentIDs = []string{"dep-1"}
	}))

	restarted := newInflightStore(path, osStateFS{})
	require.NoError(t, restarted.Load())
	m, ok := restarted.Get(idA)
	require.True(t, ok, "[AC-021] the marker must survive a restart")
	assert.Equal(t, "recreate", m.Step, "[AC-021] the last recorded step must be persisted")
	require.Len(t, m.Docker, 1)
	assert.Equal(t, "web.riot-old-11111111", m.Docker[0].SetAsideName)
	assert.Equal(t, []string{"dep-1"}, m.Docker[0].StoppedDependentIDs)

	require.NoError(t, restarted.Remove(idA))
	_, ok = restarted.Get(idA)
	assert.False(t, ok, "[AC-021] a removed marker must be gone")
}

// AC-021: UpdateDocker reuses the record with the same key and creates one per
// key (bulk keeps one record per container).
func TestCL_AC021_InflightDockerRecordsAreKeyed(t *testing.T) {
	s := newInflightStore(filepath.Join(t.TempDir(), "inflight-commands.json"), osStateFS{})
	require.NoError(t, s.Put(idA, inflightMarker{Action: "docker_bulk_update"}))

	require.NoError(t, s.UpdateDocker(idA, "web", func(r *dockerRecord) { r.OldID = "1" }))
	require.NoError(t, s.UpdateDocker(idA, "db", func(r *dockerRecord) { r.OldID = "2" }))
	require.NoError(t, s.UpdateDocker(idA, "web", func(r *dockerRecord) { r.ReplacementID = "3" }))

	m, _ := s.Get(idA)
	require.Len(t, m.Docker, 2, "[AC-021] one record per container key")
	assert.Equal(t, "1", m.Docker[0].OldID)
	assert.Equal(t, "3", m.Docker[0].ReplacementID, "[AC-021] a second update of the same key must modify the same record")
	assert.Error(t, s.UpdateDocker("missing", "web", func(*dockerRecord) {}),
		"[AC-021] updating a command without a marker must fail loudly")
}

// SEC (C-11): the three state files are created with mode 0600 through
// exclusive-create temp files with random names. Linux only: Windows has no
// POSIX mode bits.
func TestCL_SEC_AgentStateFilesMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not available on Windows")
	}
	dir := t.TempDir()
	fs := newFaultFS()
	clk := newFakeClock()

	seen := newSeenStore(filepath.Join(dir, "seen-commands.json"), fs, clk)
	_, err := seen.CheckAndAdd(idA)
	require.NoError(t, err)
	inflight := newInflightStore(filepath.Join(dir, "inflight-commands.json"), fs)
	require.NoError(t, inflight.Put(idA, inflightMarker{Action: "fetch_logs"}))
	ob := newOutbox(filepath.Join(dir, "command-outbox.json"), fs, clk, &fakeTransport{})
	require.NoError(t, ob.Add(testResult(idA)))
	require.NoError(t, inflight.SetStep(idA, "pull")) // a second write must use a different temp name

	for _, name := range []string{"seen-commands.json", "inflight-commands.json", "command-outbox.json"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), fi.Mode().Perm(), "[SEC] %s must be mode 0600", name)
	}
	seenTemps := map[string]bool{}
	for _, tmp := range fs.temps {
		assert.True(t, strings.Contains(tmp, ".json.tmp-"), "[SEC] temp file %q must use the .tmp-<random> pattern", tmp)
		assert.False(t, seenTemps[tmp], "[SEC] temp name %q was reused: names must be random", tmp)
		seenTemps[tmp] = true
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 3, "[SEC] no temp file may be left behind")
}
