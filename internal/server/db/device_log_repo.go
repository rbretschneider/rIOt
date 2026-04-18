package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// MaxDeviceLogBatch caps how many rows a single agent push can insert; protects
// the DB from a runaway journal collector.
const MaxDeviceLogBatch = 20_000

// DeviceLogRepo handles device log database operations.
type DeviceLogRepo struct {
	db *DB
}

func NewDeviceLogRepo(db *DB) *DeviceLogRepo {
	return &DeviceLogRepo{db: db}
}

// InsertBatch uses a staging table + INSERT ... SELECT ... ON CONFLICT to
// combine the speed of COPY with the dedupe behavior of the old multi-row
// INSERT. pgx.CopyFrom doesn't support ON CONFLICT directly so we copy into
// a per-connection TEMP table and merge from there in a single statement.
func (r *DeviceLogRepo) InsertBatch(ctx context.Context, deviceID string, entries []models.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > MaxDeviceLogBatch {
		entries = entries[len(entries)-MaxDeviceLogBatch:]
	}

	conn, err := r.db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Run COPY into a staging table + dedupe merge inside a single transaction.
	// ON COMMIT DROP gives us a clean per-batch table without having to manage
	// connection-scoped state (which the pool makes fragile anyway).
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup; commit path returns before this fires

	if _, err := tx.Exec(ctx,
		`CREATE TEMP TABLE device_logs_stage (
			device_id TEXT, timestamp TIMESTAMPTZ, priority INT, unit TEXT, message TEXT
		) ON COMMIT DROP`); err != nil {
		return err
	}

	rows := make([][]interface{}, len(entries))
	for i, e := range entries {
		rows[i] = []interface{}{deviceID, e.Timestamp, e.Priority, e.Unit, e.Message}
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"device_logs_stage"},
		[]string{"device_id", "timestamp", "priority", "unit", "message"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO device_logs (device_id, timestamp, priority, unit, message)
		 SELECT device_id, timestamp, priority, unit, message FROM device_logs_stage
		 ON CONFLICT (device_id, timestamp, priority, unit, (md5(message))) DO NOTHING`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *DeviceLogRepo) List(ctx context.Context, deviceID string, priority, limit int, exact bool) ([]models.LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	op := "<="
	if exact {
		op = "="
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT timestamp, priority, unit, message FROM device_logs
		 WHERE device_id=$1 AND priority`+op+`$2
		 ORDER BY timestamp DESC LIMIT $3`,
		deviceID, priority, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.LogEntry
	for rows.Next() {
		var e models.LogEntry
		if err := rows.Scan(&e.Timestamp, &e.Priority, &e.Unit, &e.Message); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *DeviceLogRepo) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx,
		`DELETE FROM device_logs WHERE timestamp < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
