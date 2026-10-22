package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type AutoUpdateRepo struct {
	db *DB
}

func NewAutoUpdateRepo(db *DB) *AutoUpdateRepo {
	return &AutoUpdateRepo{db: db}
}

const autoUpdatePolicyColumns = `id, device_id, target, is_stack, compose_work_dir, enabled, last_triggered_at, created_at,
			consecutive_failures, last_failure_at`

func (r *AutoUpdateRepo) ListByDevice(ctx context.Context, deviceID string) ([]models.AutoUpdatePolicy, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+autoUpdatePolicyColumns+`
		 FROM auto_update_policies WHERE device_id=$1 ORDER BY target`, deviceID)
	if err != nil {
		return nil, err
	}
	return scanAutoUpdatePolicies(rows)
}

// ListAll returns the policies of every device in one query, so a fleet-wide
// list can show each row's policy state without one query per host.
func (r *AutoUpdateRepo) ListAll(ctx context.Context) ([]models.AutoUpdatePolicy, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+autoUpdatePolicyColumns+`
		 FROM auto_update_policies ORDER BY device_id, target`)
	if err != nil {
		return nil, err
	}
	return scanAutoUpdatePolicies(rows)
}

func scanAutoUpdatePolicies(rows pgx.Rows) ([]models.AutoUpdatePolicy, error) {
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

// UpsertUnlessKindClash stores the policy unless the device already has an
// enabled policy of the other kind (stack or container) under the same target.
// The check and the write are one statement: the row lock taken by ON CONFLICT
// makes the WHERE see the latest committed row, so two concurrent saves of
// opposite kinds cannot overwrite each other's enabled policy. stored is false
// when the existing row was left unchanged because of such a clash.
func (r *AutoUpdateRepo) UpsertUnlessKindClash(ctx context.Context, p *models.AutoUpdatePolicy) (bool, error) {
	var id int
	err := r.db.Pool.QueryRow(ctx,
		`INSERT INTO auto_update_policies (device_id, target, is_stack, compose_work_dir, enabled)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (device_id, target)
		 DO UPDATE SET is_stack=EXCLUDED.is_stack, compose_work_dir=EXCLUDED.compose_work_dir, enabled=EXCLUDED.enabled
		 WHERE auto_update_policies.is_stack = EXCLUDED.is_stack OR NOT auto_update_policies.enabled
		 RETURNING id`,
		p.DeviceID, p.Target, p.IsStack, p.ComposeWorkDir, p.Enabled).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	p.ID = id
	return true, nil
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
