package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DeviceProbeRepo handles device_probes and device_probe_results database operations.
type DeviceProbeRepo struct {
	db *DB
}

func NewDeviceProbeRepo(db *DB) *DeviceProbeRepo {
	return &DeviceProbeRepo{db: db}
}

// --- Device Probes ---

// List returns all probes for a device.
func (r *DeviceProbeRepo) List(ctx context.Context, deviceID string) ([]models.DeviceProbe, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, device_id, type, enabled, config, assertions, interval_seconds, timeout_seconds, created_at, updated_at
		 FROM device_probes WHERE device_id=$1 ORDER BY id`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeviceProbes(rows)
}

// ListAll returns all device probes across all devices ordered by id.
func (r *DeviceProbeRepo) ListAll(ctx context.Context) ([]models.DeviceProbe, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, device_id, type, enabled, config, assertions, interval_seconds, timeout_seconds, created_at, updated_at
		 FROM device_probes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeviceProbes(rows)
}

// ListEnabled returns all enabled probes for a device (used for heartbeat delivery).
func (r *DeviceProbeRepo) ListEnabled(ctx context.Context, deviceID string) ([]models.DeviceProbe, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, device_id, type, enabled, config, assertions, interval_seconds, timeout_seconds, created_at, updated_at
		 FROM device_probes WHERE device_id=$1 AND enabled=true ORDER BY id`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeviceProbes(rows)
}

// GetByID returns a single probe.
func (r *DeviceProbeRepo) GetByID(ctx context.Context, id int64) (*models.DeviceProbe, error) {
	var p models.DeviceProbe
	var configJSON, assertionsJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, name, device_id, type, enabled, config, assertions, interval_seconds, timeout_seconds, created_at, updated_at
		 FROM device_probes WHERE id=$1`, id).Scan(
		&p.ID, &p.Name, &p.DeviceID, &p.Type, &p.Enabled, &configJSON, &assertionsJSON,
		&p.IntervalSeconds, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Config = make(map[string]interface{})
	json.Unmarshal(configJSON, &p.Config)
	p.Assertions = []models.ProbeAssertion{}
	json.Unmarshal(assertionsJSON, &p.Assertions)
	return &p, nil
}

// Create inserts a new probe.
func (r *DeviceProbeRepo) Create(ctx context.Context, p *models.DeviceProbe) error {
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}
	assertionsJSON, err := json.Marshal(p.Assertions)
	if err != nil {
		return err
	}
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO device_probes (name, device_id, type, enabled, config, assertions, interval_seconds, timeout_seconds, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		p.Name, p.DeviceID, p.Type, p.Enabled, configJSON, assertionsJSON,
		p.IntervalSeconds, p.TimeoutSeconds, p.CreatedAt, p.UpdatedAt).Scan(&p.ID)
}

// Update updates an existing probe.
func (r *DeviceProbeRepo) Update(ctx context.Context, p *models.DeviceProbe) error {
	p.UpdatedAt = time.Now().UTC()
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}
	assertionsJSON, err := json.Marshal(p.Assertions)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx,
		`UPDATE device_probes SET name=$1, type=$2, enabled=$3, config=$4, assertions=$5, interval_seconds=$6, timeout_seconds=$7, updated_at=$8
		 WHERE id=$9`,
		p.Name, p.Type, p.Enabled, configJSON, assertionsJSON,
		p.IntervalSeconds, p.TimeoutSeconds, p.UpdatedAt, p.ID)
	return err
}

// Delete removes a probe.
func (r *DeviceProbeRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM device_probes WHERE id=$1`, id)
	return err
}

// --- Device Probe Results ---

// StoreResult inserts a probe result.
func (r *DeviceProbeRepo) StoreResult(ctx context.Context, result *models.DeviceProbeResult) error {
	result.CreatedAt = time.Now().UTC()
	outputJSON, _ := json.Marshal(result.Output)
	failedJSON, _ := json.Marshal(result.FailedAssertions)
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO device_probe_results (probe_id, device_id, success, latency_ms, output, failed_assertions, error_msg, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		result.ProbeID, result.DeviceID, result.Success, result.LatencyMs,
		outputJSON, failedJSON, result.ErrorMsg, result.CreatedAt).Scan(&result.ID)
}

// ListResults returns results for a probe, newest first.
func (r *DeviceProbeRepo) ListResults(ctx context.Context, probeID int64, limit int) ([]models.DeviceProbeResult, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, probe_id, device_id, success, latency_ms, output, failed_assertions, error_msg, created_at
		 FROM device_probe_results WHERE probe_id=$1 ORDER BY created_at DESC LIMIT $2`,
		probeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []models.DeviceProbeResult{}
	for rows.Next() {
		var pr models.DeviceProbeResult
		var outputJSON, failedJSON []byte
		if err := rows.Scan(&pr.ID, &pr.ProbeID, &pr.DeviceID, &pr.Success, &pr.LatencyMs,
			&outputJSON, &failedJSON, &pr.ErrorMsg, &pr.CreatedAt); err != nil {
			return nil, err
		}
		pr.Output = make(map[string]interface{})
		json.Unmarshal(outputJSON, &pr.Output)
		pr.FailedAssertions = []models.ProbeAssertion{}
		json.Unmarshal(failedJSON, &pr.FailedAssertions)
		results = append(results, pr)
	}
	return results, nil
}

// LatestResult returns the most recent result for a probe.
func (r *DeviceProbeRepo) LatestResult(ctx context.Context, probeID int64) (*models.DeviceProbeResult, error) {
	var pr models.DeviceProbeResult
	var outputJSON, failedJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, probe_id, device_id, success, latency_ms, output, failed_assertions, error_msg, created_at
		 FROM device_probe_results WHERE probe_id=$1 ORDER BY created_at DESC LIMIT 1`, probeID).Scan(
		&pr.ID, &pr.ProbeID, &pr.DeviceID, &pr.Success, &pr.LatencyMs,
		&outputJSON, &failedJSON, &pr.ErrorMsg, &pr.CreatedAt)
	if err != nil {
		return nil, err
	}
	pr.Output = make(map[string]interface{})
	json.Unmarshal(outputJSON, &pr.Output)
	pr.FailedAssertions = []models.ProbeAssertion{}
	json.Unmarshal(failedJSON, &pr.FailedAssertions)
	return &pr, nil
}

// SuccessRate returns the success rate (0.0-1.0) and total count for a probe over the last 24h.
func (r *DeviceProbeRepo) SuccessRate(ctx context.Context, probeID int64) (float64, int, error) {
	var total, successes int
	err := r.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE success = true)
		 FROM device_probe_results WHERE probe_id=$1 AND created_at > NOW() - INTERVAL '24 hours'`,
		probeID).Scan(&total, &successes)
	if err != nil {
		return 0, 0, err
	}
	if total == 0 {
		return 0, 0, nil
	}
	return float64(successes) / float64(total), total, nil
}

// PurgeResults deletes probe results older than the given time.
func (r *DeviceProbeRepo) PurgeResults(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM device_probe_results WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func scanDeviceProbes(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
}) ([]models.DeviceProbe, error) {
	probes := []models.DeviceProbe{}
	for rows.Next() {
		var p models.DeviceProbe
		var configJSON, assertionsJSON []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.DeviceID, &p.Type, &p.Enabled, &configJSON, &assertionsJSON,
			&p.IntervalSeconds, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Config = make(map[string]interface{})
		json.Unmarshal(configJSON, &p.Config)
		p.Assertions = []models.ProbeAssertion{}
		json.Unmarshal(assertionsJSON, &p.Assertions)
		probes = append(probes, p)
	}
	return probes, nil
}
