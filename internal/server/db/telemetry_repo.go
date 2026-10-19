package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/jackc/pgx/v5"
)

// FutureSkewAllowance is how far ahead of the server clock a snapshot or
// heartbeat timestamp may be and still be trusted (REG-HARDEN-001 FR-030).
// Ingest clamps anything later to the receive time; the read queries below
// exclude any already-stored row later than now + this allowance.
const FutureSkewAllowance = 5 * time.Minute

// notFutureDatedSQL is the read-side guard spliced into every snapshot and
// heartbeat read (REG-HARDEN-001 FR-036, FR-037). It keeps a future-dated row
// from being selected as a device's "latest" or appearing in history. In the
// DISTINCT ON queries it must sit INSIDE the subquery so a device falls back to
// its newest valid row instead of disappearing. The interval literal must equal
// FutureSkewAllowance (checked by a unit test).
const notFutureDatedSQL = "timestamp <= NOW() + interval '5 minutes'"

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

// LatestSnapshotTime returns the timestamp of the device's current telemetry
// state (the newest snapshot inside notFutureDatedSQL), or ok=false when it has
// none. The server compares an incoming snapshot against it to keep history in
// order (CMD-LIFECYCLE-001 FR-069); the shared guard means a future-dated row
// stored before ingest clamping can never freeze a device's current state.
func (r *TelemetryRepo) LatestSnapshotTime(ctx context.Context, deviceID string) (time.Time, bool, error) {
	var ts time.Time
	err := r.db.Pool.QueryRow(ctx,
		`SELECT timestamp FROM telemetry_snapshots
		 WHERE device_id=$1 AND `+notFutureDatedSQL+`
		 ORDER BY timestamp DESC LIMIT 1`, deviceID).Scan(&ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return ts, true, nil
}

// GetLatestSnapshot returns the most recent telemetry for a device.
func (r *TelemetryRepo) GetLatestSnapshot(ctx context.Context, deviceID string) (*models.TelemetrySnapshot, error) {
	snap := &models.TelemetrySnapshot{}
	var dataJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, device_id, timestamp, data FROM telemetry_snapshots
		 WHERE device_id=$1 AND `+notFutureDatedSQL+`
		 ORDER BY timestamp DESC LIMIT 1`, deviceID,
	).Scan(&snap.ID, &snap.DeviceID, &snap.Timestamp, &dataJSON)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(dataJSON, &snap.Data)
	if err := r.restoreOmittedSections(ctx, snap, dataJSON); err != nil {
		return nil, err
	}
	return snap, nil
}

// GetAllLatestSnapshots returns the most recent telemetry for every device.
func (r *TelemetryRepo) GetAllLatestSnapshots(ctx context.Context) ([]models.TelemetrySnapshot, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT DISTINCT ON (device_id) id, device_id, timestamp, data
		 FROM telemetry_snapshots
		 WHERE `+notFutureDatedSQL+`
		 ORDER BY device_id, timestamp DESC`)
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
		if err := r.restoreOmittedSections(ctx, &s, dataJSON); err != nil {
			return nil, err
		}
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
			CASE WHEN data->'dropped_sections' ? 'updates' THEN (SELECT s.data->'updates' FROM telemetry_snapshots s WHERE s.device_id=latest.device_id AND s.data ? 'updates' AND s.timestamp <= now()+interval '5 minutes' ORDER BY s.timestamp DESC LIMIT 1) ELSE data->'updates' END AS updates,
			CASE WHEN data->'dropped_sections' ? 'security' THEN (SELECT s.data->'security' FROM telemetry_snapshots s WHERE s.device_id=latest.device_id AND s.data ? 'security' AND s.timestamp <= now()+interval '5 minutes' ORDER BY s.timestamp DESC LIMIT 1) ELSE data->'security' END AS security,
			CASE WHEN data->'dropped_sections' ? 'web_servers' THEN (SELECT s.data->'web_servers'->'servers' FROM telemetry_snapshots s WHERE s.device_id=latest.device_id AND s.data ? 'web_servers' AND s.timestamp <= now()+interval '5 minutes' ORDER BY s.timestamp DESC LIMIT 1) ELSE data->'web_servers'->'servers' END AS servers
		 FROM (
			SELECT DISTINCT ON (device_id) device_id, data
			FROM telemetry_snapshots
			WHERE `+notFutureDatedSQL+`
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
		 WHERE device_id=$1 AND `+notFutureDatedSQL+`
		 ORDER BY timestamp DESC LIMIT $2 OFFSET $3`,
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
		 WHERE device_id=$1 AND timestamp >= $2 AND `+notFutureDatedSQL+`
		 ORDER BY timestamp ASC`,
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

// GetFleetHeartbeats returns heartbeats for all devices since the given time,
// grouped by device ID. A single query is used so the dashboard can fetch 60
// minutes of fleet-wide data in one round trip (AD-001).
func (r *TelemetryRepo) GetFleetHeartbeats(ctx context.Context, since time.Time) (map[string][]models.Heartbeat, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT device_id, timestamp, data
		 FROM heartbeats
		 WHERE timestamp >= $1 AND `+notFutureDatedSQL+`
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
			WHERE `+notFutureDatedSQL+`
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
// from the latest Docker-bearing snapshots. SQL selects only the allowlisted
// fields before transfer, including when a trimmed snapshot omits Docker (AD-011).
func (r *TelemetryRepo) GetFleetContainerLeaderboard(ctx context.Context) ([]FleetContainerProjection, error) {
	// Build the allowlist in SQL: env, mounts, network attachments and arbitrary
	// labels never cross the database connection or enter Go memory.
	rows, err := r.db.Pool.Query(ctx, `SELECT device_id,
 COALESCE((SELECT jsonb_agg(jsonb_build_object(
 'id', left(c->>'id',128),
'short_id', left(c->>'short_id',128),
'name', left(c->>'name',128),
'image', left(c->>'image',512),
'image_id', left(c->>'image_id',128),
'state', left(c->>'state',32),
'status', left(c->>'status',512),
'health_status', left(c->>'health_status',64),
'started_at', left(c->>'started_at',64),
'finished_at', left(c->>'finished_at',64),
'exit_code', c->'exit_code',
'update_available', c->'update_available',
'update_status', left(c->>'update_status',64),
'update_reason', left(c->>'update_reason',512),
'cpu_percent', c->'cpu_percent',
'mem_usage', c->'mem_usage',
'mem_limit', c->'mem_limit',
'mem_limit_configured', c->'mem_limit_configured',
'restart_count', c->'restart_count',
'network_mode', left(c->>'network_mode',256),
'labels', jsonb_build_object('com.docker.compose.project', CASE WHEN length(c->'labels'->>'com.docker.compose.project')<=512 THEN c->'labels'->>'com.docker.compose.project' ELSE '' END,
'com.docker.compose.service', CASE WHEN length(c->'labels'->>'com.docker.compose.service')<=512 THEN c->'labels'->>'com.docker.compose.service' ELSE '' END,
'com.docker.compose.project.working_dir', CASE WHEN length(c->'labels'->>'com.docker.compose.project.working_dir')<=1024 THEN c->'labels'->>'com.docker.compose.project.working_dir' ELSE '' END,
'com.docker.compose.project.config_files', CASE WHEN length(c->'labels'->>'com.docker.compose.project.config_files')<=10250 THEN c->'labels'->>'com.docker.compose.project.config_files' ELSE '' END,
'riot.name', left(c->'labels'->>'riot.name',512),
'riot.icon', left(c->'labels'->>'riot.icon',512),
'riot.group', left(c->'labels'->>'riot.group',512),
'riot.tags', left(c->'labels'->>'riot.tags',512),
'riot.role', left(c->'labels'->>'riot.role',512)),
'ports', COALESCE((SELECT jsonb_agg(jsonb_build_object('container_port',left(p->>'container_port',16),'host_port',left(p->>'host_port',16),'protocol',left(p->>'protocol',8),'host_ip',left(p->>'host_ip',64)) ORDER BY ord) FROM jsonb_array_elements(CASE WHEN jsonb_typeof(c->'ports')='array' THEN c->'ports' ELSE '[]'::jsonb END) WITH ORDINALITY AS ports(p,ord) WHERE ord<=256),'[]'::jsonb)
 ) ORDER BY ord)
 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(data#>'{docker,containers}')='array' THEN data#>'{docker,containers}' ELSE '[]'::jsonb END) WITH ORDINALITY AS containers(c,ord)
 WHERE c->>'state' IN ('running','restarting','exited','paused','created','dead')),'[]'::jsonb) AS containers
 FROM (SELECT DISTINCT ON (device_id) device_id,data FROM telemetry_snapshots
 WHERE `+notFutureDatedSQL+` AND data->'docker' IS NOT NULL AND data->'docker'<>'null'::jsonb
 ORDER BY device_id, timestamp DESC) latest`)

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
			if err := json.Unmarshal(containersJSON, &proj.Containers); err != nil {
				return nil, err
			}
		}
		result = append(result, proj)
	}
	return result, rows.Err()
}
