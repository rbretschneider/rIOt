package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DeviceLogRepo handles device log database operations.
type DeviceLogRepo struct {
	db *DB
}

func NewDeviceLogRepo(db *DB) *DeviceLogRepo {
	return &DeviceLogRepo{db: db}
}

func (r *DeviceLogRepo) InsertBatch(ctx context.Context, deviceID string, entries []models.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	for _, e := range entries {
		_, err := r.db.Pool.Exec(ctx,
			`INSERT INTO device_logs (device_id, timestamp, priority, unit, message) VALUES ($1, $2, $3, $4, $5)`,
			deviceID, e.Timestamp, e.Priority, e.Unit, e.Message,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *DeviceLogRepo) List(ctx context.Context, deviceID string, maxPriority, limit int) ([]models.LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT timestamp, priority, unit, message FROM device_logs
		 WHERE device_id=$1 AND priority<=$2
		 ORDER BY timestamp DESC LIMIT $3`,
		deviceID, maxPriority, limit,
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
