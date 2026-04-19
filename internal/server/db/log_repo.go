package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// LogRepo handles server log database operations.
type LogRepo struct {
	db *DB
}

func NewLogRepo(db *DB) *LogRepo {
	return &LogRepo{db: db}
}

// Insert stores a batch of server log entries in a single COPY round-trip
// instead of one INSERT per row. The slog DB handler buffers up to 100 entries
// between flushes, so this matters when the server itself is spewing errors
// (exactly the moment the old one-row-per-INSERT loop amplified DB load).
func (r *LogRepo) Insert(ctx context.Context, entries []models.ServerLog) error {
	if len(entries) == 0 {
		return nil
	}
	rows := make([][]interface{}, len(entries))
	for i, e := range entries {
		var attrs []byte
		if e.Attrs != nil {
			attrs, _ = json.Marshal(e.Attrs)
		}
		rows[i] = []interface{}{e.Timestamp, e.Level, e.Message, attrs, e.Source}
	}
	_, err := r.db.Pool.CopyFrom(
		ctx,
		pgx.Identifier{"server_logs"},
		[]string{"timestamp", "level", "message", "attrs", "source"},
		pgx.CopyFromRows(rows),
	)
	return err
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

// CountSince returns the number of server_logs rows with the given level
// (e.g. "ERROR") emitted at or after the given timestamp. Used by the server
// error-rate alert worker to evaluate thresholds over a rolling window.
func (r *LogRepo) CountSince(ctx context.Context, level string, since time.Time) (int, error) {
	var count int
	err := r.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM server_logs WHERE level=$1 AND timestamp >= $2`,
		level, since).Scan(&count)
	return count, err
}

// Purge deletes server logs older than the given time.
func (r *LogRepo) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM server_logs WHERE timestamp < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeAll removes every server_logs row. Exposed so an operator can clear
// the viewer and wait for fresh entries after debugging a cascade.
func (r *LogRepo) PurgeAll(ctx context.Context) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM server_logs`)
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
