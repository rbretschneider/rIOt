package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type AutoUpdateRepo struct {
	db *DB
}

func NewAutoUpdateRepo(db *DB) *AutoUpdateRepo {
	return &AutoUpdateRepo{db: db}
}

func (r *AutoUpdateRepo) ListByDevice(ctx context.Context, deviceID string) ([]models.AutoUpdatePolicy, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, target, is_stack, compose_work_dir, enabled, last_triggered_at, created_at,
			consecutive_failures, last_failure_at
		 FROM auto_update_policies WHERE device_id=$1 ORDER BY target`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []models.AutoUpdatePolicy
	for rows.Next() {
		var p models.AutoUpdatePolicy
		if err := rows.Scan(&p.ID, &p.DeviceID, &p.Target, &p.IsStack, &p.ComposeWorkDir, &p.Enabled, &p.LastTriggeredAt, &p.CreatedAt,
			&p.ConsecutiveFailures, &p.LastFailureAt); err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

func (r *AutoUpdateRepo) Upsert(ctx context.Context, p *models.AutoUpdatePolicy) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO auto_update_policies (device_id, target, is_stack, compose_work_dir, enabled)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (device_id, target)
		 DO UPDATE SET is_stack=$3, compose_work_dir=$4, enabled=$5`,
		p.DeviceID, p.Target, p.IsStack, p.ComposeWorkDir, p.Enabled)
	return err
}

func (r *AutoUpdateRepo) Delete(ctx context.Context, deviceID, target string) error {
	_, err := r.db.Pool.Exec(ctx,
		`DELETE FROM auto_update_policies WHERE device_id=$1 AND target=$2`,
		deviceID, target)
	return err
}

// RecordSuccess records that a command the policy originated reached
// succeeded at `at`: the cooldown starts there and the failure backoff resets
// (FR-004, FR-006).
func (r *AutoUpdateRepo) RecordSuccess(ctx context.Context, id int, at time.Time) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE auto_update_policies
		 SET last_triggered_at=$1, consecutive_failures=0, last_failure_at=NULL
		 WHERE id=$2`, at, id)
	return err
}

// RecordFailure records that a command the policy originated ended failed,
// timed_out, expired or cancelled at `at`. The cooldown is not started; the
// failure count drives the backoff (FR-005).
func (r *AutoUpdateRepo) RecordFailure(ctx context.Context, id int, at time.Time) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE auto_update_policies
		 SET consecutive_failures=consecutive_failures+1, last_failure_at=$1
		 WHERE id=$2`, at, id)
	return err
}

func (r *AutoUpdateRepo) SetLastTriggered(ctx context.Context, id int) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE auto_update_policies SET last_triggered_at=$1 WHERE id=$2`,
		time.Now().UTC(), id)
	return err
}
