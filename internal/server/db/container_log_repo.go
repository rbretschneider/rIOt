package db

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// MaxContainerLogBatch caps how many rows a single agent push can insert.
// Anything above this is truncated; the server refuses to act as a DoS vector
// for a runaway container_logs collector.
const MaxContainerLogBatch = 20_000

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
	if len(entries) > MaxContainerLogBatch {
		entries = entries[len(entries)-MaxContainerLogBatch:]
	}
	// pgx.CopyFrom uses the binary COPY protocol — dramatically faster than a
	// multi-row INSERT with thousands of parameters, and crucially it doesn't
	// keep rebuilding parameter slots per batch. For busy agents with many
	// containers, the old $1..$3000 INSERT was being cancelled by the ctx
	// deadline mid-batch and cascading into every other ingest path.
	rows := make([][]interface{}, len(entries))
	for i, e := range entries {
		rows[i] = []interface{}{deviceID, e.ContainerID, e.ContainerName, e.Timestamp, e.Stream, e.Line}
	}
	_, err := r.db.Pool.CopyFrom(
		ctx,
		pgx.Identifier{"container_logs"},
		[]string{"device_id", "container_id", "container_name", "timestamp", "stream", "line"},
		pgx.CopyFromRows(rows),
	)
	return err
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
