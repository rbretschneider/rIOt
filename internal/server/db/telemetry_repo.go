package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SnapshotSummary is a lightweight projection of a telemetry snapshot
// containing only the fields needed by fleet and security overview endpoints.
// This avoids deserializing the full multi-MB JSONB blob per device.
type SnapshotSummary struct {
	DeviceID string
	Updates  *models.UpdateInfo
	Security *models.SecurityInfo
	WebCerts []models.ProxyCert // flattened from WebServers.Servers[].Certs
}

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

// GetAllLatestSnapshots returns the most recent telemetry for every device.
func (r *TelemetryRepo) GetAllLatestSnapshots(ctx context.Context) ([]models.TelemetrySnapshot, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT DISTINCT ON (device_id) id, device_id, timestamp, data
		 FROM telemetry_snapshots ORDER BY device_id, timestamp DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshots := []models.TelemetrySnapshot{}
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

// GetAllLatestSummaries returns lightweight projections of the latest snapshot
// per device, extracting only the updates, security, and web_servers.certs
// fields via PostgreSQL JSONB operators. This avoids loading multi-MB blobs.
func (r *TelemetryRepo) GetAllLatestSummaries(ctx context.Context) ([]SnapshotSummary, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT device_id,
			data->'updates' AS updates,
			data->'security' AS security,
			data->'web_servers'->'servers' AS servers
		 FROM (
			SELECT DISTINCT ON (device_id) device_id, data
			FROM telemetry_snapshots
			ORDER BY device_id, timestamp DESC
		 ) latest`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []SnapshotSummary
	for rows.Next() {
		var s SnapshotSummary
		var updatesJSON, securityJSON, serversJSON []byte
		if err := rows.Scan(&s.DeviceID, &updatesJSON, &securityJSON, &serversJSON); err != nil {
			return nil, err
		}
		if len(updatesJSON) > 0 && string(updatesJSON) != "null" {
			s.Updates = &models.UpdateInfo{}
			json.Unmarshal(updatesJSON, s.Updates)
		}
		if len(securityJSON) > 0 && string(securityJSON) != "null" {
			s.Security = &models.SecurityInfo{}
			json.Unmarshal(securityJSON, s.Security)
		}
		if len(serversJSON) > 0 && string(serversJSON) != "null" {
			var servers []models.ProxyServer
			json.Unmarshal(serversJSON, &servers)
			for _, srv := range servers {
				s.WebCerts = append(s.WebCerts, srv.Certs...)
			}
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
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

	snapshots := []models.TelemetrySnapshot{}
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

	heartbeats := []models.Heartbeat{}
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

// GetFleetHeartbeats returns heartbeats for all devices since the given time,
// grouped by device ID. A single query is used so the dashboard can fetch 60
// minutes of fleet-wide data in one round trip (AD-001).
func (r *TelemetryRepo) GetFleetHeartbeats(ctx context.Context, since time.Time) (map[string][]models.Heartbeat, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT device_id, timestamp, data
		 FROM heartbeats
		 WHERE timestamp >= $1
		 ORDER BY device_id, timestamp ASC`,
		since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]models.Heartbeat)
	for rows.Next() {
		var hb models.Heartbeat
		var dataJSON []byte
		if err := rows.Scan(&hb.DeviceID, &hb.Timestamp, &dataJSON); err != nil {
			return nil, err
		}
		json.Unmarshal(dataJSON, &hb.Data)
		result[hb.DeviceID] = append(result[hb.DeviceID], hb)
	}
	return result, rows.Err()
}

// GetGPUDeviceIDs returns the IDs of devices whose latest telemetry snapshot
// contains at least one GPU in gpu_telemetry.gpus. Uses JSONB projection to
// avoid full-blob decode (AD-008).
func (r *TelemetryRepo) GetGPUDeviceIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT device_id
		 FROM (
			SELECT DISTINCT ON (device_id) device_id, data
			FROM telemetry_snapshots
			ORDER BY device_id, timestamp DESC
		 ) latest
		 WHERE jsonb_array_length(data->'gpu_telemetry'->'gpus') > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetFleetContainerLeaderboard returns a slim container projection per device
// from the latest telemetry snapshots. Uses jsonb_path_query_array to project
// only the container sub-tree, avoiding full-blob decode on the server (AD-011).
func (r *TelemetryRepo) GetFleetContainerLeaderboard(ctx context.Context) ([]FleetContainerProjection, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT
			device_id,
			jsonb_path_query_array(
				data,
				'$.docker.containers[*] ?
				   (@.state == "running" || @.state == "restarting" || @.state == "exited" ||
				    @.state == "paused"  || @.state == "created"    || @.state == "dead")'
			) AS containers
		 FROM (
			SELECT DISTINCT ON (device_id) device_id, data
			FROM telemetry_snapshots
			ORDER BY device_id, timestamp DESC
		 ) latest
		 WHERE data ? 'docker'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []FleetContainerProjection
	for rows.Next() {
		var proj FleetContainerProjection
		var containersJSON []byte
		if err := rows.Scan(&proj.DeviceID, &containersJSON); err != nil {
			return nil, err
		}
		if len(containersJSON) > 0 && string(containersJSON) != "null" {
			json.Unmarshal(containersJSON, &proj.Containers)
		}
		result = append(result, proj)
	}
	return result, rows.Err()
}
