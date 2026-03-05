package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// CommandRepo handles commands database operations.
type CommandRepo struct {
	db *DB
}

func NewCommandRepo(db *DB) *CommandRepo {
	return &CommandRepo{db: db}
}

// Create inserts a new command.
func (r *CommandRepo) Create(ctx context.Context, cmd *models.Command) error {
	now := time.Now().UTC()
	cmd.CreatedAt = now
	cmd.UpdatedAt = now
	paramsJSON, err := json.Marshal(cmd.Params)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx,
		`INSERT INTO commands (id, device_id, action, params, status, result_msg, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		cmd.ID, cmd.DeviceID, cmd.Action, paramsJSON, cmd.Status, cmd.ResultMsg, cmd.CreatedAt, cmd.UpdatedAt)
	return err
}

// UpdateStatus updates the status and result of a command.
func (r *CommandRepo) UpdateStatus(ctx context.Context, id, status, resultMsg string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE commands SET status=$1, result_msg=$2, updated_at=NOW() WHERE id=$3`,
		status, resultMsg, id)
	return err
}

// ListByDevice returns recent commands for a device.
func (r *CommandRepo) ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Command, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, action, params, status, result_msg, created_at, updated_at
		 FROM commands WHERE device_id=$1 ORDER BY created_at DESC LIMIT $2`,
		deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	commands := []models.Command{}
	for rows.Next() {
		var cmd models.Command
		var paramsJSON []byte
		if err := rows.Scan(&cmd.ID, &cmd.DeviceID, &cmd.Action, &paramsJSON, &cmd.Status, &cmd.ResultMsg, &cmd.CreatedAt, &cmd.UpdatedAt); err != nil {
			return nil, err
		}
		cmd.Params = make(map[string]interface{})
		json.Unmarshal(paramsJSON, &cmd.Params)
		commands = append(commands, cmd)
	}
	return commands, nil
}

// GetByID returns a single command.
func (r *CommandRepo) GetByID(ctx context.Context, id string) (*models.Command, error) {
	var cmd models.Command
	var paramsJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, device_id, action, params, status, result_msg, created_at, updated_at
		 FROM commands WHERE id=$1`, id).Scan(
		&cmd.ID, &cmd.DeviceID, &cmd.Action, &paramsJSON, &cmd.Status, &cmd.ResultMsg, &cmd.CreatedAt, &cmd.UpdatedAt)
	if err != nil {
		return nil, err
	}
	cmd.Params = make(map[string]interface{})
	json.Unmarshal(paramsJSON, &cmd.Params)
	return &cmd, nil
}
