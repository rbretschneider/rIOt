package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// TelemetryRepo handles telemetry database operations.
type TelemetryRepo struct {
	db *DB
}

func NewTelemetryRepo(db *DB) *TelemetryRepo {
	return &TelemetryRepo{db: db}
}

// StoreHeartbeat inserts a heartbeat record.
func (r *TelemetryRepo) StoreHeartbeat(ctx context.Context, hb *models.Heartbeat) error {
	dataJSON, _ := json.Marshal(hb.Data)
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1, $2, $3)`,
		hb.DeviceID, hb.Timestamp, dataJSON)
	return err
}

// StoreSnapshot inserts a full telemetry snapshot.
func (r *TelemetryRepo) StoreSnapshot(ctx context.Context, snap *models.TelemetrySnapshot) error {
	dataJSON, _ := json.Marshal(snap.Data)
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO telemetry_snapshots (device_id, timestamp, data) VALUES ($1, $2, $3)`,
		snap.DeviceID, snap.Timestamp, dataJSON)
	return err
}

// GetLatestSnapshot returns the most recent telemetry for a device.
func (r *TelemetryRepo) GetLatestSnapshot(ctx context.Context, deviceID string) (*models.TelemetrySnapshot, error) {
	snap := &models.TelemetrySnapshot{}
	var dataJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, device_id, timestamp, data FROM telemetry_snapshots
		 WHERE device_id=$1 ORDER BY timestamp DESC LIMIT 1`, deviceID,
	).Scan(&snap.ID, &snap.DeviceID, &snap.Timestamp, &dataJSON)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(dataJSON, &snap.Data)
	return snap, nil
}

// GetHistory returns paginated telemetry snapshots for a device.
func (r *TelemetryRepo) GetHistory(ctx context.Context, deviceID string, limit, offset int) ([]models.TelemetrySnapshot, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, timestamp, data FROM telemetry_snapshots
		 WHERE device_id=$1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3`,
		deviceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []models.TelemetrySnapshot
	for rows.Next() {
		var s models.TelemetrySnapshot
		var dataJSON []byte
		if err := rows.Scan(&s.ID, &s.DeviceID, &s.Timestamp, &dataJSON); err != nil {
			return nil, err
		}
		json.Unmarshal(dataJSON, &s.Data)
		snapshots = append(snapshots, s)
	}
	return snapshots, nil
}

// GetHeartbeatHistory returns recent heartbeats for a device.
func (r *TelemetryRepo) GetHeartbeatHistory(ctx context.Context, deviceID string, since time.Time) ([]models.Heartbeat, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, timestamp, data FROM heartbeats
		 WHERE device_id=$1 AND timestamp >= $2 ORDER BY timestamp ASC`,
		deviceID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var heartbeats []models.Heartbeat
	for rows.Next() {
		var hb models.Heartbeat
		var dataJSON []byte
		if err := rows.Scan(&hb.ID, &hb.DeviceID, &hb.Timestamp, &dataJSON); err != nil {
			return nil, err
		}
		json.Unmarshal(dataJSON, &hb.Data)
		heartbeats = append(heartbeats, hb)
	}
	return heartbeats, nil
}

// PurgeHeartbeats deletes heartbeats older than the given duration.
func (r *TelemetryRepo) PurgeHeartbeats(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM heartbeats WHERE timestamp < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeSnapshots deletes telemetry snapshots older than the given time.
func (r *TelemetryRepo) PurgeSnapshots(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM telemetry_snapshots WHERE timestamp < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
