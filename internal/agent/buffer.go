package agent

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/DesyncTheThird/rIOt/internal/models"
	_ "modernc.org/sqlite"
)

const maxBuffered = 100

type Buffer struct {
	db *sql.DB
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

	// Enforce max buffer size
	var count int
	b.db.QueryRow("SELECT COUNT(*) FROM buffer").Scan(&count)
	if count >= maxBuffered {
		b.db.Exec("DELETE FROM buffer WHERE id IN (SELECT id FROM buffer ORDER BY id ASC LIMIT 1)")
	}

	_, err = b.db.Exec("INSERT INTO buffer (payload) VALUES (?)", string(data))
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
