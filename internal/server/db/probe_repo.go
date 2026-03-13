package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ProbeRepo handles probes and probe_results database operations.
type ProbeRepo struct {
	db *DB
}

func NewProbeRepo(db *DB) *ProbeRepo {
	return &ProbeRepo{db: db}
}

// --- Probes ---

func (r *ProbeRepo) List(ctx context.Context) ([]models.Probe, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, type, enabled, config, interval_seconds, timeout_seconds, created_at, updated_at
		 FROM probes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProbes(rows)
}

func (r *ProbeRepo) ListEnabled(ctx context.Context) ([]models.Probe, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, type, enabled, config, interval_seconds, timeout_seconds, created_at, updated_at
		 FROM probes WHERE enabled=true ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProbes(rows)
}

func (r *ProbeRepo) GetByID(ctx context.Context, id int64) (*models.Probe, error) {
	var p models.Probe
	var configJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, name, type, enabled, config, interval_seconds, timeout_seconds, created_at, updated_at
		 FROM probes WHERE id=$1`, id).Scan(
		&p.ID, &p.Name, &p.Type, &p.Enabled, &configJSON,
		&p.IntervalSeconds, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Config = make(map[string]interface{})
	json.Unmarshal(configJSON, &p.Config)
	return &p, nil
}

func (r *ProbeRepo) Create(ctx context.Context, p *models.Probe) error {
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO probes (name, type, enabled, config, interval_seconds, timeout_seconds, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		p.Name, p.Type, p.Enabled, configJSON, p.IntervalSeconds, p.TimeoutSeconds, p.CreatedAt, p.UpdatedAt).Scan(&p.ID)
}

func (r *ProbeRepo) Update(ctx context.Context, p *models.Probe) error {
	p.UpdatedAt = time.Now().UTC()
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx,
		`UPDATE probes SET name=$1, type=$2, enabled=$3, config=$4, interval_seconds=$5, timeout_seconds=$6, updated_at=$7
		 WHERE id=$8`,
		p.Name, p.Type, p.Enabled, configJSON, p.IntervalSeconds, p.TimeoutSeconds, p.UpdatedAt, p.ID)
	return err
}

func (r *ProbeRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM probes WHERE id=$1`, id)
	return err
}

// --- Probe Results ---

func (r *ProbeRepo) StoreResult(ctx context.Context, result *models.ProbeResult) error {
	result.CreatedAt = time.Now().UTC()
	metaJSON, _ := json.Marshal(result.Metadata)
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO probe_results (probe_id, success, latency_ms, status_code, error_msg, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		result.ProbeID, result.Success, result.LatencyMs, result.StatusCode, result.ErrorMsg, metaJSON, result.CreatedAt).Scan(&result.ID)
}

func (r *ProbeRepo) ListResults(ctx context.Context, probeID int64, limit int) ([]models.ProbeResult, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, probe_id, success, latency_ms, status_code, error_msg, metadata, created_at
		 FROM probe_results WHERE probe_id=$1 ORDER BY created_at DESC LIMIT $2`,
		probeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []models.ProbeResult{}
	for rows.Next() {
		var pr models.ProbeResult
		var metaJSON []byte
		if err := rows.Scan(&pr.ID, &pr.ProbeID, &pr.Success, &pr.LatencyMs, &pr.StatusCode, &pr.ErrorMsg, &metaJSON, &pr.CreatedAt); err != nil {
			return nil, err
		}
		pr.Metadata = make(map[string]interface{})
		json.Unmarshal(metaJSON, &pr.Metadata)
		results = append(results, pr)
	}
	return results, nil
}

// LatestResult returns the most recent result for a probe.
func (r *ProbeRepo) LatestResult(ctx context.Context, probeID int64) (*models.ProbeResult, error) {
	var pr models.ProbeResult
	var metaJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, probe_id, success, latency_ms, status_code, error_msg, metadata, created_at
		 FROM probe_results WHERE probe_id=$1 ORDER BY created_at DESC LIMIT 1`, probeID).Scan(
		&pr.ID, &pr.ProbeID, &pr.Success, &pr.LatencyMs, &pr.StatusCode, &pr.ErrorMsg, &metaJSON, &pr.CreatedAt)
	if err != nil {
		return nil, err
	}
	pr.Metadata = make(map[string]interface{})
	json.Unmarshal(metaJSON, &pr.Metadata)
	return &pr, nil
}

// SuccessRate returns the success rate (0.0–1.0) and total count for a probe over the last 24h.
func (r *ProbeRepo) SuccessRate(ctx context.Context, probeID int64) (float64, int, error) {
	var total, successes int
	err := r.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE success = true)
		 FROM probe_results WHERE probe_id=$1 AND created_at > NOW() - INTERVAL '24 hours'`,
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
func (r *ProbeRepo) PurgeResults(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM probe_results WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func scanProbes(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
}) ([]models.Probe, error) {
	probes := []models.Probe{}
	for rows.Next() {
		var p models.Probe
		var configJSON []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Enabled, &configJSON,
			&p.IntervalSeconds, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Config = make(map[string]interface{})
		json.Unmarshal(configJSON, &p.Config)
		probes = append(probes, p)
	}
	return probes, nil
}
