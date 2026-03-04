package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DeviceRepo handles device database operations.
type DeviceRepo struct {
	db *DB
}

func NewDeviceRepo(db *DB) *DeviceRepo {
	return &DeviceRepo{db: db}
}

func (r *DeviceRepo) Create(ctx context.Context, d *models.Device) error {
	tagsJSON, _ := json.Marshal(d.Tags)
	hwJSON, _ := json.Marshal(d.HardwareProfile)
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO devices (id, short_id, hostname, arch, agent_version, primary_ip, status, tags, hardware_profile, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		d.ID, d.ShortID, d.Hostname, d.Arch, d.AgentVersion, d.PrimaryIP, d.Status, tagsJSON, hwJSON, d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (r *DeviceRepo) Update(ctx context.Context, d *models.Device) error {
	tagsJSON, _ := json.Marshal(d.Tags)
	hwJSON, _ := json.Marshal(d.HardwareProfile)
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET hostname=$2, arch=$3, agent_version=$4, primary_ip=$5, status=$6, tags=$7, hardware_profile=$8,
		 last_heartbeat=$9, last_telemetry=$10, updated_at=$11 WHERE id=$1`,
		d.ID, d.Hostname, d.Arch, d.AgentVersion, d.PrimaryIP, d.Status, tagsJSON, hwJSON,
		d.LastHeartbeat, d.LastTelemetry, time.Now().UTC(),
	)
	return err
}

func (r *DeviceRepo) GetByID(ctx context.Context, id string) (*models.Device, error) {
	d := &models.Device{}
	var tagsJSON, hwJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, short_id, hostname, arch, agent_version, primary_ip, status, tags, hardware_profile,
		 last_heartbeat, last_telemetry, created_at, updated_at FROM devices WHERE id=$1`, id,
	).Scan(&d.ID, &d.ShortID, &d.Hostname, &d.Arch, &d.AgentVersion, &d.PrimaryIP, &d.Status, &tagsJSON, &hwJSON,
		&d.LastHeartbeat, &d.LastTelemetry, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(tagsJSON, &d.Tags)
	if len(hwJSON) > 0 {
		d.HardwareProfile = &models.HardwareProfile{}
		json.Unmarshal(hwJSON, d.HardwareProfile)
	}
	return d, nil
}

func (r *DeviceRepo) List(ctx context.Context) ([]models.Device, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, short_id, hostname, arch, agent_version, primary_ip, status, tags, hardware_profile,
		 last_heartbeat, last_telemetry, created_at, updated_at FROM devices ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []models.Device
	for rows.Next() {
		var d models.Device
		var tagsJSON, hwJSON []byte
		if err := rows.Scan(&d.ID, &d.ShortID, &d.Hostname, &d.Arch, &d.AgentVersion, &d.PrimaryIP, &d.Status, &tagsJSON, &hwJSON,
			&d.LastHeartbeat, &d.LastTelemetry, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(tagsJSON, &d.Tags)
		if len(hwJSON) > 0 {
			d.HardwareProfile = &models.HardwareProfile{}
			json.Unmarshal(hwJSON, d.HardwareProfile)
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (r *DeviceRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM devices WHERE id=$1`, id)
	return err
}

func (r *DeviceRepo) SetStatus(ctx context.Context, id string, status models.DeviceStatus) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET status=$2, updated_at=NOW() WHERE id=$1`, id, status)
	return err
}

func (r *DeviceRepo) UpdateHeartbeatTime(ctx context.Context, id string, agentVersion string) error {
	if agentVersion != "" {
		_, err := r.db.Pool.Exec(ctx,
			`UPDATE devices SET last_heartbeat=NOW(), status='online', agent_version=$2, updated_at=NOW() WHERE id=$1`, id, agentVersion)
		return err
	}
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET last_heartbeat=NOW(), status='online', updated_at=NOW() WHERE id=$1`, id)
	return err
}

func (r *DeviceRepo) UpdateTelemetryTime(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET last_telemetry=NOW(), updated_at=NOW() WHERE id=$1`, id)
	return err
}

func (r *DeviceRepo) UpdatePrimaryIP(ctx context.Context, id, ip string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET primary_ip=$2, updated_at=NOW() WHERE id=$1`, id, ip)
	return err
}

func (r *DeviceRepo) Summary(ctx context.Context) (*models.FleetSummary, error) {
	s := &models.FleetSummary{}
	err := r.db.Pool.QueryRow(ctx,
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status='online'),
			COUNT(*) FILTER (WHERE status='offline'),
			COUNT(*) FILTER (WHERE status='warning')
		FROM devices`).Scan(&s.TotalDevices, &s.OnlineCount, &s.OfflineCount, &s.WarningCount)
	if err != nil {
		return nil, err
	}
	r.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE created_at > NOW() - INTERVAL '24 hours'`).Scan(&s.RecentEvents)
	return s, nil
}

// StoreAPIKey stores a device API key.
func (r *DeviceRepo) StoreAPIKey(ctx context.Context, key, deviceID string) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO api_keys (key, device_id, created_at) VALUES ($1, $2, NOW())`, key, deviceID)
	return err
}

// LookupAPIKey returns the device_id for a given API key.
func (r *DeviceRepo) LookupAPIKey(ctx context.Context, key string) (string, error) {
	var deviceID string
	err := r.db.Pool.QueryRow(ctx, `SELECT device_id FROM api_keys WHERE key=$1`, key).Scan(&deviceID)
	if err != nil {
		return "", fmt.Errorf("invalid api key")
	}
	return deviceID, nil
}

// FindByDeviceUUID finds a device by its existing UUID (for re-registration).
func (r *DeviceRepo) FindByDeviceUUID(ctx context.Context, id string) (*models.Device, error) {
	return r.GetByID(ctx, id)
}
