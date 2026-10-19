package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// bufSnap builds a snapshot whose encoded size is dominated by payloadBytes.
func bufSnap(label string, payloadBytes int) *models.TelemetrySnapshot {
	return &models.TelemetrySnapshot{
		DeviceID:  label,
		Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Data: models.FullTelemetryData{
			Logs: []models.LogEntry{{Message: strings.Repeat("x", payloadBytes)}},
		},
	}
}

func openTestBuffer(t *testing.T, path string, maxBytes int64) *Buffer {
	t.Helper()
	b, err := NewBufferWithLimit(path, maxBytes)
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	return b
}

// drain returns the labels of every queued item, oldest first, without
// removing anything.
func drainLabels(t *testing.T, b *Buffer) []string {
	t.Helper()
	var labels []string
	rows, err := b.db.Query("SELECT payload FROM buffer ORDER BY id ASC")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var payload string
		require.NoError(t, rows.Scan(&payload))
		i := strings.Index(payload, `"device_id":"`)
		require.GreaterOrEqual(t, i, 0)
		rest := payload[i+len(`"device_id":"`):]
		labels = append(labels, rest[:strings.Index(rest, `"`)])
	}
	return labels
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Size()
}

func TestGROWTH_AC060_BufferByteCapEvictsOldest(t *testing.T) {
	t.Run("oldest items are evicted until the new item fits and a Warn is logged", func(t *testing.T) {
		logs := captureLogs(t)
		const cap = 1 << 20
		b := openTestBuffer(t, filepath.Join(t.TempDir(), "buffer.db"), cap)

		for i := 1; i <= 5; i++ {
			require.NoError(t, b.Store(bufSnap(fmt.Sprintf("item-%d", i), 300_000)))
		}

		assert.LessOrEqual(t, b.Bytes(), int64(cap), "[AC-060] queued payload must never exceed the cap")
		assert.Equal(t, []string{"item-3", "item-4", "item-5"}, drainLabels(t, b),
			"[AC-060] the oldest items go first and the newest are kept")
		assert.Equal(t, 3, b.Len(), "[AC-060] the in-memory count must match the table")
		assert.Contains(t, logs.String(), "level=WARN", "[AC-060] eviction must be logged as a Warn")
		assert.Contains(t, logs.String(), "dropped the oldest items")
	})

	t.Run("the item count cap still holds", func(t *testing.T) {
		captureLogs(t)
		b := openTestBuffer(t, filepath.Join(t.TempDir(), "buffer.db"), 50<<20)
		for i := 1; i <= maxBuffered+1; i++ {
			require.NoError(t, b.Store(bufSnap(fmt.Sprintf("item-%03d", i), 10)))
		}
		labels := drainLabels(t, b)
		assert.Len(t, labels, maxBuffered, "[AC-060] the 101st item must evict the oldest")
		assert.Equal(t, "item-002", labels[0])
		assert.Equal(t, "item-101", labels[len(labels)-1])
	})

	t.Run("an item bigger than the whole cap is refused and nothing else is lost", func(t *testing.T) {
		captureLogs(t)
		b := openTestBuffer(t, filepath.Join(t.TempDir(), "buffer.db"), 100_000)
		require.NoError(t, b.Store(bufSnap("keep", 1000)))

		err := b.Store(bufSnap("huge", 200_000))

		require.ErrorIs(t, err, errItemTooLarge, "[AC-060] an item that can never fit must be refused, not stored")
		assert.Equal(t, []string{"keep"}, drainLabels(t, b), "[AC-060] a refused item must not evict valid ones")
	})

	t.Run("counters survive a restart", func(t *testing.T) {
		captureLogs(t)
		path := filepath.Join(t.TempDir(), "buffer.db")
		b, err := NewBufferWithLimit(path, 1<<20)
		require.NoError(t, err)
		require.NoError(t, b.Store(bufSnap("a", 400_000)))
		require.NoError(t, b.Store(bufSnap("b", 400_000)))
		require.NoError(t, b.Close())

		b = openTestBuffer(t, path, 1<<20)
		assert.Equal(t, 2, b.Len())
		require.NoError(t, b.Store(bufSnap("c", 400_000)))
		assert.Equal(t, []string{"b", "c"}, drainLabels(t, b),
			"[AC-060] the byte total must be recomputed from the file on start, so the cap holds across restarts")
	})
}

