package db

import (
	"context"
	"strconv"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ContainerLogRepo handles container log database operations.
type ContainerLogRepo struct {
	db *DB
}

func NewContainerLogRepo(db *DB) *ContainerLogRepo {
	return &ContainerLogRepo{db: db}
}

func (r *ContainerLogRepo) InsertBatch(ctx context.Context, deviceID string, entries []models.ContainerLogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	for _, e := range entries {
		_, err := r.db.Pool.Exec(ctx,
			`INSERT INTO container_logs (device_id, container_id, container_name, timestamp, stream, line)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			deviceID, e.ContainerID, e.ContainerName, e.Timestamp, e.Stream, e.Line,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ContainerLogRepo) List(ctx context.Context, deviceID, containerID string, limit int, stream string, since *time.Time) ([]models.ContainerLogEntry, error) {
	query := `SELECT id, container_id, container_name, timestamp, stream, line
		 FROM container_logs
		 WHERE device_id=$1 AND container_id=$2`
	args := []interface{}{deviceID, containerID}
	argN := 3

	if stream != "" {
		query += ` AND stream=$` + strconv.Itoa(argN)
		args = append(args, stream)
		argN++
	}

	if since != nil {
		query += ` AND timestamp>=$` + strconv.Itoa(argN)
		args = append(args, *since)
		argN++
	}

	query += ` ORDER BY timestamp DESC`

	if limit > 0 {
		query += ` LIMIT $` + strconv.Itoa(argN)
		args = append(args, limit)
	}

	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ContainerLogEntry
	for rows.Next() {
		var e models.ContainerLogEntry
		if err := rows.Scan(&e.ID, &e.ContainerID, &e.ContainerName, &e.Timestamp, &e.Stream, &e.Line); err != nil {
			return nil, err
		}
		e.DeviceID = deviceID
		result = append(result, e)
	}

	// Reverse to chronological order (queried DESC for LIMIT, display ASC)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

func (r *ContainerLogRepo) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx,
		`DELETE FROM container_logs WHERE timestamp < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
