package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"modernc.org/sqlite"
)

const (
	// maxBuffered caps the number of buffered snapshots.
	maxBuffered = 100
	// defaultBufferMaxBytes is the byte cap when the config does not set one
	// (GROWTH-001 FR-066, agent.buffer_max_mb).
	defaultBufferMaxBytes int64 = 50 << 20

	// SQLite primary result codes that mean the file itself is damaged.
	sqliteCorrupt = 11
	sqliteNotADB  = 26

	// autoVacuumIncremental is PRAGMA auto_vacuum's value for INCREMENTAL.
	autoVacuumIncremental = 2
)

// errItemTooLarge is returned by Store for a snapshot that can never fit under
// the byte cap. It is dropped, not retried.
var errItemTooLarge = errors.New("telemetry item larger than the buffer cap")

// bufferConvertVacuum converts an existing buffer file to incremental
// auto_vacuum. VACUUM needs up to twice the file size in free space, so it can
// fail on a full disk; the caller treats that as non-fatal. A variable so tests
// can inject the failure.
var bufferConvertVacuum = func(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
		return err
	}
	_, err := db.Exec("VACUUM")
	return err
}

// Buffer is the agent's offline telemetry queue: a SQLite file bounded by item
// count and by payload bytes. The oldest items are evicted first. It is safe
// for concurrent use.
type Buffer struct {
	mu       sync.Mutex
	db       *sql.DB
	maxBytes int64
	count    int
	bytes    int64
}

// BufferedItem is one queued snapshot and its row id.
type BufferedItem struct {
	ID       int64
	Snapshot *models.TelemetrySnapshot
}

// NewBuffer opens the buffer with the default 50 MiB cap.
func NewBuffer(path string) (*Buffer, error) {
	return NewBufferWithLimit(path, defaultBufferMaxBytes)
}

// NewBufferWithLimit opens (or creates) the buffer at path with a payload cap
// of maxBytes. A corrupt file is moved aside, with its journal, WAL and shared
// memory siblings, replacing any earlier aside set, and a new empty buffer is
// started (GROWTH-001 FR-071, SEC-022).
func NewBufferWithLimit(path string, maxBytes int64) (*Buffer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create buffer directory: %w", err)
	}
	b, corrupt, err := openBuffer(path, maxBytes)
	if err == nil {
		return b, nil
	}
	if !corrupt {
		return nil, err
	}
	slog.Error("offline buffer is corrupt; moving it aside and starting a new empty buffer",
		"path", path, "error", err)
	if moveErr := moveBufferAside(path); moveErr != nil {
		return nil, fmt.Errorf("move corrupt buffer aside: %w", moveErr)
	}
	b, _, err = openBuffer(path, maxBytes)
	return b, err
}

// openBuffer opens the file, checks it, and makes sure it uses incremental
// auto_vacuum. corrupt reports that err means the file is damaged (as opposed
// to a transient or permission problem that must not cost the buffered data).
func openBuffer(path string, maxBytes int64) (b *Buffer, corrupt bool, err error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, false, err
	}
	// One connection: auto_vacuum must be set on the connection that creates
	// the first table, and a single writer avoids "database is locked".
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Buffer, bool, error) {
		db.Close()
		return nil, isCorruption(err), err
	}

	var check string
	if err := db.QueryRow("PRAGMA quick_check(1)").Scan(&check); err != nil {
		return fail(err)
	}
	if check != "ok" {
		db.Close()
		return nil, true, fmt.Errorf("integrity check failed: %s", check)
	}

	var pages int
	if err := db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		return fail(err)
	}
	if pages == 0 {
		// A new file: choose the mode before the first table exists.
		if _, err := db.Exec("PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
			return fail(err)
		}
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS buffer (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		payload TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fail(err)
	}

	var mode int
	if err := db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return fail(err)
	}
	if mode != autoVacuumIncremental {
		// One-time conversion of a file from an older agent. It must never stop
		// the agent: on failure keep the file and retry at the next start.
		if err := bufferConvertVacuum(db); err != nil {
			slog.Warn("could not convert the offline buffer to incremental auto_vacuum; keeping the file and retrying at the next start",
				"path", path, "error", err)
		}
	}

	buf := &Buffer{db: db, maxBytes: maxBytes}
	if err := db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(LENGTH(CAST(payload AS BLOB))), 0) FROM buffer",
	).Scan(&buf.count, &buf.bytes); err != nil {
		return fail(err)
	}
	return buf, false, nil
}

