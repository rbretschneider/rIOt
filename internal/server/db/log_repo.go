package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// LogRepo handles server log database operations.
type LogRepo struct {
	db *DB
}

func NewLogRepo(db *DB) *LogRepo {
	return &LogRepo{db: db}
}

// Insert stores a batch of log entries.
func (r *LogRepo) Insert(ctx context.Context, entries []models.ServerLog) error {
	for _, e := range entries {
		_, err := r.db.Pool.Exec(ctx,
			`INSERT INTO server_logs (timestamp, level, message, attrs, source)
			 VALUES ($1, $2, $3, $4, $5)`,
			e.Timestamp, e.Level, e.Message, e.Attrs, e.Source,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// List returns server log entries with optional level filter and cursor-based pagination.
func (r *LogRepo) List(ctx context.Context, level string, limit int, before *time.Time) ([]models.ServerLog, error) {
	if limit <= 0 {
		limit = 100
	}

	var logs []models.ServerLog

	if level != "" && before != nil {
		rows, err := r.db.Pool.Query(ctx,
			`SELECT id, timestamp, level, message, attrs, source
			 FROM server_logs WHERE level=$1 AND timestamp < $2
			 ORDER BY timestamp DESC LIMIT $3`,
			level, *before, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanLogs(rows)
	} else if level != "" {
		rows, err := r.db.Pool.Query(ctx,
			`SELECT id, timestamp, level, message, attrs, source
			 FROM server_logs WHERE level=$1
			 ORDER BY timestamp DESC LIMIT $2`,
			level, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanLogs(rows)
	} else if before != nil {
		rows, err := r.db.Pool.Query(ctx,
			`SELECT id, timestamp, level, message, attrs, source
			 FROM server_logs WHERE timestamp < $1
			 ORDER BY timestamp DESC LIMIT $2`,
			*before, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanLogs(rows)
	}

	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, timestamp, level, message, attrs, source
		 FROM server_logs ORDER BY timestamp DESC LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs, err = scanLogs(rows)
	return logs, err
}

// Purge deletes server logs older than the given time.
func (r *LogRepo) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM server_logs WHERE timestamp < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func scanLogs(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
}) ([]models.ServerLog, error) {
	logs := []models.ServerLog{}
	for rows.Next() {
		var l models.ServerLog
		if err := rows.Scan(&l.ID, &l.Timestamp, &l.Level, &l.Message, &l.Attrs, &l.Source); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}
