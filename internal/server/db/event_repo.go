package db

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/redact"
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
	e.Message, _ = redact.MaskLine(models.TruncateHead(models.SanitizeText(e.Message), 2048))
	err := r.db.Pool.QueryRow(ctx,
		`INSERT INTO events (device_id, type, severity, message, container_id, container_name, created_at,command_id,command_attr)
		 VALUES ($1, $2, $3, $4, $5, $6, $7,$8,$9) RETURNING id`,
		e.DeviceID, e.Type, e.Severity, e.Message, e.ContainerID, e.ContainerName, e.CreatedAt, e.CommandID, e.CommandAttr,
	).Scan(&e.ID)
	return err
}

// ListByDevice returns recent events for a device.
func (r *EventRepo) ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, type, severity, message, container_id, container_name, created_at, acknowledged_at,command_id,command_attr
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
		`SELECT id, device_id, type, severity, message, container_id, container_name, created_at, acknowledged_at,command_id,command_attr
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

// CountUnacknowledged returns the number of unacknowledged warning/critical events.
func (r *EventRepo) CountUnacknowledged(ctx context.Context) (int, error) {
	var count int
	err := r.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE acknowledged_at IS NULL AND severity IN ('warning', 'critical')`).Scan(&count)
	return count, err
}

// Acknowledge marks a single event as acknowledged.
func (r *EventRepo) Acknowledge(ctx context.Context, id int64) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE events SET acknowledged_at = NOW() WHERE id = $1 AND acknowledged_at IS NULL`, id)
	return err
}

// AcknowledgeAll marks all unacknowledged events as acknowledged.
func (r *EventRepo) AcknowledgeAll(ctx context.Context) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE events SET acknowledged_at = NOW() WHERE acknowledged_at IS NULL`)
	return err
}

type scannable interface {
	Next() bool
	Scan(dest ...interface{}) error
}

func scanEvents(rows scannable) ([]models.Event, error) {
	events := []models.Event{}
	for rows.Next() {
		var e models.Event
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Type, &e.Severity, &e.Message, &e.ContainerID, &e.ContainerName, &e.CreatedAt, &e.AcknowledgedAt, &e.CommandID, &e.CommandAttr); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}
