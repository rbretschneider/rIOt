package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EventRepo handles event database operations.
type EventRepo struct {
	db *DB
}

func NewEventRepo(db *DB) *EventRepo {
	return &EventRepo{db: db}
}

// Create inserts a new event.
func (r *EventRepo) Create(ctx context.Context, e *models.Event) error {
	err := r.db.Pool.QueryRow(ctx,
		`INSERT INTO events (device_id, type, severity, message, created_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		e.DeviceID, e.Type, e.Severity, e.Message, e.CreatedAt,
	).Scan(&e.ID)
	return err
}

// ListByDevice returns recent events for a device.
func (r *EventRepo) ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, type, severity, message, created_at
		 FROM events WHERE device_id=$1 ORDER BY created_at DESC LIMIT $2`,
		deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ListAll returns recent events across all devices.
func (r *EventRepo) ListAll(ctx context.Context, limit, offset int) ([]models.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, type, severity, message, created_at
		 FROM events ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// Purge deletes events older than the given time.
func (r *EventRepo) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM events WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

type scannable interface {
	Next() bool
	Scan(dest ...interface{}) error
}

func scanEvents(rows scannable) ([]models.Event, error) {
	events := []models.Event{}
	for rows.Next() {
		var e models.Event
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Type, &e.Severity, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}
