package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// AlertRuleRepo handles alert_rules database operations.
type AlertRuleRepo struct {
	db *DB
}

func NewAlertRuleRepo(db *DB) *AlertRuleRepo {
	return &AlertRuleRepo{db: db}
}

// List returns all alert rules ordered by id.
func (r *AlertRuleRepo) List(ctx context.Context) ([]models.AlertRule, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, enabled, metric, operator, threshold, severity, device_filter, cooldown_seconds, notify, created_at, updated_at
		 FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.AlertRule
	for rows.Next() {
		var rule models.AlertRule
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Enabled, &rule.Metric, &rule.Operator,
			&rule.Threshold, &rule.Severity, &rule.DeviceFilter, &rule.CooldownSeconds,
			&rule.Notify, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ListEnabled returns only enabled alert rules.
func (r *AlertRuleRepo) ListEnabled(ctx context.Context) ([]models.AlertRule, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, enabled, metric, operator, threshold, severity, device_filter, cooldown_seconds, notify, created_at, updated_at
		 FROM alert_rules WHERE enabled=true ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.AlertRule
	for rows.Next() {
		var rule models.AlertRule
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Enabled, &rule.Metric, &rule.Operator,
			&rule.Threshold, &rule.Severity, &rule.DeviceFilter, &rule.CooldownSeconds,
			&rule.Notify, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// GetByID returns a single alert rule.
func (r *AlertRuleRepo) GetByID(ctx context.Context, id int64) (*models.AlertRule, error) {
	var rule models.AlertRule
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, name, enabled, metric, operator, threshold, severity, device_filter, cooldown_seconds, notify, created_at, updated_at
		 FROM alert_rules WHERE id=$1`, id).Scan(
		&rule.ID, &rule.Name, &rule.Enabled, &rule.Metric, &rule.Operator,
		&rule.Threshold, &rule.Severity, &rule.DeviceFilter, &rule.CooldownSeconds,
		&rule.Notify, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// Create inserts a new alert rule.
func (r *AlertRuleRepo) Create(ctx context.Context, rule *models.AlertRule) error {
	now := time.Now().UTC()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO alert_rules (name, enabled, metric, operator, threshold, severity, device_filter, cooldown_seconds, notify, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		rule.Name, rule.Enabled, rule.Metric, rule.Operator, rule.Threshold,
		rule.Severity, rule.DeviceFilter, rule.CooldownSeconds, rule.Notify,
		rule.CreatedAt, rule.UpdatedAt).Scan(&rule.ID)
}

// Update modifies an existing alert rule.
func (r *AlertRuleRepo) Update(ctx context.Context, rule *models.AlertRule) error {
	rule.UpdatedAt = time.Now().UTC()
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE alert_rules SET name=$1, enabled=$2, metric=$3, operator=$4, threshold=$5,
		 severity=$6, device_filter=$7, cooldown_seconds=$8, notify=$9, updated_at=$10
		 WHERE id=$11`,
		rule.Name, rule.Enabled, rule.Metric, rule.Operator, rule.Threshold,
		rule.Severity, rule.DeviceFilter, rule.CooldownSeconds, rule.Notify,
		rule.UpdatedAt, rule.ID)
	return err
}

// Delete removes an alert rule.
func (r *AlertRuleRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM alert_rules WHERE id=$1`, id)
	return err
}
