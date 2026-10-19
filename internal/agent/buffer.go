package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/DesyncTheThird/rIOt/internal/models"
	_ "modernc.org/sqlite"
)

const maxBuffered = 100
const AgentBufferMaxBytes = 64 << 20

type Buffer struct {
	db *sql.DB
	mu sync.Mutex
}

func NewBuffer(path string) (*Buffer, error) {
	os.MkdirAll(filepath.Dir(path), 0755)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS buffer (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		payload TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Buffer{db: db}, nil
}

func (b *Buffer) Store(snap *models.TelemetrySnapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if len(data) > AgentBufferMaxBytes {
		return fmt.Errorf("snapshot exceeds buffer byte cap")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for {
		var count, total int64
		if err = tx.QueryRow("SELECT COUNT(*),COALESCE(SUM(length(CAST(payload AS BLOB))),0) FROM buffer").Scan(&count, &total); err != nil {
			return err
		}
		if count < maxBuffered && total+int64(len(data)) <= AgentBufferMaxBytes {
			break
		}
		if _, err = tx.Exec("DELETE FROM buffer WHERE id=(SELECT MIN(id) FROM buffer)"); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO buffer(payload) VALUES(?)", string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

type bufferedSnapshot struct {
	ID       int64
	Snapshot *models.TelemetrySnapshot
}

func (b *Buffer) Items() ([]bufferedSnapshot, error) {
	rows, err := b.db.Query("SELECT id,payload FROM buffer ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bufferedSnapshot
	for rows.Next() {
		var id int64
		var payload string
		if err = rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		var snap models.TelemetrySnapshot
		if json.Unmarshal([]byte(payload), &snap) != nil {
			continue
		}
		out = append(out, bufferedSnapshot{id, &snap})
	}
	return out, rows.Err()
}
func (b *Buffer) Delete(id int64) error {
	_, err := b.db.Exec("DELETE FROM buffer WHERE id=?", id)
	return err
}
func (b *Buffer) GetAll() ([]*models.TelemetrySnapshot, error) {
	rows, err := b.db.Query("SELECT payload FROM buffer ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*models.TelemetrySnapshot
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var snap models.TelemetrySnapshot
		if err := json.Unmarshal([]byte(payload), &snap); err != nil {
			continue
		}
		snapshots = append(snapshots, &snap)
	}
	return snapshots, nil
}

func (b *Buffer) Clear() error {
	_, err := b.db.Exec("DELETE FROM buffer")
	return err
}

func (b *Buffer) Close() error {
	return b.db.Close()
}
