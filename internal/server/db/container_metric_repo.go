package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ContainerMetricRepo handles container metric database operations.
type ContainerMetricRepo struct {
	db *DB
}

func NewContainerMetricRepo(db *DB) *ContainerMetricRepo {
	return &ContainerMetricRepo{db: db}
}

func (r *ContainerMetricRepo) StoreBatch(ctx context.Context, deviceID string, metrics []models.ContainerMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	for _, m := range metrics {
		_, err := r.db.Pool.Exec(ctx,
			`INSERT INTO container_metrics (device_id, container_name, container_id, timestamp, cpu_percent, mem_usage, mem_limit, cpu_limit)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			deviceID, m.ContainerName, m.ContainerID, m.Timestamp, m.CPUPercent, m.MemUsage, m.MemLimit, m.CPULimit,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ContainerMetricRepo) GetHistory(ctx context.Context, deviceID, containerName string, since time.Time) ([]models.ContainerMetric, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT container_name, container_id, timestamp, cpu_percent, mem_usage, mem_limit, cpu_limit
		 FROM container_metrics
		 WHERE device_id=$1 AND container_name=$2 AND timestamp>=$3
		 ORDER BY timestamp ASC`,
		deviceID, containerName, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ContainerMetric
	for rows.Next() {
		var m models.ContainerMetric
		if err := rows.Scan(&m.ContainerName, &m.ContainerID, &m.Timestamp, &m.CPUPercent, &m.MemUsage, &m.MemLimit, &m.CPULimit); err != nil {
			return nil, err
		}
		m.DeviceID = deviceID
		result = append(result, m)
	}
	return result, nil
}

func (r *ContainerMetricRepo) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx,
		`DELETE FROM container_metrics WHERE timestamp < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