func isCorruption(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code() & 0xff
		return code == sqliteCorrupt || code == sqliteNotADB
	}
	return false
}

// moveBufferAside renames the buffer file and its SQLite siblings to
// "<path>.corrupt<suffix>", deleting the previous aside set first, so at most
// one set ever exists. If a file cannot be renamed it is deleted instead.
func moveBufferAside(path string) error {
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		aside := path + ".corrupt" + suffix
		if err := os.Remove(aside); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		live := path + suffix
		if _, err := os.Lstat(live); err != nil {
			continue
		}
		if err := os.Rename(live, path+".corrupt"+suffix); err != nil {
			if rmErr := os.Remove(live); rmErr != nil {
				return fmt.Errorf("move %s aside: %w", live, errors.Join(err, rmErr))
			}
		}
	}
	return nil
}

// Store queues a snapshot, evicting the oldest items until both the item and
// byte caps hold. A snapshot larger than the whole byte cap is refused with
// errItemTooLarge.
func (b *Buffer) Store(snap *models.TelemetrySnapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	size := int64(len(data))
	if size > b.maxBytes {
		return fmt.Errorf("%w: %d bytes > %d", errItemTooLarge, size, b.maxBytes)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	count, bytes, evicted := b.count, b.bytes, 0
	for count >= maxBuffered || bytes+size > b.maxBytes {
		var id, itemSize int64
		err := tx.QueryRow(
			"SELECT id, LENGTH(CAST(payload AS BLOB)) FROM buffer ORDER BY id ASC LIMIT 1",
		).Scan(&id, &itemSize)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec("DELETE FROM buffer WHERE id = ?", id); err != nil {
			tx.Rollback()
			return err
		}
		count--
		bytes -= itemSize
		evicted++
	}
	if _, err := tx.Exec("INSERT INTO buffer (payload) VALUES (?)", string(data)); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	b.count, b.bytes = count+1, bytes+size

	if evicted > 0 {
		slog.Warn("offline buffer full, dropped the oldest items",
			"dropped", evicted, "items", b.count, "bytes", b.bytes, "max_bytes", b.maxBytes)
		b.reclaimLocked()
	}
	return nil
}

// Oldest returns the oldest queued snapshot, or nil when the buffer is empty.
// A row that cannot be decoded can never be delivered, so it is deleted (with a
// Warn) rather than retried forever.
func (b *Buffer) Oldest() (*BufferedItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for attempts := 0; attempts <= maxBuffered; attempts++ {
		var id int64
		var payload string
		err := b.db.QueryRow("SELECT id, payload FROM buffer ORDER BY id ASC LIMIT 1").Scan(&id, &payload)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var snap models.TelemetrySnapshot
		if err := json.Unmarshal([]byte(payload), &snap); err != nil {
			slog.Warn("dropping an undecodable buffered item", "id", id, "error", err)
			if err := b.deleteLocked(id); err != nil {
				return nil, err
			}
			continue
		}
		return &BufferedItem{ID: id, Snapshot: &snap}, nil
	}
	return nil, nil
}

// Delete removes one item after the server has acknowledged or permanently
// rejected it.
func (b *Buffer) Delete(id int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.deleteLocked(id)
}

func (b *Buffer) deleteLocked(id int64) error {
	var size int64
	err := b.db.QueryRow("SELECT LENGTH(CAST(payload AS BLOB)) FROM buffer WHERE id = ?", id).Scan(&size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := b.db.Exec("DELETE FROM buffer WHERE id = ?", id); err != nil {
		return err
	}
	b.count--
	b.bytes -= size
	return nil
}

// Compact returns freed pages to the file system (incremental auto_vacuum), so
// the file shrinks after a flush instead of keeping its high-water mark.
func (b *Buffer) Compact() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reclaimLocked()
}

func (b *Buffer) reclaimLocked() {
	// incremental_vacuum frees one page per step, and Exec stops after the
	// first step, so the result rows must be drained to free the whole list.
	rows, err := b.db.Query("PRAGMA incremental_vacuum")
	if err != nil {
		slog.Warn("could not reclaim free space in the offline buffer", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		slog.Warn("could not reclaim free space in the offline buffer", "error", err)
	}
}

// Len is the number of queued items.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// Bytes is the queued payload size.
func (b *Buffer) Bytes() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bytes
}

func (b *Buffer) Close() error {
	return b.db.Close()
}