// legacyBufferFile writes a 40 MiB buffer in the pre-GROWTH layout (no
// auto_vacuum) and then deletes every row, the state of an old agent after it
// delivered everything: all the space is free but the file never shrank.
func legacyBufferFile(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE buffer (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		payload TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	chunk := strings.Repeat("y", 4<<20)
	for i := 0; i < 10; i++ {
		_, err = db.Exec("INSERT INTO buffer (payload) VALUES (?)", chunk)
		require.NoError(t, err)
	}
	_, err = db.Exec("DELETE FROM buffer")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.Greater(t, fileSize(t, path), int64(30<<20), "test setup: the old file must still hold its high-water mark")
}

func TestGROWTH_AC064_BufferFileShrinksAfterDelivery(t *testing.T) {
	t.Run("an existing 40 MiB file is converted on the first start and ends below 1 MiB", func(t *testing.T) {
		captureLogs(t)
		path := filepath.Join(t.TempDir(), "buffer.db")
		legacyBufferFile(t, path)

		b := openTestBuffer(t, path, 50<<20)
		b.Compact() // what the flush does after delivering

		assert.Less(t, fileSize(t, path), int64(1<<20),
			"[AC-064] the file must be reclaimed once nothing is queued")
		var mode int
		require.NoError(t, b.db.QueryRow("PRAGMA auto_vacuum").Scan(&mode))
		assert.Equal(t, autoVacuumIncremental, mode, "[AC-064] the existing file must now use incremental auto_vacuum")
	})

	t.Run("a new file shrinks after the items are delivered and compacted", func(t *testing.T) {
		captureLogs(t)
		path := filepath.Join(t.TempDir(), "buffer.db")
		b := openTestBuffer(t, path, 50<<20)
		for i := 0; i < 8; i++ {
			require.NoError(t, b.Store(bufSnap(fmt.Sprintf("i%d", i), 2<<20)))
		}
		require.Greater(t, fileSize(t, path), int64(14<<20), "test setup: the file must have grown")

		for {
			item, err := b.Oldest()
			require.NoError(t, err)
			if item == nil {
				break
			}
			require.NoError(t, b.Delete(item.ID))
		}
		b.Compact()

		assert.Less(t, fileSize(t, path), int64(1<<20), "[AC-064] delivered items must not leave a high-water-mark file behind")
		assert.Equal(t, 0, b.Len())
		assert.Equal(t, int64(0), b.Bytes())
	})
}

func TestGROWTH_AC065_CorruptBufferRecoveredOneAsideSet(t *testing.T) {
	garbage := []byte(strings.Repeat("this is not a sqlite database ", 200))

	t.Run("a corrupt file is replaced by an empty buffer with one Error", func(t *testing.T) {
		logs := captureLogs(t)
		path := filepath.Join(t.TempDir(), "buffer.db")
		require.NoError(t, os.WriteFile(path, garbage, 0o600))

		b := openTestBuffer(t, path, 50<<20)

		assert.Equal(t, 0, b.Len(), "[AC-065] the agent must run with a new empty buffer")
		require.NoError(t, b.Store(bufSnap("fresh", 100)), "[AC-065] the new buffer must be usable")
		assert.Equal(t, 1, strings.Count(logs.String(), "level=ERROR"), "[AC-065] exactly one Error log")
		aside, err := os.ReadFile(path + ".corrupt")
		require.NoError(t, err, "[AC-065] the corrupt file must be kept aside for inspection")
		assert.Equal(t, garbage, aside)
	})

	t.Run("a second corrupt start leaves at most one aside set", func(t *testing.T) {
		captureLogs(t)
		dir := t.TempDir()
		path := filepath.Join(dir, "buffer.db")

		require.NoError(t, os.WriteFile(path, garbage, 0o600))
		first, err := NewBufferWithLimit(path, 50<<20)
		require.NoError(t, err)
		require.NoError(t, first.Close())

		second := append([]byte("SECOND-"), garbage...)
		require.NoError(t, os.WriteFile(path, second, 0o600))
		b := openTestBuffer(t, path, 50<<20)
		require.NoError(t, b.Close())

		matches, err := filepath.Glob(path + ".corrupt*")
		require.NoError(t, err)
		require.Len(t, matches, 1, "[AC-065] only one moved-aside file may exist, got %v", matches)
		aside, err := os.ReadFile(matches[0])
		require.NoError(t, err)
		assert.Equal(t, second, aside, "[AC-065] the aside set must be the most recent one")
	})
}

func TestGROWTH_SEC022_SiblingFilesMovedVacuumENOSPCContinues(t *testing.T) {
	t.Run("moveBufferAside moves the journal wal and shm siblings with the main file and replaces the previous set", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "buffer.db")

		// A previous aside set that must be replaced, including a sibling the
		// new set does not have.
		require.NoError(t, os.WriteFile(path+".corrupt", []byte("old-main"), 0o600))
		require.NoError(t, os.WriteFile(path+".corrupt-journal", []byte("old-journal"), 0o600))
		for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
			if suffix == "-journal" {
				continue // the new set has no journal
			}
			require.NoError(t, os.WriteFile(path+suffix, []byte("new"+suffix), 0o600))
		}

		require.NoError(t, moveBufferAside(path))

		for _, suffix := range []string{"", "-wal", "-shm"} {
			data, err := os.ReadFile(path + ".corrupt" + suffix)
			require.NoError(t, err, "[SEC-022] sibling %q must be moved aside", suffix)
			assert.Equal(t, "new"+suffix, string(data), "[SEC-022] %q aside must hold the new set", suffix)
			_, err = os.Stat(path + suffix)
			assert.True(t, os.IsNotExist(err), "[SEC-022] the live %q must be gone", suffix)
		}
		_, err := os.Stat(path + ".corrupt-journal")
		assert.True(t, os.IsNotExist(err), "[SEC-022] a stale aside sibling of the previous set must not survive")
		matches, err := filepath.Glob(path + ".corrupt*")
		require.NoError(t, err)
		assert.Len(t, matches, 3, "[SEC-022] exactly one set: %v", matches)
	})

	t.Run("a corrupt file with wal and shm siblings starts clean with none left beside the new database", func(t *testing.T) {
		captureLogs(t)
		path := filepath.Join(t.TempDir(), "buffer.db")
		garbage := []byte(strings.Repeat("garbage ", 600))
		for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
			require.NoError(t, os.WriteFile(path+suffix, append([]byte(suffix), garbage...), 0o600))
		}

		b := openTestBuffer(t, path, 50<<20)
		require.NoError(t, b.Store(bufSnap("fresh", 10)))
		require.NoError(t, b.Close())

		data, err := os.ReadFile(path + ".corrupt")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(string(data), "garbage"), "[SEC-022] the corrupt main file is kept aside")
		// SQLite itself may discard invalid siblings when it opens the file, so
		// whether they end up aside or gone is its choice (moveBufferAside is
		// covered above); what matters is that none remains beside the new file.
		for _, suffix := range []string{"-journal", "-wal", "-shm"} {
			_, err := os.Stat(path + suffix)
			assert.True(t, os.IsNotExist(err), "[SEC-022] a stale %s must not sit beside the new database", suffix)
		}
	})

	t.Run("a VACUUM conversion that fails with ENOSPC keeps the file and the agent running, then retries next start", func(t *testing.T) {
		logs := captureLogs(t)
		path := filepath.Join(t.TempDir(), "buffer.db")
		legacyBufferFile(t, path)

		// Put one item in the old-format file so we can prove it survives.
		db, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		db.SetMaxOpenConns(1)
		payload, err := json.Marshal(&models.TelemetrySnapshot{DeviceID: "survivor"})
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO buffer (payload) VALUES (?)", string(payload))
		require.NoError(t, err)
		require.NoError(t, db.Close())

		original := bufferConvertVacuum
		t.Cleanup(func() { bufferConvertVacuum = original })
		calls := 0
		bufferConvertVacuum = func(*sql.DB) error {
			calls++
			return fmt.Errorf("vacuum: %w", syscall.ENOSPC)
		}

		b, err := NewBufferWithLimit(path, 50<<20)
		require.NoError(t, err, "[SEC-022] a failed conversion must never stop the agent")
		assert.Equal(t, 1, calls)
		assert.Contains(t, logs.String(), "retrying at the next start")
		assert.Equal(t, []string{"survivor"}, drainLabels(t, b), "[SEC-022] the existing buffer must stay usable")
		require.NoError(t, b.Close())

		bufferConvertVacuum = original
		b = openTestBuffer(t, path, 50<<20)
		var mode int
		require.NoError(t, b.db.QueryRow("PRAGMA auto_vacuum").Scan(&mode))
		assert.Equal(t, autoVacuumIncremental, mode, "[SEC-022] the next start must retry and complete the conversion")
		assert.Equal(t, []string{"survivor"}, drainLabels(t, b), "[SEC-022] the conversion must keep the data")
	})
}

func TestGROWTH_AC062_UndecodableBufferedItemDroppedNotRetried(t *testing.T) {
	captureLogs(t)
	b := openTestBuffer(t, filepath.Join(t.TempDir(), "buffer.db"), 50<<20)
	_, err := b.db.Exec("INSERT INTO buffer (payload) VALUES ('{not json')")
	require.NoError(t, err)
	b.count, b.bytes = 1, int64(len("{not json"))
	require.NoError(t, b.Store(bufSnap("good", 10)))

	item, err := b.Oldest()

	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "good", item.Snapshot.DeviceID, "[AC-062] an item that can never be decoded must be skipped")
	assert.Equal(t, []string{"good"}, drainLabels(t, b), "[AC-062] and removed, so it is not retried forever")
	assert.Equal(t, 1, b.Len())
}
