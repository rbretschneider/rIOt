package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// UpdateCommandResult updates status, result message, duration, and exit code of a command.
func (r *CommandRepo) UpdateCommandResult(ctx context.Context, id, status, resultMsg string, durationMs *int64, exitCode *int) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE commands SET status=$1, result_msg=$2, duration_ms=$3, exit_code=$4, updated_at=NOW() WHERE id=$5`,
		status, resultMsg, durationMs, exitCode, id)
	return err
}

// scanCommand scans a single command row from the given columns.
func scanCommand(scan func(dest ...interface{}) error) (models.Command, error) {
	var cmd models.Command
	var paramsJSON []byte
	if err := scan(&cmd.ID, &cmd.DeviceID, &cmd.Action, &paramsJSON, &cmd.Status, &cmd.ResultMsg,
		&cmd.DurationMs, &cmd.ExitCode, &cmd.CreatedAt, &cmd.UpdatedAt); err != nil {
		return cmd, err
	}
	cmd.Params = make(map[string]interface{})
	json.Unmarshal(paramsJSON, &cmd.Params)
	return cmd, nil
}

const commandColumns = `id, device_id, action, params, status, result_msg, duration_ms, exit_code, created_at, updated_at`

// ListByDevice returns recent commands for a device.
func (r *CommandRepo) ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Command, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+commandColumns+`
		 FROM commands WHERE device_id=$1 ORDER BY created_at DESC LIMIT $2`,
		deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	commands := []models.Command{}
	for rows.Next() {
		cmd, err := scanCommand(rows.Scan)
		if err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}

// ListByDeviceFiltered returns commands with pagination and optional filtering.
func (r *CommandRepo) ListByDeviceFiltered(ctx context.Context, deviceID string, limit, offset int, statuses []string, action string) ([]models.Command, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT ` + commandColumns + ` FROM commands WHERE device_id=$1`
	args := []interface{}{deviceID}
	argIdx := 2

	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, s := range statuses {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, s)
			argIdx++
		}
		query += ` AND status IN (` + strings.Join(placeholders, ",") + `)`
	}

	if action != "" {
		query += fmt.Sprintf(` AND action=$%d`, argIdx)
		args = append(args, action)
		argIdx++
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	commands := []models.Command{}
	for rows.Next() {
		cmd, err := scanCommand(rows.Scan)
		if err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}

// ListPending returns commands with status "pending" or "queued" for a device.
func (r *CommandRepo) ListPending(ctx context.Context, deviceID string) ([]models.Command, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+commandColumns+`
		 FROM commands WHERE device_id=$1 AND status IN ('pending', 'queued')
		 ORDER BY created_at ASC LIMIT 10`,
		deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	commands := []models.Command{}
	for rows.Next() {
		cmd, err := scanCommand(rows.Scan)
		if err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}

// GetByID returns a single command.
func (r *CommandRepo) GetByID(ctx context.Context, id string) (*models.Command, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return r.db.Pool.QueryRow(ctx,
			`SELECT `+commandColumns+` FROM commands WHERE id=$1`, id).Scan(dest...)
	})
	if err != nil {
		return nil, err
	}
	return &cmd, nil
}

// SaveCommandOutput inserts a command output record.
func (r *CommandRepo) SaveCommandOutput(ctx context.Context, output *models.CommandOutput) error {
	err := r.db.Pool.QueryRow(ctx,
		`INSERT INTO command_output (command_id, stream, content, created_at)
		 VALUES ($1, $2, $3, NOW())
		 RETURNING id, created_at`,
		output.CommandID, output.Stream, output.Content).Scan(&output.ID, &output.CreatedAt)
	return err
}

// GetCommandOutput returns all output records for a command.
func (r *CommandRepo) GetCommandOutput(ctx context.Context, commandID string) ([]models.CommandOutput, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, command_id, stream, content, created_at
		 FROM command_output WHERE command_id=$1 ORDER BY created_at ASC`,
		commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outputs []models.CommandOutput
	for rows.Next() {
		var o models.CommandOutput
		if err := rows.Scan(&o.ID, &o.CommandID, &o.Stream, &o.Content, &o.CreatedAt); err != nil {
			return nil, err
		}
		outputs = append(outputs, o)
	}
	return outputs, nil
}
