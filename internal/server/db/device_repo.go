package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/auth"
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
		`INSERT INTO devices (id, short_id, hostname, arch, agent_version, primary_ip, status, location, tags, hardware_profile, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		d.ID, d.ShortID, d.Hostname, d.Arch, d.AgentVersion, d.PrimaryIP, d.Status, d.Location, tagsJSON, hwJSON, d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (r *DeviceRepo) Update(ctx context.Context, d *models.Device) error {
	tagsJSON, _ := json.Marshal(d.Tags)
	hwJSON, _ := json.Marshal(d.HardwareProfile)
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET hostname=$2, arch=$3, agent_version=$4, primary_ip=$5, status=$6, location=$7, tags=$8, hardware_profile=$9,
		 last_heartbeat=$10, last_telemetry=$11, updated_at=$12 WHERE id=$1`,
		d.ID, d.Hostname, d.Arch, d.AgentVersion, d.PrimaryIP, d.Status, d.Location, tagsJSON, hwJSON,
		d.LastHeartbeat, d.LastTelemetry, time.Now().UTC(),
	)
	return err
}

func (r *DeviceRepo) GetByID(ctx context.Context, id string) (*models.Device, error) {
	d := &models.Device{}
	var tagsJSON, hwJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, short_id, hostname, arch, agent_version, primary_ip, status, location, tags, docker_available, docker_container_count, auto_patch, hardware_profile,
		 last_heartbeat, last_telemetry, created_at, updated_at FROM devices WHERE id=$1`, id,
	).Scan(&d.ID, &d.ShortID, &d.Hostname, &d.Arch, &d.AgentVersion, &d.PrimaryIP, &d.Status, &d.Location, &tagsJSON, &d.DockerAvailable, &d.DockerContainerCount, &d.AutoPatch, &hwJSON,
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
		`SELECT id, short_id, hostname, arch, agent_version, primary_ip, status, location, tags, docker_available, docker_container_count, auto_patch, hardware_profile,
		 last_heartbeat, last_telemetry, created_at, updated_at FROM devices ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := []models.Device{}
	for rows.Next() {
		var d models.Device
		var tagsJSON, hwJSON []byte
		if err := rows.Scan(&d.ID, &d.ShortID, &d.Hostname, &d.Arch, &d.AgentVersion, &d.PrimaryIP, &d.Status, &d.Location, &tagsJSON, &d.DockerAvailable, &d.DockerContainerCount, &d.AutoPatch, &hwJSON,
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

func (r *DeviceRepo) UpdateDockerAvailable(ctx context.Context, id string, available bool, containerCount ...int) error {
	count := 0
	if len(containerCount) > 0 {
		count = containerCount[0]
	}
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET docker_available=$2, docker_container_count=$3, updated_at=NOW() WHERE id=$1`, id, available, count)
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

// AgentVersionCount represents a version and its device count.
type AgentVersionCount struct {
	Version string `json:"version"`
	Count   int    `json:"count"`
}

// AgentVersionSummary returns device counts grouped by agent_version.
func (r *DeviceRepo) AgentVersionSummary(ctx context.Context) ([]AgentVersionCount, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT COALESCE(agent_version, 'unknown') AS version, COUNT(*) AS count
		 FROM devices GROUP BY COALESCE(agent_version, 'unknown') ORDER BY count DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []AgentVersionCount{}
	for rows.Next() {
		var v AgentVersionCount
		if err := rows.Scan(&v.Version, &v.Count); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}

// ListByVersion returns devices with a specific agent_version.
func (r *DeviceRepo) ListByVersion(ctx context.Context, version string) ([]models.Device, error) {
	query := `SELECT id, short_id, hostname, arch, agent_version, primary_ip, status, location, tags, docker_available, docker_container_count, auto_patch, hardware_profile,
		 last_heartbeat, last_telemetry, created_at, updated_at FROM devices WHERE COALESCE(agent_version, 'unknown') = $1 ORDER BY hostname`
	rows, err := r.db.Pool.Query(ctx, query, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := []models.Device{}
	for rows.Next() {
		var d models.Device
		var tagsJSON, hwJSON []byte
		if err := rows.Scan(&d.ID, &d.ShortID, &d.Hostname, &d.Arch, &d.AgentVersion, &d.PrimaryIP, &d.Status, &d.Location, &tagsJSON, &d.DockerAvailable, &d.DockerContainerCount, &d.AutoPatch, &hwJSON,
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

// StoreAPIKey hashes the key and stores only the hash.
func (r *DeviceRepo) StoreAPIKey(ctx context.Context, plaintextKey, deviceID string) error {
	hash := auth.HashAPIKey(plaintextKey)
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO api_keys (key_hash, device_id, created_at) VALUES ($1, $2, NOW())`, hash, deviceID)
	return err
}

// LookupAPIKey hashes the incoming key and looks up by hash.
func (r *DeviceRepo) LookupAPIKey(ctx context.Context, plaintextKey string) (string, error) {
	hash := auth.HashAPIKey(plaintextKey)
	var deviceID string
	err := r.db.Pool.QueryRow(ctx, `SELECT device_id FROM api_keys WHERE key_hash=$1`, hash).Scan(&deviceID)
	if err != nil {
		return "", fmt.Errorf("invalid api key")
	}
	return deviceID, nil
}

// DeleteAPIKeysByDevice removes all API keys for a device (for key rotation).
func (r *DeviceRepo) DeleteAPIKeysByDevice(ctx context.Context, deviceID string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM api_keys WHERE device_id=$1`, deviceID)
	return err
}

// FindByDeviceUUID finds a device by its existing UUID (for re-registration).
func (r *DeviceRepo) FindByDeviceUUID(ctx context.Context, id string) (*models.Device, error) {
	return r.GetByID(ctx, id)
}

// UpdateLocation sets the location for a device.
func (r *DeviceRepo) UpdateLocation(ctx context.Context, id, location string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET location=$2, updated_at=NOW() WHERE id=$1`, id, location)
	return err
}

// UpdateTags replaces the tags for a device.
func (r *DeviceRepo) UpdateTags(ctx context.Context, id string, tags []string) error {
	tagsJSON, _ := json.Marshal(tags)
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET tags=$2, updated_at=NOW() WHERE id=$1`, id, tagsJSON)
	return err
}

// UpdateAutoPatch sets the auto_patch flag for a device.
func (r *DeviceRepo) UpdateAutoPatch(ctx context.Context, id string, enabled bool) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE devices SET auto_patch=$2, updated_at=NOW() WHERE id=$1`, id, enabled)
	return err
}

// GetAutoPatch returns the auto_patch flag for a device.
func (r *DeviceRepo) GetAutoPatch(ctx context.Context, id string) (bool, error) {
	var enabled bool
	err := r.db.Pool.QueryRow(ctx, `SELECT auto_patch FROM devices WHERE id=$1`, id).Scan(&enabled)
	return enabled, err
}
